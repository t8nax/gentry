package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
)

// TestReleaseTextFile checks the texts about the releases, which are no
// change for a new release: the journal and the files .md right in the
// folder releases, and nothing else.
func TestReleaseTextFile(t *testing.T) {
	for name, want := range map[string]bool{
		"CHANGELOG.md":            true,
		"releases/0.2.1.md":       true,
		"releases/0.2.0.md":       true,
		"releases/notes.md":       true,
		"releases/img/cart.md":    false,
		"releases/cart.png":       false,
		"releases.md":             false,
		"docs/CHANGELOG.md":       false,
		"cmd/releases/0.2.1.md":   false,
		"cart.go":                 false,
		"releases/0.2.1.md.orig":  false,
		"releases/0.2.1.markdown": false,
	} {
		if got := releaseTextFile(name); got != want {
			t.Errorf("releaseTextFile(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestChangedSince checks that a change of the journal and of the contents
// of the pages of the releases is no change since the tag, and a change of
// another file is.
func TestChangedSince(t *testing.T) {
	dir := t.TempDir()
	write := func(files map[string]string) {
		t.Helper()
		for name, text := range files {
			file := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gittest.Run(t, dir, "add", "--all")
		gittest.Run(t, dir, "commit", "--quiet", "--message", "change")
	}
	gittest.Run(t, dir, "init", "--quiet")
	write(map[string]string{"cart.go": "package cart\n", "CHANGELOG.md": "# Журнал\n", "releases/0.2.0.md": "# Новое\n"})
	gittest.Run(t, dir, "tag", "v0.2.0")
	write(map[string]string{"CHANGELOG.md": "# Журнал изменений\n", "releases/0.2.0.md": "# Новое\n\n## Каталог\n", "releases/0.2.1.md": "# Сообщение в группу\n"})
	if changed, err := changedSince(dir, "v0.2.0"); err != nil || changed {
		t.Errorf("changedSince after a change of the texts about the releases = %v, %v; want false", changed, err)
	}
	write(map[string]string{"releases/img/cart.md": "Картинка.\n"})
	if changed, err := changedSince(dir, "v0.2.0"); err != nil || !changed {
		t.Errorf("changedSince after a change of a file in a folder of releases = %v, %v; want true", changed, err)
	}
}
