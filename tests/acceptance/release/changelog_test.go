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

// TestChangelog checks the journal of changes of the module: its heading,
// no paragraph before the sections, «Не выпущено» first, and the entries of
// the task of release and installation in order under «Новое».
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

// The entries of the journal of changes the task of the pages of the
// releases and the task of the update wrote in the section «Новое».
var (
	pagesEntries = []string{
		"* Добавлена страница каждого выпуска: что изменилось, что сделать при обновлении",
		"  и примеры новых возможностей — `https://t8nax.github.io/gentry/<версия>/`.",
		"  Список всех выпусков — `https://t8nax.github.io/gentry/`.",
	}
	updateEntries = []string{
		"* Добавлена команда `gentry update`: она ставит последний выпуск Gentry,",
		"  обновляет подключённые ИИ-инструменты и субагентов в рабочих копиях",
		"  и показывает несовместимые изменения.",
	}
)

// TestChangelogUpdate checks that the section «Новое» of the unreleased
// changes, or of the first release once there is one, has the entry of the
// update after the entries of the tasks before it, which stay.
func TestChangelogUpdate(t *testing.T) {
	testkit.SkipOnBinary(t)
	b, err := os.ReadFile(filepath.Join(testkit.ModuleRoot(t), "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	find := func(entries []string) int {
		for i := range ls {
			if i+len(entries) <= len(ls) && slices.Equal(ls[i:i+len(entries)], entries) {
				return i
			}
		}
		return -1
	}
	at := map[string]int{"release and installation": find(taskEntries), "pages": find(pagesEntries), "update": find(updateEntries)}
	for name, i := range at {
		if i < 0 {
			t.Errorf("the journal has no entries of the task of %s:\n%s", name, b)
		}
	}
	if t.Failed() {
		return
	}
	if !(at["release and installation"] < at["update"] && at["pages"] < at["update"]) {
		t.Errorf("the entry of the update is not after the entries of the tasks before it:\n%s", b)
	}
	// The headings over the entry: «Новое», then the section of the
	// unreleased changes or of the first release.
	var sub, sec string
	for i := at["update"] - 1; i >= 0 && sec == ""; i-- {
		switch {
		case strings.HasPrefix(ls[i], "### ") && sub == "":
			sub = ls[i]
		case strings.HasPrefix(ls[i], "## "):
			sec = ls[i]
		}
	}
	if sub != "### Новое" {
		t.Errorf("the entry of the update is under %q, want «### Новое»", sub)
	}
	hasRelease := slices.ContainsFunc(ls, versionHeading.MatchString)
	switch {
	case !hasRelease && sec != "## Не выпущено":
		t.Errorf("the journal has no release, and the entry of the update is in %q, want «## Не выпущено»", sec)
	case hasRelease && !strings.HasPrefix(sec, "## 0.1.0 — "):
		t.Errorf("the journal has a release, and the entry of the update is in %q, want the section of the first release 0.1.0", sec)
	}
	// The entries between are those of «Новое» too: no heading apart them.
	for _, l := range ls[min(at["pages"], at["release and installation"]):at["update"]] {
		if strings.HasPrefix(l, "#") {
			t.Errorf("a heading %q is between the entries of the tasks before and the entry of the update", l)
		}
	}
}
