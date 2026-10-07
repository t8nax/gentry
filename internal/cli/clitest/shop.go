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
	"github.com/t8nax/gentry/internal/paths"
)

// ShopDir makes a shop repository with a secondary worktree in a temporary
// directory, gives the test a data root of its own and returns the directory.
func ShopDir(t *testing.T) string {
	t.Helper()
	root, err := paths.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, filepath.Join(root, "home"))
	shop := gittest.Repo(t, filepath.Join(root, "shop"))
	gittest.Worktree(t, shop, filepath.Join(root, "shop-fix"), "fix")
	return root
}

// ConnectedShop connects the shop of ShopDir from its main worktree and
// returns the directory of ShopDir.
func ConnectedShop(t *testing.T) string {
	t.Helper()
	root := ShopDir(t)
	t.Chdir(filepath.Join(root, "shop"))
	if code, _, stderr := Run("project", "add", "shop", "--knowledge", "../shop-knowledge"); code != contract.ExitOK {
		t.Fatalf("project add: %s", stderr)
	}
	return root
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
	ConnectedShop(t)
	p := ShopPlaces()
	if err := os.CopyFS(p.Library, os.DirFS(filepath.Join(flowTestdata, "agents"))); err != nil {
		t.Fatal(err)
	}
	MustRun(t, "library", "apply")
	return p
}

// ShopFlow connects the shop and applies the flow of the flow testdata. It
// returns the places of the flow.
func ShopFlow(t *testing.T) flow.Places {
	t.Helper()
	p := EmptyShopFlow(t)
	CopyShopFlow(t, p)
	MustRun(t, "flow", "apply")
	return p
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
	t.Setenv(caller.ClaudeSessionEnv, "")
	ShopFlow(t)
	root := filepath.Dir(MustWd(t))
	shop, fix = filepath.Join(root, "shop"), filepath.Join(root, "shop-fix")
	MustRun(t, "worktree", "add", fix)
	return shop, fix
}

// TakeArgs are the flags of a task of the feature scenario.
func TakeArgs(extra ...string) []string {
	return append([]string{"task", "take", "--scenario", "feature", "--title", "Частичный возврат по карте", "--statement", Statement}, extra...)
}
