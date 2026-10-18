package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestReplaceRestore checks that on Windows a failure of the new program to
// take the place of the one renamed to .old gives the old one its name back,
// and that a failure of that too is told by the WriteError.
func TestReplaceRestore(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a program is renamed to .old only on Windows")
	}
	defer func(r func(string, string) error) { rename = r }(rename)
	for _, restoreFails := range []bool{false, true} {
		dir := t.TempDir()
		program := filepath.Join(dir, Program())
		os.WriteFile(program, []byte("old"), 0o755)
		downloaded := filepath.Join(t.TempDir(), "gentry-new")
		os.WriteFile(downloaded, []byte("new"), 0o644)
		busy, denied := errors.New("busy"), errors.New("denied")
		rename = func(from, to string) error {
			switch {
			case from == program+".new":
				return busy // the program is held, then the new one cannot take its place
			case from == Old(program) && restoreFails:
				return denied
			}
			return os.Rename(from, to)
		}
		var we *WriteError
		err := Replace(downloaded, program)
		if !errors.As(err, &we) || !errors.Is(we.Err, busy) {
			t.Fatalf("Replace: %v, want WriteError of the rename", err)
		}
		if restoreFails {
			if !errors.Is(we.Restore, denied) {
				t.Errorf("Restore %v, want the failure to give the name back", we.Restore)
			}
			if b, _ := os.ReadFile(Old(program)); string(b) != "old" {
				t.Errorf("the program is not at .old: %q", b)
			}
			continue
		}
		if b, _ := os.ReadFile(program); we.Restore != nil || string(b) != "old" {
			t.Errorf("the program after a failed Replace: %q, Restore %v; want it as it was", b, we.Restore)
		}
	}
}
