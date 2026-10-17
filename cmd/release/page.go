package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/msg"
)

// The addresses of the site of the pages of the releases on GitHub Pages and
// of the repository of Gentry on GitHub.
const (
	siteURL    = "https://t8nax.github.io/gentry/"
	repoURL    = "https://github.com/t8nax/gentry"
	installURL = repoURL + "/releases/latest/download/"
)

// pageURL returns the address of the page of the release of the version.
func pageURL(v version) string { return siteURL + v.String() + "/" }

//go:embed page.html
var pageTemplates string

//go:embed page.css
var pageStyle string

// templates are the templates of the page of a release, "page", and of the
// list of the releases, "list".
var templates = template.Must(template.New("").Parse(pageTemplates))

// A release is a released version on the site: its section in the journal
// and the content of its page, nil when it has none.
type release struct {
	version version
	section journalSection
	content *content
}

// hasBefore reports whether the page has the block «Перед обновлением»:
// the journal has breaking changes or the content has the section.
func (r release) hasBefore() bool {
	return r.section.has(journalBreaking) || r.content != nil && r.content.before != nil
}

// The words of the pages, from the catalog of messages.
type words struct {
	Badge, Before, New, Update                string
	Unix, Windows, InstallSh, InstallPs1      string
	NextStep, ListNextStep                    template.HTML
	All, Files, Changelog, ListTitle, Install string
}

func pageWords() words {
	return words{
		Badge:        msg.Text(msg.ReleasePageBadge),
		Before:       msg.Text(msg.ReleasePageBefore),
		New:          msg.Text(msg.ReleasePageNew),
		Update:       msg.Text(msg.ReleasePageUpdate),
		Unix:         msg.Text(msg.ReleasePageUnix),
		Windows:      msg.Text(msg.ReleasePageWindows),
		InstallSh:    "curl -fsSL " + installURL + "install.sh | sh",
		InstallPs1:   "irm " + installURL + "install.ps1 | iex",
		NextStep:     inline(msg.Text(msg.ReleasePageNextStep)),
		ListNextStep: inline(msg.Text(msg.ReleaseListNextStep)),
		All:          msg.Text(msg.ReleasePageAll),
		Files:        msg.Text(msg.ReleasePageFiles),
		Changelog:    msg.Text(msg.ReleasePageChangelog),
		ListTitle:    msg.Text(msg.ReleaseListTitle),
		Install:      msg.Text(msg.ReleaseListInstall),
	}
}

// months are the months in the genitive, as a date names them.
var months = [12]msg.Key{
	msg.ReleaseMonthJanuary, msg.ReleaseMonthFebruary, msg.ReleaseMonthMarch,
	msg.ReleaseMonthApril, msg.ReleaseMonthMay, msg.ReleaseMonthJune,
	msg.ReleaseMonthJuly, msg.ReleaseMonthAugust, msg.ReleaseMonthSeptember,
	msg.ReleaseMonthOctober, msg.ReleaseMonthNovember, msg.ReleaseMonthDecember,
}

// russianDate returns the date YYYY-MM-DD in Russian: «15 октября 2026»; a
// date that is no date is returned as it is.
func russianDate(date string) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return msg.Text(msg.ReleasePageDate, d.Day(), msg.Text(months[d.Month()-1]), d.Year())
}

// The model of a page for the templates.
type (
	pageView struct {
		Title, Date                    string
		Style                          template.CSS
		Phrase                         template.HTML
		Before                         *beforeView
		Cards                          []cardView
		Lists                          []listView
		Words                          words
		AllURL, FilesURL, ChangelogURL string
	}
	beforeView struct {
		Entries []template.HTML
		Blocks  []blockView
	}
	cardView struct {
		Title  template.HTML
		Blocks []blockView
	}
	listView struct {
		Heading string
		Items   []template.HTML
	}
	blockView struct {
		Kind  string // p, ul, table, term, tabs, pair
		HTML  template.HTML
		Items []template.HTML
		Head  []template.HTML
		Rows  [][]template.HTML
		// Windows are the windows of an example, of tabs or of a pair.
		Windows []windowView
		Group   string // the name of the radio buttons of tabs
		Hint    string // the hint under tabs with a window that plays
	}
	windowView struct {
		Class string // term and its kinds
		Lines []lineView
		Name  string // the name of a tab or the caption of a pair
		ID    string // the radio button of a tab
		First bool
		play  bool
	}
	lineView struct {
		Class string // line and its kind
		Text  string
	}
)

// A styleNeeds is what the styles of a page depend on: the most tabs of a
// group and the most lines of a window that plays.
type styleNeeds struct {
	tabs, lines int
}

// style returns the styles of a page: the sheet and the rules for its tabs
// and the delays of the lines of its windows that play.
func (n styleNeeds) style() template.CSS {
	var b strings.Builder
	b.WriteString(pageStyle)
	for k := 1; k <= n.tabs; k++ {
		fmt.Fprintf(&b, ".example > input:nth-of-type(%d):checked ~ .panels > .panel:nth-child(%d) { display: block; }\n", k, k)
		fmt.Fprintf(&b, ".example > input:nth-of-type(%d):checked ~ .tabs > .tab:nth-child(%d) { background: var(--term-bg); color: var(--term-text); border-color: var(--term-bg); }\n", k, k)
	}
	for k := 1; k <= n.lines; k++ {
		fmt.Fprintf(&b, ".term.play .line:nth-child(%d) { animation-delay: %ss; }\n", k, lineDelay(k))
	}
	return template.CSS(b.String())
}

