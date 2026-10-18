package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// shopRelease returns the release 0.4.0 of the shop: its section in the
// journal has the phrase and the entries of the subsections, its content the
// text for the group of the subsections.
func shopRelease(phrase string, entries map[string][]string, notice ...noticeSub) release {
	return release{
		version: version{0, 4, 0},
		section: journalSection{date: "2026-11-02", phrase: phrase, entries: entries},
		content: &content{notice: append([]noticeSub{}, notice...)},
	}
}

// TestNoticeMessage checks the message to the group: the title, the phrase
// when there is one, the mark when the page has the block «Перед
// обновлением», the subsections in the order of the journal, not of the
// file, the items with their signs escaped and code, and the address of the
// page.
func TestNoticeMessage(t *testing.T) {
	title := "<b>" + msg.Text(msg.ReleasePageTitle, "0.4.0") + "</b>"
	mark := noticeMark + msg.Text(msg.ReleasePageBadge)
	tail := []string{"", msg.Text(msg.ReleaseNoticeDetails), "https://t8nax.github.io/gentry/0.4.0/"}
	for _, tc := range []struct {
		name string
		r    release
		want []string
	}{
		{
			name: "breaking, new and fixed",
			r: shopRelease("Корзина сохраняется <после> входа & оплаты.",
				map[string][]string{journalBreaking: {"a"}, journalNew: {"b"}, journalFixed: {"c"}},
				noticeSub{title: journalFixed, items: []string{"Поиск по `SKU-<номер>` работает."}},
				noticeSub{title: journalNew, items: []string{"Список желаний.", "Оплата частями: `a & b`, ` без пары."}},
				noticeSub{title: journalBreaking, items: []string{"Скидки задаются заново."}},
			),
			want: []string{
				title,
				"Корзина сохраняется &lt;после&gt; входа &amp; оплаты.",
				mark,
				"",
				"<b>Несовместимые изменения</b>",
				"• Скидки задаются заново.",
				"",
				"<b>Новое</b>",
				"• Список желаний.",
				"• Оплата частями: <code>a &amp; b</code>, ` без пары.",
				"",
				"<b>Исправлено</b>",
				"• Поиск по <code>SKU-&lt;номер&gt;</code> работает.",
			},
		},
		{
			name: "only fixes with no phrase",
			r: shopRelease("", map[string][]string{journalFixed: {"c"}},
				noticeSub{title: journalFixed, items: []string{"Поиск работает."}}),
			want: []string{title, "", "<b>Исправлено</b>", "• Поиск работает."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Join(append(tc.want, tail...), "\n") + "\n"
			if got := noticeMessage(tc.r); got != want {
				t.Errorf("noticeMessage:\n%s\nwant:\n%s", got, want)
			}
		})
	}
	// The mark of a content with the section «Перед обновлением» and no
	// breaking changes in the journal, as the page has it.
	r := shopRelease("Корзина.", map[string][]string{journalChanged: {"c"}}, noticeSub{title: journalChanged, items: []string{"Корзина."}})
	r.content.before = []block{}
	if got := noticeMessage(r); !strings.Contains(got, "\n"+mark+"\n") {
		t.Errorf("noticeMessage of a content with «Перед обновлением» has no mark:\n%s", got)
	}
}

// TestRenderPageNoNotice checks that the page of a release does not show the
// text for the group.
func TestRenderPageNoNotice(t *testing.T) {
	r := shopRelease("Корзина.", map[string][]string{journalFixed: {"Исправлен поиск."}},
		noticeSub{title: journalFixed, items: []string{"Поиск находит товары с дефисом."}})
	b, err := renderPage(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Поиск находит товары с дефисом.") || strings.Contains(string(b), sectionNotice) {
		t.Errorf("the page shows the text for the group:\n%s", b)
	}
}

