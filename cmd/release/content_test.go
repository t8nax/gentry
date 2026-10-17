package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// lines joins the lines of a file.
func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// TestParseContent checks the reading of a right content: the sections, the
// cards in the order of the file, the explanation, the examples alone, in
// tabs and in pairs.
func TestParseContent(t *testing.T) {
	text := lines(
		"# Перед обновлением",
		"",
		"| Было | Стало |",
		"| --- | :---: |",
		"| `shop discount add` | `shop promo add` |",
		"| a \\| b | c |",
		"Что сделать:",
		"* заменить команды;",
		"* задать акции",
		"  заново.",
		"",
		"```",
		"$ shop promo list",
		"```",
		"",
		"# Новое",
		"",
		"## Оплата заказа частями",
		"",
		"Заказ можно оплатить",
		"частями.",
		"",
		"### Вкладка: В настройках",
		"",
		"```yaml",
		"payments:",
		"+   installments: 3",
		"```",
		"### Вкладка: Оформление заказа",
		"```",
		"$ shop checkout --installments 3",
		"",
		"```",
		"",
		"### Рядом: Было",
		"```",
		"$ shop cart add SKU-7",
		"```",
		"### Рядом: Стало",
		"```",
		"$ shop wishlist add SKU-7",
		"```",
		"### Рядом: До",
		"```",
		"a",
		"```",
		"### Рядом: После",
		"```",
		"b",
		"```",
		"",
		"Ещё абзац.",
		"",
		"## Акции",
		"",
		"* Скидка задаётся один раз.",
		"",
		"```",
		"$ shop promo add",
		"```",
	)
	got, err := parseContent("releases/0.4.0.md", text)
	if err != nil {
		t.Fatal(err)
	}
	want := &content{
		before: []block{
			{kind: tableBlock, rows: [][]string{{"Было", "Стало"}, {"`shop discount add`", "`shop promo add`"}, {"a | b", "c"}}},
			{kind: paragraphBlock, text: "Что сделать:"},
			{kind: listBlock, items: []string{"заменить команды;", "задать акции заново."}},
			{kind: exampleBlock, windows: [][]string{{"$ shop promo list"}}},
		},
		hasNew: true,
		cards: []card{
			{title: "Оплата заказа частями", blocks: []block{
				{kind: paragraphBlock, text: "Заказ можно оплатить частями."},
				{kind: tabsBlock, names: []string{"В настройках", "Оформление заказа"}, windows: [][]string{
					{"payments:", "+   installments: 3"},
					{"$ shop checkout --installments 3", ""},
				}},
				{kind: pairBlock, names: []string{"Было", "Стало"}, windows: [][]string{{"$ shop cart add SKU-7"}, {"$ shop wishlist add SKU-7"}}},
				{kind: pairBlock, names: []string{"До", "После"}, windows: [][]string{{"a"}, {"b"}}},
				{kind: paragraphBlock, text: "Ещё абзац."},
			}},
			{title: "Акции", blocks: []block{
				{kind: listBlock, items: []string{"Скидка задаётся один раз."}},
				{kind: exampleBlock, windows: [][]string{{"$ shop promo add"}}},
			}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseContent:\n%+v\nwant:\n%+v", got, want)
	}
}

// TestParseContentSections checks the sections a content may have and lack.
func TestParseContentSections(t *testing.T) {
	for _, tc := range []struct {
		name       string
		text       string
		before     bool
		hasNew     bool
		cards      int
		beforeSize int
	}{
		{"empty file", "", false, false, 0, 0},
		{"only before", lines("# Перед обновлением", "", "Текст."), true, false, 0, 1},
		{"empty before", lines("# Перед обновлением", "", "# Новое"), true, true, 0, 0},
		{"new with no cards", lines("# Новое"), false, true, 0, 0},
		{"lines ended by CRLF", "# Новое\r\n\r\n## Каталог\r\n\r\nТекст.\r\n\r\n```\r\n$ shop catalog\r\n```\r\n", false, true, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := parseContent("releases/0.4.0.md", tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if (c.before != nil) != tc.before || c.hasNew != tc.hasNew || len(c.cards) != tc.cards || len(c.before) != tc.beforeSize {
				t.Errorf("before %v (%d blocks), new %v, %d cards; want %v (%d), %v, %d", c.before != nil, len(c.before), c.hasNew, len(c.cards), tc.before, tc.beforeSize, tc.hasNew, tc.cards)
			}
		})
	}
	c, err := parseContent("releases/0.4.0.md", "# Новое\r\n\r\n## Каталог\r\n\r\nТекст.\r\n\r\n```\r\n$ shop catalog\r\n```\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.cards[0].blocks[1].windows[0]; !reflect.DeepEqual(got, []string{"$ shop catalog"}) {
		t.Errorf("the lines of an example of a file with CRLF: %q", got)
	}
}

// TestParseContentInvalid checks each error of a content: its line, its
// reason and what it names; the first error in the order of the file is
// told.
func TestParseContentInvalid(t *testing.T) {
	card := []string{"## Каталог", "", "Пояснение.", "", "```", "$ shop catalog", "```"}
	newCard := append([]string{"# Новое", ""}, card...)
	for _, tc := range []struct {
		name    string
		text    string
		line    int
		reason  contentReason
		subject string
	}{
		{"unknown section", lines("# Скидки"), 1, reasonUnknownSection, "Скидки"},
		{"section of a heading of no section", lines("#  Новое  "), 0, 0, ""},
		{"repeated section", lines(append(newCard, "", "# Новое")...), 11, reasonRepeated, "Новое"},
		{"repeated before", lines("# Перед обновлением", "# Перед обновлением"), 2, reasonRepeated, "Перед обновлением"},
		{"text before the sections", lines("Текст.", "# Новое"), 1, reasonOutside, ""},
		{"example before the sections", lines("```", "a", "```"), 1, reasonOutside, ""},
		{"subheading before the sections", lines("### Вкладка: А"), 1, reasonOutside, ""},
		{"text in «Новое» before the cards", lines("# Новое", "", "Текст.", "", "## Каталог"), 3, reasonOutsideCard, ""},
		{"example in «Новое» before the cards", lines("# Новое", "```", "a", "```", "## Каталог"), 2, reasonOutsideCard, ""},
		{"table in «Новое» before the cards", lines("# Новое", "| Было | Стало |", "## Каталог"), 2, reasonOutsideCard, ""},
		{"subheading in «Новое» before the cards", lines("# Перед обновлением", "Текст.", "# Новое", "### Вкладка: А"), 4, reasonOutsideCard, ""},
		{"card before the sections", lines("## Каталог"), 1, reasonCardOutside, ""},
		{"card in «Перед обновлением»", lines("# Перед обновлением", "", "## Акции", "", "Текст."), 3, reasonCardOutside, ""},
		{"card with an example first", lines("# Новое", "## Каталог", "", "```", "a", "```"), 2, reasonNoExplanation, "Каталог"},
		{"card with a subheading first", lines("# Новое", "## Каталог", "### Пример"), 2, reasonNoExplanation, "Каталог"},
		{"card with nothing", lines("# Новое", "## Каталог", "", "## Акции", "", "Текст."), 2, reasonNoExplanation, "Каталог"},
		{"card with nothing at the end", lines("# Новое", "## Каталог", ""), 2, reasonNoExplanation, "Каталог"},
		{"card with an unclosed example first", lines("# Новое", "## Каталог", "```", "a"), 2, reasonNoExplanation, "Каталог"},
		{"unknown subheading", lines(append(newCard, "### Пример", "```", "a", "```")...), 10, reasonUnknownSubheading, "Пример"},
		{"tab with no name", lines(append(newCard, "### Вкладка:", "```", "a", "```")...), 10, reasonUnknownSubheading, "Вкладка:"},
		{"heading of the fourth level", lines(append(newCard, "#### Вкладка: А", "```", "a", "```")...), 10, reasonUnknownSubheading, "Вкладка: А"},
		{"subheading before a subheading", lines(append(newCard, "### Вкладка: А", "", "### Вкладка: Б", "```", "a", "```")...), 10, reasonNoExample, "Вкладка: А"},
		{"subheading before text", lines(append(newCard, "### Рядом: А", "Текст.")...), 10, reasonNoExample, "Рядом: А"},
		{"subheading at the end", lines(append(newCard, "### Вкладка: А", "")...), 10, reasonNoExample, "Вкладка: А"},
		{"subheading before the next card", lines(append(newCard, "### Вкладка: А", "## Акции", "Текст.")...), 10, reasonNoExample, "Вкладка: А"},
		{"one side", lines(append(newCard, "### Рядом: Было", "```", "a", "```", "", "Текст.")...), 10, reasonNoPair, "Рядом: Было"},
		{"three sides", lines(append(newCard, "### Рядом: А", "```", "a", "```", "### Рядом: Б", "```", "b", "```", "### Рядом: В", "```", "c", "```")...), 18, reasonNoPair, "Рядом: В"},
		{"side after tabs", lines(append(newCard, "### Вкладка: А", "```", "a", "```", "### Рядом: Б", "```", "b", "```")...), 14, reasonNoPair, "Рядом: Б"},
		{"unclosed example", lines(append(newCard, "", "```", "a")...), 11, reasonUnclosed, ""},
		{"unclosed example of a subheading", lines(append(newCard, "### Вкладка: А", "", "```", "a")...), 12, reasonUnclosed, ""},
		{"unclosed example in «Перед обновлением»", lines("# Перед обновлением", "", "```", "a"), 3, reasonUnclosed, ""},
		{"table of other columns", lines("# Перед обновлением", "", "| Команда | Замена |", "| --- | --- |"), 3, reasonTableColumns, ""},
		{"table of three columns", lines("# Перед обновлением", "| Было | Стало | Ещё |"), 2, reasonTableColumns, ""},
		{"the first error in the file", lines("# Новое", "## Каталог", "### Пример", "# Скидки"), 2, reasonNoExplanation, "Каталог"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseContent("releases/0.4.0.md", tc.text)
			if tc.reason == 0 {
				if err != nil {
					t.Errorf("parseContent: %v, want no error", err)
				}
				return
			}
			var e *contentInvalidError
			if !errors.As(err, &e) {
				t.Fatalf("parseContent: %v, want a content written wrong", err)
			}
			want := contentInvalidError{file: "releases/0.4.0.md", line: tc.line, reason: tc.reason, subject: tc.subject}
			if *e != want {
				t.Errorf("parseContent: %+v, want %+v", *e, want)
			}
		})
	}
}

// TestTextBlocks checks the paragraphs, the lists and the tables of a text.
func TestTextBlocks(t *testing.T) {
	invalid := func(line int, reason contentReason, subject string) error {
		return &contentInvalidError{line: line, reason: reason, subject: subject}
	}
	for _, tc := range []struct {
		name string
		text []string
		want []block
	}{
		{"paragraphs", []string{"Первая", "строка.", "", "Второй."}, []block{
			{kind: paragraphBlock, text: "Первая строка."}, {kind: paragraphBlock, text: "Второй."},
		}},
		{"a list right after a paragraph", []string{"Что сделать:", "* одно;", "* другое."}, []block{
			{kind: paragraphBlock, text: "Что сделать:"}, {kind: listBlock, items: []string{"одно;", "другое."}},
		}},
		{"a list with blank lines", []string{"* одно;", "", "* другое."}, []block{
			{kind: listBlock, items: []string{"одно;", "другое."}},
		}},
		{"an item on two lines", []string{"* одно", "  и то же."}, []block{
			{kind: listBlock, items: []string{"одно и то же."}},
		}},
		{"a paragraph after a list", []string{"* одно.", "", "Абзац."}, []block{
			{kind: listBlock, items: []string{"одно."}}, {kind: paragraphBlock, text: "Абзац."},
		}},
		{"a star in a paragraph", []string{"Цена *со скидкой*.", "*не пункт"}, []block{
			{kind: paragraphBlock, text: "Цена *со скидкой*. *не пункт"},
		}},
		{"a table and a paragraph", []string{"| Было | Стало |", "| a | b |", "Текст."}, []block{
			{kind: tableBlock, rows: [][]string{{"Было", "Стало"}, {"a", "b"}}}, {kind: paragraphBlock, text: "Текст."},
		}},
		{"two tables", []string{"| Было | Стало |", "", "|Было|Стало|", "|-|-|", "|a|b|"}, []block{
			{kind: tableBlock, rows: [][]string{{"Было", "Стало"}}}, {kind: tableBlock, rows: [][]string{{"Было", "Стало"}, {"a", "b"}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var items []item
			for i, l := range tc.text {
				kind := textItem
				if strings.TrimSpace(l) == "" {
					kind = blankItem
				}
				items = append(items, item{kind: kind, line: i + 1, text: l})
			}
			got, err := textBlocks(items, invalid)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("textBlocks = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
