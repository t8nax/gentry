package release_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/t8nax/gentry/tests/testkit"
)

// The message about a release to the group in Telegram, which release
// notice <version> prints, and the refusals of the text for the group of
// the content of its page, «Сообщение в группу».

// The lines of the message to the group that do not depend on the text for
// the group.
const (
	noticeMark    = "⚠️ " + badge
	noticeDetails = "Подробности — на странице выпуска:"
)

// notice runs the build of the message to the group of the version.
func (r *repo) notice(version string) result {
	r.t.Helper()
	return release(r.t, r.dir, "notice", version)
}

// shopNotice is a text for the group of 0.4.0 with its subsections out of
// the order of the journal, an item on two lines, code and signs of HTML.
const shopNotice = "# Сообщение в группу\n" +
	"\n" +
	"## Изменено\n" +
	"* Корзина сохраняется после входа.\n" +
	"\n" +
	"## Несовместимые изменения\n" +
	"* Скидки старого формата нужно задать заново\n" +
	"  командой `shop promo add <акция>`.\n" +
	"\n" +
	"## Новое\n" +
	"* Оплата заказа частями & напоминания о платежах.\n" +
	"* Список желаний.\n" +
	"\n" +
	"## Исправлено\n" +
	"* Поиск находит товары по артикулу с дефисом.\n"

// TestNotice checks the message to the group of a release of the shop: the
// title with the version, the phrase about the main thing, the mark of the
// actions on the update, the subsections in the order of the journal with
// their items on one line each, the signs of HTML escaped and code, and the
// address of the page; the checks of the button pass the text.
func TestNotice(t *testing.T) {
	r := shopRepo(t, shopFiles(map[string]string{contentDir + "0.4.0.md": content040 + "\n" + shopNotice}), "0.3.0")
	want := testkit.Lines(
		"<b>Gentry 0.4.0</b>",
		shop040Phrase,
		noticeMark,
		"",
		"<b>Несовместимые изменения</b>",
		"• Скидки старого формата нужно задать заново командой <code>shop promo add &lt;акция&gt;</code>.",
		"",
		"<b>Новое</b>",
		"• Оплата заказа частями &amp; напоминания о платежах.",
		"• Список желаний.",
		"",
		"<b>Изменено</b>",
		"• Корзина сохраняется после входа.",
		"",
		"<b>Исправлено</b>",
		"• Поиск находит товары по артикулу с дефисом.",
		"",
		noticeDetails,
		"https://t8nax.github.io/gentry/0.4.0/",
	)
	if res := r.notice("0.4.0"); res.code != 0 || res.stdout != want || res.stderr != "" {
		t.Errorf("%s\nwant exit code 0 and the message:\n%s", res, want)
	}
	if res := r.check("0.4.0"); res.code != 0 {
		t.Errorf("the checks of the button refused a right text for the group: %s", res)
	}
}

// TestNoticeFixesOnly checks the message of a release with only fixes and
// no phrase: no line of the phrase and no mark.
func TestNoticeFixesOnly(t *testing.T) {
	fixes040 := "## 0.4.0 — 2026-11-02\n\n### Исправлено\n* " + shop040Fixed + "\n"
	r := shopRepo(t, shopFiles(map[string]string{
		changelogFile:           changelog(fixes040, shop030, shop021),
		contentDir + "0.4.0.md": "# Сообщение в группу\n\n## Исправлено\n* Поиск находит товары с дефисом.\n",
	}), "0.3.0")
	want := testkit.Lines("<b>Gentry 0.4.0</b>", "", "<b>Исправлено</b>", "• Поиск находит товары с дефисом.", "", noticeDetails, "https://t8nax.github.io/gentry/0.4.0/")
	if res := r.notice("0.4.0"); res.code != 0 || res.stdout != want {
		t.Errorf("%s\nwant exit code 0 and the message:\n%s", res, want)
	}
}

// TestNoticeMissing checks that the button refuses a release with only
// fixes and no content of its page, and that the build of the message
// refuses it alike.
func TestNoticeMissing(t *testing.T) {
	fixes040 := "## 0.4.0 — 2026-11-02\n\n" + shop040Phrase + "\n\n### Исправлено\n* " + shop040Fixed + "\n"
	r, res := checkShop(t, map[string]string{changelogFile: changelog(fixes040, shop030, shop021), contentDir + "0.4.0.md": ""})
	want := []string{noContent, "Версия: 0.4.0", "Файл: releases/0.4.0.md"}
	wantRefusal(t, res, want...)
	r.wantTags("v0.3.0")
	wantRefusal(t, r.notice("0.4.0"), want...)
}

