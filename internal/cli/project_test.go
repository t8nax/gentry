package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
)

// shopDir makes a shop repository with a secondary worktree in a temporary
// directory, gives the test a data root of its own and returns the directory.
func shopDir(t *testing.T) string {
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

func TestProjectAddText(t *testing.T) {
	root := shopDir(t)
	shop, fix, know := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix"), filepath.Join(root, "shop-knowledge")
	t.Chdir(fix)

	code, stdout, stderr := run("project", "add", "shop", "--knowledge", filepath.Join("..", "shop-knowledge"))
	want := strings.Join([]string{
		msg.Text(msg.ProjectAdded, "shop"),
		msg.Text(msg.ProjectKnowledgeNew, know),
		msg.Text(msg.ProjectPrefix, "SHOP"),
		msg.Text(msg.ProjectMainWorktree, shop),
		msg.Text(msg.ProjectWorktreeAdded, fix),
	}, "\n") + "\n"
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	_, stdout, _ = run("project", "add", "--knowledge", know)
	if want := msg.Text(msg.ProjectUnchanged, "shop") + "\n"; stdout != want {
		t.Errorf("repeat: %q, want %q", stdout, want)
	}
	_, stdout, _ = run("project", "list")
	if want := msg.Text(msg.ProjectLine, "shop", "SHOP", know, shop) + "\n"; stdout != want {
		t.Errorf("list: %q, want %q", stdout, want)
	}
}

func TestProjectAddJSON(t *testing.T) {
	root := shopDir(t)
	shop, know := filepath.Join(root, "shop"), filepath.Join(root, "shop-knowledge")
	t.Chdir(shop)

	code, stdout, _ := run("project", "add", "shop", "--knowledge", know, "--prefix", "SH", "--json")
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	validate(t, "schemas/project-add.json", stdout)
	var out contract.ProjectAddOutput
	json.Unmarshal([]byte(stdout), &out)
	want := contract.ProjectAddOutput{Project: "shop", Prefix: "SH", Knowledge: know, KnowledgeCreated: true,
		MainWorktree: shop, Worktrees: []string{shop}, Action: "added"}
	if !jsonEqual(out, want) {
		t.Errorf("got %+v, want %+v", out, want)
	}

	_, stdout, _ = run("project", "add", "--knowledge", know, "--json")
	validate(t, "schemas/project-add.json", stdout)
	json.Unmarshal([]byte(stdout), &out)
	if out.Action != "unchanged" || out.KnowledgeCreated || out.Worktrees == nil || len(out.Worktrees) != 0 {
		t.Errorf("repeat: %s", stdout)
	}

	_, stdout, _ = run("project", "list", "--json")
	validate(t, "schemas/project-list.json", stdout)
	if want := `{"projects":[{"knowledge":` + jsonString(know) + `,"main_worktree":` + jsonString(shop) +
		`,"prefix":"SH","project":"shop"}]}` + "\n"; stdout != want {
		t.Errorf("list: %s, want %s", stdout, want)
	}

	// Every event data matches the schema of its type.
	_, stdout, _ = run("events", "--json")
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("events:\n%s", stdout)
	}
	for _, line := range lines {
		var e struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		json.Unmarshal([]byte(line), &e)
		validate(t, "schemas/events/"+e.Type+".json", string(e.Data))
	}
}

func TestProjectListEmpty(t *testing.T) {
	root := shopDir(t)
	if _, stdout, _ := run("project", "list"); stdout != msg.Text(msg.ProjectsNone)+"\n" {
		t.Errorf("got %q", stdout)
	}
	if _, stdout, _ := run("project", "list", "--json"); stdout != `{"projects":[]}`+"\n" {
		t.Errorf("got %q", stdout)
	}
	if p, _ := state.Path(); fileExists(p) || !strings.HasPrefix(p, root) {
		t.Errorf("project list must not create the state store %s", p)
	}
}

func TestProjectAddErrors(t *testing.T) {
	root := shopDir(t)
	shop := filepath.Join(root, "shop")
	t.Chdir(shop)
	run("project", "add", "shop", "--knowledge", "../shop-knowledge")
	clone := filepath.Join(root, "shop-clone")
	gittest.Run(t, root, "clone", "--quiet", shop, clone)

	tests := []struct {
		dir  string
		args []string
		exit int
		code string
	}{
		{shop, []string{"--knowledge", "../new"}, contract.ExitUsage, contract.CodeMissingArgument},
		{shop, []string{"cart"}, contract.ExitUsage, contract.CodeMissingArgument},
		{shop, []string{"Cart", "--knowledge", "../k"}, contract.ExitUsage, contract.CodeInvalidArgument},
		{shop, []string{"cart", "--knowledge", "../k", "--prefix", "c"}, contract.ExitUsage, contract.CodeFlagValue},
		{shop, []string{"online-store-backend", "--knowledge", "../k"}, contract.ExitUsage, contract.CodeMissingArgument},
		{shop, []string{"cart", "--knowledge", "docs"}, contract.ExitError, contract.CodeKnowledgeInvalid},
		{root, []string{"cart", "--knowledge", "k"}, contract.ExitError, contract.CodeNotGitRepo},
		{clone, []string{"--knowledge", "../shop-knowledge"}, contract.ExitError, contract.CodeProjectExists},
		{clone, []string{"cart", "--knowledge", "../shop-knowledge"}, contract.ExitError, contract.CodeKnowledgeMismatch},
		{clone, []string{"cart", "--knowledge", "../cart-knowledge", "--prefix", "SHOP"}, contract.ExitError, contract.CodePrefixTaken},
		{shop, []string{"cart", "--knowledge", "../cart-knowledge"}, contract.ExitError, contract.CodeWorktreeTaken},
	}
	for _, tt := range tests {
		t.Chdir(tt.dir)
		args := append([]string{"project", "add"}, tt.args...)
		exit, stdout, stderr := run(append(args, "--json")...)
		validate(t, "schemas/error.json", stdout)
		var out contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &out)
		if exit != tt.exit || out.Error.Code != tt.code || stderr != "" {
			t.Errorf("%v: exit code %d, %s; want %d, %s", tt.args, exit, stdout, tt.exit, tt.code)
		}
		// The same refusal as text has no program prefix.
		if exit, stdout, stderr := run(args...); exit != tt.exit || stdout != "" || stderr == "" || strings.HasPrefix(stderr, "gentry") {
			t.Errorf("%v as text: exit code %d, stdout %q, stderr %q", tt.args, exit, stdout, stderr)
		}
	}
}

func TestProjectAddRefusalText(t *testing.T) {
	root := shopDir(t)
	t.Chdir(root)
	_, _, stderr := run("project", "add", "shop", "--knowledge", "k")
	if want := msg.Text(msg.ErrNotGitRepo, root) + " " + msg.Text(msg.HintNotGitRepo) + "\n"; stderr != want {
		t.Errorf("got %q, want %q", stderr, want)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
