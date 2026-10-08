package cli

import (
	"strings"
	"testing"
)

func TestWriteTable(t *testing.T) {
	var b strings.Builder
	writeTable(&b, [][]string{
		{"ПРОЕКТ", "РАБОЧАЯ КОПИЯ", "ВЕТКА"},
		{"shop", `C:\Dev\shop`, "main"},
		{"cart", `C:\Dev\cart-long-name`, "—"},
	})
	want := "ПРОЕКТ  РАБОЧАЯ КОПИЯ          ВЕТКА\n" +
		`shop    C:\Dev\shop            main` + "\n" +
		`cart    C:\Dev\cart-long-name  —` + "\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteList(t *testing.T) {
	var b strings.Builder
	writeList(&b, "Рабочие копии:", []string{`C:\Dev\shop-2`, `C:\Dev\shop-fix`})
	if want := "Рабочие копии:\n  " + `C:\Dev\shop-2` + "\n  " + `C:\Dev\shop-fix` + "\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

// table returns rows printed as a table, the header row first.
func table(rows ...[]string) string {
	var b strings.Builder
	writeTable(&b, rows)
	return b.String()
}
