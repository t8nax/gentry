package release_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file holds the tests of the cases the specification got on the
// implementation: the gaps it closed.

// TestPageBreakingParagraphs checks that the block «Перед обновлением» starts
// with a paragraph for each breaking change of the journal, in its order,
// then the table.
func TestPageBreakingParagraphs(t *testing.T) {
	second := "Команда `shop discount` удалена. Используйте `shop promo`."
	journal := strings.Replace(shopJournal, "* "+shop040Break+"\n", "* "+shop040Break+"\n* "+second+"\n", 1)
	p := shopPage(t, map[string]string{changelogFile: journal}, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Перед обновлением")
	tables := p.within(lo, hi, byTag("table"))
	if len(tables) != 1 {
		t.Fatalf("the block «Перед обновлением» of %s has %d tables, want one", p.name, len(tables))
	}
	want := []string{shop040Break, "Команда shop discount удалена. Используйте shop promo."}
	if got := p.texts(p.within(lo, tables[0].start, byTag("p"))); !slices.Equal(got, want) {
		t.Errorf("the paragraphs <p> of «Перед обновлением» of %s before the table are %q, want %q", p.name, got, want)
	}
	if got := p.textIn(lo, tables[0].start); got != strings.Join(want, " ") {
		t.Errorf("the block «Перед обновлением» of %s before the table is %q, want only %q", p.name, got, strings.Join(want, " "))
	}
}

// TestPageEmptyBeforeUpdate checks that an empty section «Перед обновлением»
// of the content shows the block and the mark.
func TestPageEmptyBeforeUpdate(t *testing.T) {
	p := shopPage(t, map[string]string{contentDir + "0.3.0.md": "# Перед обновлением\n\n" + content}, "0.3.0", "0.3.0")
	if p.heading("Перед обновлением") == nil {
		t.Errorf("%s has no heading «Перед обновлением»; its headings: %q", p.name, p.headingTexts())
	}
	wantText(t, "the header of "+p.name, header(t, p), badge)
}

// TestPageBeforeUpdateExample checks an example in the block «Перед
// обновлением».
func TestPageBeforeUpdateExample(t *testing.T) {
	table := "| `shop discount list` | `shop promo list` |\n\n"
	text := strings.Replace(content040, table, table+"```\n$ shop promo add SPRING\n+ Акция SPRING создана.\n```\n\n", 1)
	p := shopPage(t, map[string]string{contentDir + "0.4.0.md": text}, "0.4.0", "0.4.0")
	lo, hi := p.section(t, "Перед обновлением")
	terms := p.within(lo, hi, byClass("term"))
	if len(terms) != 1 {
		t.Fatalf("the block «Перед обновлением» of %s has %d windows, want one", p.name, len(terms))
	}
	if got, want := p.lines(terms[0]), []line{{"$ shop promo add SPRING", "cmd"}, {"Акция SPRING создана.", "ok"}}; !slices.Equal(got, want) {
		t.Errorf("the window of «Перед обновлением» of %s has the lines %q, want %q", p.name, got, want)
	}
}

// TestPageUnknownJournalSection checks that a subsection of the journal other
// than the five of its format is not shown and does not stop the build.
func TestPageUnknownJournalSection(t *testing.T) {
	section021 := "## 0.2.1 — 2026-10-15\n\n" + shop021Phrase + "\n\n" +
		"### Исправлено\n* Поиск по артикулу теперь находит товары с дефисом.\n\n" +
		"### Прочее\n* Обновлены зависимости.\n"
	p := shopPage(t, map[string]string{changelogFile: changelog(shop040, shop030, section021)}, "0.2.1", "0.2.1")
	if p.heading("Исправлено") == nil {
		t.Errorf("%s has no heading «Исправлено»: %q", p.name, p.headingTexts())
	}
	if p.heading("Прочее") != nil {
		t.Errorf("%s has the heading «Прочее»: %q", p.name, p.headingTexts())
	}
	wantNoText(t, p.name, p.bodyText(), "Обновлены зависимости.")
}

// TestPageTwoPairs checks that four subheadings «Рядом» in a row are two
// pairs side by side.
func TestPageTwoPairs(t *testing.T) {
	text := content040 + "\n### Рядом: Было в корзине\n\n```\n$ shop cart list\n```\n\n### Рядом: Стало в корзине\n\n```\n$ shop wishlist list\n```\n"
	p := shopPage(t, map[string]string{contentDir + "0.4.0.md": text}, "0.4.0", "0.4.0")
	lo, hi := card(t, p, "Список желаний")
	pairs := p.within(lo, hi, byClass("beforeafter"))
	if len(pairs) != 2 {
		t.Fatalf("the card «Список желаний» of %s has %d pairs side by side (beforeafter), want 2", p.name, len(pairs))
	}
	for i, want := range [][]string{{"Было", "Стало"}, {"Было в корзине", "Стало в корзине"}} {
		labels := p.within(pairs[i].start, pairs[i].end, byClass("label"))
		terms := p.within(pairs[i].start, pairs[i].end, byClass("term"))
		if got := p.texts(labels); !slices.Equal(got, want) || len(terms) != 2 {
			t.Errorf("the pair %d of the card «Список желаний» of %s has the captions %q and %d windows, want %q and 2", i+1, p.name, got, len(terms), want)
		}
	}
}

// TestPageSinglePlay checks that a single example with a command shows its
// lines one by one, with no hint of the tabs.
func TestPageSinglePlay(t *testing.T) {
	text := "# Новое\n\n## Заказы\n\nСписок заказов покупателя.\n\n```\n$ shop orders\nЗаказов: 2\n```\n"
	p := shopPage(t, map[string]string{contentDir + "0.3.0.md": text}, "0.3.0", "0.3.0")
	lo, hi := card(t, p, "Заказы")
	terms := p.within(lo, hi, byClass("term"))
	if len(terms) != 1 || !terms[0].hasClass("play") {
		t.Errorf("the card «Заказы» of %s has %d windows, want one shown line by line (class play)", p.name, len(terms))
	}
	wantNoText(t, "the card «Заказы» of "+p.name, p.textIn(lo, hi), playHint)
}

// TestPageLinkNotHTTP checks that a link to an address other than http or
// https is no link and is shown as written.
func TestPageLinkNotHTTP(t *testing.T) {
	text := "# Новое\n\n## Справка\n\nСмотрите [справку](ftp://shop.example/help).\n\n```\n$ shop help\n```\n"
	p := shopPage(t, map[string]string{contentDir + "0.3.0.md": text}, "0.3.0", "0.3.0")
	lo, hi := card(t, p, "Справка")
	for _, a := range p.within(p.root.start, p.root.end, byTag("a")) {
		if strings.Contains(a.attrs["href"], "ftp://shop.example/help") {
			t.Errorf("%s has a link to %q", p.name, a.attrs["href"])
		}
	}
	wantText(t, "the card «Справка» of "+p.name, p.textIn(lo, hi), "Смотрите [справку](ftp://shop.example/help).")
}

// TestContentInvalidPlace checks the refusals of a content with a text, a
// card, an example or a subheading where it may not be.
func TestContentInvalidPlace(t *testing.T) {
	before := strings.Split(strings.TrimSuffix(strings.TrimSuffix(content040, content040New), "\n"), "\n")
	allowedSubheadings := []string{"Допустимые подзаголовки:", "  Вкладка: <название>", "  Рядом: <подпись>"}
	side := func(caption, cmd string) []string {
		return []string{"### Рядом: " + caption, "", "```", "$ " + cmd, "```"}
	}
	for _, c := range []struct {
		name  string
		lines []string
		line  int    // the line of the error
		at    string // the line of the file there
		error []string
	}{
		{
			name:  "text_outside_card",
			lines: cat(before, []string{"# Новое", "", "Карточки ниже описывают новое.", ""}, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line:  12, at: "Карточки ниже описывают новое.",
			error: []string{"Ошибка: текст вне карточки"},
		},
		{
			name:  "card_before_section",
			lines: cat(cardLines("Оплата заказа частями", "shop checkout --installments 3", 1), []string{"", "# Новое", ""}, cardLines("Список желаний", "shop wishlist add SKU-7", 0)),
			line:  1, at: "## Оплата заказа частями",
			error: []string{"Ошибка: карточка вне раздела «Новое»"},
		},
		{
			name:  "example_before_section",
			lines: cat([]string{"```", "$ shop orders", "```", "", "# Новое", ""}, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line:  1, at: "```",
			error: []string{"Ошибка: текст вне раздела"},
		},
		{
			name: "side_three",
			lines: cat([]string{"# Новое", ""},
				cardLines("Оплата заказа частями", "shop checkout --installments 3", 3), []string{""},
				cardLines("Акции", "shop promo add", 2), []string{""},
				cardLines("Доставка", "shop delivery list", 2), []string{""},
				[]string{"## Список желаний", "", "Товар можно отложить и купить позже.", ""},
				side("Было", "shop cart add SKU-7"), []string{""},
				side("Стало", "shop wishlist add SKU-7"), []string{""},
				side("Итог", "shop wishlist list")),
			line: 50, at: "### Рядом: Итог",
			error: []string{"Ошибка: у подзаголовка «Рядом: Итог» нет пары"},
		},
		{
			name: "fourth_level_subheading",
			lines: cat([]string{"# Новое", "", "## Оплата заказа частями", ""}, explanation, []string{
				"",
				"#### Вкладка: Оформление",
				"",
				"```",
				"$ shop checkout --installments 3",
				"```",
			}),
			line: 14, at: "#### Вкладка: Оформление",
			error: append([]string{"Ошибка: неизвестный подзаголовок «Вкладка: Оформление»"}, allowedSubheadings...),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := file(c.lines...)
			wantLine(t, text, c.line, c.at)
			r, res := checkShop(t, map[string]string{contentDir + "0.4.0.md": text})
			want := append([]string{badContent, "Файл: releases/0.4.0.md", "Строка: " + strconv.Itoa(c.line)}, c.error...)
			wantRefusal(t, res, want...)
			r.wantTags("v0.3.0")
		})
	}
}
