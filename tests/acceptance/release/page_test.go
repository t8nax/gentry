package release_test

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file reads the pages of the site the way the tests compare them: a
// small reader of HTML builds the tree of the elements, and the text of a
// page is what is left of it with no markup and no extra spaces, the
// references of characters read.
//
// What the tests read of the markup is that of the approved mockup of the
// page, docs/GEN-5/release-page-example.html:
//
//   - the heading of the page is h1, the sections are headings of one level
//     and the cards in «Новое» are headings of a lower one; the part of a
//     section or of a card is all after its heading up to the next heading
//     of the same or a higher level;
//   - a window of an example is an element of the class term, its lines are
//     the elements of the class line in it, with the text as shown; a line
//     of the class cmd is a command, ok is marked, err is an error, dim is
//     a note, none of them is a plain line; a window of the class play
//     shows its lines one by one;
//   - the names of tabs are the elements of the class tab, the switches are
//     the radio buttons of a form, the windows of the tabs go in their
//     order; a pair side by side is two windows in an element of the class
//     beforeafter, each after its caption of the class label;
//   - the entries of the journal are the items li of the list of a section;
//     a row of the list of the releases is the item li with the link of the
//     version.

// A node is an element or a text of a page.
type node struct {
	tag    string // the name of an element in lower case; "" for a text
	attrs  map[string]string
	text   string // the text, its references of characters read
	parent *node
	kids   []*node
	// start and end order the nodes in the page: a node is inside another
	// when its start is between the start and the end of the other.
	start, end int
}

// voidTags are the elements of HTML that have no end.
var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// rawTags are the elements whose content is text up to their end tag.
var rawTags = map[string]bool{"script": true, "style": true, "title": true, "textarea": true}

// blockTags are the elements that part words of the text of a page.
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "body": true, "br": true,
	"dd": true, "details": true, "div": true, "dl": true, "dt": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hr": true, "label": true,
	"li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"summary": true, "table": true, "tbody": true, "td": true, "tfoot": true, "th": true,
	"thead": true, "tr": true, "ul": true,
}

// pCloser are the elements whose start ends an open paragraph.
var pCloser = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"div": true, "dl": true, "fieldset": true, "figure": true, "footer": true, "form": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "header": true,
	"hr": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"table": true, "ul": true,
}

// implied tells, for an element whose start ends an open element, which
// elements it ends and which elements it stops the search at.
var implied = map[string]struct{ ends, stops []string }{
	"li": {[]string{"li"}, []string{"ul", "ol"}},
	"tr": {[]string{"tr", "td", "th"}, []string{"table", "thead", "tbody", "tfoot"}},
	"td": {[]string{"td", "th"}, []string{"tr", "table"}},
	"th": {[]string{"td", "th"}, []string{"tr", "table"}},
	"dt": {[]string{"dt", "dd"}, []string{"dl"}},
	"dd": {[]string{"dt", "dd"}, []string{"dl"}},
}

var attrName = regexp.MustCompile(`^[^\s"'>/=]+`)

