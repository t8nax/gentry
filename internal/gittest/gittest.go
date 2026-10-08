// Package gittest helps tests that run git: it isolates git from the
// configuration of the machine and builds repositories.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate points git at a global configuration in dir with a test identity
// and no system configuration, so tests neither read the operator's settings
// nor depend on them. Call it from TestMain.
func Isolate(dir string) error {
	cfg := filepath.Join(dir, "gitconfig")
	content := "[user]\n\tname = Gentry Test\n\temail = test@example.com\n" +
		"[init]\n\tdefaultBranch = main\n" +
		"[commit]\n\tgpgsign = false\n" +
		// The repositories of tests are small and short-lived: git need not
		// start its maintenance after each commit.
		"[maintenance]\n\tauto = false\n" +
		"[gc]\n\tauto = 0\n"
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		return err
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return nil
}

// Run runs git in dir and returns its output, failing the test on error.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// Repo creates a repository with one commit in dir and returns dir.
func Repo(t testing.TB, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Run(t, dir, "init", "--quiet")
	Run(t, dir, "commit", "--quiet", "--allow-empty", "--message", "init")
	return dir
}

// Worktree adds a worktree of the repository in repo at dir on a new branch.
func Worktree(t testing.TB, repo, dir, branch string) string {
	t.Helper()
	Run(t, repo, "worktree", "add", "--quiet", "-b", branch, dir)
	return dir
}
