package codex

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The settings of Codex are TOML, which the operator writes too: Gentry reads
// them by a reader of TOML 1.0 of its own, which keeps where each key-value
// and each table header is in the text, and changes them by edits of the
// text, so that the comments and the layout of the operator stay. Dates and
// times are kept as strings: Gentry reads none.
//
// Values are string, int64, float64, bool, []any and map[string]any.

// A tomlDoc is a TOML document as read: its values and where they are.
type tomlDoc struct {
	text    string
	root    map[string]any
	entries []tomlEntry
	headers []tomlHeader
	// rootEnd is where the key-values of the root table end: the start of
	// the first header, or the end of the text.
	rootEnd int
}

// A tomlEntry is a key-value of the text.
type tomlEntry struct {
	path   []string // the keys from the root
	header int      // the header the key-value is under; -1 for the root
	inline bool     // inside an inline table
	// line is the start of the line of the key; end is after the end of the
	// line of the value, its newline included; for an entry outside an
	// inline table.
	line, end int
	// valueStart and valueEnd are the bounds of the value.
	valueStart, valueEnd int
	// array tells a value that is an array; last is the end of its last
	// element, -1 if it has none.
	array bool
	last  int
	// inArr tells a key-value of a table of an array of tables: its path
	// names no single value, and it is not edited.
	inArr bool
}

// A tomlHeader is a table header of the text.
type tomlHeader struct {
	path  []string
	array bool // [[ ]]: a table of an array of tables
	inArr bool // the table is in a table of an array of tables
	body  int  // after the line of the header
}

// errTOML is an error of the text that is no TOML.
var errTOML = errors.New("toml")

// parseTOML reads a TOML document.
func parseTOML(text string) (*tomlDoc, error) {
	p := &tomlParser{s: text, root: map[string]any{}, defined: map[string]bool{}, dotted: map[string]bool{}, static: map[string]bool{}}
	p.doc = &tomlDoc{text: text, root: p.root, rootEnd: -1}
	p.table, p.header = p.root, -1
	if strings.HasPrefix(text, bom) {
		p.pos = len(bom)
	}
	if err := p.document(); err != nil {
		return nil, fmt.Errorf("%w: line %d: %v", errTOML, strings.Count(text[:min(p.pos, len(text))], "\n")+1, err)
	}
	if p.doc.rootEnd < 0 {
		p.doc.rootEnd = len(text)
	}
	return p.doc, nil
}

type tomlParser struct {
	s     string
	pos   int
	root  map[string]any
	table map[string]any
	// path and header are those of the current table.
	path   []string
	header int
	inArr  bool // the current table is one of an array of tables
	// defined are the tables of headers, dotted the tables of dotted keys,
	// static the inline tables and the arrays given as values, by their path.
	defined, dotted, static map[string]bool
	doc                     *tomlDoc
	// lastElem is the end of the last element of the last array read; -1
	// if it had none.
	lastElem int
}

func (p *tomlParser) eof() bool { return p.pos >= len(p.s) }

func (p *tomlParser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.s[p.pos]
}

func (p *tomlParser) has(s string) bool { return strings.HasPrefix(p.s[p.pos:], s) }

// space skips spaces and tabs.
func (p *tomlParser) space() {
	for !p.eof() && (p.peek() == ' ' || p.peek() == '\t') {
		p.pos++
	}
}

// comment skips a comment up to the end of the line.
func (p *tomlParser) comment() error {
	if p.peek() != '#' {
		return nil
	}
	for !p.eof() && p.peek() != '\n' {
		c := p.peek()
		if c < 0x20 && c != '\t' && c != '\r' || c == 0x7f {
			return fmt.Errorf("control character in a comment")
		}
		if c == '\r' && !p.has("\r\n") {
			return fmt.Errorf("bare carriage return")
		}
		p.pos++
	}
	return nil
}

// newline takes the end of a line: a newline, CRLF or the end of the text.
func (p *tomlParser) newline() error {
	switch {
	case p.eof():
		return nil
	case p.peek() == '\n':
		p.pos++
		return nil
	case p.has("\r\n"):
		p.pos += 2
		return nil
	}
	return fmt.Errorf("unexpected %q", p.peek())
}

// blank skips spaces, comments and newlines.
func (p *tomlParser) blank() error {
	for {
		p.space()
		if err := p.comment(); err != nil {
			return err
		}
		switch {
		case p.peek() == '\n':
			p.pos++
		case p.has("\r\n"):
			p.pos += 2
		default:
			return nil
		}
	}
}

