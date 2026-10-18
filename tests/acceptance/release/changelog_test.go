package release_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// TestChangelog checks the journal of changes of the module: its heading,
// no paragraph before the sections, and «Не выпущено» first.
//
// The entries are text for the user, which the release may rewrite: the test
// checks the layout of the journal, not their words.
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
}
