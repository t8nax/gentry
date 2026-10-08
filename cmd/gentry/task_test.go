package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/paths"
)

// writers is the number of gentry processes that take tasks at once.
const writers = 8

// shopWithFlow connects a shop with the flow of the flow testdata through
// the binary, adds n worktrees to its pool and returns their paths.
func shopWithFlow(t *testing.T, bin string, n int) []string {
	t.Helper()
	root, err := paths.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, filepath.Join(root, "home"))
	t.Setenv(caller.SessionEnv, "")
	t.Setenv(caller.ClaudeSessionEnv, "")
	shop := gittest.Repo(t, filepath.Join(root, "shop"))
	gentry(t, bin, shop, "project", "add", "shop", "--knowledge", filepath.Join(root, "shop-knowledge"))
	process := filepath.Join(root, "home", "process")
	testdata := filepath.Join("..", "..", "internal", "flow", "testdata", "process")
	if err := os.CopyFS(filepath.Join(process, "agents"), os.DirFS(filepath.Join(testdata, "agents"))); err != nil {
		t.Fatal(err)
	}
	gentry(t, bin, shop, "library", "apply")
	if err := os.CopyFS(filepath.Join(process, "shop", "flow"), os.DirFS(filepath.Join(testdata, "shop", "flow"))); err != nil {
		t.Fatal(err)
	}
	gentry(t, bin, shop, "flow", "apply")
	worktrees := []string{shop}
	for i := 2; i <= n; i++ {
		w := gittest.Worktree(t, shop, filepath.Join(root, fmt.Sprintf("shop-%d", i)), fmt.Sprintf("w%d", i))
		gentry(t, bin, shop, "worktree", "add", w)
		worktrees = append(worktrees, w)
	}
	return worktrees
}

// gentry runs a command of the binary in dir that must succeed.
func gentry(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gentry %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// result is the outcome of one process of takeAtOnce.
type result struct {
	code   int
	stdout string
}

// takeAtOnce starts a process of task take for each worktree at once and
// waits for all of them.
func takeAtOnce(t *testing.T, bin string, worktrees []string) []result {
	t.Helper()
	results := make([]result, len(worktrees))
	cmds := make([]*exec.Cmd, len(worktrees))
	outs := make([]strings.Builder, len(worktrees))
	for i, w := range worktrees {
		cmds[i] = exec.Command(bin, "task", "take", "--scenario", "feature", "--title", fmt.Sprintf("Задача %d", i),
			"--statement", "Постановка.", "--worktree", w, "--json")
		cmds[i].Stdout = &outs[i]
	}
	var wg sync.WaitGroup
	for i, c := range cmds {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Wait()
			results[i] = result{code: c.ProcessState.ExitCode(), stdout: outs[i].String()}
		}()
	}
	wg.Wait()
	return results
}

// TestTakeOneWorktreeAtOnce checks that of several processes taking a task in
// one worktree at once exactly one does; the others are refused.
func TestTakeOneWorktreeAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop := shopWithFlow(t, bin, 1)[0]
	same := make([]string, writers)
	for i := range same {
		same[i] = shop
	}
	taken := 0
	for _, r := range takeAtOnce(t, bin, same) {
		switch {
		case r.code == 0:
			taken++
		case r.code != 1 || !strings.Contains(r.stdout, `"code":"worktree_busy"`):
			t.Errorf("exit code %d, output %s; want worktree_busy", r.code, r.stdout)
		}
	}
	if taken != 1 {
		t.Errorf("%d processes took a task, want 1", taken)
	}
	out := gentry(t, bin, shop, "task", "list", "--all", "--json")
	var list struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Tasks) != 1 {
		t.Errorf("%d tasks recorded, want 1: %s", len(list.Tasks), out)
	}
}

// TestTakeManyWorktreesAtOnce checks that processes taking tasks in different
// worktrees at once all do, with the numbers 1 to n without repeats.
func TestTakeManyWorktreesAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	worktrees := shopWithFlow(t, bin, writers)
	var ids []string
	for _, r := range takeAtOnce(t, bin, worktrees) {
		var out struct {
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil {
			t.Fatalf("exit code %d, output %s", r.code, r.stdout)
		}
		ids = append(ids, out.Task.ID)
	}
	slices.Sort(ids)
	var want []string
	for i := 1; i <= writers; i++ {
		want = append(want, fmt.Sprintf("SHOP-%d", i))
	}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("numbers %v, want %v", ids, want)
	}
}
