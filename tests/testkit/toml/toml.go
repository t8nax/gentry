// Package toml reads and writes the TOML of the settings of Codex and of the
// files of its subagents for the tests and the fake Codex: the module has no
// library of TOML, and the tests must not depend on the one the program
// chooses. Parse reads TOML 1.0 but for the dates, which it keeps as strings;
// Marshal writes tables with their keys sorted, as a document the tests and
// the fake Codex compare by value, not by text.
//
// Values are string, int64, float64, bool, []any and map[string]any.
package toml

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse reads a TOML document.
func Parse(text string) (map[string]any, error) {
	p := &parser{src: []rune(text), root: map[string]any{}, defined: map[string]bool{}}
	p.table = p.root
	if err := p.document(); err != nil {
		return nil, fmt.Errorf("toml: line %d: %w", p.line(), err)
	}
	return p.root, nil
}

type parser struct {
	src   []rune
	pos   int
	root  map[string]any
	table map[string]any
	// defined are the tables of headers and of dotted keys, by their path, to
	// tell a table defined twice.
	defined map[string]bool
	// inline are the tables and arrays given inline, which cannot be extended.
	inline []any
}

func (p *parser) line() int {
	n := 1
	for _, r := range p.src[:min(p.pos, len(p.src))] {
		if r == '\n' {
			n++
		}
	}
	return n
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) has(s string) bool {
	rs := []rune(s)
	return p.pos+len(rs) <= len(p.src) && string(p.src[p.pos:p.pos+len(rs)]) == s
}

// space skips spaces and tabs.
func (p *parser) space() {
	for !p.eof() && (p.peek() == ' ' || p.peek() == '\t') {
		p.pos++
	}
}

// comment skips a comment up to the end of the line.
func (p *parser) comment() error {
	if p.peek() != '#' {
		return nil
	}
	for !p.eof() && p.peek() != '\n' {
		r := p.peek()
		if r < 0x20 && r != '\t' && r != '\r' || r == 0x7f {
			return fmt.Errorf("control character in a comment")
		}
		p.pos++
	}
	return nil
}

// newline takes the end of a line: a newline, CRLF or the end of the text.
func (p *parser) newline() error {
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

// blank skips spaces, comments and newlines, as between the values of an
// array.
func (p *parser) blank() error {
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

func (p *parser) document() error {
	for {
		if err := p.blank(); err != nil {
			return err
		}
		if p.eof() {
			return nil
		}
		var err error
		if p.peek() == '[' {
			err = p.header()
		} else {
			err = p.keyValue(p.table)
		}
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
	}
}

func pathName(keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(strconv.Quote(k))
		b.WriteString(".")
	}
	return b.String()
}

func (p *parser) header() error {
	p.pos++
	array := false
	if p.peek() == '[' {
		array = true
		p.pos++
	}
	p.space()
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
	parent := p.root
	for i, k := range keys[:len(keys)-1] {
		next, err := p.descend(parent, k, pathName(keys[:i+1]))
		if err != nil {
			return err
		}
		parent = next
	}
	last := keys[len(keys)-1]
	name := pathName(keys)
	if array {
		v, ok := parent[last]
		if !ok {
			v = []any{}
		}
		arr, isArr := v.([]any)
		if !isArr || p.isInline(v) {
			return fmt.Errorf("%s is not an array of tables", strings.Join(keys, "."))
		}
		t := map[string]any{}
		parent[last] = append(arr, t)
		// The tables of the new element are defined anew.
		for k := range p.defined {
			if strings.HasPrefix(k, name) {
				delete(p.defined, k)
			}
		}
		p.table = t
		return nil
	}
	if p.defined[name] {
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
		if p.isInline(t) {
			return fmt.Errorf("%s is an inline table", strings.Join(keys, "."))
		}
		p.table = t
	default:
		return fmt.Errorf("%s is not a table", strings.Join(keys, "."))
	}
	p.defined[name] = true
	return nil
}

func (p *parser) isInline(v any) bool {
	return slices.ContainsFunc(p.inline, func(x any) bool { return sameRef(x, v) })
}

// sameRef reports whether a and b are the same map or slice.
func sameRef(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && fmt.Sprintf("%p", x) == fmt.Sprintf("%p", y)
	case []any:
		y, ok := b.([]any)
		return ok && len(x) > 0 && len(y) > 0 && &x[0] == &y[0]
	}
	return false
}