// TestNoticeInvalid checks that the button refuses a text for the group
// written wrong or not matching the section of the version in the journal,
// and names the file, the line when the error is of one, and the error.
func TestNoticeInvalid(t *testing.T) {
	page := content040 + "\n"
	for _, c := range []struct {
		name  string
		text  string
		line  int // of the error; 0 when it is of no line
		at    string
		error []string
	}{
		{
			name:  "no_section",
			text:  content040,
			error: []string{"Ошибка: нет раздела «Сообщение в группу»"},
		},
		{
			name: "subsection_not_in_journal",
			text: page + notice040 + "\n## Безопасность\n* Пароли хранятся надёжнее.\n",
			line: 73, at: "## Безопасность",
			error: []string{"Ошибка: подраздела «Безопасность» нет в журнале изменений этой версии"},
		},
		{
			name:  "subsection_of_journal_missing",
			text:  page + strings.TrimSuffix(notice040, "\n## Исправлено\n* Поиск находит товары по артикулу с дефисом.\n"),
			error: []string{"Ошибка: нет подраздела «Исправлено», который есть в журнале изменений этой версии"},
		},
		{
			name: "subsection_without_items",
			text: page + strings.TrimSuffix(notice040, "* Поиск находит товары по артикулу с дефисом.\n"),
			line: 70, at: "## Исправлено",
			error: []string{"Ошибка: в подразделе «Исправлено» нет пунктов"},
		},
		{
			name: "text_outside_item",
			text: page + notice040 + "\nПоиск стал быстрее.\n",
			line: 73, at: "Поиск стал быстрее.",
			error: []string{"Ошибка: текст вне пункта: пункт начинается с «* », а его продолжение — с отступа"},
		},
		{
			name: "unknown_subsection",
			text: page + notice040 + "\n## Акции\n* Скидки задаются в одном месте.\n",
			line: 73, at: "## Акции",
			error: []string{
				"Ошибка: неизвестный подраздел «Акции»", "Допустимые подразделы:",
				"  Несовместимые изменения", "  Новое", "  Изменено", "  Исправлено", "  Безопасность",
			},
		},
		{
			name: "repeated_subsection",
			text: page + notice040 + "\n## Новое\n* Список желаний.\n",
			line: 73, at: "## Новое",
			error: []string{"Ошибка: подраздел «Новое» повторяется"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := []string{badContent, "Файл: releases/0.4.0.md"}
			if c.line > 0 {
				wantLine(t, c.text, c.line, c.at)
				want = append(want, "Строка: "+strconv.Itoa(c.line))
			}
			r, res := checkShop(t, map[string]string{contentDir + "0.4.0.md": c.text})
			wantRefusal(t, res, append(want, c.error...)...)
			r.wantTags("v0.3.0")
		})
	}
}

// TestNoticeTooLong checks that the button refuses a text for the group
// whose message is longer than Telegram takes, 4096 signs of the visible
// text, and names its length.
func TestNoticeTooLong(t *testing.T) {
	var items, shown []string
	for i := 1; i <= 80; i++ {
		item := "Поиск находит товары по артикулу с дефисом, случай " + strconv.Itoa(i) + "."
		items = append(items, "* "+item)
		shown = append(shown, "• "+item)
	}
	text := strings.Replace(content040+"\n"+notice040, "* Поиск находит товары по артикулу с дефисом.\n", strings.Join(items, "\n")+"\n", 1)
	// The visible text of the message: its lines with no tags.
	visible := strings.Join(append([]string{
		"Gentry 0.4.0", shop040Phrase, noticeMark,
		"", "Несовместимые изменения", "• Скидки старого формата нужно задать заново в разделе «Акции».",
		"", "Новое", "• Оплата заказа частями.",
		"", "Изменено", "• Корзина сохраняется после входа.",
		"", "Исправлено",
	}, append(shown, "", noticeDetails, "https://t8nax.github.io/gentry/0.4.0/")...), "\n")
	n := len(utf16.Encode([]rune(visible)))
	if n <= 4096 {
		t.Fatalf("the stand is wrong: the message has %d signs, not more than 4096", n)
	}
	r, res := checkShop(t, map[string]string{contentDir + "0.4.0.md": text})
	wantRefusal(t, res, badContent, "Файл: releases/0.4.0.md",
		"Ошибка: сообщение в группу длиннее допустимого: "+strconv.Itoa(n)+" знаков при пределе 4096")
	r.wantTags("v0.3.0")
}

// TestNoticeNotOnPage checks that the site of the pages builds with past
// versions with no text for the group, 0.2.1 with no content and 0.3.0 with
// a content of «Новое» only, and that the page of 0.4.0 does not show its
// text for the group.
func TestNoticeNotOnPage(t *testing.T) {
	dir := buildSite(t, shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0", "0.4.0"))
	b, err := os.ReadFile(filepath.Join(dir, "0.4.0", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	wantNoText(t, "the page of 0.4.0", string(b), "Сообщение в группу", "Скидки старого формата нужно задать заново")
}
