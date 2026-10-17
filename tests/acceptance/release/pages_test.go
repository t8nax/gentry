package release_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The journal of changes of the shop of the cases of the pages: the
// sections of 0.4.0, 0.3.0 and 0.2.1.
const (
	shop040Phrase  = "Корзина сохраняется после входа, а скидки задаются в одном месте."
	shop040Break   = "Скидки старого формата больше не применяются. Задайте их заново в разделе «Акции»."
	shop040Changed = "Корзина теперь сохраняется после входа в аккаунт."
	shop040Fixed   = "Исправлен поиск товаров по артикулу с дефисом: теперь он их находит."
	shop030Phrase  = "Добавлена оплата заказа частями."
	shop021Phrase  = "Поиск по артикулу работает с дефисом."

	shop040 = "## 0.4.0 — 2026-11-02\n\n" + shop040Phrase + "\n\n" +
		"### Несовместимые изменения\n* " + shop040Break + "\n\n" +
		"### Новое\n* Добавлена оплата заказа частями.\n\n" +
		"### Изменено\n* " + shop040Changed + "\n\n" +
		"### Исправлено\n* " + shop040Fixed + "\n"
	shop030 = "## 0.3.0 — 2026-10-20\n\n" + shop030Phrase + "\n\n### Новое\n* Добавлена оплата заказа частями.\n"
	shop021 = "## 0.2.1 — 2026-10-15\n\n" + shop021Phrase + "\n\n### Исправлено\n* Поиск по артикулу теперь находит товары с дефисом.\n"
)

// shopJournal is the journal of the shop.
var shopJournal = changelog(shop040, shop030, shop021)

// content040 is the content of the page of 0.4.0 of the example of the
// section 2 of the specification.
const content040 = "# Перед обновлением\n" +
	"\n" +
	"| Было | Стало |\n" +
	"| --- | --- |\n" +
	"| `shop discount add` | `shop promo add` |\n" +
	"| `shop discount list` | `shop promo list` |\n" +
	"\n" +
	"Что сделать: заменить старые команды в своих скриптах.\n" +
	"\n" +
	content040New

// content040New is the section «Новое» of the content of 0.4.0.
const content040New = "# Новое\n" +
	"\n" +
	"## Оплата заказа частями\n" +
	"\n" +
	"Заказ можно оплатить в несколько платежей: магазин сам напомнит\n" +
	"покупателю о следующем.\n" +
	"\n" +
	"### Вкладка: В настройках\n" +
	"\n" +
	"```\n" +
	"~ # shop.yaml\n" +
	"payments:\n" +
	"+   installments: 3\n" +
	"```\n" +
	"\n" +
	"### Вкладка: Оформление заказа\n" +
	"\n" +
	"```\n" +
	"$ shop checkout --installments 3\n" +
	"+ Заказ 1042 оформлен.\n" +
	"Платежей: 3\n" +
	"```\n" +
	"\n" +
	"### Вкладка: Когда частями нельзя\n" +
	"\n" +
	"```\n" +
	"$ shop checkout --installments 3\n" +
	"! Оплата частями недоступна для заказа меньше 3000 ₽.\n" +
	"Сумма заказа: 1200 ₽\n" +
	"```\n" +
	"\n" +
	"## Список желаний\n" +
	"\n" +
	"Товар можно отложить и купить позже.\n" +
	"\n" +
	"### Рядом: Было\n" +
	"\n" +
	"```\n" +
	"$ shop cart add SKU-7\n" +
	"Товар SKU-7 добавлен в корзину.\n" +
	"```\n" +
	"\n" +
	"### Рядом: Стало\n" +
	"\n" +
	"```\n" +
	"$ shop wishlist add SKU-7\n" +
	"+ Товар SKU-7 отложен в список желаний.\n" +
	"```\n"

// The words of the pages.
const (
	badge          = "Требует действий при обновлении"
	playHint       = "Вывод появляется по строкам при открытии вкладки; чтобы повторить, откройте вкладку ещё раз."
	installSh      = "curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh"
	installPs1     = "irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex"
	pageNextStep   = "Затем в новом окне терминала обновить интеграцию с ИИ-инструментом: gentry setup <ИИ-инструмент>"
	listNextStep   = "Затем в новом окне терминала подключить ИИ-инструмент: gentry setup <ИИ-инструмент>"
	siteURL        = "https://t8nax.github.io/gentry/"
	releaseTagURL  = "https://github.com/t8nax/gentry/releases/tag/v"
	changelogAtURL = "https://github.com/t8nax/gentry/blob/v"
)