// parseHTML reads a page into the tree of its nodes under a root.
func parseHTML(src string) *node {
	pos := 0
	next := func() int { pos++; return pos }
	root := &node{tag: "#document", start: next()}
	stack := []*node{root}
	add := func(n *node) {
		top := stack[len(stack)-1]
		n.parent = top
		top.kids = append(top.kids, n)
	}
	text := func(s string, decode bool) {
		if s == "" {
			return
		}
		if decode {
			s = html.UnescapeString(s)
		}
		p := next()
		add(&node{text: s, start: p, end: p})
	}
	closeFrom := func(i int) {
		for len(stack) > i {
			stack[len(stack)-1].end = next()
			stack = stack[:len(stack)-1]
		}
	}
	// find returns the index in the stack of the nearest open element of
	// one of the names, looking down to an element of the stops.
	find := func(names, stops []string) int {
		for i := len(stack) - 1; i > 0; i-- {
			for _, n := range names {
				if stack[i].tag == n {
					return i
				}
			}
			for _, s := range stops {
				if stack[i].tag == s {
					return -1
				}
			}
		}
		return -1
	}
	i := 0
	for i < len(src) {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			text(src[i:], true)
			break
		}
		text(src[i:i+lt], true)
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			e := strings.Index(rest[4:], "-->")
			if e < 0 {
				i = len(src)
			} else {
				i += 4 + e + 3
			}
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			e := strings.IndexByte(rest, '>')
			if e < 0 {
				i = len(src)
			} else {
				i += e + 1
			}
		case strings.HasPrefix(rest, "</"):
			e := strings.IndexByte(rest, '>')
			if e < 0 {
				i = len(src)
				break
			}
			name := strings.ToLower(strings.TrimSpace(rest[2:e]))
			i += e + 1
			for j := len(stack) - 1; j > 0; j-- {
				if stack[j].tag == name {
					closeFrom(j)
					break
				}
			}
		default:
			m := attrName.FindString(rest[1:])
			if m == "" {
				text("<", false)
				i++
				break
			}
			tag := strings.ToLower(m)
			j := 1 + len(m)
			attrs := map[string]string{}
			selfClosed := false
			for j < len(rest) {
				for j < len(rest) && strings.ContainsRune(" \t\r\n\f", rune(rest[j])) {
					j++
				}
				if j >= len(rest) {
					break
				}
				if rest[j] == '>' {
					j++
					break
				}
				if strings.HasPrefix(rest[j:], "/>") {
					selfClosed = true
					j += 2
					break
				}
				if rest[j] == '/' {
					j++
					continue
				}
				name := attrName.FindString(rest[j:])
				if name == "" {
					j++
					continue
				}
				j += len(name)
				value := ""
				k := j
				for k < len(rest) && strings.ContainsRune(" \t\r\n\f", rune(rest[k])) {
					k++
				}
				if k < len(rest) && rest[k] == '=' {
					k++
					for k < len(rest) && strings.ContainsRune(" \t\r\n\f", rune(rest[k])) {
						k++
					}
					if k < len(rest) && (rest[k] == '"' || rest[k] == '\'') {
						q := rest[k]
						e := strings.IndexByte(rest[k+1:], q)
						if e < 0 {
							e = len(rest) - k - 1
						}
						value = rest[k+1 : k+1+e]
						k += 1 + e + 1
					} else {
						e := k
						for e < len(rest) && !strings.ContainsRune(" \t\r\n\f>", rune(rest[e])) {
							e++
						}
						value = rest[k:e]
						k = e
					}
					j = k
				}
				attrs[strings.ToLower(name)] = html.UnescapeString(value)
			}
			i += j
			if pCloser[tag] {
				if p := find([]string{"p"}, []string{"div", "section", "article", "td", "th", "li", "main", "body", "header", "footer", "button", "table"}); p > 0 {
					closeFrom(p)
				}
			}
			if im, ok := implied[tag]; ok {
				if p := find(im.ends, im.stops); p > 0 {
					closeFrom(p)
				}
			}
			n := &node{tag: tag, attrs: attrs, start: next()}
			add(n)
			switch {
			case voidTags[tag] || selfClosed:
				n.end = next()
			case rawTags[tag]:
				end := strings.Index(strings.ToLower(src[i:]), "</"+tag)
				if end < 0 {
					end = len(src) - i
				}
				stack = append(stack, n)
				text(src[i:i+end], tag == "title" || tag == "textarea")
				closeFrom(len(stack) - 1)
				i += end
				if e := strings.IndexByte(src[i:], '>'); e >= 0 {
					i += e + 1
				} else {
					i = len(src)
				}
			default:
				stack = append(stack, n)
			}
		}
	}
	closeFrom(1)
	root.end = next()
	return root
}

// walk calls f for the node and each node inside it, in the order of the
// page.
func (n *node) walk(f func(*node)) {
	f(n)
	for _, k := range n.kids {
		k.walk(f)
	}
}

// hasClass reports whether the element has the class.
func (n *node) hasClass(c string) bool {
	for _, f := range strings.Fields(n.attrs["class"]) {
		if f == c {
			return true
		}
	}
	return false
}

// rawText returns the text of the node as it is shown in a line of a
// window: all its texts, the spaces kept.
func (n *node) rawText() string {
	var b strings.Builder
	n.walk(func(k *node) {
		if k.tag == "" {
			b.WriteString(k.text)
		}
	})
	return b.String()
}

// A page is a page of the site the test reads.
type page struct {
	name string // the file of the page in the site
	raw  string // the HTML of the page
	root *node
	all  []*node // the nodes in the order of their starts
	at   []*node // the node that starts or ends at a position
}

// readPage reads the page name, a path apart by / in the site dir.
func readPage(t *testing.T, dir, name string) *page {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("the site has no page %s: %v", name, err)
	}
	p := &page{name: name, raw: string(b), root: parseHTML(string(b))}
	p.at = make([]*node, p.root.end+1)
	p.root.walk(func(n *node) {
		p.all = append(p.all, n)
		p.at[n.start], p.at[n.end] = n, n
	})
	return p
}

