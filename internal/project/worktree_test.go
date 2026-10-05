package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/state"
)

func (f *fixture) addWorktree(dir, path, project string) (WorktreeResult, error) {
	return AddWorktree(f.st, WorktreeRequest{Dir: dir, Path: path, Project: project})
}

func (f *fixture) pool(t *testing.T) []state.Worktree {
	t.Helper()
	ws, err := f.st.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestAddWorktreeAfterConnect(t *testing.T) {
	f := newFixture(t)
	f.mustAdd(t, f.shop, "shop", f.path("shop-knowledge"), "")
	two := gittest.Worktree(t, f.shop, f.path("shop-2"), "two")

	// From the worktree itself: the project is that of the main worktree.
	res, err := f.addWorktree(two, "", "")
	if err != nil || res.Unchanged || res.Worktree.Path != two || res.Worktree.Project != "shop" || res.Worktree.Main {
		t.Fatalf("got %+v, %v", res, err)
	}
	events := f.events(t)
	if len(events) != 3 || events[2].Type != EventWorktreeAdded || events[2].Project != "shop" {
		t.Fatalf("events %+v", events)
	}
	var data contract.WorktreeAddedData
	json.Unmarshal(events[2].Data, &data)
	if data != (contract.WorktreeAddedData{Path: two}) {
		t.Errorf("worktree.added: %s", events[2].Data)
	}

	// Again from a directory inside it: the root is named, nothing changes.
	sub := filepath.Join(two, "src")
	os.MkdirAll(sub, 0o755)
	res, err = f.addWorktree(sub, "", "")
	if err != nil || !res.Unchanged || res.Worktree.Path != two || res.Worktree.Project != "shop" {
		t.Errorf("repeat: %+v, %v", res, err)
	}
	if len(f.events(t)) != 3 {
		t.Errorf("repeat wrote events: %+v", f.events(t))
	}

	// By a relative path from outside any project.
	three := gittest.Worktree(t, f.shop, f.path("shop-3"), "three")
	res, err = f.addWorktree(f.root, "shop-3", "")
	if err != nil || res.Worktree.Path != three || res.Worktree.Project != "shop" {
		t.Errorf("relative path: %+v, %v", res, err)
	}
}

func TestAddWorktreeClone(t *testing.T) {
	f := newFixture(t)
	f.mustAdd(t, f.shop, "shop", f.path("shop-knowledge"), "")
	clone := f.path("shop-clone")
	gittest.Run(t, f.root, "clone", "--quiet", f.shop, clone)

	var ue *UndeterminedError
	if _, err := f.addWorktree(clone, "", ""); !errors.As(err, &ue) || ue.Dir != clone {
		t.Errorf("clone without a project: got %v, want UndeterminedError for %s", err, clone)
	}
	var nf *NotFoundError
	if _, err := f.addWorktree(clone, "", "nope"); !errors.As(err, &nf) || nf.Project != "nope" {
		t.Errorf("unknown project: got %v, want NotFoundError", err)
	}
	res, err := f.addWorktree(clone, "", "shop")
	if err != nil || res.Unchanged || res.Worktree.Project != "shop" {
		t.Errorf("clone with --project: %+v, %v", res, err)
	}

	// A clone added from a worktree of the project takes its project.
	other := f.path("shop-clone2")
	gittest.Run(t, f.root, "clone", "--quiet", f.shop, other)
	res, err = f.addWorktree(f.shop, other, "")
	if err != nil || res.Worktree.Path != other || res.Worktree.Project != "shop" {
		t.Errorf("clone from the project directory: %+v, %v", res, err)
	}
	if len(f.pool(t)) != 3 {
		t.Errorf("pool %+v", f.pool(t))
	}
}

func TestAddWorktreeRefusals(t *testing.T) {
	f := newFixture(t)
	know := f.path("shop-knowledge")
	f.mustAdd(t, f.shop, "shop", know, "")
	cart := gittest.Repo(t, f.path("cart"))
	f.mustAdd(t, cart, "cart", f.path("cart-knowledge"), "")
	plain := f.path("plain")
	os.MkdirAll(filepath.Join(know, "notes"), 0o755)
	os.MkdirAll(plain, 0o755)
	before := len(f.events(t))

	tests := []struct {
		name          string
		dir, path, id string
		check         func(error) bool
	}{
		{"knowledge", know, "", "", func(err error) bool {
			var e *InvalidWorktreeError
			return errors.As(err, &e) && e.Path == know && e.Project == "shop"
		}},
		{"inside the knowledge", f.shop, filepath.Join(know, "notes"), "", func(err error) bool {
			var e *InvalidWorktreeError
			return errors.As(err, &e) && e.Path == know
		}},
		{"not a repository", plain, "", "shop", isA[*NotRepoError]},
		{"no directory", f.shop, f.path("missing"), "shop", isA[*NotRepoError]},
		{"another pool", f.shop, "", "cart", func(err error) bool {
			var e *WorktreeTakenError
			return errors.As(err, &e) && e.Project == "shop" && e.Path == f.shop
		}},
	}
	for _, tt := range tests {
		if _, err := f.addWorktree(tt.dir, tt.path, tt.id); !tt.check(err) {
			t.Errorf("%s: unexpected error %v", tt.name, err)
		}
	}
	if got := len(f.events(t)); got != before {
		t.Errorf("refusals wrote %d events", got-before)
	}
}

func TestAddWorktreePathCase(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("letter case matters in paths on this system")
	}
	f := newFixture(t)
	f.mustAdd(t, f.shop, "shop", f.path("shop-knowledge"), "")
	two := gittest.Worktree(t, f.shop, f.path("shop-2"), "two")
	f.addWorktree(two, "", "")
	res, err := f.addWorktree(f.root, strings.ToUpper(two), "")
	if err != nil || !res.Unchanged || res.Worktree.Path != two {
		t.Errorf("the same path in another case: %+v, %v", res, err)
	}
}