// shopFiles returns the journal of the shop and the contents of the pages of
// 0.3.0 and 0.4.0, with the files changed by the given ones; an empty text
// removes a file.
func shopFiles(changed map[string]string) map[string]string {
	files := map[string]string{
		changelogFile:           shopJournal,
		contentDir + "0.3.0.md": content,
		contentDir + "0.4.0.md": content040,
	}
	maps.Copy(files, changed)
	for name, text := range files {
		if text == "" {
			delete(files, name)
		}
	}
	return files
}

// shopRepo makes a repository of the shop on main: the files committed and
// tagged v<version> for each of the versions, then a change of the code.
func shopRepo(t *testing.T, files map[string]string, tagged ...string) *repo {
	t.Helper()
	r := newRepo(t)
	all := map[string]string{"cart.go": "package cart\n"}
	maps.Copy(all, files)
	r.commit(all)
	for _, v := range tagged {
		r.tag(v)
	}
	r.commit(map[string]string{"cart.go": "package cart\n\nconst Limit = 10\n"})
	return r
}

// buildSite runs the build of the pages and fails the test when it fails.
func buildSite(t *testing.T, r *repo) string {
	t.Helper()
	dir, res := r.pages()
	if res.code != 0 {
		t.Fatalf("the build of the pages failed: %s", res)
	}
	return dir
}

// shopPage builds the site of the shop with the files changed by the given
// ones and the tags of the versions, and reads the page of the version.
func shopPage(t *testing.T, changed map[string]string, version string, tagged ...string) *page {
	t.Helper()
	dir := buildSite(t, shopRepo(t, shopFiles(changed), tagged...))
	return readPage(t, dir, version+"/index.html")
}

// header returns the text of the header of the page: from its heading h1
// to the next heading.
func header(t *testing.T, p *page) string {
	t.Helper()
	hs := p.headings()
	if len(hs) == 0 || hs[0].tag != "h1" {
		t.Fatalf("%s does not start with a heading h1; its headings: %q", p.name, p.headingTexts())
	}
	hi := p.root.end
	if len(hs) > 1 {
		hi = hs[1].start
	}
	return p.textIn(hs[0].start, hi)
}

// card returns the part of the page under the heading of the card in the
// section «Новое».
func card(t *testing.T, p *page, title string) (lo, hi int) {
	t.Helper()
	slo, shi := p.section(t, "Новое")
	for _, h := range p.within(slo, shi, func(n *node) bool { return headingLevel(n) > 0 }) {
		if p.text(h) == title {
			return p.part(h)
		}
	}
	t.Fatalf("%s has no card «%s» in «Новое»: %q", p.name, title, p.texts(p.within(slo, shi, func(n *node) bool { return headingLevel(n) > 0 })))
	return 0, 0
}

// wantText checks that the text has each of the words.
func wantText(t *testing.T, where, text string, words ...string) {
	t.Helper()
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Errorf("%s has no «%s»:\n%s", where, w, text)
		}
	}
}

// wantNoText checks that the text has none of the words.
func wantNoText(t *testing.T, where, text string, words ...string) {
	t.Helper()
	for _, w := range words {
		if strings.Contains(text, w) {
			t.Errorf("%s has «%s»:\n%s", where, w, text)
		}
	}
}

