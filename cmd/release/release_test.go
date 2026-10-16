package main

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want version
		ok   bool
	}{
		{"0.1.0", version{0, 1, 0}, true},
		{"10.20.300", version{10, 20, 300}, true},
		{"0.1", version{}, false},
		{"v0.1.0", version{}, false},
		{"0.1.0-rc1", version{}, false},
		{"0.1.0+build", version{}, false},
		{"01.1.0", version{}, false},
		{"0.1.0 ", version{}, false},
		{"", version{}, false},
		{"1.2.99999999999999999999", version{}, false},
	} {
		got, ok := parseVersion(tc.s)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tc.s, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b version
		want int
	}{
		{version{0, 1, 5}, version{0, 2, 0}, -1},
		{version{0, 10, 0}, version{0, 9, 9}, 1},
		{version{1, 0, 0}, version{0, 99, 99}, 1},
		{version{0, 2, 1}, version{0, 2, 1}, 0},
	} {
		if got := tc.a.compare(tc.b); got != tc.want {
			t.Errorf("%v.compare(%v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLatestRelease(t *testing.T) {
	got, ok := latestRelease([]string{"v0.2.0", "v0.10.0", "v0.9.1", "v1.0.0-rc1", "release-2", "v0.11"})
	if !ok || got != (version{0, 10, 0}) {
		t.Errorf("latestRelease = %v, %v; want 0.10.0", got, ok)
	}
	if _, ok := latestRelease([]string{"start", "v1"}); ok {
		t.Error("latestRelease found a release among tags of no release")
	}
}

func TestNotes(t *testing.T) {
	journal := "# Журнал изменений\n\n## Не выпущено\n\n" +
		"## 0.3.0 — 2026-11-02\n\nКорзина сохраняется после входа.\n\n### Новое\n* Добавлен список желаний.\n\n" +
		"## 0.2.0 — 2026-10-20\n\nКорзина появилась.\n"
	for _, tc := range []struct {
		name    string
		journal string
		v       version
		want    string
		ok      bool
	}{
		{"section up to the next one", journal, version{0, 3, 0}, "Корзина сохраняется после входа.\n\n### Новое\n* Добавлен список желаний.\n", true},
		{"last section", journal, version{0, 2, 0}, "Корзина появилась.\n", true},
		{"no section", journal, version{0, 4, 0}, "", false},
		{"heading without date", "## 0.3.0\n\nТекст.\n", version{0, 3, 0}, "", false},
		{"heading of another version that begins alike", "## 0.3.00 — 2026-11-02\n\nТекст.\n", version{0, 3, 0}, "", false},
		{"lines ended by CRLF", "## 0.3.0 — 2026-11-02\r\n\r\nТекст.\r\n\r\n## 0.2.0 — 2026-10-20\r\n", version{0, 3, 0}, "Текст.\n", true},
		{"heading of the first level ends the section", "## 0.3.0 — 2026-11-02\nТекст.\n# Другое\nНе текст.\n", version{0, 3, 0}, "Текст.\n", true},
		{"empty section", "## 0.3.0 — 2026-11-02\n\n## 0.2.0 — 2026-10-20\n", version{0, 3, 0}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := notes(tc.journal, tc.v)
			if got != tc.want || ok != tc.ok {
				t.Errorf("notes = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestChecksums(t *testing.T) {
	got := string(checksums(map[string]string{"gentry-linux-amd64": "bb", "gentry-darwin-arm64": "aa"}))
	want := "aa  gentry-darwin-arm64\nbb  gentry-linux-amd64\n"
	if got != want {
		t.Errorf("checksums:\n%s\nwant:\n%s", got, want)
	}
}

// TestRefusal checks that each refusal turns into its lines and any other
// error into its text.
func TestRefusal(t *testing.T) {
	v := version{0, 3, 0}
	latest := version{0, 2, 0}
	for _, tc := range []struct {
		err  error
		want []string
	}{
		{&versionFormatError{value: "0.1"}, []string{"Версия указана неверно.", "Версия: 0.1"}},
		{&notMainError{branch: "feat/cart"}, []string{"Выпуск делается только из main.", "Ветка: feat/cart"}},
		{&releasedError{version: latest}, []string{"Выпуск с этой версией уже есть.", "Версия: 0.2.0"}},
		{&notGreaterError{version: version{0, 1, 5}, latest: latest}, []string{"Версия должна быть больше версии последнего выпуска.", "Версия: 0.1.5", "Последний выпуск: 0.2.0"}},
		{&noChangesError{latest: latest}, []string{"После последнего выпуска в main нет изменений, кроме журнала изменений.", "Последний выпуск: 0.2.0"}},
		{&noSectionError{version: v}, []string{"В журнале изменений нет раздела этой версии.", "Версия: 0.3.0"}},
		{&versionMismatchError{want: v, got: "0.3.0+dirty"}, []string{"Собранная программа называет другую версию.", "Ожидалась: 0.3.0", "Получена: 0.3.0+dirty"}},
		{errors.New("git: not found"), []string{"git: not found"}},
	} {
		if got := refusal(tc.err); !slices.Equal(got, tc.want) {
			t.Errorf("refusal(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// TestRunUsage checks that a wrong command line exits with code 2 and the
// usage.
func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"check"}, {"build", "0.1.0"}, {"publish", "0.1.0"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stderr.String() != usage || stdout.Len() != 0 {
			t.Errorf("run(%q) = %d, stdout %q, stderr %q; want 2 and the usage", args, code, stdout.String(), stderr.String())
		}
	}
}
