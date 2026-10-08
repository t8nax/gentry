package pool_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

func TestProjectAddText(t *testing.T) {
	root := clitest.ShopDir(t)
	shop, fix, know := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix"), filepath.Join(root, "shop-knowledge")
	t.Chdir(fix)

	code, stdout, stderr := clitest.Run("project", "add", "shop", "--knowledge", filepath.Join("..", "shop-knowledge"))
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

	_, stdout, _ = clitest.Run("project", "add", "--knowledge", know)
	if want := msg.Text(msg.ProjectUnchanged, "shop") + "\n"; stdout != want {
		t.Errorf("repeat: %q, want %q", stdout, want)
	}
	_, stdout, _ = clitest.Run("project", "list")
	if want := clitest.Table(projectHeader(), []string{"shop", "SHOP", know, shop}); stdout != want {
		t.Errorf("list: %q, want %q", stdout, want)
	}
}

func TestProjectAddJSON(t *testing.T) {
	root := clitest.ShopDir(t)
	shop, know := filepath.Join(root, "shop"), filepath.Join(root, "shop-knowledge")
	t.Chdir(shop)

	code, stdout, _ := clitest.Run("project", "add", "shop", "--knowledge", know, "--prefix", "SH", "--json")
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	clitest.Validate(t, "schemas/project-add.json", stdout)
	var out contract.ProjectAddOutput
	json.Unmarshal([]byte(stdout), &out)
	want := contract.ProjectAddOutput{Project: "shop", Prefix: "SH", Knowledge: know, KnowledgeCreated: true,
		MainWorktree: shop, Worktrees: []string{shop}, Action: "added"}
	if !clitest.JSONEqual(out, want) {
		t.Errorf("got %+v, want %+v", out, want)
	}

	_, stdout, _ = clitest.Run("project", "add", "--knowledge", know, "--json")
	clitest.Validate(t, "schemas/project-add.json", stdout)
	json.Unmarshal([]byte(stdout), &out)
	if out.Action != "unchanged" || out.KnowledgeCreated || out.Worktrees == nil || len(out.Worktrees) != 0 {
		t.Errorf("repeat: %s", stdout)
	}

	_, stdout, _ = clitest.Run("project", "list", "--json")
	clitest.Validate(t, "schemas/project-list.json", stdout)
	if want := `{"projects":[{"knowledge":` + clitest.JSONString(know) + `,"main_worktree":` + clitest.JSONString(shop) +
		`,"prefix":"SH","project":"shop"}]}` + "\n"; stdout != want {
		t.Errorf("list: %s, want %s", stdout, want)
	}

	// Every event data matches the schema of its type.
	_, stdout, _ = clitest.Run("events", "--json")
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
		clitest.Validate(t, "schemas/events/"+e.Type+".json", string(e.Data))
	}
}

func TestProjectListEmpty(t *testing.T) {
	root := clitest.ShopDir(t)
	if _, stdout, _ := clitest.Run("project", "list"); stdout != msg.Text(msg.ProjectsNone)+"\n" {
		t.Errorf("got %q", stdout)
	}
	if _, stdout, _ := clitest.Run("project", "list", "--json"); stdout != `{"projects":[]}`+"\n" {
		t.Errorf("got %q", stdout)
	}
	if p, _ := state.Path(); clitest.FileExists(p) || !strings.HasPrefix(p, root) {
		t.Errorf("project list must not create the state store %s", p)
	}
}

func TestProjectAddErrors(t *testing.T) {
	root := clitest.ShopDir(t)
	shop := filepath.Join(root, "shop")
	t.Chdir(shop)
	clitest.Run("project", "add", "shop", "--knowledge", "../shop-knowledge")
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
		exit, stdout, stderr := clitest.Run(append(args, "--json")...)
		clitest.Validate(t, "schemas/error.json", stdout)
		var out contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &out)
		if exit != tt.exit || out.Error.Code != tt.code || stderr != "" {
			t.Errorf("%v: exit code %d, %s; want %d, %s", tt.args, exit, stdout, tt.exit, tt.code)
		}
		// The same refusal as text has no program prefix.
		if exit, stdout, stderr := clitest.Run(args...); exit != tt.exit || stdout != "" || stderr == "" || strings.HasPrefix(stderr, "gentry") {
			t.Errorf("%v as text: exit code %d, stdout %q, stderr %q", tt.args, exit, stdout, stderr)
		}
	}
}

func TestProjectAddRefusalText(t *testing.T) {
	root := clitest.ShopDir(t)
	t.Chdir(root)
	_, _, stderr := clitest.Run("project", "add", "shop", "--knowledge", "k")
	if want := msg.Text(msg.ErrNotGitRepo, root) + "\n\n" + msg.Text(msg.HintNotGitRepo) + "\n"; stderr != want {
		t.Errorf("got %q, want %q", stderr, want)
	}

	// Every refusal of the command line names what to do on its own line.
	t.Chdir(filepath.Join(root, "shop"))
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"shop"}, msg.Text(msg.ErrKnowledgeFlagMissing) + "\n\n" + msg.Text(msg.HintKnowledgeFlag)},
		{[]string{"--knowledge", "../new"}, msg.Text(msg.ErrProjectIDMissing) + "\n\n" + msg.Text(msg.HintProjectIDMissing)},
		{[]string{"Shop", "--knowledge", "../k"}, msg.Text(msg.ErrProjectIDInvalid, "Shop") + "\n\n" + msg.Text(msg.HintProjectIDInvalid)},
		{[]string{"shop", "--knowledge", "../k", "--prefix", "s"}, msg.Text(msg.ErrPrefixInvalid, "s") + "\n\n" + msg.Text(msg.HintPrefixInvalid)},
	}
	for _, tt := range tests {
		if _, _, stderr := clitest.Run(append([]string{"project", "add"}, tt.args...)...); stderr != tt.want+"\n" {
			t.Errorf("%v: got %q, want %q", tt.args, stderr, tt.want)
		}
	}
}

func projectHeader() []string {
	return []string{msg.Text(msg.ColProject), msg.Text(msg.ColPrefix), msg.Text(msg.ColKnowledge), msg.Text(msg.ColMainWorktree)}
}