func (p *tomlParser) document() error {
	if !utf8.ValidString(p.s) {
		return fmt.Errorf("invalid UTF-8")
	}
	for {
		if err := p.blank(); err != nil {
			return err
		}
		if p.eof() {
			return nil
		}
		line := strings.LastIndexByte(p.s[:p.pos], '\n') + 1
		if p.peek() == '[' {
			if p.doc.rootEnd < 0 {
				p.doc.rootEnd = line
			}
			if err := p.headerLine(); err != nil {
				return err
			}
			continue
		}
		e, err := p.keyValue(p.table, p.path)
		if err != nil {
			return err
		}
		p.space()
		if err := p.comment(); err != nil {
			return err
		}
		if err := p.newline(); err != nil {
			return err
		}
		e.line, e.end, e.header = line, p.pos, p.header
		p.doc.entries = append(p.doc.entries, e)
	}
}

// pathName names a path of keys, to tell the tables apart.
func pathName(keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(strconv.Quote(k))
		b.WriteString(".")
	}
	return b.String()
}

func (p *tomlParser) headerLine() error {
	p.pos++
	array := false
	if p.peek() == '[' {
		array = true
		p.pos++
	}
	keys, err := p.key()
	if err != nil {
		return err
	}
	p.space()
	if p.peek() != ']' {
		return fmt.Errorf("unclosed table header")
	}
	p.pos++
	if array {
		if p.peek() != ']' {
			return fmt.Errorf("unclosed header of an array of tables")
		}
		p.pos++
	}
	p.space()
	if err := p.comment(); err != nil {
		return err
	}
	if err := p.newline(); err != nil {
		return err
	}
	parent := p.root
	inArr := false
	for i, k := range keys[:len(keys)-1] {
		next, arr, err := p.descend(parent, k, keys[:i+1])
		if err != nil {
			return err
		}
		parent, inArr = next, inArr || arr
	}
	last := keys[len(keys)-1]
	name := pathName(keys)
	h := tomlHeader{path: keys, array: array, inArr: inArr || array, body: p.pos}
	if array {
		v, ok := parent[last]
		if !ok {
			v = []any{}
		}
		arr, isArr := v.([]any)
		if !isArr || p.static[name] {
			return fmt.Errorf("%s is not an array of tables", strings.Join(keys, "."))
		}
		t := map[string]any{}
		parent[last] = append(arr, t)
		// The tables of the new element are defined anew.
		for _, set := range []map[string]bool{p.defined, p.dotted, p.static} {
			for k := range set {
				if strings.HasPrefix(k, name) && k != name {
					delete(set, k)
				}
			}
		}
		p.table, p.path, p.inArr = t, keys, true
	} else {
		if p.defined[name] || p.dotted[name] {
			return fmt.Errorf("table %s defined twice", strings.Join(keys, "."))
		}
		v, ok := parent[last]
		switch t := v.(type) {
		case nil:
			if ok {
				return fmt.Errorf("%s is not a table", strings.Join(keys, "."))
			}
			nt := map[string]any{}
			parent[last] = nt
			p.table = nt
		case map[string]any:
			if p.static[name] {
				return fmt.Errorf("%s is an inline table", strings.Join(keys, "."))
			}
			p.table = t
		default:
			return fmt.Errorf("%s is not a table", strings.Join(keys, "."))
		}
		p.defined[name] = true
		p.path, p.inArr = keys, inArr
	}
	p.doc.headers = append(p.doc.headers, h)
	p.header = len(p.doc.headers) - 1
	return nil
}

// descend returns the table k of parent on the way of a header, creating
// it; the last element of an array of tables, then arr is true.
func (p *tomlParser) descend(parent map[string]any, k string, path []string) (map[string]any, bool, error) {
	v, ok := parent[k]
	if !ok {
		t := map[string]any{}
		parent[k] = t
		return t, false, nil
	}
	name := pathName(path)
	switch t := v.(type) {
	case map[string]any:
		if p.static[name] {
			return nil, false, fmt.Errorf("%s is an inline table", strings.Join(path, "."))
		}
		return t, false, nil
	case []any:
		if len(t) == 0 || p.static[name] {
			return nil, false, fmt.Errorf("%s is not a table", strings.Join(path, "."))
		}
		last, ok := t[len(t)-1].(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("%s is not a table", strings.Join(path, "."))
		}
		return last, true, nil
	}
	return nil, false, fmt.Errorf("%s is not a table", strings.Join(path, "."))
}