// descend returns the table k of parent for a header or a dotted key,
// creating it; the last element of an array of tables.
func (p *parser) descend(parent map[string]any, k, name string) (map[string]any, error) {
	v, ok := parent[k]
	if !ok {
		t := map[string]any{}
		parent[k] = t
		return t, nil
	}
	switch t := v.(type) {
	case map[string]any:
		if p.isInline(t) {
			return nil, fmt.Errorf("%s is an inline table", k)
		}
		return t, nil
	case []any:
		if len(t) == 0 || p.isInline(t) {
			return nil, fmt.Errorf("%s is not a table", k)
		}
		last, ok := t[len(t)-1].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s is not a table", k)
		}
		return last, nil
	}
	return nil, fmt.Errorf("%s is not a table", k)
}

func (p *parser) keyValue(table map[string]any) error {
	keys, err := p.key()
	if err != nil {
		return err
	}
	p.space()
	if p.peek() != '=' {
		return fmt.Errorf("no = after the key %s", strings.Join(keys, "."))
	}
	p.pos++
	p.space()
	v, err := p.value()
	if err != nil {
		return err
	}
	t := table
	for i, k := range keys[:len(keys)-1] {
		next, ok := t[k]
		if ok {
			m, isMap := next.(map[string]any)
			if !isMap || p.isInline(m) {
				return fmt.Errorf("%s is not a table", strings.Join(keys[:i+1], "."))
			}
			t = m
			continue
		}
		m := map[string]any{}
		t[k] = m
		t = m
	}
	last := keys[len(keys)-1]
	if _, ok := t[last]; ok {
		return fmt.Errorf("key %s defined twice", strings.Join(keys, "."))
	}
	t[last] = v
	return nil
}

var bare = regexp.MustCompile(`^[A-Za-z0-9_-]$`)