func TestAddWorktreeConcurrent(t *testing.T) {
	f := newFixture(t)
	f.mustAdd(t, f.shop, "shop", f.path("shop-knowledge"), "")
	two := gittest.Worktree(t, f.shop, f.path("shop-2"), "two")
	const n = 6

	var wg sync.WaitGroup
	errs := make([]error, n)
	added := make([]bool, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := AddWorktree(f.open(t), WorktreeRequest{Dir: two})
			errs[i], added[i] = err, !res.Unchanged
		}()
	}
	wg.Wait()
	count := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("attempt %d: %v", i, errs[i])
		}
		if added[i] {
			count++
		}
	}
	if count != 1 || len(f.events(t)) != 3 {
		t.Errorf("added %d times, %d events; want once", count, len(f.events(t)))
	}
}

func TestListWorktrees(t *testing.T) {
	f := newFixture(t)
	fix := gittest.Worktree(t, f.shop, f.path("shop-fix"), "fix/refund")
	f.mustAdd(t, fix, "shop", f.path("shop-knowledge"), "")
	two := gittest.Worktree(t, f.shop, f.path("shop-2"), "two")
	gittest.Run(t, two, "checkout", "--quiet", "--detach")
	gone := gittest.Worktree(t, f.shop, f.path("shop-gone"), "gone")
	f.addWorktree(two, "", "")
	f.addWorktree(gone, "", "")
	os.RemoveAll(gone)
	cart := gittest.Repo(t, f.path("cart"))
	f.mustAdd(t, cart, "cart", f.path("cart-knowledge"), "")

	projects, _ := f.st.Projects()
	worktrees := f.pool(t)
	list := func(dir, flag string) string {
		t.Helper()
		ws, err := ListWorktrees(projects, worktrees, dir, flag)
		if err != nil {
			t.Fatalf("ListWorktrees(%s, %q): %v", dir, flag, err)
		}
		var lines []string
		for _, w := range ws {
			lines = append(lines, strings.Join([]string{w.Project, filepath.Base(w.Path),
				map[bool]string{true: "main", false: "-"}[w.Main],
				map[bool]string{true: "exists", false: "gone"}[w.Exists], w.Branch}, " "))
		}
		return strings.Join(lines, "\n")
	}

	shopWant := "shop shop main exists main\nshop shop-2 - exists \nshop shop-fix - exists fix/refund\nshop shop-gone - gone "
	sub := filepath.Join(fix, "src")
	os.MkdirAll(sub, 0o755)
	if got := list(sub, ""); got != shopWant {
		t.Errorf("in a worktree of shop:\n%s\nwant:\n%s", got, shopWant)
	}
	if got := list(f.path("shop-knowledge"), ""); got != shopWant {
		t.Errorf("in the knowledge of shop:\n%s", got)
	}
	if got, want := list(f.root, ""), "cart cart main exists main\n"+shopWant; got != want {
		t.Errorf("outside projects:\n%s\nwant:\n%s", got, want)
	}
	if got := list(fix, "cart"); got != "cart cart main exists main" {
		t.Errorf("--project cart:\n%s", got)
	}
	if _, err := ListWorktrees(projects, worktrees, f.root, "nope"); !isA[*NotFoundError](err) {
		t.Errorf("unknown project: got %v", err)
	}
}

func TestResolve(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"dev", "x")
	if runtime.GOOS == "windows" {
		root = `C:\dev\x`
	}
	at := func(name string) string { return filepath.Join(root, name) }
	t.Setenv(home.EnvVar, at("home"))
	process := filepath.Join(at("home"), "process")
	projects := []state.Project{
		{ID: "cart", Knowledge: at("cart-knowledge")},
		{ID: "shop", Knowledge: at("shop-knowledge")},
	}
	worktrees := []state.Worktree{
		{Path: at("shop"), Project: "shop", Main: true},
		// A worktree kept inside another one.
		{Path: filepath.Join(at("shop"), ".worktrees", "cart-fix"), Project: "cart"},
	}
	tests := []struct {
		dir, flag, want string
	}{
		{filepath.Join(at("shop"), "src"), "", "shop"},
		{filepath.Join(at("shop"), ".worktrees", "cart-fix", "src"), "", "cart"},
		{filepath.Join(at("shop-knowledge"), "notes"), "", "shop"},
		{filepath.Join(process, "cart", "flow", "stages"), "", "cart"},
		{filepath.Join(process, "shop"), "", "shop"},
		{at("shop"), "cart", "cart"},
	}
	for _, tt := range tests {
		p, err := Resolve(projects, worktrees, tt.dir, tt.flag)
		if err != nil || p.ID != tt.want {
			t.Errorf("Resolve(%s, %q) = %q, %v; want %q", tt.dir, tt.flag, p.ID, err, tt.want)
		}
	}
	// A directory next to a worktree with a common name prefix is outside it.
	if _, err := Resolve(projects, worktrees, at("shop-2"), ""); !isA[*UndeterminedError](err) {
		t.Errorf("shop-2: got %v, want UndeterminedError", err)
	}
	// The shared subagents belong to no project.
	if _, err := Resolve(projects, worktrees, filepath.Join(process, "agents"), ""); !isA[*UndeterminedError](err) {
		t.Errorf("shared subagents: got %v, want UndeterminedError", err)
	}
	if _, err := Resolve(projects, worktrees, at("shop"), "nope"); !isA[*NotFoundError](err) {
		t.Errorf("unknown project: got %v, want NotFoundError", err)
	}
}