// TestPagesReleased checks that the build makes the list and a page of each
// tagged version and no other page.
func TestPagesReleased(t *testing.T) {
	r := shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0", "0.4.0")
	dir := buildSite(t, r)
	var got []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".html") {
			rel, _ := filepath.Rel(dir, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{"0.2.1/index.html", "0.3.0/index.html", "0.4.0/index.html", "index.html"}
	if !slices.Equal(got, want) {
		t.Errorf("the pages of the site:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestPagesUntagged checks that a version with its section and its content
// but with no tag has no page and is not in the list.
func TestPagesUntagged(t *testing.T) {
	dir := buildSite(t, shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0"))
	if _, err := os.Stat(filepath.Join(dir, "0.4.0")); !os.IsNotExist(err) {
		t.Errorf("the site has the folder 0.4.0 of the version with no tag: %v", err)
	}
	if p := readPage(t, dir, "index.html"); strings.Contains(p.raw, "0.4.0") {
		t.Errorf("index.html has 0.4.0, the version with no tag:\n%s", p.raw)
	}
}

// TestPageHeader checks the title of the page and its header: the heading,
// the date and the phrase about the main thing.
func TestPageHeader(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	if !strings.Contains(p.raw, "<title>Gentry 0.4.0</title>") {
		t.Errorf("%s has no <title>Gentry 0.4.0</title>:\n%s", p.name, p.raw)
	}
	if hs := p.headings(); len(hs) == 0 || hs[0].tag != "h1" || p.text(hs[0]) != "Gentry 0.4.0" {
		t.Errorf("%s does not start with the heading h1 «Gentry 0.4.0»; its headings: %q", p.name, p.headingTexts())
	}
	wantText(t, "the header of "+p.name, header(t, p), "2 ноября 2026", shop040Phrase)
}

// TestPageDate checks the date of the page in Russian.
func TestPageDate(t *testing.T) {
	p := shopPage(t, nil, "0.2.1", "0.2.1")
	wantText(t, "the header of "+p.name, header(t, p), "15 октября 2026")
}

// TestPageBadge checks the mark of the actions on update in the header of
// a page with breaking changes in the journal.
func TestPageBadge(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	wantText(t, "the header of "+p.name, header(t, p), badge)
}

// TestPageBadgeFromContent checks the block «Перед обновлением» and the mark
// of a page whose journal has no breaking changes and whose content has the
// section «Перед обновлением».
func TestPageBadgeFromContent(t *testing.T) {
	before := "# Перед обновлением\n\n| Было | Стало |\n| --- | --- |\n| `shop cart` | `shop basket` |\n\n"
	p := shopPage(t, map[string]string{contentDir + "0.3.0.md": before + content}, "0.3.0", "0.3.0")
	if p.heading("Перед обновлением") == nil {
		t.Errorf("%s has no heading «Перед обновлением»; its headings: %q", p.name, p.headingTexts())
	}
	wantText(t, "the header of "+p.name, header(t, p), badge)
}

// TestPageNoBadge checks that a page of fixes only has neither the mark nor
// the block «Перед обновлением».
func TestPageNoBadge(t *testing.T) {
	p := shopPage(t, nil, "0.2.1", "0.2.1")
	wantNoText(t, p.name, p.bodyText(), badge, "Перед обновлением")
}

// TestPageBeforeUpdate checks the block «Перед обновлением»: the breaking
// changes of the journal, then the table «Было — Стало», then the text of
// the content.
func TestPageBeforeUpdate(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Перед обновлением")
	tables := p.within(lo, hi, byTag("table"))
	if len(tables) != 1 {
		t.Fatalf("the block «Перед обновлением» of %s has %d tables, want one:\n%s", p.name, len(tables), p.textIn(lo, hi))
	}
	tb := tables[0]
	var rows [][]string
	for _, tr := range p.within(tb.start, tb.end, byTag("tr")) {
		rows = append(rows, p.texts(p.within(tr.start, tr.end, func(n *node) bool { return n.tag == "td" || n.tag == "th" })))
	}
	want := [][]string{
		{"Было", "Стало"},
		{"shop discount add", "shop promo add"},
		{"shop discount list", "shop promo list"},
	}
	if !slices.EqualFunc(rows, want, slices.Equal) {
		t.Errorf("the table of the block «Перед обновлением» of %s has the rows %q, want %q", p.name, rows, want)
	}
	before, after := p.textIn(lo, tb.start), p.textIn(tb.end, hi)
	wantText(t, "the block «Перед обновлением» before the table", before, shop040Break)
	wantText(t, "the block «Перед обновлением» after the table", after, "Что сделать: заменить старые команды в своих скриптах.")
	wantNoText(t, "the block «Перед обновлением» before the table", before, "Что сделать")
}

// TestPageBeforeUpdateJournalOnly checks the block «Перед обновлением» of a
// page whose content has no such section: the breaking changes of the
// journal and no table.
func TestPageBeforeUpdateJournalOnly(t *testing.T) {
	p := shopPage(t, map[string]string{contentDir + "0.4.0.md": content040New}, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Перед обновлением")
	wantText(t, "the block «Перед обновлением» of "+p.name, p.textIn(lo, hi), shop040Break)
	if tables := p.within(lo, hi, byTag("table")); len(tables) != 0 {
		t.Errorf("the block «Перед обновлением» of %s has a table:\n%s", p.name, p.textIn(lo, hi))
	}
}

// TestPageNewCards checks the cards of «Новое»: those of the content in its
// order, with the explanations, and not the entries of the journal.
func TestPageNewCards(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Новое")
	titles := p.texts(p.within(lo, hi, func(n *node) bool { return headingLevel(n) > 0 }))
	if want := []string{"Оплата заказа частями", "Список желаний"}; !slices.Equal(titles, want) {
		t.Errorf("the cards of «Новое» of %s are %q, want %q", p.name, titles, want)
	}
	clo, chi := card(t, p, "Оплата заказа частями")
	wantText(t, "the card «Оплата заказа частями»", p.textIn(clo, chi), "Заказ можно оплатить в несколько платежей: магазин сам напомнит покупателю о следующем.")
	clo, chi = card(t, p, "Список желаний")
	wantText(t, "the card «Список желаний»", p.textIn(clo, chi), "Товар можно отложить и купить позже.")
	wantNoText(t, "«Новое» of "+p.name, p.textIn(lo, hi), "Добавлена оплата заказа частями.")
}

// TestPageJournalLines checks the sections of the entries of the journal:
// «Изменено» and «Исправлено» as lists, no «Безопасность».
func TestPageJournalLines(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	for _, s := range []struct {
		heading string
		entries []string
	}{
		{"Изменено", []string{shop040Changed}},
		{"Исправлено", []string{shop040Fixed}},
	} {
		lo, hi := p.section(t, s.heading)
		if got := p.texts(p.within(lo, hi, byTag("li"))); !slices.Equal(got, s.entries) {
			t.Errorf("the list «%s» of %s is %q, want %q", s.heading, p.name, got, s.entries)
		}
	}
	if p.heading("Безопасность") != nil {
		t.Errorf("%s has the heading «Безопасность», which the journal has not: %q", p.name, p.headingTexts())
	}
}

// TestPageMultilineEntry checks that an entry of the journal on two lines of
// the file is one item, its lines apart by a space.
func TestPageMultilineEntry(t *testing.T) {
	journal := strings.Replace(shopJournal, "* "+shop040Changed+"\n", "* Корзина теперь сохраняется\n  после входа в аккаунт.\n", 1)
	p := shopPage(t, map[string]string{changelogFile: journal}, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Изменено")
	if got, want := p.texts(p.within(lo, hi, byTag("li"))), []string{shop040Changed}; !slices.Equal(got, want) {
		t.Errorf("the list «Изменено» of %s is %q, want %q", p.name, got, want)
	}
}

// TestPageUpdate checks that the page ends with the section «Как обновиться»:
// the commands of installation, the next step and the links.
func TestPageUpdate(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	h := p.heading("Как обновиться")
	if hs := p.headings(); h == nil || hs[len(hs)-1] != h {
		t.Fatalf("the last heading of %s is not «Как обновиться»: %q", p.name, p.headingTexts())
	}
	lo, hi := p.part(h)
	wantText(t, "«Как обновиться» of "+p.name, p.textIn(lo, hi), "Linux и macOS", "Windows", pageNextStep, installSh, installPs1)
	links := map[string]string{}
	for _, a := range p.within(lo, hi, byTag("a")) {
		links[p.text(a)] = a.attrs["href"]
	}
	for text, href := range map[string]string{
		"Все выпуски":      siteURL,
		"Файлы выпуска":    releaseTagURL + "0.4.0",
		"Журнал изменений": changelogAtURL + "0.4.0/CHANGELOG.md",
	} {
		if links[text] != href {
			t.Errorf("the link «%s» of %s leads to %q, want %q; the links: %q", text, p.name, links[text], href, links)
		}
	}
}

// wantSelfContained checks that the page is one file with no scripts and no
// resources outside it: no script, no handlers on…=, no style sheet or
// other file linked, src and url( only of data inside the page.
func wantSelfContained(t *testing.T, p *page) {
	t.Helper()
	if strings.Contains(strings.ToLower(p.raw), "<script") {
		t.Errorf("%s has <script", p.name)
	}
	for _, n := range p.all {
		for name, value := range n.attrs {
			switch {
			case strings.HasPrefix(name, "on"):
				t.Errorf("%s: <%s> has the handler %s=%q", p.name, n.tag, name, value)
			case name == "src" && !strings.HasPrefix(strings.TrimSpace(value), "data:"):
				t.Errorf("%s: <%s> loads %q", p.name, n.tag, value)
			}
		}
		if n.tag == "link" && strings.Contains(strings.ToLower(n.attrs["rel"]), "stylesheet") {
			t.Errorf("%s links the style sheet %q", p.name, n.attrs["href"])
		}
	}
	lower := strings.ToLower(p.raw)
	if strings.Contains(lower, "@import") {
		t.Errorf("%s imports a style sheet: @import", p.name)
	}
	for rest := lower; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			break
		}
		rest = rest[i+len("url("):]
		v := strings.TrimLeft(rest, " \t\r\n\"'")
		if !strings.HasPrefix(v, "data:") {
			end := strings.IndexByte(v, ')')
			if end < 0 {
				end = len(v)
			}
			t.Errorf("%s loads url(%s", p.name, v[:end+min(1, len(v)-end)])
		}
	}
}

// TestPageNoScripts checks that the page of a release is one file with no
// JavaScript and no resources outside it.
func TestPageNoScripts(t *testing.T) {
	wantSelfContained(t, shopPage(t, nil, "0.4.0", "0.4.0"))
}

// TestPageLightOnly checks that the page and the list do not change by the
// theme of the system.
func TestPageLightOnly(t *testing.T) {
	dir := buildSite(t, shopRepo(t, shopFiles(nil), "0.4.0"))
	for _, name := range []string{"0.4.0/index.html", "index.html"} {
		if p := readPage(t, dir, name); strings.Contains(p.raw, "prefers-color-scheme") {
			t.Errorf("%s has prefers-color-scheme", name)
		}
	}
}

// TestPageEscaping checks that the signs of markup in the journal and in an
// example are shown as written and are no markup of the page.
func TestPageEscaping(t *testing.T) {
	journal := strings.Replace(shopJournal, "* "+shop040Fixed+"\n", "* Исправлен вывод `<артикул> & <цена>`.\n", 1)
	example := strings.Replace(content040, "Платежей: 3\n", "Платежей: 3\n<b>Итого</b>\n", 1)
	p := shopPage(t, map[string]string{changelogFile: journal, contentDir + "0.4.0.md": example}, "0.4.0", "0.4.0")
	wantText(t, p.name, p.bodyText(), "<артикул> & <цена>", "<b>Итого</b>")
	for _, s := range []string{"&lt;артикул&gt; &amp; &lt;цена&gt;", "&lt;b&gt;Итого&lt;/b&gt;"} {
		if !strings.Contains(p.raw, s) {
			t.Errorf("the HTML of %s has no %s", p.name, s)
		}
	}
	for _, b := range p.within(p.root.start, p.root.end, byTag("b")) {
		if strings.Contains(p.text(b), "Итого") {
			t.Errorf("%s has the element <b> «%s»", p.name, p.text(b))
		}
	}
}

// TestPageContentEdited checks that the page of a past release is built from
// its content on the head of the repository, not on its tag.
func TestPageContentEdited(t *testing.T) {
	old := "# Новое\n\n## Оплата частями\n\nЗаказ можно оплатить в несколько платежей.\n\n```\n$ shop checkout --installments 3\n```\n"
	r := shopRepo(t, shopFiles(map[string]string{contentDir + "0.3.0.md": old}), "0.3.0")
	r.commit(map[string]string{contentDir + "0.3.0.md": strings.Replace(old, "## Оплата частями", "## Оплата заказа частями", 1)})
	p := readPage(t, buildSite(t, r), "0.3.0/index.html")
	card(t, p, "Оплата заказа частями")
	wantNoText(t, p.name, p.bodyText(), "Оплата частями")
}

// TestPageSingleExample checks that a block of code of a card with no
// subheading is one window with no tabs and no captions.
func TestPageSingleExample(t *testing.T) {
	p := shopPage(t, nil, "0.3.0", "0.3.0")
	lo, hi := card(t, p, "Каталог")
	if terms := p.within(lo, hi, byClass("term")); len(terms) != 1 {
		t.Errorf("the card «Каталог» of %s has %d windows, want one", p.name, len(terms))
	} else if got, want := p.lines(terms[0]), []line{{"$ shop catalog", "cmd"}, {"Товаров: 120", "ok"}}; !slices.Equal(got, want) {
		t.Errorf("the window of the card «Каталог» of %s has the lines %q, want %q", p.name, got, want)
	}
	if n := p.within(lo, hi, func(n *node) bool { return n.hasClass("tab") || n.hasClass("label") || n.tag == "input" }); len(n) != 0 {
		t.Errorf("the card «Каталог» of %s has tabs or captions: %q", p.name, p.texts(n))
	}
}

// TestPageTabs checks the tabs of a card: their names in the order of the
// file, a switch of a form for each, the first open.
func TestPageTabs(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := card(t, p, "Оплата заказа частями")
	if got, want := p.texts(p.within(lo, hi, byClass("tab"))), []string{"В настройках", "Оформление заказа", "Когда частями нельзя"}; !slices.Equal(got, want) {
		t.Errorf("the tabs of the card «Оплата заказа частями» of %s are %q, want %q", p.name, got, want)
	}
	radios := p.within(lo, hi, func(n *node) bool { return n.tag == "input" && strings.EqualFold(n.attrs["type"], "radio") })
	if len(radios) != 3 {
		t.Fatalf("the card «Оплата заказа частями» of %s has %d radio buttons, want one per tab, 3", p.name, len(radios))
	}
	for i, r := range radios {
		_, checked := r.attrs["checked"]
		if checked != (i == 0) {
			t.Errorf("the radio button %d of the card «Оплата заказа частями» of %s: checked is %v, want only the first", i+1, p.name, checked)
		}
		if r.attrs["name"] == "" || r.attrs["name"] != radios[0].attrs["name"] {
			t.Errorf("the radio buttons of the card «Оплата заказа частями» of %s are not one group: name %q and %q", p.name, radios[0].attrs["name"], r.attrs["name"])
		}
	}
	if terms := p.within(lo, hi, byClass("term")); len(terms) != 3 {
		t.Errorf("the card «Оплата заказа частями» of %s has %d windows, want one per tab, 3", p.name, len(terms))
	}
	if strings.Contains(strings.ToLower(p.raw), "<script") {
		t.Errorf("%s has <script", p.name)
	}
}

// TestPagePair checks a pair side by side: two windows with their captions.
func TestPagePair(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := card(t, p, "Список желаний")
	terms := p.within(lo, hi, byClass("term"))
	labels := p.within(lo, hi, byClass("label"))
	if len(terms) != 2 || len(labels) != 2 {
		t.Fatalf("the card «Список желаний» of %s has %d windows and %d captions, want 2 and 2", p.name, len(terms), len(labels))
	}
	if got, want := p.texts(labels), []string{"Было", "Стало"}; !slices.Equal(got, want) {
		t.Errorf("the captions of the card «Список желаний» of %s are %q, want %q", p.name, got, want)
	}
	if !(labels[0].start < terms[0].start && terms[0].start < labels[1].start && labels[1].start < terms[1].start) {
		t.Errorf("the card «Список желаний» of %s does not have each window after its caption", p.name)
	}
	pairs := p.within(lo, hi, byClass("beforeafter"))
	if len(pairs) != 1 || terms[0].start < pairs[0].start || terms[1].end > pairs[0].end {
		t.Errorf("the windows of the card «Список желаний» of %s are not in one element side by side, of the class beforeafter", p.name)
	}
	for i, cmd := range []string{"$ shop cart add SKU-7", "$ shop wishlist add SKU-7"} {
		if ls := p.lines(terms[i]); len(ls) == 0 || ls[0] != (line{cmd, "cmd"}) {
			t.Errorf("the window %d of the card «Список желаний» of %s has the lines %q, want the command %q first", i+1, p.name, ls, cmd)
		}
	}
	if n := p.within(lo, hi, func(n *node) bool { return n.hasClass("tab") || n.tag == "input" }); len(n) != 0 {
		t.Errorf("the card «Список желаний» of %s has tabs: %q", p.name, p.texts(n))
	}
}

// TestPageLineMarks checks the marks of the lines of the examples: the
// command with $, the marked line, the error and the note with no mark.
func TestPageLineMarks(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := card(t, p, "Оплата заказа частями")
	terms := p.within(lo, hi, byClass("term"))
	want := [][]line{
		{{"# shop.yaml", "dim"}, {"payments:", ""}, {"  installments: 3", "ok"}},
		{{"$ shop checkout --installments 3", "cmd"}, {"Заказ 1042 оформлен.", "ok"}, {"Платежей: 3", ""}},
		{{"$ shop checkout --installments 3", "cmd"}, {"Оплата частями недоступна для заказа меньше 3000 ₽.", "err"}, {"Сумма заказа: 1200 ₽", ""}},
	}
	if len(terms) != len(want) {
		t.Fatalf("the card «Оплата заказа частями» of %s has %d windows, want %d", p.name, len(terms), len(want))
	}
	for i, term := range terms {
		if got := p.lines(term); !slices.Equal(got, want[i]) {
			t.Errorf("the window %d of the card «Оплата заказа частями» of %s has the lines %q, want %q", i+1, p.name, got, want[i])
		}
	}
	for _, term := range p.within(p.root.start, p.root.end, byClass("term")) {
		for _, l := range p.lines(term) {
			for _, mark := range []string{"+ ", "! ", "~ "} {
				if strings.HasPrefix(l.text, mark) {
					t.Errorf("%s has a line with the mark %q: %q", p.name, mark, l.text)
				}
			}
		}
	}
}

// TestPagePlay checks that a window of an example with a command shows its
// lines one by one and one without a command shows them at once, and the
// hint under the tabs.
func TestPagePlay(t *testing.T) {
	p := shopPage(t, nil, "0.4.0", "0.4.0")
	lo, hi := card(t, p, "Оплата заказа частями")
	terms := p.within(lo, hi, byClass("term"))
	if len(terms) != 3 {
		t.Fatalf("the card «Оплата заказа частями» of %s has %d windows, want 3", p.name, len(terms))
	}
	for i, play := range []bool{false, true, true} {
		if terms[i].hasClass("play") != play {
			t.Errorf("the window %d of the card «Оплата заказа частями» of %s: shows the lines one by one (class play) is %v, want %v", i+1, p.name, !play, play)
		}
	}
	hints := p.within(lo, hi, func(n *node) bool { return p.text(n) == playHint })
	if len(hints) == 0 || hints[0].start < terms[2].end {
		t.Errorf("the card «Оплата заказа частями» of %s has no hint «%s» under its tabs:\n%s", p.name, playHint, p.textIn(lo, hi))
	}
}

// TestPageMarkup checks the code and the link in an explanation of a card.
func TestPageMarkup(t *testing.T) {
	text := "# Новое\n\n## Акции\n\nКоманда `shop promo add` описана [в справке](https://shop.example/help).\n\n```\n$ shop promo list\n```\n"
	p := shopPage(t, map[string]string{contentDir + "0.3.0.md": text}, "0.3.0", "0.3.0")
	lo, hi := card(t, p, "Акции")
	wantText(t, "the card «Акции» of "+p.name, p.textIn(lo, hi), "Команда shop promo add описана в справке.")
	if codes := p.within(lo, hi, func(n *node) bool { return n.tag == "code" && p.text(n) == "shop promo add" }); len(codes) != 1 {
		t.Errorf("the card «Акции» of %s has %d elements <code> «shop promo add», want one", p.name, len(codes))
	}
	links := p.within(lo, hi, func(n *node) bool { return n.tag == "a" && p.text(n) == "в справке" })
	if len(links) != 1 || links[0].attrs["href"] != "https://shop.example/help" {
		t.Errorf("the card «Акции» of %s has no link «в справке» to https://shop.example/help", p.name)
	}
}

// TestPageFixesOnly checks that a release of fixes only needs no content: the
// build passes and the page is made of the journal.
func TestPageFixesOnly(t *testing.T) {
	p := shopPage(t, nil, "0.2.1", "0.2.1")
	wantText(t, p.name, p.bodyText(), shop021Phrase)
	for _, h := range []string{"Исправлено", "Как обновиться"} {
		if p.heading(h) == nil {
			t.Errorf("%s has no heading «%s»: %q", p.name, h, p.headingTexts())
		}
	}
	if p.heading("Новое") != nil {
		t.Errorf("%s has the heading «Новое»: %q", p.name, p.headingTexts())
	}
}

// TestPagesNoSection checks that the build refuses a tagged version with no
// section in the journal.
func TestPagesNoSection(t *testing.T) {
	r := shopRepo(t, shopFiles(map[string]string{changelogFile: changelog(shop040, shop021), contentDir + "0.3.0.md": ""}), "0.3.0")
	_, res := r.pages()
	wantRefusal(t, res, "В журнале изменений нет раздела этой версии.", "Версия: 0.3.0")
}

// listRows returns the rows of the list of the releases by version: the item
// li with the link <version>/.
func listRows(t *testing.T, p *page, versions ...string) map[string]*node {
	t.Helper()
	rows := map[string]*node{}
	for _, v := range versions {
		links := p.within(p.root.start, p.root.end, func(n *node) bool { return n.tag == "a" && n.attrs["href"] == v+"/" })
		if len(links) != 1 {
			t.Fatalf("%s has %d links to %s/, want one", p.name, len(links), v)
		}
		if got := p.text(links[0]); got != v {
			t.Errorf("the link to %s/ of %s is «%s», want «%s»", v, p.name, got, v)
		}
		row := links[0].parent
		for row != nil && row.tag != "li" {
			row = row.parent
		}
		if row == nil {
			t.Fatalf("the link to %s/ of %s is in no item li of the list", v, p.name)
		}
		rows[v] = row
	}
	return rows
}

// TestReleasesList checks the list of the releases: its title, the section of
// installation over the list, the rows of the versions, new first.
func TestReleasesList(t *testing.T) {
	dir := buildSite(t, shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0", "0.4.0"))
	p := readPage(t, dir, "index.html")
	if !strings.Contains(p.raw, "<title>Выпуски Gentry</title>") {
		t.Errorf("index.html has no <title>Выпуски Gentry</title>")
	}
	if hs := p.headings(); len(hs) == 0 || hs[0].tag != "h1" || p.text(hs[0]) != "Выпуски Gentry" {
		t.Errorf("index.html does not start with the heading h1 «Выпуски Gentry»: %q", p.headingTexts())
	}
	if p.heading("Как установить") == nil {
		t.Errorf("index.html has no heading «Как установить»: %q", p.headingTexts())
	}
	rows := listRows(t, p, "0.4.0", "0.3.0", "0.2.1")
	if !(rows["0.4.0"].start < rows["0.3.0"].start && rows["0.3.0"].start < rows["0.2.1"].start) {
		t.Errorf("the rows of index.html are not in the order 0.4.0, 0.3.0, 0.2.1")
	}
	first := min(rows["0.4.0"].start, rows["0.3.0"].start, rows["0.2.1"].start)
	wantText(t, "index.html over the list", p.textIn(p.root.start, first), "Как установить", "Linux и macOS", "Windows", installSh, installPs1, listNextStep)
	for _, r := range []struct {
		version, date, phrase string
		badge                 bool
	}{
		{"0.4.0", "2 ноября 2026", shop040Phrase, true},
		{"0.3.0", "20 октября 2026", shop030Phrase, false},
		{"0.2.1", "15 октября 2026", shop021Phrase, false},
	} {
		text := p.text(rows[r.version])
		wantText(t, "the row "+r.version+" of index.html", text, r.date, r.phrase)
		if strings.Contains(text, badge) != r.badge {
			t.Errorf("the row %s of index.html: has «%s» is %v, want %v:\n%s", r.version, badge, !r.badge, r.badge, text)
		}
	}
}

// TestReleasesListOrder checks that the list orders the versions by number,
// not as text.
func TestReleasesListOrder(t *testing.T) {
	files := map[string]string{changelogFile: changelog(
		fixes("0.10.0", "2026-11-20", "Корзина считает доставку.", "Исправлен расчёт доставки."),
		fixes("0.9.0", "2026-11-10", "Каталог открывается быстрее.", "Исправлена сортировка каталога."),
	)}
	p := readPage(t, buildSite(t, shopRepo(t, files, "0.9.0", "0.10.0")), "index.html")
	rows := listRows(t, p, "0.10.0", "0.9.0")
	if rows["0.10.0"].start > rows["0.9.0"].start {
		t.Errorf("the row 0.10.0 of index.html is under 0.9.0")
	}
}

// TestReleasesListNoScripts checks that the list is one file with no
// JavaScript and no resources outside it.
func TestReleasesListNoScripts(t *testing.T) {
	dir := buildSite(t, shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0", "0.4.0"))
	wantSelfContained(t, readPage(t, dir, "index.html"))
}

// TestPagesNotMain checks that the button Publish pages refuses to publish
// from a branch other than main and names the branch.
func TestPagesNotMain(t *testing.T) {
	r := shopRepo(t, shopFiles(nil), "0.2.1", "0.3.0", "0.4.0")
	r.branch("feat/cart")
	_, res := r.pages()
	wantRefusal(t, res, "Страницы выпусков публикуются только из main.", "Ветка: feat/cart")
}

// TestPagesNoReleases checks that the button Publish pages refuses when there
// is no release yet.
func TestPagesNoReleases(t *testing.T) {
	r := shopRepo(t, shopFiles(nil))
	_, res := r.pages()
	wantRefusal(t, res, "Выпусков ещё нет.")
}
