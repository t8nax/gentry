package agents_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/git"
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

var (
	layout   = claude.Agents{}
	reviewer = flow.Agent{ID: "reviewer", Purpose: "ревью изменений", Capabilities: []string{"read", "search"}, Instruction: "Проверить корзину."}
	tester   = flow.Agent{ID: "tester", Purpose: "тесты", Capabilities: []string{"read", "run"}, Instruction: "Запустить тесты."}
)

// shop returns a repository with a commit and a linked worktree of it.
func shop(t *testing.T) (main, linked string) {
	t.Helper()
	root, _ := paths.Canonical(t.TempDir())
	main = gittest.Repo(t, filepath.Join(root, "shop"))
	linked = gittest.Worktree(t, main, filepath.Join(root, "shop-wt"), "wt")
	return main, linked
}

func agentFile(w, id string) string { return filepath.Join(w, ".claude", "agents", id+".md") }

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sync(t *testing.T, targets []agents.Target, pool ...string) agents.Result {
	t.Helper()
	res, err := agents.Sync(layout, targets, pool)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func clean(t *testing.T, w string) {
	t.Helper()
	if files, err := git.Changed(w); err != nil || len(files) > 0 {
		t.Errorf("git status of %s: %v, %v; want clean", w, files, err)
	}
}

func TestSyncLaysOutAndHides(t *testing.T) {
	main, linked := shop(t)
	res := sync(t, []agents.Target{{Path: linked, Agents: []flow.Agent{tester, reviewer}}}, main, linked)
	c := res.Worktrees[0]
	if !slices.Equal(c.Agents, []string{"reviewer", "tester"}) || !slices.Equal(c.Added, []string{"reviewer", "tester"}) || len(res.Conflicts) > 0 {
		t.Fatalf("first layout = %+v", res)
	}
	if got := read(t, agentFile(linked, "reviewer")); got != string(layout.File(reviewer)) {
		t.Errorf("file of reviewer = %q", got)
	}
	clean(t, linked)
	clean(t, main)

	res = sync(t, []agents.Target{{Path: linked, Agents: []flow.Agent{tester, reviewer}}}, main, linked)
	if res.Worktrees[0].Changed() {
		t.Errorf("second layout changed: %+v", res.Worktrees[0])
	}

	changed := reviewer
	changed.Instruction = "Проверить оплату."
	res = sync(t, []agents.Target{{Path: linked, Agents: []flow.Agent{changed}}}, main, linked)
	c = res.Worktrees[0]
	if !slices.Equal(c.Updated, []string{"reviewer"}) || !slices.Equal(c.Removed, []string{"tester"}) || len(c.Added) > 0 {
		t.Errorf("update and removal = %+v", c)
	}
	if _, err := os.Stat(agentFile(linked, "tester")); !os.IsNotExist(err) {
		t.Errorf("tester is still there: %v", err)
	}
	clean(t, linked)
}

func TestSyncKeepsOperatorFiles(t *testing.T) {
	main, _ := shop(t)
	own := agentFile(main, "helper")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("---\nname: helper\n---\nМой субагент.\n"), 0o644)
	res := sync(t, []agents.Target{{Path: main, Agents: nil}}, main)
	if res.Worktrees[0].Changed() {
		t.Errorf("layout changed the file of the operator: %+v", res.Worktrees[0])
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("the file of the operator is gone: %v", err)
	}
}

func TestConflicts(t *testing.T) {
	main, linked := shop(t)
	tracked := agentFile(main, "reviewer")
	os.MkdirAll(filepath.Dir(tracked), 0o755)
	os.WriteFile(tracked, []byte("проектный"), 0o644)
	gittest.Run(t, main, "add", ".claude/agents/reviewer.md")
	gittest.Run(t, main, "commit", "--quiet", "-m", "reviewer")
	own := agentFile(linked, "tester")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("личный"), 0o644)

	targets := []agents.Target{
		{Path: main, Agents: []flow.Agent{reviewer, tester}},
		{Path: linked, Agents: []flow.Agent{tester}},
	}
	got, err := agents.Check(layout, targets)
	want := []agents.Conflict{
		{Worktree: main, Agent: "reviewer", File: ".claude/agents/reviewer.md", Reason: agents.Tracked},
		{Worktree: linked, Agent: "tester", File: ".claude/agents/tester.md", Reason: agents.Foreign},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Check = %+v, %v; want %+v", got, err, want)
	}
	if _, err := os.Stat(agentFile(main, "tester")); !os.IsNotExist(err) {
		t.Error("Check wrote a file")
	}

	res := sync(t, targets, main, linked)
	if !slices.Equal(res.Conflicts, want) || !slices.Equal(res.Worktrees[0].Added, []string{"tester"}) {
		t.Errorf("Sync = %+v", res)
	}
	if read(t, tracked) != "проектный" || read(t, own) != "личный" {
		t.Error("Sync changed a file in conflict")
	}
}

func TestExcludeSharedByWorktrees(t *testing.T) {
	main, linked := shop(t)
	exclude, err := git.ExcludeFile(main)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(exclude), 0o755)
	os.WriteFile(exclude, []byte("# своё\r\n*.log\r\n"), 0o644)

	sync(t, []agents.Target{{Path: main, Agents: []flow.Agent{reviewer}}, {Path: linked, Agents: []flow.Agent{reviewer, tester}}}, main, linked)
	want := "# своё\r\n*.log\r\n" + agents.MarkPrefix + " субагенты, разложенные в рабочие копии\r\n" +
		"/.claude/agents/reviewer.md\r\n/.claude/agents/tester.md\r\n" + agents.MarkPrefix + " конец блока\r\n"
	if got := read(t, exclude); got != want {
		t.Errorf("exclude = %q, want %q", got, want)
	}

	// The main worktree drops tester: the linked one still has it hidden.
	sync(t, []agents.Target{{Path: main, Agents: nil}}, main, linked)
	clean(t, linked)
	clean(t, main)
	if got := read(t, exclude); !strings.Contains(got, "/.claude/agents/tester.md") {
		t.Errorf("exclude lost tester of the linked worktree: %q", got)
	}

	sync(t, []agents.Target{{Path: linked, Agents: nil}}, main, linked)
	if got := read(t, exclude); got != "# своё\r\n*.log\r\n" {
		t.Errorf("exclude without subagents = %q", got)
	}
}

func TestMissingWorktree(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	res := sync(t, []agents.Target{{Path: gone, Agents: []flow.Agent{reviewer}}}, gone)
	if c := res.Worktrees[0]; !c.Missing || c.Changed() {
		t.Errorf("missing worktree = %+v", c)
	}
}
