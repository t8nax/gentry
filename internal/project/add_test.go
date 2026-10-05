package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
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

// fixture is a shop repository and a state store in a temporary directory.
type fixture struct {
	root, shop string
	statePath  string
	st         *state.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := paths.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, shop: gittest.Repo(t, filepath.Join(root, "shop")), statePath: filepath.Join(root, "state.db")}
	f.st = f.open(t)
	return f
}

func (f *fixture) open(t *testing.T) *state.Store {
	t.Helper()
	st, err := state.Open(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func (f *fixture) path(name string) string { return filepath.Join(f.root, name) }

func (f *fixture) add(dir, id, knowledge, prefix string) (AddResult, error) {
	return Add(f.st, AddRequest{Dir: dir, ID: id, Knowledge: knowledge, Prefix: prefix})
}

func (f *fixture) mustAdd(t *testing.T, dir, id, knowledge, prefix string) AddResult {
	t.Helper()
	res, err := f.add(dir, id, knowledge, prefix)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return res
}

func (f *fixture) events(t *testing.T) []state.Event {
	t.Helper()
	events, err := f.st.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// commits returns the subjects of the commits of the repository in dir.
func commits(t *testing.T, dir string) []string {
	t.Helper()
	out := strings.TrimSpace(gittest.Run(t, dir, "log", "--format=%s"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestAddCreatesKnowledge(t *testing.T) {
	f := newFixture(t)
	gittest.Worktree(t, f.shop, f.path("shop-fix"), "fix")
	know := f.path("shop-knowledge")

	res := f.mustAdd(t, filepath.Join(f.shop), "shop", filepath.Join("..", "shop-knowledge"), "")
	want := AddResult{
		Project:          state.Project{ID: "shop", Prefix: "SHOP", Knowledge: know, MainWorktree: f.shop},
		KnowledgeCreated: true,
		RepoCreated:      true,
		Added:            []string{f.shop},
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v, want %+v", res, want)
	}

	info, err := ReadInfo(know)
	if err != nil || info != (Info{Format: KnowledgeFormat, Project: "shop", Language: "ru"}) {
		t.Errorf("gentry.yaml: %+v, %v", info, err)
	}
	if got := commits(t, know); !reflect.DeepEqual(got, []string{"Подключение проекта shop к Gentry"}) {
		t.Errorf("commits %q", got)
	}
	if out := gittest.Run(t, know, "status", "--porcelain"); out != "" {
		t.Errorf("knowledge left uncommitted: %q", out)
	}

	events := f.events(t)
	if len(events) != 2 || events[0].Type != EventProjectAdded || events[1].Type != EventWorktreeAdded {
		t.Fatalf("events %+v", events)
	}
	var pa contract.ProjectAddedData
	json.Unmarshal(events[0].Data, &pa)
	if pa != (contract.ProjectAddedData{Knowledge: know, Prefix: "SHOP", KnowledgeCreated: true}) || events[0].Project != "shop" {
		t.Errorf("project.added: %+v", events[0])
	}
	var wa contract.WorktreeAddedData
	json.Unmarshal(events[1].Data, &wa)
	if wa != (contract.WorktreeAddedData{Path: f.shop, Main: true}) {
		t.Errorf("worktree.added: %s", events[1].Data)
	}

	projects, _ := f.st.Projects()
	if len(projects) != 1 || projects[0].MainWorktree != f.shop || projects[0].Prefix != "SHOP" {
		t.Errorf("projects %+v", projects)
	}
}

func TestAddFromSecondaryWorktree(t *testing.T) {
	f := newFixture(t)
	fix := gittest.Worktree(t, f.shop, f.path("shop-fix"), "fix")
	two := gittest.Worktree(t, f.shop, f.path("shop-2"), "two")

	// From a subdirectory: the worktree root is taken.
	sub := filepath.Join(fix, "src")
	os.MkdirAll(sub, 0o755)
	res := f.mustAdd(t, sub, "shop", f.path("shop-knowledge"), "")
	if !reflect.DeepEqual(res.Added, []string{f.shop, fix}) || res.Project.MainWorktree != f.shop {
		t.Errorf("added %q, main %q; want the main worktree and the current one", res.Added, res.Project.MainWorktree)
	}
	if len(f.events(t)) != 3 {
		t.Errorf("want project.added and two worktree.added, got %+v", f.events(t))
	}

	// Again from the third worktree: nothing changes, it is not added.
	res = f.mustAdd(t, two, "", f.path("shop-knowledge"), "")
	if !res.Unchanged || len(res.Added) != 0 {
		t.Errorf("repeat: %+v", res)
	}
	if len(f.events(t)) != 3 {
		t.Errorf("repeat wrote events: %+v", f.events(t))
	}
}

func TestAddExistingKnowledge(t *testing.T) {
	f := newFixture(t)
	know := gittest.Repo(t, f.path("shop-knowledge"))
	if err := WriteInfo(know, Info{Format: 1, Project: "shop", Language: "en"}); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, know, "add", InfoFile)
	gittest.Run(t, know, "commit", "--quiet", "-m", "colleague")

	if _, err := f.add(f.shop, "store", know, ""); !errors.As(err, new(*MismatchError)) {
		t.Errorf("another identifier: got %v, want MismatchError", err)
	}
	res := f.mustAdd(t, f.shop, "", know, "SH")
	if res.KnowledgeCreated || res.RepoCreated || res.Project.ID != "shop" || res.Project.Prefix != "SH" {
		t.Errorf("got %+v", res)
	}
	if got := commits(t, know); len(got) != 2 {
		t.Errorf("knowledge of a colleague must not change, commits %q", got)
	}
}

func TestAddFreshRepository(t *testing.T) {
	f := newFixture(t)
	know := gittest.Repo(t, f.path("shop-knowledge"))
	for _, name := range []string{"README.md", "LICENSE", ".gitignore"} {
		os.WriteFile(filepath.Join(know, name), []byte("x\n"), 0o644)
	}
	gittest.Run(t, know, "add", ".")
	gittest.Run(t, know, "commit", "--quiet", "-m", "Initial commit")

	res := f.mustAdd(t, f.shop, "shop", know, "")
	if !res.KnowledgeCreated || res.RepoCreated {
		t.Errorf("got %+v, want knowledge created in the existing repository", res)
	}
	if got := commits(t, know); len(got) != 3 || got[0] != msg.Knowledge("ru", msg.CommitProjectAdded, "shop") {
		t.Errorf("commits %q", got)
	}
}

func TestAddFinishesFailedCommit(t *testing.T) {
	f := newFixture(t)
	know := f.path("shop-knowledge")
	if err := git.Init(know); err != nil {
		t.Fatal(err)
	}
	// An empty name makes git refuse to commit, as on a machine without one.
	gittest.Run(t, know, "config", "user.name", "")

	_, err := f.add(f.shop, "shop", know, "")
	if !errors.As(err, new(*git.CommandError)) {
		t.Fatalf("got %v, want a failed git command", err)
	}
	if projects, _ := f.st.Projects(); len(projects) != 0 || len(f.events(t)) != 0 {
		t.Errorf("a failed connection must leave no record: %+v", projects)
	}

	gittest.Run(t, know, "config", "--unset", "user.name")
	res := f.mustAdd(t, f.shop, "", know, "")
	if !res.KnowledgeCreated || res.Project.ID != "shop" {
		t.Errorf("got %+v", res)
	}
	if got := commits(t, know); len(got) != 1 {
		t.Errorf("commits %q", got)
	}
}

func TestAddEnglishKnowledge(t *testing.T) {
	f := newFixture(t)
	know := f.path("shop-knowledge")
	git.Init(know)
	WriteInfo(know, Info{Format: 1, Project: "shop", Language: "en"})
	// The first commit failed earlier: the next connection makes it in English.
	f.mustAdd(t, f.shop, "", know, "")
	if got := commits(t, know); !reflect.DeepEqual(got, []string{"Connect project shop to Gentry"}) {
		t.Errorf("commits %q", got)
	}
}

func TestAddRefusals(t *testing.T) {
	f := newFixture(t)
	full := f.path("full")
	os.MkdirAll(full, 0o755)
	os.WriteFile(filepath.Join(full, "notes.txt"), []byte("x"), 0o644)
	foreign := gittest.Repo(t, f.path("foreign"))
	os.WriteFile(filepath.Join(foreign, "main.go"), []byte("package main"), 0o644)
	bad := func(name, content string) string {
		dir := gittest.Repo(t, f.path(name))
		os.WriteFile(filepath.Join(dir, InfoFile), []byte(content), 0o644)
		return dir
	}

	tests := []struct {
		name      string
		dir       string
		id        string
		knowledge string
		check     func(error) bool
	}{
		{"outside git", f.root, "shop", "k", isA[*NotRepoError]},
		{"inside the code", f.shop, "shop", filepath.Join(f.shop, "knowledge"), reason(ReasonNested)},
		{"around the code", f.shop, "shop", f.root, reason(ReasonNested)},
		{"not empty", f.shop, "shop", full, reason(ReasonNotEmpty)},
		{"foreign repository", f.shop, "shop", foreign, reason(ReasonForeignRepo)},
		{"no identifier", f.shop, "", f.path("new"), isA[*IDMissingError]},
		{"no default prefix", f.shop, "online-store-backend", f.path("new"), isA[*PrefixUnderivableError]},
		{"syntax", f.shop, "", bad("syntax", "format: [1\n"), reason(ReasonBadFile)},
		{"unknown key", f.shop, "", bad("unknown", "format: 1\nproject: shop\nlanguage: ru\nprefix: SHOP\n"), reason(ReasonBadFile)},
		{"no project", f.shop, "", bad("noproject", "format: 1\nlanguage: ru\n"), reason(ReasonBadFile)},
		{"bad language", f.shop, "", bad("lang", "format: 1\nproject: shop\nlanguage: de\n"), reason(ReasonBadFile)},
		{"newer format", f.shop, "", bad("newer", "format: 2\nproject: shop\nlanguage: ru\nsomething: new\n"), isA[*NewerFormatError]},
	}
	for _, tt := range tests {
		_, err := f.add(tt.dir, tt.id, tt.knowledge, "")
		if !tt.check(err) {
			t.Errorf("%s: unexpected error %v", tt.name, err)
		}
	}
	if projects, _ := f.st.Projects(); len(projects) != 0 {
		t.Errorf("refusals recorded projects: %+v", projects)
	}
	if _, err := os.Stat(f.path("new")); err == nil {
		t.Error("a refusal created the knowledge directory")
	}
}

func isA[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

func reason(r string) func(error) bool {
	return func(err error) bool {
		var ke *KnowledgeError
		return errors.As(err, &ke) && ke.Reason == r
	}
}

func TestAddConflicts(t *testing.T) {
	f := newFixture(t)
	know := f.path("shop-knowledge")
	f.mustAdd(t, f.shop, "shop", know, "")

	clone := f.path("shop-clone")
	gittest.Run(t, f.root, "clone", "--quiet", f.shop, clone)
	var ee *ExistsError
	if _, err := f.add(clone, "", know, ""); !errors.As(err, &ee) || !ee.Clone || ee.MainWorktree != f.shop {
		t.Errorf("separate clone: got %v, want ExistsError for the main worktree", err)
	}

	// The same identifier with knowledge elsewhere.
	other := gittest.Repo(t, f.path("other-knowledge"))
	WriteInfo(other, Info{Format: 1, Project: "shop", Language: "ru"})
	gittest.Run(t, other, "add", InfoFile)
	gittest.Run(t, other, "commit", "--quiet", "-m", "x")
	if _, err := f.add(clone, "", other, ""); !errors.As(err, &ee) || ee.Clone || ee.Knowledge != know {
		t.Errorf("other knowledge: got %v, want ExistsError for the knowledge", err)
	}

	cart := gittest.Repo(t, f.path("cart"))
	var pt *PrefixTakenError
	if _, err := f.add(cart, "cart", f.path("cart-knowledge"), "SHOP"); !errors.As(err, &pt) || pt.Project != "shop" {
		t.Errorf("prefix taken: got %v", err)
	}
	var wt *WorktreeTakenError
	if _, err := f.add(f.shop, "store", f.path("store-knowledge"), ""); !errors.As(err, &wt) || wt.Project != "shop" {
		t.Errorf("worktree taken: got %v", err)
	}
	if len(f.events(t)) != 2 {
		t.Errorf("conflicts wrote events: %+v", f.events(t))
	}
}

func TestAddPathCase(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("letter case matters in paths on this system")
	}
	f := newFixture(t)
	f.mustAdd(t, f.shop, "shop", f.path("shop-knowledge"), "")
	res, err := f.add(strings.ToUpper(f.shop), "", strings.ToUpper(f.path("shop-knowledge")), "")
	if err != nil || !res.Unchanged {
		t.Errorf("the same paths in another case: %+v, %v", res, err)
	}
}

func TestAddConcurrent(t *testing.T) {
	f := newFixture(t)
	const n = 6

	// Different projects at once: all are connected.
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		repo := gittest.Repo(t, f.path(fmt.Sprintf("repo%d", i)))
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := f.open(t)
			id := "shop" + strings.Repeat("x", i)
			_, errs[i] = Add(st, AddRequest{Dir: repo, ID: id, Knowledge: f.path(id + "-knowledge")})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("project %d: %v", i, err)
		}
	}
	if got := len(f.events(t)); got != 2*n {
		t.Errorf("%d events, want %d", got, 2*n)
	}

	// One project with ready knowledge at once: connected exactly once.
	know := gittest.Repo(t, f.path("cart-knowledge"))
	WriteInfo(know, Info{Format: 1, Project: "cart", Language: "ru"})
	gittest.Run(t, know, "add", InfoFile)
	gittest.Run(t, know, "commit", "--quiet", "-m", "x")
	cart := gittest.Repo(t, f.path("cart"))
	added := make([]bool, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := Add(f.open(t), AddRequest{Dir: cart, Knowledge: know})
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
	if count != 1 || len(f.events(t)) != 2*n+2 {
		t.Errorf("connected %d times, %d events; want once", count, len(f.events(t)))
	}
}

func TestDefaultPrefix(t *testing.T) {
	tests := map[string]string{"shop": "SHOP", "my-shop": "MYSHOP", "shop2": "SHOP", "online-store-backend": "", "a1": ""}
	for id, want := range tests {
		got, ok := DefaultPrefix(id)
		if ok != (want != "") || ok && got != want {
			t.Errorf("DefaultPrefix(%q) = %q, %v; want %q", id, got, ok, want)
		}
	}
}