// key reads a key: bare or quoted parts apart by dots.
func (p *parser) key() ([]string, error) {
	var keys []string
	for {
		p.space()
		var k string
		var err error
		switch {
		case p.peek() == '"':
			k, err = p.basic()
		case p.peek() == '\'':
			k, err = p.literal()
		default:
			start := p.pos
			for !p.eof() && bare.MatchString(string(p.peek())) {
				p.pos++
			}
			if p.pos == start {
				return nil, fmt.Errorf("no key")
			}
			k = string(p.src[start:p.pos])
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

func (p *parser) value() (any, error) {
	switch r := p.peek(); {
	case p.has(`"""`):
		return p.multiBasic()
	case p.has(`'''`):
		return p.multiLiteral()
	case r == '"':
		return p.basic()
	case r == '\'':
		return p.literal()
	case r == '[':
		return p.array()
	case r == '{':
		return p.inlineTable()
	case p.has("true"):
		p.pos += 4
		return true, nil
	case p.has("false"):
		p.pos += 5
		return false, nil
	}
	return p.scalar()
}

var (
	dateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([Tt ]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})?)?$|^\d{2}:\d{2}:\d{2}(\.\d+)?$`)
	decInt   = regexp.MustCompile(`^[+-]?(0|[1-9](_?\d)*)$`)
	hexInt   = regexp.MustCompile(`^0x[0-9A-Fa-f](_?[0-9A-Fa-f])*$`)
	octInt   = regexp.MustCompile(`^0o[0-7](_?[0-7])*$`)
	binInt   = regexp.MustCompile(`^0b[01](_?[01])*$`)
	float    = regexp.MustCompile(`^[+-]?(0|[1-9](_?\d)*)(\.\d(_?\d)*)?([eE][+-]?\d(_?\d)*)?$`)
)

// scalar reads a number or a date up to the end of the value.
func (p *parser) scalar() (any, error) {
	start := p.pos
	for !p.eof() {
		r := p.peek()
		if r == ',' || r == ']' || r == '}' || r == '\n' || r == '\r' || r == '#' || r == '\t' {
			break
		}
		// A space ends the value but in a date with a space before its time.
		if r == ' ' {
			s := string(p.src[start:p.pos])
			if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(s) || p.pos+1 >= len(p.src) || p.src[p.pos+1] < '0' || p.src[p.pos+1] > '9' {
				break
			}
		}
		p.pos++
	}
	s := string(p.src[start:p.pos])
	clean := strings.ReplaceAll(s, "_", "")
	switch {
	case s == "":
		return nil, fmt.Errorf("no value")
	case dateTime.MatchString(s):
		return s, nil
	case decInt.MatchString(s):
		return strconv.ParseInt(clean, 10, 64)
	case hexInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 16, 64)
	case octInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 8, 64)
	case binInt.MatchString(s):
		return strconv.ParseInt(clean[2:], 2, 64)
	case float.MatchString(s):
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

func (p *parser) array() (any, error) {
	p.pos++
	arr := []any{}
	for {
		if err := p.blank(); err != nil {
			return nil, err
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		if err := p.blank(); err != nil {
			return nil, err
		}
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			p.inline = append(p.inline, arr)
			return arr, nil
		default:
			return nil, fmt.Errorf("unexpected %q in an array", p.peek())
		}
	}
	p.inline = append(p.inline, arr)
	return arr, nil
}

func (p *parser) inlineTable() (any, error) {
	p.pos++
	t := map[string]any{}
	p.space()
	if p.peek() == '}' {
		p.pos++
		p.inline = append(p.inline, t)
		return t, nil
	}
	for {
		p.space()
		if err := p.keyValue(t); err != nil {
			return nil, err
		}
		p.space()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			p.inline = append(p.inline, t)
			return t, nil
		default:
			return nil, fmt.Errorf("unexpected %q in an inline table", p.peek())
		}
	}
}

func (p *parser) literal() (string, error) {
	p.pos++
	start := p.pos
	for !p.eof() && p.peek() != '\'' {
		if p.peek() == '\n' {
			return "", fmt.Errorf("newline in a literal string")
		}
		p.pos++
	}
	if p.eof() {
		return "", fmt.Errorf("unclosed literal string")
	}
	s := string(p.src[start:p.pos])
	p.pos++
	return s, nil
}

func (p *parser) multiLiteral() (string, error) {
	p.pos += 3
	if p.peek() == '\n' {
		p.pos++
	} else if p.has("\r\n") {
		p.pos += 2
	}
	start := p.pos
	for !p.eof() {
		if p.has("'''") {
			n := 0
			for p.pos+n < len(p.src) && p.src[p.pos+n] == '\'' {
				n++
			}
			if n > 5 {
				return "", fmt.Errorf("too many quotes")
			}
			s := string(p.src[start : p.pos+n-3])
			p.pos += n
			return s, nil
		}
		p.pos++
	}
	return "", fmt.Errorf("unclosed multi-line literal string")
}

func (p *parser) basic() (string, error) {
	p.pos++
	var b strings.Builder
	for {
		if p.eof() || p.peek() == '\n' {
			return "", fmt.Errorf("unclosed string")
		}
		r := p.peek()
		switch {
		case r == '"':
			p.pos++
			return b.String(), nil
		case r == '\\':
			if err := p.escape(&b); err != nil {
				return "", err
			}
		case r < 0x20 && r != '\t' || r == 0x7f:
			return "", fmt.Errorf("control character in a string")
		default:
			b.WriteRune(r)
			p.pos++
		}
	}
}

func (p *parser) multiBasic() (string, error) {
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
			for p.pos+n < len(p.src) && p.src[p.pos+n] == '"' {
				n++
			}
			if n > 5 {
				return "", fmt.Errorf("too many quotes")
			}
			b.WriteString(strings.Repeat(`"`, n-3))
			p.pos += n
			return b.String(), nil
		}
		r := p.peek()
		switch {
		case r == '\\':
			// A backslash at the end of a line trims the whitespace after it.
			q := p.pos + 1
			for q < len(p.src) && (p.src[q] == ' ' || p.src[q] == '\t') {
				q++
			}
			if q < len(p.src) && (p.src[q] == '\n' || p.src[q] == '\r') {
				p.pos = q
				for !p.eof() && (p.peek() == ' ' || p.peek() == '\t' || p.peek() == '\n' || p.peek() == '\r') {
					p.pos++
				}
				continue
			}
			if err := p.escape(&b); err != nil {
				return "", err
			}
		case r == '\r' && p.has("\r\n"):
			b.WriteString("\n")
			p.pos += 2
		case r < 0x20 && r != '\t' && r != '\n' || r == 0x7f:
			return "", fmt.Errorf("control character in a string")
		default:
			b.WriteRune(r)
			p.pos++
		}
	}
}

