package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
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

// TestWorktree finds the worktree of the pool a directory lies in by its path:
// a repository nested in a worktree belongs to it, and of nested worktrees
// the innermost one wins.
func TestWorktree(t *testing.T) {
	root, _ := paths.Canonical(t.TempDir())
	shop := gittest.Repo(t, filepath.Join(root, "shop"))
	fix := gittest.Worktree(t, shop, filepath.Join(root, "shop-fix"), "fix")
	inner := gittest.Worktree(t, shop, filepath.Join(shop, "inner"), "inner")
	vendor := gittest.Repo(t, filepath.Join(fix, "vendor", "payments"))
	src := filepath.Join(fix, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	pool := []state.Worktree{
		{Path: shop, Project: "shop", Main: true},
		{Path: fix, Project: "shop"},
		{Path: inner, Project: "shop"},
	}
	for _, tt := range []struct{ dir, want string }{
		{fix, fix},
		{src, fix},
		{vendor, fix},
		{filepath.Join(fix, "missing"), fix},
		{shop, shop},
		{inner, inner},
	} {
		w, err := Worktree(pool, tt.dir)
		if err != nil || w.Path != tt.want {
			t.Errorf("Worktree(%s) = %q, %v; want %q", tt.dir, w.Path, err, tt.want)
		}
	}

	// Outside the pool the refusal names the root of the repository, or the
	// directory itself if it is no repository.
	other := gittest.Repo(t, filepath.Join(root, "other"))
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(filepath.Join(other, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ dir, want string }{
		{filepath.Join(other, "src"), other},
		{plain, plain},
	} {
		_, err := Worktree(pool, tt.dir)
		var np *NotPooledError
		if !errors.As(err, &np) || np.Path != tt.want {
			t.Errorf("Worktree(%s): %v; want not pooled %s", tt.dir, err, tt.want)
		}
	}
}
