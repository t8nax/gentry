package clitest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
)

// work is the directory of the shop of the running test. The tests of a
// package run one after another, so they take turns in it.
var work string

// layers are the shop as each fixture leaves it, built by the first test that
// needs it and copied into work for the next ones. The copy is at the same
// place, so the absolute paths in git and in the state store stay true, and
// a test runs no git to prepare the shop.
var layers struct {
	dir   string
	saved map[string]layer
}

// layer is a saved shop and the data root and the current directory of the
// test that built it.
type layer struct{ snapshot, home, wd string }

// Layer gives the test the shop of fixture name: a copy of the saved one,
// or one made by build and saved for the next tests.
func Layer(t *testing.T, name string, build func()) {
	t.Helper()
	if l, ok := layers.saved[name]; ok {
		cleanWork(t)
		if err := os.CopyFS(work, os.DirFS(l.snapshot)); err != nil {
			t.Fatal(err)
		}
		t.Setenv(home.EnvVar, l.home)
		t.Chdir(l.wd)
		return
	}
	build()
	if t.Failed() {
		return
	}
	l := layer{snapshot: filepath.Join(layers.dir, name), home: os.Getenv(home.EnvVar), wd: MustWd(t)}
	if err := os.CopyFS(l.snapshot, os.DirFS(work)); err != nil {
		t.Fatal(err)
	}
	if layers.saved == nil {
		layers.saved = map[string]layer{}
	}
	layers.saved[name] = l
}

// cleanWork empties work for the test and removes the shop once the test
// ends. The removal is registered before the test changes the current
// directory, so it runs after the directory is restored.
func cleanWork(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
}

// ShopDir makes a shop repository with a secondary worktree, gives the test a
// data root of its own and returns the directory of them.
func ShopDir(t *testing.T) string {
	t.Helper()
	Layer(t, "dir", func() {
		cleanWork(t)
		t.Setenv(home.EnvVar, filepath.Join(work, "home"))
		shop := gittest.Repo(t, filepath.Join(work, "shop"))
		gittest.Worktree(t, shop, filepath.Join(work, "shop-fix"), "fix")
	})
	return work
}

// ConnectedShop connects the shop of ShopDir from its main worktree and
// returns the directory of ShopDir. The test runs in the main worktree.
func ConnectedShop(t *testing.T) string {
	t.Helper()
	Layer(t, "connected", func() {
		ShopDir(t)
		t.Chdir(filepath.Join(work, "shop"))
		if code, _, stderr := Run("project", "add", "shop", "--knowledge", "../shop-knowledge"); code != contract.ExitOK {
			t.Fatalf("project add: %s", stderr)
		}
	})
	return work
}

// ShopPlaces returns the places of the flow of the shop in the process
// repository of the data root.
func ShopPlaces() flow.Places {
	process := filepath.Join(os.Getenv(home.EnvVar), "process")
	return flow.Places{
		Project:  "shop",
		Dir:      filepath.Join(process, "shop", "flow"),
		Library:  filepath.Join(process, "agents"),
		Conflict: filepath.Join(process, "shop", "conflict"),
	}
}

// EmptyShopFlow connects the shop and applies the library of the flow
// testdata; the shop has no flow. It returns the places of the flow. The test
// runs in the main worktree of the shop.
func EmptyShopFlow(t *testing.T) flow.Places {
	t.Helper()
	Layer(t, "library", func() {
		ConnectedShop(t)
		if err := os.CopyFS(ShopPlaces().Library, os.DirFS(filepath.Join(flowTestdata, "agents"))); err != nil {
			t.Fatal(err)
		}
		MustRun(t, "library", "apply")
	})
	return ShopPlaces()
}

// ShopFlow connects the shop and applies the flow of the flow testdata. It
// returns the places of the flow.
func ShopFlow(t *testing.T) flow.Places {
	t.Helper()
	Layer(t, "flow", func() {
		CopyShopFlow(t, EmptyShopFlow(t))
		MustRun(t, "flow", "apply")
	})
	return ShopPlaces()
}

// CopyShopFlow puts the flow of the flow testdata into the flow directory.
func CopyShopFlow(t *testing.T, p flow.Places) {
	t.Helper()
	if err := os.RemoveAll(p.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(p.Dir, os.DirFS(filepath.Join(flowTestdata, "shop", "flow"))); err != nil {
		t.Fatal(err)
	}
}

// flowTestdata is the process directory of the flow testdata.
var flowTestdata = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "flow", "testdata", "process")
}()

// WriteDraft writes files of the flow directory by path inside it; an empty
// text removes the file.
func WriteDraft(t *testing.T, p flow.Places, files map[string]string) {
	t.Helper()
	WriteFiles(t, p.Dir, files)
}

// WriteFiles writes files of directory dir by path inside it; an empty text
// removes the file.
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if text == "" {
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ShopTables are the tables of flow show for the flow of the flow testdata.
const ShopTables = `СЦЕНАРИЙ  НАЗВАНИЕ
bug       Баг
feature   Фича

ЭТАП            НАЗВАНИЕ    ИСПОЛНИТЕЛЬ   ВЫХОД
branch          Ветка       orchestrator  создана ветка задачи
implementation  Реализация  orchestrator  изменения сделаны, тесты проходят
merge           Слияние     operator      ветка задачи влита в main
plan-bug        План бага   orchestrator  причина установлена, план исправления согласован
plan-feature    План фичи   orchestrator  план согласован с оператором
review          Ревью       reviewer      замечания ревью записаны и разобраны

СУБАГЕНТ  ИСТОЧНИК    ЭТАПЫ
reviewer  библиотека  review
`

// Statement is the statement of the example of the plan.
const Statement = "Клиент возвращает часть заказа. Деньги должны вернуться на карту, которой он платил."

// TaskShop applies the flow of the shop and adds shop-fix to its pool. The
// test runs in the main worktree; the calls are the operator's. It returns
// the main worktree and shop-fix.
func TaskShop(t *testing.T) (shop, fix string) {
	t.Helper()
	t.Setenv(caller.SessionEnv, "")
	t.Setenv(caller.ClaudeCodeEnv, "")
	shop, fix = filepath.Join(work, "shop"), filepath.Join(work, "shop-fix")
	Layer(t, "pool", func() {
		ShopFlow(t)
		MustRun(t, "worktree", "add", fix)
	})
	return shop, fix
}

// TakeArgs are the flags of a task of the feature scenario.
func TakeArgs(extra ...string) []string {
	return append([]string{"task", "take", "--scenario", "feature", "--title", "Частичный возврат по карте", "--statement", Statement}, extra...)
}
