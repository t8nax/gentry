package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/paths"
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

func TestRootAndWorktrees(t *testing.T) {
	root, _ := paths.Canonical(t.TempDir())
	shop := gittest.Repo(t, filepath.Join(root, "shop"))
	fix := gittest.Worktree(t, shop, filepath.Join(root, "shop-fix"), "fix")
	sub := filepath.Join(fix, "src")
	os.MkdirAll(sub, 0o755)

	if got, err := Root(sub); err != nil || got != fix {
		t.Errorf("Root(%s) = %q, %v; want %q", sub, got, err, fix)
	}
	if _, err := Root(root); !errors.Is(err, ErrNotRepo) {
		t.Errorf("Root outside git: got %v, want ErrNotRepo", err)
	}
	list, err := Worktrees(fix)
	want := []Worktree{{Path: shop, Main: true}, {Path: fix}}
	if err != nil || len(list) != 2 || list[0] != want[0] || list[1] != want[1] {
		t.Errorf("Worktrees = %+v, %v; want %+v", list, err, want)
	}
}

func TestInitAndCommit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "knowledge")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if ok, err := HasCommits(dir); ok || err != nil {
		t.Errorf("new repository: HasCommits = %v, %v", ok, err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	if err := Commit(dir, "a.txt", "Подключение проекта shop к Gentry"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := HasCommits(dir); !ok {
		t.Error("no commit after Commit")
	}
	// Only the named file is committed.
	if out := gittest.Run(t, dir, "status", "--porcelain"); out != "?? b.txt\n" {
		t.Errorf("status %q", out)
	}
	if out := gittest.Run(t, dir, "log", "--format=%s"); out != "Подключение проекта shop к Gentry\n" {
		t.Errorf("log %q", out)
	}

	var ce *CommandError
	if err := Commit(dir, "missing.txt", "x"); !errors.As(err, &ce) || ce.Output == "" {
		t.Errorf("failed command: got %v", err)
	}
}

func TestBranch(t *testing.T) {
	root, _ := paths.Canonical(t.TempDir())
	shop := gittest.Repo(t, filepath.Join(root, "shop"))
	fix := gittest.Worktree(t, shop, filepath.Join(root, "shop-fix"), "fix/refund")

	if got, err := Branch(fix); err != nil || got != "fix/refund" {
		t.Errorf("Branch(%s) = %q, %v; want fix/refund", fix, got, err)
	}
	gittest.Run(t, fix, "checkout", "--quiet", "--detach")
	if got, err := Branch(fix); err != nil || got != "" {
		t.Errorf("detached: Branch = %q, %v; want no branch", got, err)
	}
	var ce *CommandError
	if _, err := Branch(root); !errors.As(err, &ce) {
		t.Errorf("outside git: got %v, want a failed command", err)
	}
}

func TestChanged(t *testing.T) {
	shop := gittest.Repo(t, filepath.Join(t.TempDir(), "shop"))
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(shop, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "*.log\n")
	write("cart.go", "package shop\n")
	gittest.Run(t, shop, "add", ".")
	gittest.Run(t, shop, "commit", "--quiet", "-m", "files")
	if files, err := Changed(shop); err != nil || len(files) != 0 {
		t.Fatalf("clean worktree: %v, %v", files, err)
	}

	write("build.log", "ignored")
	write("backend/payments/refund.go", "package payments\n")
	gittest.Run(t, shop, "mv", "cart.go", "basket.go")
	files, err := Changed(shop)
	want := []string{"basket.go", "backend/payments/refund.go"}
	if err != nil || len(files) != 2 || files[0] != want[0] || files[1] != want[1] {
		t.Errorf("Changed = %q, %v; want %q", files, err, want)
	}
}
