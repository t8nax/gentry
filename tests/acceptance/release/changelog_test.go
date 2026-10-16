package release_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// taskEntries are the entries of the journal of changes the task of release
// and installation wrote in the section «Новое».
var taskEntries = []string{
	"* Добавлена установка одной командой: на Linux и macOS —",
	"  `curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh`,",
	"  на Windows — `irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex`.",
	"  Права администратора и Go не нужны.",
	"* Добавлена установка конкретной версии параметром `--version`",
	"  (на Windows — `-Version`), в том числе возврат на прежнюю.",
	"* Добавлена установка без изменения PATH параметром `--no-modify-path`",
	"  (на Windows — `-NoModifyPath`).",
}

// versionHeading is the heading of the section of a version.
var versionHeading = regexp.MustCompile(`^## \d+\.\d+\.\d+ — \d{4}-\d{2}-\d{2}$`)

// Кейс: начальный журнал.
//
// The test stays true as the journal grows: the entries of the task stay in
// «Не выпущено» until the first release and are in its section 0.1.0 after.
func TestChangelog(t *testing.T) {
	testkit.SkipOnBinary(t)
	b, err := os.ReadFile(filepath.Join(testkit.ModuleRoot(t), "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	if ls[0] != "# Журнал изменений" {
		t.Errorf("the first line is %q, want «# Журнал изменений»", ls[0])
	}
	// No paragraph before the first section, and it is «Не выпущено».
	first := slices.IndexFunc(ls[1:], func(l string) bool { return l != "" }) + 1
	if first == 0 || ls[first] != "## Не выпущено" {
		t.Errorf("after the heading of the journal, want only empty lines and «## Не выпущено»:\n%s", b)
	}
	at := -1
	for i := range ls {
		if i+len(taskEntries) <= len(ls) && slices.Equal(ls[i:i+len(taskEntries)], taskEntries) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the journal has no entries of the task, in order:\n%s\njournal:\n%s", strings.Join(taskEntries, "\n"), b)
	}
	// The headings over the entries: of the third level, then of the second.
	var sub, sec string
	for i := at - 1; i >= 0 && sec == ""; i-- {
		switch {
		case strings.HasPrefix(ls[i], "### ") && sub == "":
			sub = ls[i]
		case strings.HasPrefix(ls[i], "## "):
			sec = ls[i]
		}
	}
	if sub != "### Новое" {
		t.Errorf("the entries of the task are under %q, want «### Новое»", sub)
	}
	hasRelease := slices.ContainsFunc(ls, versionHeading.MatchString)
	switch {
	case !hasRelease && sec != "## Не выпущено":
		t.Errorf("the journal has no release, and the entries of the task are in %q, want «## Не выпущено»", sec)
	case hasRelease && !strings.HasPrefix(sec, "## 0.1.0 — "):
		t.Errorf("the journal has a release, and the entries of the task are in %q, want the section of the first release 0.1.0", sec)
	}
}
