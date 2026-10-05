package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// connectedShop connects the shop of shopDir from its main worktree and
// returns the directory of shopDir.
func connectedShop(t *testing.T) string {
	t.Helper()
	root := shopDir(t)
	t.Chdir(filepath.Join(root, "shop"))
	if code, _, stderr := run("project", "add", "shop", "--knowledge", "../shop-knowledge"); code != contract.ExitOK {
		t.Fatalf("project add: %s", stderr)
	}
	return root
}

func TestProjectAddNamesUnpooled(t *testing.T) {
	root := shopDir(t)
	shop, fix, know := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix"), filepath.Join(root, "shop-knowledge")
	t.Chdir(shop)
	_, stdout, _ := run("project", "add", "shop", "--knowledge", know)
	tail := "\n" + msg.Text(msg.ProjectUnpooled) + "\n  " + fix + "\n\n" + msg.Text(msg.HintProjectUnpooled) + "\n"
	if !strings.HasPrefix(stdout, msg.Text(msg.ProjectAdded, "shop")+"\n") || !strings.HasSuffix(stdout, msg.Text(msg.ProjectMainWorktree, shop)+"\n"+tail) {
		t.Errorf("output:\n%s", stdout)
	}
	// A repeat names them too, and stops once they are in the pool.
	_, stdout, _ = run("project", "add", "--knowledge", know)
	if want := msg.Text(msg.ProjectUnchanged, "shop") + "\n" + tail; stdout != want {
		t.Errorf("repeat: %q, want %q", stdout, want)
	}
	run("worktree", "add", fix)
	_, stdout, _ = run("project", "add", "--knowledge", know)
	if want := msg.Text(msg.ProjectUnchanged, "shop") + "\n"; stdout != want {
		t.Errorf("all in the pool: %q, want %q", stdout, want)
	}
}

func TestProjectAddCloneHint(t *testing.T) {
	root := connectedShop(t)
	clone := filepath.Join(root, "shop-clone")
	gittest.Run(t, root, "clone", "--quiet", filepath.Join(root, "shop"), clone)
	t.Chdir(clone)
	_, _, stderr := run("project", "add", "--knowledge", "../shop-knowledge")
	want := msg.Text(msg.ErrProjectClone, "shop", filepath.Join(root, "shop")) + "\n" + msg.Text(msg.HintProjectClone, "shop") + "\n"
	if stderr != want {
		t.Errorf("got %q, want %q", stderr, want)
	}
	// The hint works as given.
	if code, stdout, _ := run("worktree", "add", "--project", "shop"); code != contract.ExitOK || stdout != msg.Text(msg.WorktreeAdded, "shop", clone)+"\n" {
		t.Errorf("worktree add --project shop: exit code %d, %q", code, stdout)
	}
}

func TestWorktreeAddText(t *testing.T) {
	root := connectedShop(t)
	shop := filepath.Join(root, "shop")
	two := gittest.Worktree(t, shop, filepath.Join(root, "shop-2"), "two")

	code, stdout, stderr := run("worktree", "add", filepath.Join("..", "shop-2"))
	if want := msg.Text(msg.WorktreeAdded, "shop", two) + "\n"; code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output %q, want %q", code, stderr, stdout, want)
	}
	sub := filepath.Join(two, "src")
	os.MkdirAll(sub, 0o755)
	t.Chdir(sub)
	code, stdout, _ = run("worktree", "add")
	if want := msg.Text(msg.WorktreeUnchanged, "shop", two) + "\n"; code != contract.ExitOK || stdout != want {
		t.Errorf("repeat: exit code %d, %q, want %q", code, stdout, want)
	}
}

func TestWorktreeJSON(t *testing.T) {
	root := connectedShop(t)
	shop, fix := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix")

	code, stdout, _ := run("worktree", "add", fix, "--json")
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	validate(t, "schemas/worktree-add.json", stdout)
	if want := `{"action":"added","main":false,"path":` + jsonString(fix) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("got %s, want %s", stdout, want)
	}
	_, stdout, _ = run("worktree", "add", fix, "--json")
	validate(t, "schemas/worktree-add.json", stdout)
	if !strings.Contains(stdout, `"action":"unchanged"`) {
		t.Errorf("repeat: %s", stdout)
	}

	gittest.Run(t, fix, "checkout", "--quiet", "--detach")
	_, stdout, _ = run("worktree", "list", "--json")
	validate(t, "schemas/worktree-list.json", stdout)
	want := `{"worktrees":[{"branch":"main","exists":true,"main":true,"path":` + jsonString(shop) + `,"project":"shop"},` +
		`{"exists":true,"main":false,"path":` + jsonString(fix) + `,"project":"shop"}]}` + "\n"
	if stdout != want {
		t.Errorf("list: %s, want %s", stdout, want)
	}

	_, stdout, _ = run("events", "--json")
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	last := lines[len(lines)-1]
	if len(lines) != 3 || !strings.Contains(last, `"type":"worktree.added"`) || !strings.Contains(last, `"project":"shop"`) {
		t.Fatalf("events:\n%s", stdout)
	}
	var e struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(last), &e)
	validate(t, "schemas/events/worktree.added.json", string(e.Data))
}

