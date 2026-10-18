package changelog

import (
	"reflect"
	"strings"
	"testing"
)

// TestSections checks the sections of the versions in the order of the
// journal: the date, the lines, the phrase and the entries, an entry on
// several lines joined by a space, and the headings that are no section of
// a version left out.
func TestSections(t *testing.T) {
	journal := strings.Join([]string{
		"# Журнал изменений",
		"",
		"## Не выпущено",
		"",
		"### Новое",
		"* Не выпущенное.",
		"",
		"## 0.3.0 — 2026-11-02",
		"",
		"Корзина сохраняется",
		"после входа.",
		"",
		"### Несовместимые изменения",
		"* Поле `price` заказа заменено",
		"  на `total`.",
		"",
		"### Новое",
		"* Добавлен список желаний.",
		"",
		"## 0.2.0",
		"Без даты.",
		"## 0.1.0 — 2026-10-20",
		"",
		"### Исправлено",
		"* Исправлен поиск.",
	}, "\r\n")
	got := Sections(journal)
	want := []Section{
		{Version: "0.3.0", Date: "2026-11-02",
			Lines:   []string{"Корзина сохраняется", "после входа.", "", "### Несовместимые изменения", "* Поле `price` заказа заменено", "  на `total`.", "", "### Новое", "* Добавлен список желаний."},
			Phrase:  "Корзина сохраняется после входа.",
			Entries: map[string][]string{Breaking: {"Поле `price` заказа заменено на `total`."}, New: {"Добавлен список желаний."}}},
		{Version: "0.1.0", Date: "2026-10-20", Lines: []string{"### Исправлено", "* Исправлен поиск."},
			Entries: map[string][]string{Fixed: {"Исправлен поиск."}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sections:\n%+v\nwant:\n%+v", got, want)
	}
	if s, ok := Find(journal, "0.1.0"); !ok || !s.Has(Fixed) || s.Has(Breaking) {
		t.Errorf("Find 0.1.0 = %+v, %v", s, ok)
	}
	for _, v := range []string{"0.2.0", "0.4.0"} {
		if _, ok := Find(journal, v); ok {
			t.Errorf("Find found a section of %s", v)
		}
	}
	if s := Sections(""); len(s) != 0 {
		t.Errorf("Sections of an empty journal: %+v", s)
	}
}
