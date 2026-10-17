package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/gittest"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestHeadFile checks the reading of a file of the commit HEAD: its text, an
// empty text for a file the commit has not, even when the work tree has
// it, and an error of git as it is.
func TestHeadFile(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Журнал\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "--all")
	gittest.Run(t, dir, "commit", "--quiet", "--message", "journal")
	if err := os.WriteFile(filepath.Join(dir, "cart.go"), []byte("package cart\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, err := headFile(dir, "CHANGELOG.md"); err != nil || text != "# Журнал\n" {
		t.Errorf("headFile of a file of the commit = %q, %v", text, err)
	}
	if text, err := headFile(dir, "cart.go"); err != nil || text != "" {
		t.Errorf("headFile of a file not committed = %q, %v; want an empty text", text, err)
	}
	var ce *git.CommandError
	if _, err := headFile(t.TempDir(), "CHANGELOG.md"); !errors.As(err, &ce) {
		t.Errorf("headFile out of a repository: %v, want the error of git", err)
	}
	if _, err := showHead(dir, "cart.go"); !errors.As(err, &ce) {
		t.Errorf("showHead of a file the commit has not: %v, want the error of git", err)
	}
}