// keyValue reads a key-value into table, whose path is base.
func (p *tomlParser) keyValue(table map[string]any, base []string) (tomlEntry, error) {
	keys, err := p.key()
	if err != nil {
		return tomlEntry{}, err
	}
	p.space()
	if p.peek() != '=' {
		return tomlEntry{}, fmt.Errorf("no = after the key %s", strings.Join(keys, "."))
	}
	p.pos++
	p.space()
	full := append(append([]string{}, base...), keys...)
	t := table
	for i, k := range keys[:len(keys)-1] {
		path := full[:len(base)+i+1]
		name := pathName(path)
		next, ok := t[k]
		if ok {
			m, isMap := next.(map[string]any)
			if !isMap || p.static[name] || p.defined[name] {
				return tomlEntry{}, fmt.Errorf("%s is not a table to extend", strings.Join(path, "."))
			}
			t = m
			continue
		}
		m := map[string]any{}
		t[k] = m
		p.dotted[name] = true
		t = m
	}
	last := keys[len(keys)-1]
	if _, ok := t[last]; ok {
		return tomlEntry{}, fmt.Errorf("key %s defined twice", strings.Join(full, "."))
	}
	e := tomlEntry{path: full, valueStart: p.pos, inArr: p.inArr, last: -1}
	array := p.peek() == '['
	v, err := p.value(full)
	if err != nil {
		return tomlEntry{}, err
	}
	e.valueEnd = p.pos
	if array {
		e.array, e.last = true, p.lastElem
	}
	t[last] = v
	return e, nil
}

