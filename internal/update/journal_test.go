package update

import (
	"slices"
	"strings"
	"testing"
)

// TestBreakingOf checks the incompatible changes of the versions an update
// skips: greater than the installed one and not greater than the new one,
// in the order of the versions, an entry on several lines joined by a space.
func TestBreakingOf(t *testing.T) {
	journal := strings.Join([]string{
		"# Журнал изменений",
		"",
		"## Не выпущено",
		"",
		"### Несовместимые изменения",
		"* Не выпущенное изменение.",
		"",
		"## 0.4.0 — 2026-11-09",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.4.0.",
		"",
		"## 0.3.0 — 2026-11-02",
		"",
		"Путь задачи показывает отдельная команда.",
		"",
		"### Несовместимые изменения",
		"* Команда `gentry task show --path` заменена",
		"  на `gentry task path`. Скрипты с прежней",
		"  командой нужно поправить.",
		"* Второе изменение.",
		"",
		"### Новое",
		"* Новое 0.3.0.",
		"",
		"## 0.2.1 — 2026-10-30",
		"",
		"### Исправлено",
		"* Исправление.",
		"",
		"## 0.2.0 — 2026-10-26",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.2.0.",
		"",
		"## 0.1.0 — 2026-10-20",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.1.0.",
	}, "\r\n")
	got := BreakingOf(journal, "0.1.0", "0.3.0")
	want := []Breaking{
		{"0.2.0", []string{"Изменение 0.2.0."}},
		{"0.3.0", []string{"Команда `gentry task show --path` заменена на `gentry task path`. Скрипты с прежней командой нужно поправить.", "Второе изменение."}},
	}
	if !slices.EqualFunc(got, want, func(a, b Breaking) bool { return a.Version == b.Version && slices.Equal(a.Lines, b.Lines) }) {
		t.Errorf("BreakingOf 0.1.0 → 0.3.0:\n%q\nwant:\n%q", got, want)
	}
	if got := BreakingOf(journal, "0.2.0", "0.2.1"); len(got) != 0 {
		t.Errorf("BreakingOf 0.2.0 → 0.2.1 = %q, want none", got)
	}
}
