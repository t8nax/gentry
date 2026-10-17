package main

import (
	"regexp"
	"strings"
)

// The content of the page of a release is the file releases/<version>.md
// of the repository: markdown with two sections of the first level, both
// optional. «Перед обновлением» holds the table «Было | Стало» and text;
// «Новое» holds the cards, each a heading of the second level, an
// explanation and examples: a block of code alone is one window, blocks
// under subheadings «Вкладка: <name>» in a row are tabs, two under
// «Рядом: <caption>» in a row are a pair side by side. The names of the
// sections and of the subheadings are the format of the file, as the names
// of the subsections of the journal are.
const (
	contentDir    = "releases"
	sectionBefore = "Перед обновлением"
	sectionNew    = "Новое"
	subTab        = "Вкладка: "
	subSide       = "Рядом: "
	columnBefore  = "Было"
	columnAfter   = "Стало"
	fence         = "```"
)

// contentFile returns the file of the content of the page of the version,
// apart by / from the root of the repository.
func contentFile(v version) string { return contentDir + "/" + v.String() + ".md" }

// A content is the content of the page of a release, read.
type content struct {
	before []block // the section «Перед обновлением»; nil when there is none
	hasNew bool    // the file has the section «Новое»
	cards  []card
}

// A card is a card of «Новое»: its heading and its blocks, the explanation
// first.
type card struct {
	title  string
	blocks []block
}

// A blockKind is the kind of a block of a text of a page.
type blockKind int

const (
	paragraphBlock blockKind = iota
	listBlock
	tableBlock
	exampleBlock // one window
	tabsBlock    // windows under tabs
	pairBlock    // two windows side by side
)

// A block is a part of a text of a page.
type block struct {
	kind blockKind
	// text is the text of a paragraph, its lines joined by a space.
	text string
	// items are the items of a list, each its lines joined by a space.
	items []string
	// rows are the rows of a table, the heading first, of their cells.
	rows [][]string
	// windows are the examples of an example, tabs or a pair: each the
	// lines of a block of code as written.
	windows [][]string
	// names are the names of the tabs or the captions of a pair.
	names []string
}

// isText reports whether the block is text, not an example.
func (b block) isText() bool {
	return b.kind == paragraphBlock || b.kind == listBlock || b.kind == tableBlock
}

// A contentReason is what is wrong in a content.
type contentReason int

const (
	reasonUnknownSection    contentReason = iota + 1 // a section of the first level other than the two
	reasonRepeated                                   // a section twice
	reasonOutside                                    // text outside a section or a card
	reasonCardOutside                                // a heading of the second level outside «Новое»
	reasonNoExplanation                              // a card with no text before its first example
	reasonUnknownSubheading                          // a subheading other than the two
	reasonNoExample                                  // a subheading with no block of code after it
	reasonNoPair                                     // «Рядом» not in a pair
	reasonUnclosed                                   // a block of code not closed
	reasonTableColumns                               // a table whose columns are not «Было» and «Стало»
	reasonNoNew                                      // no cards of «Новое», though the journal has new
)

// The refusals of the content of a page. main.go turns them into words.
type (
	// contentMissingError: the version needs the content of its page, and
	// the repository has no file of it.
	contentMissingError struct {
		version version
		file    string
	}
	// contentInvalidError: the content of the page is written wrong.
	contentInvalidError struct {
		file    string
		line    int // of the file, from 1; 0 when the error is of no line
		reason  contentReason
		subject string // the heading or the subheading the reason names
	}
)

func (e *contentMissingError) Error() string {
	return "the release " + e.version.String() + " has no content of its page " + e.file
}

func (e *contentInvalidError) Error() string {
	return "the content of the page " + e.file + " is written wrong"
}

// An itemKind is the kind of an item of a content.
type itemKind int

const (
	blankItem    itemKind = iota
	textItem              // a line of text
	headingItem           // a heading of a level
	codeItem              // a block of code
	unclosedItem          // the start of a block of code with no end
)

// An item is a line of a content, or a block of code, read.
type item struct {
	kind  itemKind
	line  int    // of the file, from 1: of the line, or of the start of the block
	text  string // of a line of text as written, or of a heading
	level int    // of a heading
	code  []string
}

