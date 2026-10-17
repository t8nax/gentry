package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// TestRussianDate states the date of a page: the day with no leading zero,
// the month in the genitive and the year, for each month; a date that is no
// date is shown as it is.
func TestRussianDate(t *testing.T) {
	for _, tc := range []struct{ date, want string }{
		{"2026-01-05", "5 января 2026"},
		{"2026-02-05", "5 февраля 2026"},
		{"2026-03-05", "5 марта 2026"},
		{"2026-04-05", "5 апреля 2026"},
		{"2026-05-05", "5 мая 2026"},
		{"2026-06-05", "5 июня 2026"},
		{"2026-07-05", "5 июля 2026"},
		{"2026-08-05", "5 августа 2026"},
		{"2026-09-05", "5 сентября 2026"},
		{"2026-10-15", "15 октября 2026"},
		{"2026-11-30", "30 ноября 2026"},
		{"2027-12-31", "31 декабря 2027"},
		{"2026-13-45", "2026-13-45"},
	} {
		if got := russianDate(tc.date); got != tc.want {
			t.Errorf("russianDate(%q) = %q, want %q", tc.date, got, tc.want)
		}
	}
}

// TestWindow checks the lines of a window of an example: the marks of the
// kinds taken off, the command with its mark, the spaces kept; a window
// with a command plays.
func TestWindow(t *testing.T) {
	var n styleNeeds
	w := n.window([]string{"$ shop checkout", "+ Заказ оформлен.", "! Ошибка.", "~ # shop.yaml", "+   installments: 3", "  обычная", "", "+", "$"}, true)
	want := []lineView{
		{"line cmd", "$ shop checkout"}, {"line ok", "Заказ оформлен."}, {"line err", "Ошибка."},
		{"line dim", "# shop.yaml"}, {"line ok", "  installments: 3"}, {"line", "  обычная"}, {"line", ""},
		{"line", "+"}, {"line", "$"},
	}
	if !reflect.DeepEqual(w.Lines, want) || w.Class != "term solo play" || n.lines != 9 {
		t.Errorf("window: %q, %+v; lines %d", w.Class, w.Lines, n.lines)
	}
	if w := n.window([]string{"payments:", "+ a"}, false); w.Class != "term" || w.play || n.lines != 9 {
		t.Errorf("a window with no command: %q, plays %v; lines %d", w.Class, w.play, n.lines)
	}
}

// TestStyle checks the rules of the styles of a page for its tabs and the
// delays of the lines of its windows that play.
func TestStyle(t *testing.T) {
	css := string(styleNeeds{tabs: 4, lines: 3}.style())
	for _, rule := range []string{
		".example > input:nth-of-type(4):checked ~ .panels > .panel:nth-child(4) { display: block; }",
		".example > input:nth-of-type(4):checked ~ .tabs > .tab:nth-child(4) {",
		".term.play .line:nth-child(1) { animation-delay: 0.1s; }",
		".term.play .line:nth-child(2) { animation-delay: 0.7s; }",
		".term.play .line:nth-child(3) { animation-delay: 1.1s; }",
		"@media (prefers-reduced-motion: reduce)",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("the styles have no %q", rule)
		}
	}
	for _, rule := range []string{"nth-of-type(5)", "nth-child(4) { animation-delay", "prefers-color-scheme"} {
		if strings.Contains(css, rule) {
			t.Errorf("the styles have %q", rule)
		}
	}
}

// TestRenderPage checks a page with each kind of block: the hint only under
// tabs with a window that plays, and one group of radio buttons per group
// of tabs.
func TestRenderPage(t *testing.T) {
	journal := "## 0.4.0 — 2026-11-02\n\nКорзина `сохраняется`.\n\n### Несовместимые изменения\n* Скидки — заново.\n\n### Новое\n* Оплата частями.\n\n### Безопасность\n* Пароль скрыт.\n"
	s, ok := readSection(journal, version{0, 4, 0})
	if !ok {
		t.Fatal("no section")
	}
	c, err := parseContent("releases/0.4.0.md", lines(
		"# Новое",
		"## Оплата",
		"Пояснение.",
		"### Вкладка: А", "```", "a", "```",
		"### Вкладка: Б", "```", "b", "```",
		"Текст.",
		"### Вкладка: В", "```", "$ c", "```",
		"### Рядом: Было", "```", "$ d", "```",
		"### Рядом: Стало", "```", "e", "```",
		"```", "f", "```",
	))
	if err != nil {
		t.Fatal(err)
	}
	b, err := renderPage(release{version: version{0, 4, 0}, section: s, content: c})
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	hint := msg.Text(msg.ReleasePageHint)
	for _, tc := range []struct {
		what string
		n    int
	}{
		{hint, 1},
		{`name="ex1"`, 2},
		{`name="ex2"`, 1},
		{`<div class="term play">`, 1},
		{`<div class="term solo play">`, 1},
		{`<div class="term solo">`, 4}, // e, f and the two commands of installation
		{`<div class="beforeafter">`, 1},
		{"<h2>" + msg.Text(msg.ReleasePageBefore) + "</h2>", 1},
		{"<p>Скидки — заново.</p>", 1},
		{"<h2>" + msg.Text(msg.ReleasePageSecurity) + "</h2>", 1},
		{"<h2>" + msg.Text(msg.ReleasePageChanged) + "</h2>", 0},
		{`<p class="lead">Корзина <code>сохраняется</code>.</p>`, 1},
		{`<span class="badge">`, 1},
	} {
		if got := strings.Count(page, tc.what); got != tc.n {
			t.Errorf("the page has %q %d times, want %d", tc.what, got, tc.n)
		}
	}
	if i, j := strings.Index(page, hint), strings.Index(page, "$ c"); i < j {
		t.Errorf("the hint is not under the tabs with the window that plays")
	}
}

// TestRenderList checks the rows of the list of the releases in the order
// given, the link of each version and the mark of the actions on update.
func TestRenderList(t *testing.T) {
	plain := journalSection{date: "2026-10-15", phrase: "Поиск работает.", entries: map[string][]string{journalFixed: {"a"}}}
	breaking := journalSection{date: "2026-11-02", phrase: "Скидки иначе.", entries: map[string][]string{journalBreaking: {"b"}}}
	b, err := renderList([]release{{version: version{0, 10, 0}, section: breaking}, {version: version{0, 9, 0}, section: plain}})
	if err != nil {
		t.Fatal(err)
	}
	list := string(b)
	i, j := strings.Index(list, `<a class="version" href="0.10.0/">0.10.0</a>`), strings.Index(list, `<a class="version" href="0.9.0/">0.9.0</a>`)
	if i < 0 || j < 0 || i > j {
		t.Errorf("the rows of the list are not 0.10.0, then 0.9.0:\n%s", list)
	}
	if n := strings.Count(list, msg.Text(msg.ReleasePageBadge)); n != 1 {
		t.Errorf("the list has the mark %d times, want once:\n%s", n, list)
	}
}