var bareKeyChar = func(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// key reads a key: bare or quoted parts apart by dots.
func (p *tomlParser) key() ([]string, error) {
	var keys []string
	for {
		p.space()
		var k string
		var err error
		switch {
		case p.peek() == '"':
			if p.has(`"""`) {
				return nil, fmt.Errorf("multi-line string as a key")
			}
			k, err = p.basic()
		case p.peek() == '\'':
			if p.has(`'''`) {
				return nil, fmt.Errorf("multi-line string as a key")
			}
			k, err = p.literal()
		default:
			start := p.pos
			for !p.eof() && bareKeyChar(p.peek()) {
				p.pos++
			}
			if p.pos == start {
				return nil, fmt.Errorf("no key")
			}
			k = p.s[start:p.pos]
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
		p.space()
		if p.peek() != '.' {
			return keys, nil
		}
		p.pos++
	}
}

// value reads a value at path.
func (p *tomlParser) value(path []string) (any, error) {
	switch c := p.peek(); {
	case p.has(`"""`):
		return p.multiBasic()
	case p.has(`'''`):
		return p.multiLiteral()
	case c == '"':
		return p.basic()
	case c == '\'':
		return p.literal()
	case c == '[':
		return p.array(path)
	case c == '{':
		return p.inlineTable(path)
	case p.has("true") && p.ends(4):
		p.pos += 4
		return true, nil
	case p.has("false") && p.ends(5):
		p.pos += 5
		return false, nil
	}
	return p.scalar()
}

// ends reports whether the value of n bytes at the position ends there.
func (p *tomlParser) ends(n int) bool {
	if p.pos+n >= len(p.s) {
		return true
	}
	return strings.IndexByte(" \t\r\n,]}#", p.s[p.pos+n]) >= 0
}

var (
	tomlDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([Tt ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?([Zz]|[+-]\d{2}:\d{2})?)?$|^\d{2}:\d{2}(:\d{2}(\.\d+)?)?$`)
	tomlDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	tomlDecInt   = regexp.MustCompile(`^[+-]?(0|[1-9](_?\d)*)$`)
	tomlHexInt   = regexp.MustCompile(`^0x[0-9A-Fa-f](_?[0-9A-Fa-f])*$`)
	tomlOctInt   = regexp.MustCompile(`^0o[0-7](_?[0-7])*$`)
	tomlBinInt   = regexp.MustCompile(`^0b[01](_?[01])*$`)
	tomlFloat    = regexp.MustCompile(`^[+-]?(0|[1-9](_?\d)*)(\.\d(_?\d)*)?([eE][+-]?\d(_?\d)*)?$`)
)

// scalar reads a number or a date up to the end of the value.
func (p *tomlParser) scalar() (any, error) {
	start := p.pos
	for !p.eof() {
		c := p.peek()
		if strings.IndexByte(",]}\n\r#\t", c) >= 0 {
			break
		}
		// A space ends the value but in a date with a space before its time.
		if c == ' ' {
			if !tomlDate.MatchString(p.s[start:p.pos]) || p.pos+1 >= len(p.s) || p.s[p.pos+1] < '0' || p.s[p.pos+1] > '9' {
				break
			}
		}
		p.pos++
	}
	s := p.s[start:p.pos]
	clean := strings.ReplaceAll(s, "_", "")
	switch {
	case s == "":
		return nil, fmt.Errorf("no value")
	case tomlDateTime.MatchString(s):
		return s, nil
	case tomlDecInt.MatchString(s):
		return strconv.ParseInt(clean, 10, 64)
	case tomlHexInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 16, 64)
	case tomlOctInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 8, 64)
	case tomlBinInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 2, 64)
	case tomlFloat.MatchString(s):
		return strconv.ParseFloat(clean, 64)
	case s == "inf" || s == "+inf":
		return math.Inf(1), nil
	case s == "-inf":
		return math.Inf(-1), nil
	case s == "nan" || s == "+nan" || s == "-nan":
		return math.NaN(), nil
	}
	return nil, fmt.Errorf("invalid value %q", s)
}

func (p *tomlParser) array(path []string) (any, error) {
	p.pos++
	arr := []any{}
	last := -1
	for {
		if err := p.blank(); err != nil {
			return nil, err
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		v, err := p.value(path)
		if err != nil {
			return nil, err
		}
		last = p.pos
		arr = append(arr, v)
		if err := p.blank(); err != nil {
			return nil, err
		}
		if p.peek() == ',' {
			p.pos++
			continue
		}
		if p.peek() != ']' {
			return nil, fmt.Errorf("unexpected %q in an array", p.peek())
		}
		p.pos++
		break
	}
	p.static[pathName(path)] = true
	p.lastElem = last
	return arr, nil
}

func (p *tomlParser) inlineTable(path []string) (any, error) {
	p.pos++
	t := map[string]any{}
	p.space()
	if p.peek() == '}' {
		p.pos++
		p.static[pathName(path)] = true
		return t, nil
	}
	for {
		p.space()
		e, err := p.keyValue(t, path)
		if err != nil {
			return nil, err
		}
		e.inline, e.header = true, p.header
		p.doc.entries = append(p.doc.entries, e)
		p.space()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			p.static[pathName(path)] = true
			return t, nil
		default:
			return nil, fmt.Errorf("unexpected %q in an inline table", p.peek())
		}
	}
}

func (p *tomlParser) literal() (string, error) {
	p.pos++
	start := p.pos
	for !p.eof() && p.peek() != '\'' {
		if c := p.peek(); c == '\n' || c < 0x20 && c != '\t' || c == 0x7f {
			return "", fmt.Errorf("control character in a literal string")
		}
		p.pos++
	}
	if p.eof() {
		return "", fmt.Errorf("unclosed literal string")
	}
	s := p.s[start:p.pos]
	p.pos++
	return s, nil
}

func (p *tomlParser) multiLiteral() (string, error) {
	p.pos += 3
	if p.peek() == '\n' {
		p.pos++
	} else if p.has("\r\n") {
		p.pos += 2
	}
	var b strings.Builder
	for !p.eof() {
		if p.has("'''") {
			n := 0
			for p.pos+n < len(p.s) && p.s[p.pos+n] == '\'' {
				n++
			}
			if n > 5 {
				return "", fmt.Errorf("too many quotes")
			}
			b.WriteString(strings.Repeat("'", n-3))
			p.pos += n
			return b.String(), nil
		}
		c := p.peek()
		switch {
		case p.has("\r\n"):
			b.WriteString("\n")
			p.pos += 2
			continue
		case c < 0x20 && c != '\t' && c != '\n' || c == 0x7f:
			return "", fmt.Errorf("control character in a string")
		}
		b.WriteByte(c)
		p.pos++
	}
	return "", fmt.Errorf("unclosed multi-line literal string")
}

func (p *tomlParser) basic() (string, error) {
	p.pos++
	var b strings.Builder
	for {
		if p.eof() || p.peek() == '\n' {
			return "", fmt.Errorf("unclosed string")
		}
		c := p.peek()
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\\':
			if err := p.escape(&b); err != nil {
				return "", err
			}
		case c < 0x20 && c != '\t' || c == 0x7f:
			return "", fmt.Errorf("control character in a string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *tomlParser) multiBasic() (string, error) {
	p.pos += 3
	if p.peek() == '\n' {
		p.pos++
	} else if p.has("\r\n") {
		p.pos += 2
	}
	var b strings.Builder
	for {
		if p.eof() {
			return "", fmt.Errorf("unclosed multi-line string")
		}
		if p.has(`"""`) {
			n := 0
			for p.pos+n < len(p.s) && p.s[p.pos+n] == '"' {
				n++
			}
			if n > 5 {
				return "", fmt.Errorf("too many quotes")
			}
			b.WriteString(strings.Repeat(`"`, n-3))
			p.pos += n
			return b.String(), nil
		}
		c := p.peek()
		switch {
		case c == '\\':
			// A backslash at the end of a line trims the whitespace after it.
			q := p.pos + 1
			for q < len(p.s) && (p.s[q] == ' ' || p.s[q] == '\t') {
				q++
			}
			if q < len(p.s) && (p.s[q] == '\n' || p.s[q] == '\r') {
				p.pos = q
				for !p.eof() && strings.IndexByte(" \t\r\n", p.peek()) >= 0 {
					p.pos++
				}
				continue
			}
			if err := p.escape(&b); err != nil {
				return "", err
			}
		case p.has("\r\n"):
			b.WriteString("\n")
			p.pos += 2
		case c < 0x20 && c != '\t' && c != '\n' || c == 0x7f:
			return "", fmt.Errorf("control character in a string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *tomlParser) escape(b *strings.Builder) error {
	p.pos++
	if p.eof() {
		return fmt.Errorf("unclosed escape")
	}
	c := p.peek()
	p.pos++
	switch c {
	case 'b':
		b.WriteByte('\b')
	case 't':
		b.WriteByte('\t')
	case 'n':
		b.WriteByte('\n')
	case 'f':
		b.WriteByte('\f')
	case 'r':
		b.WriteByte('\r')
	case 'e':
		b.WriteByte(0x1b)
	case '"':
		b.WriteByte('"')
	case '\\':
		b.WriteByte('\\')
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if p.pos+n > len(p.s) {
			return fmt.Errorf("short unicode escape")
		}
		code, err := strconv.ParseUint(p.s[p.pos:p.pos+n], 16, 32)
		if err != nil || !utf8.ValidRune(rune(code)) {
			return fmt.Errorf("invalid unicode escape")
		}
		b.WriteRune(rune(code))
		p.pos += n
	default:
		return fmt.Errorf("invalid escape \\%c", c)
	}
	return nil
}

// tomlAt returns the value at the path of keys; false if there is none.
func tomlAt(doc map[string]any, keys ...string) (any, bool) {
	var v any = doc
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return v, true
}

// tomlString writes s as a string of TOML: a literal string if it can be
// one, as for a path of Windows, else a basic string.
func tomlString(s string) string {
	literal := !strings.ContainsAny(s, "'\n\r\x7f")
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			literal = false
		}
	}
	if literal {
		return "'" + s + "'"
	}
	return tomlBasic(s)
}

// tomlBasic writes s as a basic string of TOML.
func tomlBasic(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlKey writes a key: bare if it can be, else a string.
func tomlKey(k string) string {
	bare := k != ""
	for i := 0; i < len(k); i++ {
		bare = bare && bareKeyChar(k[i])
	}
	if bare {
		return k
	}
	return tomlString(k)
}

// tomlKeys writes a dotted key.
func tomlKeys(keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tomlKey(k)
	}
	return strings.Join(parts, ".")
}

// samePathKeys tells whether two paths of keys are the same.
func samePathKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// entry returns the key-value of path outside the tables of arrays of
// tables; false if the text has none.
func (d *tomlDoc) entry(path []string) (tomlEntry, bool) {
	for _, e := range d.entries {
		if !e.inArr && samePathKeys(e.path, path) {
			return e, true
		}
	}
	return tomlEntry{}, false
}

// newline is the line end of the text: CRLF if the text has one.
func (d *tomlDoc) newline() string {
	if strings.Contains(d.text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// errTOMLEdit means the text has the value in a place the edits of Gentry
// do not change, such as an inline table; the text is left as it is.
var errTOMLEdit = errors.New("the settings have the value in a form Gentry does not edit")

// tomlSet sets the value at path to the TOML text of the value lit, in the
// text: in place of the value it has, or as a key-value of the table of the
// path.
func tomlSet(text string, path []string, lit string) (string, error) {
	d, err := parseTOML(text)
	if err != nil {
		return "", err
	}
	if e, ok := d.entry(path); ok {
		return text[:e.valueStart] + lit + text[e.valueEnd:], nil
	}
	if _, ok := tomlAt(d.root, path...); ok {
		return "", errTOMLEdit
	}
	return d.add(path, lit)
}

// add adds the key-value of path, which the text has not, to the text: in
// the table of the longest header its path starts with, else in the root
// table if it is a key of the root or its first key is a table of dotted
// keys there, else under a header of its table at the end of the text.
func (d *tomlDoc) add(path []string, lit string) (string, error) {
	// The tables on the way are tables the text can extend: not values,
	// inline tables or arrays of tables.
	for i := 1; i < len(path); i++ {
		v, ok := tomlAt(d.root, path[:i]...)
		if !ok {
			break
		}
		if _, isMap := v.(map[string]any); !isMap {
			return "", errTOMLEdit
		}
		if _, inline := d.entry(path[:i]); inline {
			return "", errTOMLEdit
		}
	}
	nl := d.newline()
	for k := len(path) - 1; k >= 1; k-- {
		for hi, h := range d.headers {
			if h.inArr || !samePathKeys(h.path, path[:k]) {
				continue
			}
			at := h.body
			for _, e := range d.entries {
				if e.header == hi && !e.inline && e.end > at {
					at = e.end
				}
			}
			return d.insertLine(at, tomlKeys(path[k:])+" = "+lit, nl), nil
		}
	}
	dotted := false
	at := -1
	for _, e := range d.entries {
		if e.header == -1 && !e.inline {
			at = max(at, e.end)
			dotted = dotted || e.path[0] == path[0]
		}
	}
	if len(path) == 1 || dotted {
		line := tomlKeys(path) + " = " + lit
		switch {
		case at >= 0:
			return d.insertLine(at, line, nl), nil
		case d.rootEnd < len(d.text):
			// Before the first header, with a blank line after it.
			return d.text[:d.rootEnd] + line + nl + nl + d.text[d.rootEnd:], nil
		}
		return d.insertLine(len(d.text), line, nl), nil
	}
	text := d.text
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += nl
	}
	if strings.TrimSpace(text) != "" {
		text += nl
	}
	return text + "[" + tomlKeys(path[:len(path)-1]) + "]" + nl + tomlKey(path[len(path)-1]) + " = " + lit + nl, nil
}

// insertLine puts the line at the offset at, the start of a line or the end
// of the text.
func (d *tomlDoc) insertLine(at int, line, nl string) string {
	before := d.text[:at]
	if before != "" && !strings.HasSuffix(before, "\n") {
		before += nl
	}
	return before + line + nl + d.text[at:]
}

// tomlRemove removes the key-value of path from the text; the text without
// it is as it was.
func tomlRemove(text string, path []string) (string, error) {
	d, err := parseTOML(text)
	if err != nil {
		return "", err
	}
	e, ok := d.entry(path)
	if !ok {
		if _, has := tomlAt(d.root, path...); has {
			return "", errTOMLEdit
		}
		return text, nil
	}
	if e.inline {
		return "", errTOMLEdit
	}
	return text[:e.line] + text[e.end:], nil
}

// tomlAppend adds the TOML text of the value lit to the array at path, after
// its last element; an array that is not there is set.
func tomlAppend(text string, path []string, lit string) (string, error) {
	d, err := parseTOML(text)
	if err != nil {
		return "", err
	}
	e, ok := d.entry(path)
	if !ok {
		if _, has := tomlAt(d.root, path...); has {
			return "", errTOMLEdit
		}
		return d.add(path, "["+lit+"]")
	}
	if !e.array {
		return "", errTOMLEdit
	}
	if e.last < 0 {
		return text[:e.valueStart+1] + lit + text[e.valueStart+1:], nil
	}
	insert := ", " + lit
	if strings.Contains(text[e.valueStart:e.valueEnd], "\n") {
		// An array over lines gets the element on a line of its own, with
		// the indent of the line of its last element.
		lineStart := strings.LastIndexByte(text[:e.last], '\n') + 1
		indent := lineStart
		for indent < len(text) && (text[indent] == ' ' || text[indent] == '\t') {
			indent++
		}
		insert = "," + d.newline() + text[lineStart:indent] + lit
	}
	return text[:e.last] + insert + text[e.last:], nil
}

// bom is the byte order mark a text may start with.
const bom = "\ufeff"
