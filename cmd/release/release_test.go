package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
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

// TestRefusal checks what the acceptance tests of the release do not: a
// refusal wrapped in another error is told in its own words, and any other
// error is printed as it is. The words of each refusal are stated by the
// acceptance tests of tests/acceptance/release.
func TestRefusal(t *testing.T) {
	refused := &notMainError{branch: "feat/cart"}
	if got, want := refusal(fmt.Errorf("check: %w", refused)), refusal(refused); got != want {
		t.Errorf("a wrapped refusal: %q, want %q", got, want)
	}
	if got := refusal(errors.New("git: not found")); got != "git: not found" {
		t.Errorf("another error: %q, want it as it is", got)
	}
}

// TestRunUsage checks that a wrong command line exits with code 2 and the
// usage.
func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"check"}, {"build", "0.1.0"}, {"publish", "0.1.0"}, {"pages"}, {"pages", "a", "b"}, {"notice"}, {"notice", "0.1.0", "b"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stderr.String() != usage || stdout.Len() != 0 {
			t.Errorf("run(%q) = %d, stdout %q, stderr %q; want 2 and the usage", args, code, stdout.String(), stderr.String())
		}
	}
}

// TestReadSection checks the reading of the section of a version for its
// page: the date, the phrase about the main thing, the entries of the
// subsections, an entry on two lines.
func TestReadSection(t *testing.T) {
	journal := "# Журнал изменений\n\n## Не выпущено\n\n" +
		"## 0.4.0 — 2026-11-02\n\nКорзина сохраняется\nпосле входа.\n\nВторой абзац.\n\n" +
		"### Несовместимые изменения\n\n### Новое\n* Добавлен список\n  желаний.\n* Добавлена оплата.\nпродолжение без отступа\n\n" +
		"### Изменено\n* Корзина\nсохраняется.\n\n  отступ после пустой строки\n" +
		"## 0.3.0 — 2026-10-20\n\n### Исправлено\n* Исправлен поиск.\n"
	s, ok := readSection(journal, version{0, 4, 0})
	want := journalSection{
		date:   "2026-11-02",
		phrase: "Корзина сохраняется после входа.",
		entries: map[string][]string{
			journalBreaking: {},
			journalNew:      {"Добавлен список желаний.", "Добавлена оплата. продолжение без отступа"},
			journalChanged:  {"Корзина сохраняется."},
		},
	}
	if !ok || !reflect.DeepEqual(s, want) {
		t.Errorf("readSection = %+v, %v; want %+v", s, ok, want)
	}
	if !s.has(journalBreaking) || s.has(journalFixed) {
		t.Errorf("has: breaking %v, fixed %v; want true, false", s.has(journalBreaking), s.has(journalFixed))
	}
	s, ok = readSection(journal, version{0, 3, 0})
	if !ok || s.phrase != "" || !reflect.DeepEqual(s.entries, map[string][]string{journalFixed: {"Исправлен поиск."}}) {
		t.Errorf("readSection of a section with no phrase = %+v, %v", s, ok)
	}
	if _, ok := readSection(journal, version{0, 2, 0}); ok {
		t.Error("readSection found a section of a version with none")
	}
}

// TestReleaseText checks the text of a release: the section, an empty line
// and the line with the address of the page; with no section, the line
// alone.
func TestReleaseText(t *testing.T) {
	link := msg.Text(msg.ReleasePageLink, "https://t8nax.github.io/gentry/0.3.0/") + "\n"
	if got, want := releaseText("Корзина.\n", version{0, 3, 0}), "Корзина.\n\n"+link; got != want {
		t.Errorf("releaseText = %q, want %q", got, want)
	}
	if got := releaseText("", version{0, 3, 0}); got != link {
		t.Errorf("releaseText of an empty section = %q, want %q", got, link)
	}
}

// TestContentRefusal checks the refusal of a content written wrong: the
// line only when the error is of one, and the sections and the subheadings
// a content may have after the errors of an unknown one.
func TestContentRefusal(t *testing.T) {
	file := msg.Text(msg.ReleaseContentInvalid, "releases/0.4.0.md")
	for _, tc := range []struct {
		err  contentInvalidError
		want []string
	}{
		{contentInvalidError{line: 1, reason: reasonUnknownSection, subject: "Скидки"}, []string{
			file, msg.Text(msg.ReleaseContentLine, 1),
			msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentUnknownSection, "Скидки")), msg.Text(msg.ReleaseContentSections),
		}},
		{contentInvalidError{line: 14, reason: reasonUnknownSubheading, subject: "Пример"}, []string{
			file, msg.Text(msg.ReleaseContentLine, 14),
			msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentUnknownSubheading, "Пример")), msg.Text(msg.ReleaseContentSubheadings),
		}},
		{contentInvalidError{line: 12, reason: reasonOutsideCard}, []string{
			file, msg.Text(msg.ReleaseContentLine, 12), msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentOutsideCard)),
		}},
		{contentInvalidError{reason: reasonNoNew}, []string{file, msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentNoNew))}},
		{contentInvalidError{line: 3, reason: reasonNoticeUnknown, subject: "Акции"}, []string{
			file, msg.Text(msg.ReleaseContentLine, 3),
			msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentNoticeUnknown, "Акции")), msg.Text(msg.ReleaseContentNoticeSubsections),
		}},
		{contentInvalidError{reason: reasonNoticeLong, length: 4100}, []string{
			file, msg.Text(msg.ReleaseContentLength, 4100), msg.Text(msg.ReleaseContentLimit, noticeLimit),
			msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentNoticeLong)),
		}},
		{contentInvalidError{line: 7, reason: reasonNoticeOutside}, []string{
			file, msg.Text(msg.ReleaseContentLine, 7),
			msg.Text(msg.ReleaseContentError, msg.Text(msg.ReleaseContentNoticeOutside)), msg.Text(msg.ReleaseContentNoticeItemRule),
		}},
	} {
		tc.err.file = "releases/0.4.0.md"
		if got, want := refusal(fmt.Errorf("site: %w", &tc.err)), strings.Join(tc.want, "\n"); got != want {
			t.Errorf("refusal of %+v:\n%s\nwant:\n%s", tc.err, got, want)
		}
	}
}