func (p *parser) escape(b *strings.Builder) error {
	p.pos++
	if p.eof() {
		return fmt.Errorf("unclosed escape")
	}
	r := p.peek()
	p.pos++
	switch r {
	case 'b':
		b.WriteRune('\b')
	case 't':
		b.WriteRune('\t')
	case 'n':
		b.WriteRune('\n')
	case 'f':
		b.WriteRune('\f')
	case 'r':
		b.WriteRune('\r')
	case 'e':
		b.WriteRune(0x1b)
	case '"':
		b.WriteRune('"')
	case '\\':
		b.WriteRune('\\')
	case 'u', 'U':
		n := 4
		if r == 'U' {
			n = 8
		}
		if p.pos+n > len(p.src) {
			return fmt.Errorf("short unicode escape")
		}
		code, err := strconv.ParseUint(string(p.src[p.pos:p.pos+n]), 16, 32)
		if err != nil || !utf8.ValidRune(rune(code)) {
			return fmt.Errorf("invalid unicode escape")
		}
		b.WriteRune(rune(code))
		p.pos += n
	default:
		return fmt.Errorf("invalid escape \\%c", r)
	}
	return nil
}

// Marshal writes a document: the values of a table, then its tables, each
// with its keys sorted.
func Marshal(doc map[string]any) string {
	var b strings.Builder
	writeTable(&b, nil, doc)
	return strings.TrimLeft(b.String(), "\n")
}

func isTable(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func isTableArray(v any) bool {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return false
	}
	for _, x := range arr {
		if !isTable(x) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func writeTable(b *strings.Builder, path []string, t map[string]any) {
	keys := sortedKeys(t)
	for _, k := range keys {
		if v := t[k]; !isTable(v) && !isTableArray(v) {
			fmt.Fprintf(b, "%s = %s\n", Key(k), Value(v))
		}
	}
	for _, k := range keys {
		sub := append(slices.Clone(path), k)
		switch v := t[k].(type) {
		case map[string]any:
			// A table of tables only has no header of its own.
			if len(v) == 0 || slices.ContainsFunc(sortedKeys(v), func(k string) bool { return !isTable(v[k]) && !isTableArray(v[k]) }) {
				fmt.Fprintf(b, "\n[%s]\n", keyPath(sub))
			}
			writeTable(b, sub, v)
		case []any:
			if !isTableArray(v) {
				continue
			}
			for _, x := range v {
				fmt.Fprintf(b, "\n[[%s]]\n", keyPath(sub))
				writeTable(b, sub, x.(map[string]any))
			}
		}
	}
}

func keyPath(keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = Key(k)
	}
	return strings.Join(parts, ".")
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Key writes a key: bare if it can be, else a basic string.
func Key(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return String(k)
}

// String writes a basic string.
func String(s string) string {
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

// Value writes a value inline.
func Value(v any) string {
	switch x := v.(type) {
	case string:
		return String(x)
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		switch {
		case math.IsInf(x, 1):
			return "inf"
		case math.IsInf(x, -1):
			return "-inf"
		case math.IsNaN(x):
			return "nan"
		}
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEn") {
			s += ".0"
		}
		return s
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = Value(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = String(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedKeys(x)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = Key(k) + " = " + Value(x[k])
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case nil:
		return `""`
	}
	return String(fmt.Sprint(v))
}

// At returns the value at the path of keys; false if there is none.
func At(doc map[string]any, keys ...string) (any, bool) {
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

// Set sets the value at the path of keys, creating the tables on the way.
func Set(doc map[string]any, v any, keys ...string) {
	t := doc
	for _, k := range keys[:len(keys)-1] {
		next, ok := t[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			t[k] = next
		}
		t = next
	}
	t[keys[len(keys)-1]] = v
}

// Delete removes the value at the path of keys, if there is one.
func Delete(doc map[string]any, keys ...string) {
	parent, ok := At(doc, keys[:len(keys)-1]...)
	if m, isMap := parent.(map[string]any); ok && isMap {
		delete(m, keys[len(keys)-1])
	}
}