func TestWorktreeListText(t *testing.T) {
	root := connectedShop(t)
	shop, fix := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix")
	run("worktree", "add", fix)
	gone := gittest.Worktree(t, shop, filepath.Join(root, "shop-gone"), "gone")
	run("worktree", "add", gone)
	os.RemoveAll(gone)
	cart := gittest.Repo(t, filepath.Join(root, "cart"))
	t.Chdir(cart)
	run("project", "add", "cart", "--knowledge", "../cart-knowledge")

	header := []string{"ПРОЕКТ", "РАБОЧАЯ КОПИЯ", "ВЕТКА", "СОСТОЯНИЕ"}
	shopRows := [][]string{
		{"shop", shop, "main", "основная, свободна"},
		{"shop", fix, "fix", "свободна"},
		{"shop", gone, "—", "папка не найдена"},
	}
	cartRow := []string{"cart", cart, "main", "основная, свободна"}

	t.Chdir(fix)
	if _, stdout, _ := run("worktree", "list"); stdout != table(append([][]string{header}, shopRows...)...) {
		t.Errorf("in shop:\n%s", stdout)
	}
	if _, stdout, _ := run("worktree", "list", "--project", "cart"); stdout != table(header, cartRow) {
		t.Errorf("--project cart:\n%s", stdout)
	}
	t.Chdir(root)
	if _, stdout, _ := run("worktree", "list"); stdout != table(append([][]string{header, cartRow}, shopRows...)...) {
		t.Errorf("outside projects:\n%s", stdout)
	}
}

func TestWorktreeListEmpty(t *testing.T) {
	root := shopDir(t)
	if _, stdout, _ := run("worktree", "list"); stdout != msg.Text(msg.WorktreesNone)+"\n" {
		t.Errorf("got %q", stdout)
	}
	if _, stdout, _ := run("worktree", "list", "--json"); stdout != `{"worktrees":[]}`+"\n" {
		t.Errorf("got %q", stdout)
	}
	if code, _, _ := run("worktree", "list", "--project", "shop"); code != contract.ExitError {
		t.Errorf("unknown project without a store: exit code %d", code)
	}
	if p, _ := state.Path(); fileExists(p) || !strings.HasPrefix(p, root) {
		t.Errorf("worktree list must not create the state store %s", p)
	}
}

func TestWorktreeErrors(t *testing.T) {
	root := connectedShop(t)
	know := filepath.Join(root, "shop-knowledge")
	clone := filepath.Join(root, "shop-clone")
	gittest.Run(t, root, "clone", "--quiet", filepath.Join(root, "shop"), clone)

	tests := []struct {
		dir  string
		args []string
		exit int
		code string
	}{
		{root, []string{"list", "--project", "nope"}, contract.ExitError, contract.CodeProjectNotFound},
		{root, []string{"list", "--project"}, contract.ExitUsage, contract.CodeFlagValue},
		{root, []string{"list", "extra"}, contract.ExitUsage, contract.CodeUnexpectedArgs},
		{know, []string{"add"}, contract.ExitError, contract.CodeWorktreeInvalid},
		{root, []string{"add", clone}, contract.ExitError, contract.CodeProjectUndetermined},
		{root, []string{"add", clone, "--project", "nope"}, contract.ExitError, contract.CodeProjectNotFound},
		{root, []string{"add"}, contract.ExitError, contract.CodeNotGitRepo},
		{root, []string{"add", "a", "b"}, contract.ExitUsage, contract.CodeUnexpectedArgs},
		{root, []string{}, contract.ExitUsage, contract.CodeMissingArgument},
		{root, []string{"move"}, contract.ExitUsage, contract.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Chdir(tt.dir)
		args := append([]string{"worktree"}, tt.args...)
		exit, stdout, stderr := run(append(args, "--json")...)
		validate(t, "schemas/error.json", stdout)
		var out contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &out)
		if exit != tt.exit || out.Error.Code != tt.code || stderr != "" {
			t.Errorf("%v: exit code %d, %s; want %d, %s", tt.args, exit, stdout, tt.exit, tt.code)
		}
		if exit, stdout, stderr := run(args...); exit != tt.exit || stdout != "" || stderr == "" {
			t.Errorf("%v as text: exit code %d, stdout %q, stderr %q", tt.args, exit, stdout, stderr)
		}
	}
}

func TestWorktreeRefusalTexts(t *testing.T) {
	root := connectedShop(t)
	know := filepath.Join(root, "shop-knowledge")
	tests := []struct {
		dir    string
		args   []string
		stderr string
	}{
		{know, []string{"worktree", "add"}, msg.Text(msg.ErrWorktreeKnowledge, "shop", know)},
		{root, []string{"worktree", "add", "--project", "shop"}, msg.Text(msg.ErrNotGitRepo, root)},
		{root, []string{"worktree", "list", "--project", "nope"}, msg.Text(msg.ErrProjectNotFound, "nope") + "\n" + msg.Text(msg.HintProjectNotFound)},
	}
	for _, tt := range tests {
		t.Chdir(tt.dir)
		if _, _, stderr := run(tt.args...); stderr != tt.stderr+"\n" {
			t.Errorf("%v: got %q, want %q", tt.args, stderr, tt.stderr)
		}
	}
	clone := filepath.Join(root, "shop-clone")
	gittest.Run(t, root, "clone", "--quiet", filepath.Join(root, "shop"), clone)
	t.Chdir(root)
	want := msg.Text(msg.ErrProjectUndetermined, root) + "\n" + msg.Text(msg.HintProjectUndetermined) + "\n"
	if _, _, stderr := run("worktree", "add", clone); stderr != want {
		t.Errorf("undetermined: got %q, want %q", stderr, want)
	}
}