// TestCheckNotice checks the text for the group against the section of the
// journal: each error, the first in the order of the file, and a right text
// with a subsection of the journal with no entries left out.
func TestCheckNotice(t *testing.T) {
	entries := map[string][]string{journalNew: {"a"}, journalFixed: {"b"}, journalSecurity: {}}
	newSub := noticeSub{title: journalNew, line: 3, items: []string{"Список желаний."}}
	fixedSub := noticeSub{title: journalFixed, line: 6, items: []string{"Поиск."}}
	for _, tc := range []struct {
		name string
		r    release
		want *contentInvalidError
	}{
		{"right", shopRelease("", entries, newSub, fixedSub), nil},
		{"subsection of an empty one of the journal", shopRelease("", entries, newSub, fixedSub,
			noticeSub{title: journalSecurity, line: 9, items: []string{"Пароли."}}),
			&contentInvalidError{line: 9, reason: reasonNoticeExtra, subject: journalSecurity}},
		{"no section", release{section: journalSection{entries: entries}, content: &content{}},
			&contentInvalidError{reason: reasonNoNotice}},
		{"subsection the journal has not", shopRelease("", entries, newSub,
			noticeSub{title: journalChanged, line: 5, items: []string{"Корзина."}}, fixedSub),
			&contentInvalidError{line: 5, reason: reasonNoticeExtra, subject: journalChanged}},
		{"subsection with no items", shopRelease("", entries, noticeSub{title: journalNew, line: 3}, fixedSub),
			&contentInvalidError{line: 3, reason: reasonNoticeEmpty, subject: journalNew}},
		{"no subsection of one of the journal", shopRelease("", entries, newSub),
			&contentInvalidError{reason: reasonNoticeMissing, subject: journalFixed}},
		{"the first error in the file", shopRelease("", entries, noticeSub{title: journalNew, line: 3},
			noticeSub{title: journalChanged, line: 5, items: []string{"Корзина."}}),
			&contentInvalidError{line: 3, reason: reasonNoticeEmpty, subject: journalNew}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNotice("releases/0.4.0.md", tc.r)
			if tc.want == nil {
				if err != nil {
					t.Errorf("checkNotice: %v, want no error", err)
				}
				return
			}
			var e *contentInvalidError
			tc.want.file = "releases/0.4.0.md"
			if !errors.As(err, &e) || *e != *tc.want {
				t.Errorf("checkNotice: %+v, want %+v", err, *tc.want)
			}
		})
	}
}

// TestTelegramInline checks the HTML of Telegram of a line: the signs
// escaped, code, a link by the rules of the page with its address escaped
// and its text only escaped, and what starts no markup as written.
func TestTelegramInline(t *testing.T) {
	for _, tc := range []struct{ s, want string }{
		{"Цена < 100 & > 10", "Цена &lt; 100 &amp; &gt; 10"},
		{"Команда `shop add <SKU>`.", "Команда <code>shop add &lt;SKU&gt;</code>."},
		{"` без пары", "` без пары"},
		{"См. [раздел «Оплата»](https://shop.example/pay?a=1&b=\"2\").", `См. <a href="https://shop.example/pay?a=1&amp;b=&quot;2&quot;">раздел «Оплата»</a>.`},
		{"[`shop` <x>](http://shop.example)", `<a href="http://shop.example">` + "`shop` &lt;x&gt;</a>"},
		{"[файл](ftp://shop.example) и [пробел](https://shop.example/a b)", "[файл](ftp://shop.example) и [пробел](https://shop.example/a b)"},
		{"[без адреса] и `код` [x](https://a.example)", `[без адреса] и <code>код</code> <a href="https://a.example">x</a>`},
	} {
		if got := telegramInline(tc.s); got != tc.want {
			t.Errorf("telegramInline(%q) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

// TestNoticeLength checks the length of a message as Telegram counts it: no
// tags, an entity one sign, signs out of the plane of UTF-16 two units, no
// blanks at the ends.
func TestNoticeLength(t *testing.T) {
	for _, tc := range []struct {
		html string
		want int
	}{
		{"<b>Магазин</b>\n", 7},
		{"<code>&lt;SKU&gt; &amp; цена</code>", 12},
		{`См. <a href="https://shop.example/?a=1&amp;b=2">оплату</a>`, 10},
		{"⚠️ Скидка", 9},
		{"🛒 корзина", 10},
		{"\n a\nb \n", 3},
		{"", 0},
	} {
		if got := noticeLength(tc.html); got != tc.want {
			t.Errorf("noticeLength(%q) = %d, want %d", tc.html, got, tc.want)
		}
	}
}
