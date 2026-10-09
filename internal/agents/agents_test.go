package agents_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/filelock"
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

// fake lays out a subagent as tools/agents/<id>.txt: the mark, then its
// identifier and instruction.
type fake struct{}

func (fake) Dir() string           { return "tools/agents" }
func (fake) Name(id string) string { return id + ".txt" }
func (fake) ID(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, ".txt")
	return id, ok
}
func (fake) File(a flow.Agent) []byte {
	return []byte(agents.MarkPrefix + " laid out\n" + a.ID + "\n" + a.Instruction + "\n")
}
func (fake) Marked(content []byte) bool { return strings.HasPrefix(string(content), agents.MarkPrefix) }

var (
	layout   = fake{}
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

func agentFile(w, id string) string { return filepath.Join(w, "tools", "agents", id+".txt") }

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
	os.WriteFile(own, []byte("Мой субагент.\n"), 0o644)
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
	gittest.Run(t, main, "add", "tools/agents/reviewer.txt")
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
		{Worktree: main, Agent: "reviewer", File: "tools/agents/reviewer.txt", Reason: agents.Tracked},
		{Worktree: linked, Agent: "tester", File: "tools/agents/tester.txt", Reason: agents.Foreign},
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
		"/tools/agents/reviewer.txt\r\n/tools/agents/tester.txt\r\n" + agents.MarkPrefix + " конец блока\r\n"
	if got := read(t, exclude); got != want {
		t.Errorf("exclude = %q, want %q", got, want)
	}

	// The main worktree drops tester: the linked one still has it hidden.
	sync(t, []agents.Target{{Path: main, Agents: nil}}, main, linked)
	clean(t, linked)
	clean(t, main)
	if got := read(t, exclude); !strings.Contains(got, "/tools/agents/tester.txt") {
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

func TestTrackedMarkedFileStays(t *testing.T) {
	main, _ := shop(t)
	sync(t, []agents.Target{{Path: main, Agents: []flow.Agent{tester}}}, main)
	// The project commits the file Gentry laid out.
	gittest.Run(t, main, "add", "--force", "tools/agents/tester.txt")
	gittest.Run(t, main, "commit", "--quiet", "-m", "tester")

	res := sync(t, []agents.Target{{Path: main, Agents: nil}}, main)
	if res.Worktrees[0].Changed() {
		t.Errorf("layout changed a tracked file: %+v", res.Worktrees[0])
	}
	clean(t, main)
}

func TestBrokenWorktreeOfPool(t *testing.T) {
	main, linked := shop(t)
	broken := filepath.Join(filepath.Dir(main), "broken")
	os.MkdirAll(broken, 0o755) // in the pool, but no worktree of git
	sync(t, []agents.Target{{Path: linked, Agents: []flow.Agent{reviewer}}}, main, linked, broken)
	clean(t, linked)

	// A worktree laid out that git does not take: nothing is written.
	if _, err := agents.Sync(layout, []agents.Target{{Path: broken, Agents: []flow.Agent{reviewer}}}, []string{broken}); err == nil {
		t.Error("no error for a directory that is not a worktree")
	}
	if _, err := os.Stat(agentFile(broken, "reviewer")); !os.IsNotExist(err) {
		t.Errorf("a file is written where it cannot be hidden: %v", err)
	}
}

func TestConcurrentLayouts(t *testing.T) {
	main, linked := shop(t)
	done := make(chan error)
	for i, w := range []string{main, linked} {
		a := []flow.Agent{reviewer, tester}[i]
		go func() {
			var err error
			for range 20 {
				if _, err = agents.Sync(layout, []agents.Target{{Path: w, Agents: []flow.Agent{a}}}, []string{main, linked}); err != nil {
					break
				}
			}
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	clean(t, main)
	clean(t, linked)
}

func TestBusyBlock(t *testing.T) {
	main, _ := shop(t)
	exclude, err := git.ExcludeFile(main)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := filelock.Acquire(filepath.Join(filepath.Dir(exclude), "gentry-exclude.lock"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	wait := agents.LockWait
	agents.LockWait = 100 * time.Millisecond
	defer func() { agents.LockWait = wait }()

	_, err = agents.Sync(layout, []agents.Target{{Path: main, Agents: []flow.Agent{reviewer}}}, []string{main})
	var busy *agents.BusyError
	if !errors.As(err, &busy) || !paths.Same(busy.Path, exclude) {
		t.Fatalf("Sync with the block held: %v", err)
	}
	if _, err := os.Stat(agentFile(main, "reviewer")); !os.IsNotExist(err) {
		t.Errorf("a file is written while the block is held: %v", err)
	}
}

func TestExcludeKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits of a group on Windows")
	}
	main, _ := shop(t)
	exclude, _ := git.ExcludeFile(main)
	os.MkdirAll(filepath.Dir(exclude), 0o755)
	os.WriteFile(exclude, []byte("*.log\n"), 0o664)
	os.Chmod(exclude, 0o664)
	sync(t, []agents.Target{{Path: main, Agents: []flow.Agent{reviewer}}}, main)
	if fi, err := os.Stat(exclude); err != nil || fi.Mode().Perm() != 0o664 {
		t.Errorf("mode of info/exclude = %v, %v; want 0664", fi.Mode().Perm(), err)
	}
}
