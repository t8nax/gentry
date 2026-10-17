package release_test

import (
	"strconv"
	"strings"
	"testing"
)

// The refusals of the content of a page.
const (
	noContent  = "Нет содержимого страницы выпуска."
	badContent = "Содержимое страницы выпуска записано неверно."
)

// file returns the text of a file of the lines.
func file(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// wantLine checks that the line n of the text of a file of the stand, from
// 1, is the given one: the case names the line of the error.
func wantLine(t *testing.T, text string, n int, want string) {
	t.Helper()
	ls := strings.Split(text, "\n")
	if n > len(ls) || ls[n-1] != want {
		t.Fatalf("the stand is wrong: the line %d of the file is not %q:\n%s", n, want, text)
	}
}

// checkShop makes the repository of the shop with the release 0.3.0 tagged
// and a change of the code after it, the files changed by the given ones,
// and runs the checks of the button for 0.4.0.
func checkShop(t *testing.T, changed map[string]string) (*repo, result) {
	t.Helper()
	r := shopRepo(t, shopFiles(changed), "0.3.0")
	return r, r.check("0.4.0")
}

// TestContentMissingNew checks that the button refuses a version with «Новое»
// in the journal and no content of its page, and names the file.
func TestContentMissingNew(t *testing.T) {
	r, res := checkShop(t, map[string]string{contentDir + "0.4.0.md": ""})
	wantRefusal(t, res, noContent, "Версия: 0.4.0", "Файл: releases/0.4.0.md")
	r.wantTags("v0.3.0")
}

// TestContentMissingBreaking checks that the button refuses a version with
// only breaking changes in the journal and no content of its page.
func TestContentMissingBreaking(t *testing.T) {
	breaking := "## 0.4.0 — 2026-11-02\n\n" + shop040Phrase + "\n\n### Несовместимые изменения\n* " + shop040Break + "\n"
	r, res := checkShop(t, map[string]string{changelogFile: changelog(breaking, shop030, shop021), contentDir + "0.4.0.md": ""})
	wantRefusal(t, res, noContent, "Версия: 0.4.0", "Файл: releases/0.4.0.md")
	r.wantTags("v0.3.0")
}

// TestContentNoNew checks that the button refuses a content with no section
// «Новое» of a version with «Новое» in the journal, with no line.
func TestContentNoNew(t *testing.T) {
	before := strings.TrimSuffix(content040, content040New)
	r, res := checkShop(t, map[string]string{contentDir + "0.4.0.md": before})
	wantRefusal(t, res, badContent, "Файл: releases/0.4.0.md", "Ошибка: нет раздела «Новое», хотя в журнале есть новое")
	r.wantTags("v0.3.0")
}

// cardLines returns the lines of a card of a content: its heading, the
// explanation and one example of a command and n lines of its output, 7+n
// lines in all.
func cardLines(title, cmd string, n int) []string {
	ls := []string{"## " + title, "", "Пояснение карточки «" + title + "».", "", "```", "$ " + cmd}
	for i := 1; i <= n; i++ {
		ls = append(ls, "Строка вывода "+strconv.Itoa(i))
	}
	return append(ls, "```")
}

// cat joins the parts of the lines of a file.
func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// explanation is an explanation of a card of eight lines: paragraphs and a
// list.
var explanation = []string{
	"Заказ можно оплатить в несколько платежей:",
	"магазин сам напомнит покупателю о следующем.",
	"",
	"* Число платежей — от 2 до 6.",
	"* Первый платёж — при оформлении.",
	"* Остальные — раз в месяц.",
	"",
	"Подробности — в разделе «Оплата».",
}

// TestContentInvalid checks that the button refuses a content written wrong
// and names the file, the line and the error.
func TestContentInvalid(t *testing.T) {
	newSection := []string{"# Новое", ""}
	for _, c := range []struct {
		name  string
		lines []string
		line  int    // the line of the error
		at    string // the line of the file there
		error []string
	}{
		{
			name:  "unknown_section",
			lines: cat([]string{"# Скидки", "", "Скидки задаются в разделе «Акции».", ""}, newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line:  1, at: "# Скидки",
			error: []string{"Ошибка: неизвестный раздел «Скидки»", "Допустимые разделы:", "  Перед обновлением", "  Новое"},
		},
		{
			name: "repeated_section",
			lines: cat(newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1), []string{""},
				cardLines("Список желаний", "shop wishlist add SKU-7", 0), []string{""},
				newSection, cardLines("Акции", "shop promo add", 0)),
			line: 20, at: "# Новое",
			error: []string{"Ошибка: раздел «Новое» повторяется"},
		},
		{
			name:  "text_outside",
			lines: cat([]string{"Скидки задаются в одном месте.", ""}, newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line:  1, at: "Скидки задаются в одном месте.",
			error: []string{"Ошибка: текст вне раздела"},
		},
		{
			name: "card_outside_new",
			lines: cat([]string{
				"# Перед обновлением",
				"",
				"| Было | Стало |",
				"| --- | --- |",
				"| `shop discount add` | `shop promo add` |",
				"| `shop discount list` | `shop promo list` |",
				"",
				"## Акции",
				"",
				"Скидки задаются в одном месте.",
				"",
			}, newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line: 8, at: "## Акции",
			error: []string{"Ошибка: карточка вне раздела «Новое»"},
		},
		{
			name: "card_without_explanation",
			lines: cat(newSection,
				cardLines("Оплата заказа частями", "shop checkout --installments 3", 1), []string{""},
				cardLines("Акции", "shop promo add", 1), []string{""},
				cardLines("Доставка", "shop delivery list", 1), []string{""},
				[]string{
					"## Список желаний",
					"",
					"### Вкладка: Было",
					"",
					"```",
					"$ shop cart add SKU-7",
					"```",
					"",
					"### Вкладка: Стало",
					"",
					"```",
					"$ shop wishlist add SKU-7",
					"```",
				}),
			line: 30, at: "## Список желаний",
			error: []string{"Ошибка: у карточки «Список желаний» нет пояснения"},
		},
		{
			name: "unknown_subheading",
			lines: cat(newSection, []string{"## Оплата заказа частями", ""}, explanation, []string{
				"",
				"### Пример",
				"",
				"```",
				"$ shop checkout --installments 3",
				"```",
			}),
			line: 14, at: "### Пример",
			error: []string{"Ошибка: неизвестный подзаголовок «Пример»", "Допустимые подзаголовки:", "  Вкладка: <название>", "  Рядом: <подпись>"},
		},
		{
			name: "subheading_without_example",
			lines: cat(newSection, []string{"## Оплата заказа частями", ""}, explanation, []string{
				"",
				"### Вкладка: Оформление",
				"",
				"### Вкладка: Когда частями нельзя",
				"",
				"```",
				"$ shop checkout --installments 3",
				"! Оплата частями недоступна для заказа меньше 3000 ₽.",
				"```",
			}),
			line: 14, at: "### Вкладка: Оформление",
			error: []string{"Ошибка: у подзаголовка «Вкладка: Оформление» нет примера"},
		},
		{
			name: "side_without_pair",
			lines: cat(newSection,
				cardLines("Оплата заказа частями", "shop checkout --installments 3", 3), []string{""},
				cardLines("Акции", "shop promo add", 3), []string{""},
				cardLines("Доставка", "shop delivery list", 3), []string{""},
				[]string{
					"## Список желаний",
					"",
					"Товар можно отложить и купить позже.",
					"",
					"### Рядом: Было",
					"",
					"```",
					"$ shop cart add SKU-7",
					"```",
				}),
			line: 40, at: "### Рядом: Было",
			error: []string{"Ошибка: у подзаголовка «Рядом: Было» нет пары"},
		},
		{
			name: "unclosed_example",
			lines: cat(newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 7), []string{
				"",
				"## Список желаний",
				"",
				"Товар можно отложить и купить позже.",
				"",
				"```",
				"$ shop wishlist add SKU-7",
				"+ Товар SKU-7 отложен в список желаний.",
			}),
			line: 22, at: "```",
			error: []string{"Ошибка: блок примера не закрыт"},
		},
		{
			name: "table_columns",
			lines: cat([]string{
				"# Перед обновлением",
				"",
				"| Команда | Замена |",
				"| --- | --- |",
				"| `shop discount add` | `shop promo add` |",
				"",
			}, newSection, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1)),
			line: 3, at: "| Команда | Замена |",
			error: []string{"Ошибка: у таблицы должны быть столбцы «Было» и «Стало»"},
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

// TestContentPastRelease checks that the button refuses when the content of
// the page of a past release on main is written wrong, though that of the new
// version is right.
func TestContentPastRelease(t *testing.T) {
	r := shopRepo(t, shopFiles(nil), "0.3.0")
	broken := file(cat([]string{"# Скидки", "", "Скидки задаются в разделе «Акции».", "", "# Новое", ""}, cardLines("Оплата заказа частями", "shop checkout --installments 3", 1))...)
	r.commit(map[string]string{contentDir + "0.3.0.md": broken})
	wantRefusal(t, r.check("0.4.0"), badContent, "Файл: releases/0.3.0.md", "Строка: 1",
		"Ошибка: неизвестный раздел «Скидки»", "Допустимые разделы:", "  Перед обновлением", "  Новое")
	r.wantTags("v0.3.0")
}