// inStyle reports whether the node is in an element whose content is no
// text of the page.
func inStyle(n *node) bool {
	for a := n.parent; a != nil; a = a.parent {
		if a.tag == "style" || a.tag == "script" || a.tag == "head" || a.tag == "template" {
			return true
		}
	}
	return false
}

// textIn returns the text of the page between the positions lo and hi,
// both excluded: the texts there with no markup, the words parted at the
// starts and the ends of the blocks, the spaces folded.
func (p *page) textIn(lo, hi int) string {
	var b strings.Builder
	for pos := lo + 1; pos < hi && pos < len(p.at); pos++ {
		n := p.at[pos]
		switch {
		case n == nil:
		case n.tag == "":
			if !inStyle(n) {
				b.WriteString(n.text)
			}
		case blockTags[n.tag]:
			b.WriteString(" ")
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// text returns the text of the node.
func (p *page) text(n *node) string {
	if n.tag == "" {
		return strings.Join(strings.Fields(n.text), " ")
	}
	return p.textIn(n.start, n.end)
}

// bodyText returns the text of the whole page.
func (p *page) bodyText() string { return p.textIn(p.root.start, p.root.end) }

// within returns the elements that start between lo and hi and match.
func (p *page) within(lo, hi int, match func(*node) bool) []*node {
	var out []*node
	for _, n := range p.all {
		if n.tag != "" && n.start > lo && n.start < hi && match(n) {
			out = append(out, n)
		}
	}
	return out
}

// headingLevel returns the level of a heading element, 0 for another node.
func headingLevel(n *node) int {
	if len(n.tag) == 2 && n.tag[0] == 'h' && n.tag[1] >= '1' && n.tag[1] <= '6' {
		return int(n.tag[1] - '0')
	}
	return 0
}

// headings returns the headings of the page in its order.
func (p *page) headings() []*node {
	return p.within(p.root.start, p.root.end, func(n *node) bool { return headingLevel(n) > 0 })
}

// headingTexts returns the texts of the headings of the page.
func (p *page) headingTexts() []string {
	var out []string
	for _, h := range p.headings() {
		out = append(out, p.text(h))
	}
	return out
}

// heading returns the first heading of the page with the text, or nil.
func (p *page) heading(text string) *node {
	for _, h := range p.headings() {
		if p.text(h) == text {
			return h
		}
	}
	return nil
}

// part returns the part of the page under the heading: from its end to the
// next heading of the same or a higher level, or to the end of the page.
func (p *page) part(h *node) (lo, hi int) {
	lo, hi = h.end, p.root.end
	for _, g := range p.headings() {
		if g.start > h.end && headingLevel(g) <= headingLevel(h) {
			hi = g.start
			break
		}
	}
	return lo, hi
}

// section returns the part of the page under the heading with the text; the
// test fails when there is no such heading.
func (p *page) section(t *testing.T, text string) (lo, hi int) {
	t.Helper()
	h := p.heading(text)
	if h == nil {
		t.Fatalf("%s has no heading «%s»; its headings: %q", p.name, text, p.headingTexts())
	}
	return p.part(h)
}

// texts returns the texts of the nodes.
func (p *page) texts(ns []*node) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, p.text(n))
	}
	return out
}

// byTag matches the elements of the name.
func byTag(name string) func(*node) bool {
	return func(n *node) bool { return n.tag == name }
}

// byClass matches the elements of the class.
func byClass(c string) func(*node) bool {
	return func(n *node) bool { return n.hasClass(c) }
}

// A line is a line of a window of an example: its text and its kind.
type line struct {
	text, kind string
}

// lineKinds are the classes of the kinds of the lines of a window.
var lineKinds = []string{"cmd", "ok", "err", "dim"}

// lines returns the lines of the window: the kind is the class of the kind
// the line has, "" for a plain line, and the classes joined by + when it
// has more than one.
func (p *page) lines(term *node) []line {
	out := []line{}
	for _, n := range p.within(term.start, term.end, byClass("line")) {
		var kinds []string
		for _, k := range lineKinds {
			if n.hasClass(k) {
				kinds = append(kinds, k)
			}
		}
		out = append(out, line{text: n.rawText(), kind: strings.Join(kinds, "+")})
	}
	return out
}