// lineDelay returns the delay of the line k of a window that plays, from 1,
// in seconds: the command first, the output a moment after it, then a line
// at a time.
func lineDelay(k int) string {
	ms := 100
	if k > 1 {
		ms = 700 + 400*(k-2)
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
}

// The marks of the lines of an example and the classes of their kinds.
var lineMarks = []struct{ mark, class string }{
	{"+ ", "ok"}, {"! ", "err"}, {"~ ", "dim"},
}

// commandMark starts a line of a command, which is shown with it.
const commandMark = "$ "

// window returns the window of the lines of an example and notes what its
// styles need. A window with a line of a command plays.
func (n *styleNeeds) window(lines []string, solo bool) windowView {
	w := windowView{Lines: []lineView{}}
	for _, l := range lines {
		lv := lineView{Class: "line", Text: l}
		if strings.HasPrefix(l, commandMark) {
			lv.Class += " cmd"
			w.play = true
		} else {
			for _, m := range lineMarks {
				if text, ok := strings.CutPrefix(l, m.mark); ok {
					lv.Class, lv.Text = lv.Class+" "+m.class, text
					break
				}
			}
		}
		w.Lines = append(w.Lines, lv)
	}
	w.Class = "term"
	if solo {
		w.Class += " solo"
	}
	if w.play {
		w.Class += " play"
		n.lines = max(n.lines, len(lines))
	}
	return w
}

// blocks returns the model of the blocks; group numbers the groups of tabs
// of the page.
func (n *styleNeeds) blocks(bs []block, group *int) []blockView {
	var out []blockView
	for _, b := range bs {
		v := blockView{}
		switch b.kind {
		case paragraphBlock:
			v.Kind, v.HTML = "p", inline(b.text)
		case listBlock:
			v.Kind = "ul"
			for _, it := range b.items {
				v.Items = append(v.Items, inline(it))
			}
		case tableBlock:
			v.Kind = "table"
			for i, row := range b.rows {
				var cells []template.HTML
				for _, c := range row {
					cells = append(cells, inline(c))
				}
				if i == 0 {
					v.Head = cells
				} else {
					v.Rows = append(v.Rows, cells)
				}
			}
		case exampleBlock:
			v.Kind, v.Windows = "term", []windowView{n.window(b.windows[0], true)}
		case tabsBlock:
			*group++
			v.Kind, v.Group = "tabs", "ex"+strconv.Itoa(*group)
			n.tabs = max(n.tabs, len(b.windows))
			for i, lines := range b.windows {
				w := n.window(lines, false)
				w.Name, w.ID, w.First = b.names[i], v.Group+"-"+strconv.Itoa(i+1), i == 0
				if w.play {
					v.Hint = msg.Text(msg.ReleasePageHint)
				}
				v.Windows = append(v.Windows, w)
			}
		case pairBlock:
			v.Kind = "pair"
			for i, lines := range b.windows {
				w := n.window(lines, true)
				w.Name = b.names[i]
				v.Windows = append(v.Windows, w)
			}
		}
		out = append(out, v)
	}
	return out
}

// entries returns the HTML of the entries of the journal.
func entries(es []string) []template.HTML {
	var out []template.HTML
	for _, e := range es {
		out = append(out, inline(e))
	}
	return out
}

// renderPage returns the page of the release.
func renderPage(r release) ([]byte, error) {
	var needs styleNeeds
	group := 0
	ver := r.version.String()
	v := pageView{
		Title:        msg.Text(msg.ReleasePageTitle, ver),
		Date:         russianDate(r.section.date),
		Phrase:       inline(r.section.phrase),
		Words:        pageWords(),
		AllURL:       siteURL,
		FilesURL:     repoURL + "/releases/tag/" + tagPrefix + ver,
		ChangelogURL: repoURL + "/blob/" + tagPrefix + ver + "/" + changelogFile,
	}
	if r.hasBefore() {
		v.Before = &beforeView{Entries: entries(r.section.entries[journalBreaking])}
		if r.content != nil {
			v.Before.Blocks = needs.blocks(r.content.before, &group)
		}
	}
	if r.content != nil {
		for _, c := range r.content.cards {
			v.Cards = append(v.Cards, cardView{Title: inline(c.title), Blocks: needs.blocks(c.blocks, &group)})
		}
	}
	for _, s := range []struct {
		sub     string
		heading msg.Key
	}{
		{journalChanged, msg.ReleasePageChanged},
		{journalFixed, msg.ReleasePageFixed},
		{journalSecurity, msg.ReleasePageSecurity},
	} {
		if es := r.section.entries[s.sub]; len(es) > 0 {
			v.Lists = append(v.Lists, listView{Heading: msg.Text(s.heading), Items: entries(es)})
		}
	}
	v.Style = needs.style()
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "page", v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// The model of the list of the releases for the templates.
type (
	listPageView struct {
		Title    string
		Style    template.CSS
		Words    words
		Releases []releaseRow
	}
	releaseRow struct {
		Version, Href, Date string
		Phrase              template.HTML
		Badge               bool
	}
)

// renderList returns the list of the releases, in the order given: new
// first.
func renderList(rs []release) ([]byte, error) {
	v := listPageView{
		Title: msg.Text(msg.ReleaseListTitle),
		Style: styleNeeds{}.style(),
		Words: pageWords(),
	}
	for _, r := range rs {
		v.Releases = append(v.Releases, releaseRow{
			Version: r.version.String(),
			Href:    r.version.String() + "/",
			Date:    russianDate(r.section.date),
			Phrase:  inline(r.section.phrase),
			Badge:   r.hasBefore(),
		})
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "list", v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