// headingPattern is a heading of markdown: one to six # and a space.
var headingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.*)$`)

// readItems reads the lines of the text into items. A block of code that
// is not closed is the last item.
func readItems(text string) []item {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var items []item
	for i := 0; i < len(lines); i++ {
		l, n := lines[i], i+1
		switch m := headingPattern.FindStringSubmatch(l); {
		case strings.HasPrefix(l, fence):
			end := -1
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimRight(lines[j], " \t") == fence {
					end = j
					break
				}
			}
			if end < 0 {
				return append(items, item{kind: unclosedItem, line: n})
			}
			items = append(items, item{kind: codeItem, line: n, code: lines[i+1 : end]})
			i = end
		case m != nil:
			items = append(items, item{kind: headingItem, line: n, level: len(m[1]), text: strings.TrimSpace(m[2])})
		case strings.TrimSpace(l) == "":
			items = append(items, item{kind: blankItem, line: n})
		default:
			items = append(items, item{kind: textItem, line: n, text: l})
		}
	}
	return items
}

// parseContent reads the content of the page in the file. The first error
// in the order of the file is returned.
func parseContent(file, text string) (*content, error) {
	c := &content{}
	invalid := func(line int, reason contentReason, subject string) error {
		return &contentInvalidError{file: file, line: line, reason: reason, subject: subject}
	}
	// The container is the section «Перед обновлением» or a card: its items
	// are read into blocks when it ends.
	type container struct {
		card  bool
		title string
		line  int
		items []item
	}
	var open *container
	closeOpen := func() error {
		if open == nil {
			return nil
		}
		blocks, err := parseBody(open.items, invalid)
		if err != nil {
			return err
		}
		if open.card {
			if len(blocks) == 0 || !blocks[0].isText() {
				return invalid(open.line, reasonNoExplanation, open.title)
			}
			c.cards = append(c.cards, card{title: open.title, blocks: blocks})
		} else {
			c.before = blocks
			if c.before == nil {
				c.before = []block{}
			}
		}
		open = nil
		return nil
	}
	section := ""
	seen := map[string]bool{}
	for _, it := range readItems(text) {
		if it.kind == headingItem && it.level <= 2 {
			// A card must have its explanation before anything else: it is
			// told at the heading of the card, before the errors of its
			// body that come later in the file.
			if err := closeOpen(); err != nil {
				return nil, err
			}
		}
		switch {
		case it.kind == headingItem && it.level == 1:
			if it.text != sectionBefore && it.text != sectionNew {
				return nil, invalid(it.line, reasonUnknownSection, it.text)
			}
			if seen[it.text] {
				return nil, invalid(it.line, reasonRepeated, it.text)
			}
			seen[it.text], section = true, it.text
			if section == sectionBefore {
				open = &container{}
			} else {
				c.hasNew = true
			}
		case it.kind == headingItem && it.level == 2:
			if section != sectionNew {
				return nil, invalid(it.line, reasonCardOutside, "")
			}
			open = &container{card: true, title: it.text, line: it.line}
		case it.kind == blankItem:
			if open != nil {
				open.items = append(open.items, it)
			}
		case open == nil:
			return nil, invalid(it.line, reasonOutside, "")
		case open.card && onlyBlank(open.items):
			// The first item of a card after its heading is its
			// explanation.
			if it.kind != textItem {
				return nil, invalid(open.line, reasonNoExplanation, open.title)
			}
			open.items = append(open.items, it)
		default:
			open.items = append(open.items, it)
		}
	}
	if err := closeOpen(); err != nil {
		return nil, err
	}
	return c, nil
}

// onlyBlank reports whether the items are all blank.
func onlyBlank(items []item) bool {
	for _, it := range items {
		if it.kind != blankItem {
			return false
		}
	}
	return true
}

// A subheading is a subheading of an example: «Вкладка: <name>» or
// «Рядом: <caption>».
type subheading struct {
	side bool   // «Рядом», not «Вкладка»
	name string // the name of the tab or the caption
}

// readSubheading reads the text of a subheading; ok is false when it is
// neither of the two.
func readSubheading(text string) (s subheading, ok bool) {
	if name, found := strings.CutPrefix(text, subTab); found && strings.TrimSpace(name) != "" {
		return subheading{name: strings.TrimSpace(name)}, true
	}
	if name, found := strings.CutPrefix(text, subSide); found && strings.TrimSpace(name) != "" {
		return subheading{side: true, name: strings.TrimSpace(name)}, true
	}
	return subheading{}, false
}

// parseBody reads the items of a section or of a card into blocks: the
// lines of text into paragraphs, lists and tables, a block of code alone
// into an example, blocks under subheadings into tabs and pairs.
func parseBody(items []item, invalid func(int, contentReason, string) error) ([]block, error) {
	var blocks []block
	// next returns the index of the next item that is not blank, from i.
	next := func(i int) int {
		for i < len(items) && items[i].kind == blankItem {
			i++
		}
		return i
	}
	// example reads the subheading at i and its block of code, and returns
	// the subheading and the index after the block.
	example := func(i int) (subheading, []string, int, error) {
		h := items[i]
		s, ok := readSubheading(h.text)
		if !ok || h.level != 3 {
			return s, nil, 0, invalid(h.line, reasonUnknownSubheading, h.text)
		}
		j := next(i + 1)
		switch {
		case j < len(items) && items[j].kind == unclosedItem:
			return s, nil, 0, invalid(items[j].line, reasonUnclosed, "")
		case j >= len(items) || items[j].kind != codeItem:
			return s, nil, 0, invalid(h.line, reasonNoExample, h.text)
		}
		return s, items[j].code, j + 1, nil
	}
	for i := 0; i < len(items); {
		it := items[i]
		switch it.kind {
		case blankItem, textItem:
			end := i
			for end < len(items) && (items[end].kind == blankItem || items[end].kind == textItem) {
				end++
			}
			text, err := textBlocks(items[i:end], invalid)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, text...)
			i = end
		case codeItem:
			blocks = append(blocks, block{kind: exampleBlock, windows: [][]string{it.code}})
			i++
		case unclosedItem:
			return nil, invalid(it.line, reasonUnclosed, "")
		case headingItem:
			s, code, after, err := example(i)
			if err != nil {
				return nil, err
			}
			b := block{kind: tabsBlock, names: []string{s.name}, windows: [][]string{code}}
			if s.side {
				b.kind = pairBlock
			}
			last := it
			for i = after; ; {
				j := next(i)
				if j >= len(items) || items[j].kind != headingItem {
					break
				}
				t, ok := readSubheading(items[j].text)
				if ok && t.side != s.side {
					break // another kind of examples starts
				}
				t, code, after, err := example(j)
				if err != nil {
					return nil, err
				}
				b.names, b.windows = append(b.names, t.name), append(b.windows, code)
				last, i = items[j], after
			}
			if b.kind == pairBlock {
				if len(b.windows)%2 != 0 {
					return nil, invalid(last.line, reasonNoPair, last.text)
				}
				for k := 0; k < len(b.windows); k += 2 {
					blocks = append(blocks, block{kind: pairBlock, names: b.names[k : k+2], windows: b.windows[k : k+2]})
				}
				continue
			}
			blocks = append(blocks, b)
		}
	}
	return blocks, nil
}

// tableSeparator is a cell of the line under the heading of a table.
var tableSeparator = regexp.MustCompile(`^:?-+:?$`)

// textBlocks reads lines of text and blank lines into paragraphs, lists
// and tables. A line that starts with «* » is an item of a list, also right
// after a line of a paragraph; a line that is not, after an item with no
// blank line between them, goes on with the item. A line that starts with
// | is a row of a table: the first row is the heading, «Было | Стало».
func textBlocks(items []item, invalid func(int, contentReason, string) error) ([]block, error) {
	var (
		blocks []block
		para   []string
		list   []string
		open   bool // the last item of the list goes on with the next line
		table  [][]string
	)
	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, block{kind: paragraphBlock, text: strings.Join(para, " ")})
			para = nil
		}
	}
	flushList := func() {
		if len(list) > 0 {
			blocks = append(blocks, block{kind: listBlock, items: list})
			list, open = nil, false
		}
	}
	flushTable := func() {
		if len(table) > 0 {
			blocks = append(blocks, block{kind: tableBlock, rows: table})
			table = nil
		}
	}
	for _, it := range items {
		l := strings.TrimSpace(it.text)
		switch {
		case it.kind == blankItem:
			flushPara()
			flushTable()
			open = false
		case strings.HasPrefix(l, "|"):
			flushPara()
			flushList()
			cells := tableCells(l)
			switch {
			case len(table) == 0:
				if len(cells) != 2 || cells[0] != columnBefore || cells[1] != columnAfter {
					return nil, invalid(it.line, reasonTableColumns, "")
				}
				table = append(table, cells)
			case len(table) == 1 && separatorRow(cells):
			default:
				table = append(table, cells)
			}
		case strings.HasPrefix(it.text, "* "):
			flushPara()
			flushTable()
			list = append(list, strings.TrimSpace(it.text[2:]))
			open = true
		case open:
			list[len(list)-1] += " " + l
		default:
			flushTable()
			flushList()
			para = append(para, l)
		}
	}
	flushPara()
	flushList()
	flushTable()
	return blocks, nil
}

// tableCells returns the cells of a row of a table, | apart, with no | at
// the ends; \| is | in a cell.
func tableCells(row string) []string {
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	var cells []string
	var b strings.Builder
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row) && row[i+1] == '|':
			b.WriteByte('|')
			i++
		case row[i] == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(row[i])
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// separatorRow reports whether the cells are the line under the heading of
// a table: dashes, with colons for the alignment.
func separatorRow(cells []string) bool {
	for _, c := range cells {
		if !tableSeparator.MatchString(c) {
			return false
		}
	}
	return true
}
