package task

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/state"
)

// TestCheckLogs checks where the files of the output of the commands of
// checks of a pass lie, for the first attempt of a task and a later one, and
// that the files of a pass are those numbered from 1 without a gap: the
// files of another pass are not its.
func TestCheckLogs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	first := state.Task{Project: "shop", Prefix: "SHOP", Number: 1, Attempt: 1}
	again := first
	again.Attempt = 2
	if got, _ := CheckLogPath(first, "review", 2, 1); got != filepath.Join(root, "state", "shop", "tasks", "SHOP-1", "checks", "review-2-1.log") {
		t.Errorf("first attempt: %s", got)
	}
	if got, _ := CheckLogPath(again, "review", 2, 3); got != filepath.Join(root, "state", "shop", "tasks", "SHOP-1", "attempts", "2", "checks", "review-2-3.log") {
		t.Errorf("second attempt: %s", got)
	}

	pass := state.Pass{Node: "review", Round: 2}
	if logs := CheckLogs(first, pass); logs != nil {
		t.Errorf("no files: %q", logs)
	}
	var want []string
	for _, f := range []struct {
		node     string
		round, n int
	}{{"review", 2, 1}, {"review", 2, 2}, {"review", 2, 4}, {"review", 1, 3}, {"review-2", 1, 1}} {
		path, _ := CheckLogPath(first, f.node, f.round, f.n)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if f.node == "review" && f.round == 2 && f.n < 3 {
			want = append(want, path)
		}
	}
	if logs := CheckLogs(first, pass); !slices.Equal(logs, want) {
		t.Errorf("files %q, want %q", logs, want)
	}
	if logs := CheckLogs(again, pass); logs != nil {
		t.Errorf("files of another attempt: %q", logs)
	}
}
