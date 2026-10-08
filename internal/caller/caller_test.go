package caller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/t8nax/gentry/internal/home"
)

// setup gives the test an empty data root and no session in the environment.
func setup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv(SessionEnv, "")
	t.Setenv(ClaudeSessionEnv, "")
	return root
}

func TestByAgent(t *testing.T) {
	root := setup(t)
	if ByAgent() {
		t.Error("outside a session the operator calls")
	}
	t.Setenv(ClaudeSessionEnv, "s1")
	if ByAgent() {
		t.Error("in a session without marks the operator calls: a command typed with !")
	}
	if err := Mark("s1", "toolu_1"); err != nil {
		t.Fatal(err)
	}
	if err := Mark("s1", "toolu_2"); err != nil {
		t.Fatal(err)
	}
	if !ByAgent() {
		t.Error("a marked call is the agent's")
	}
	if err := Unmark("s1", "toolu_1"); err != nil {
		t.Fatal(err)
	}
	if !ByAgent() {
		t.Error("another call of the session is still marked")
	}
	if err := Unmark("s1", "toolu_2"); err != nil {
		t.Fatal(err)
	}
	if ByAgent() {
		t.Error("all marks removed: the operator calls")
	}
	if _, err := os.Stat(filepath.Join(root, "state", "calls", "s1")); !os.IsNotExist(err) {
		t.Errorf("the directory of the session must go with its last mark: %v", err)
	}
	// A mark of another session does not count.
	if err := Mark("s2", "toolu_3"); err != nil {
		t.Fatal(err)
	}
	if ByAgent() {
		t.Error("a mark of another session counts")
	}
}

func TestClear(t *testing.T) {
	setup(t)
	t.Setenv(ClaudeSessionEnv, "s1")
	Mark("s1", "toolu_1")
	if err := Clear("s1"); err != nil {
		t.Fatal(err)
	}
	if ByAgent() {
		t.Error("marks left after Clear")
	}
	if err := Clear("s1"); err != nil {
		t.Errorf("Clear without marks: %v", err)
	}
	if err := Unmark("s1", "toolu_1"); err != nil {
		t.Errorf("Unmark without the mark: %v", err)
	}
}

func TestDriverSession(t *testing.T) {
	setup(t)
	t.Setenv(SessionEnv, "drv-1")
	if !ByAgent() {
		t.Error("a session of the driver is the agent's")
	}
}

func TestInvalidIdentifiers(t *testing.T) {
	root := setup(t)
	for _, bad := range [][2]string{{"../x", "c"}, {"s", "../../x"}, {"", "c"}, {"s", ""}} {
		if err := Mark(bad[0], bad[1]); err == nil {
			t.Errorf("Mark(%q, %q) must fail", bad[0], bad[1])
		}
	}
	if err := Clear(".."); err == nil {
		t.Error("Clear(..) must fail")
	}
	t.Setenv(ClaudeSessionEnv, "..")
	if ByAgent() {
		t.Error("an invalid session is the operator's")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("files written for invalid identifiers: %v", entries)
	}
}
