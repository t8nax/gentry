package pool_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-70, 6. Новые рабочие копии доверяются в Codex: project add and worktree
// add with Codex connected by gentry setup codex with the fake Codex before
// the shop is connected. The shop has the flow of the flow testdata.

// The texts of project add and worktree add about the trust in Codex, as
// the specification gives them.
const (
	trustUnwritten    = "Доверить рабочие копии в Codex не удалось: настройки Codex не записаны."
	trustUnread       = "Доверить рабочие копии в Codex не удалось: настройки Codex не прочитаны."
	trustFixHint      = "Исправить настройки Codex и повторить: gentry setup codex"
	distrustedHeading = "Рабочие копии, которым оператор не доверил в Codex, оставлены как есть:"
)

// The subagent reviewer of the specification: the shorthand «субагент
// reviewer».
func specReviewer(t *testing.T) {
	t.Helper()
	testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{
		"reviewer.yaml": "purpose: ревью изменений заказа — корректность и тесты\ncapabilities: [read, search, task]\nmodel: strong\n",
		"reviewer.md":   "Проверить изменения задачи и вернуть замечания с местом в коде.\n",
	})
	testkit.MustRun(t, "flow", "apply")
}

// clone makes the copy C of the shop: a clone of its repository, no
// worktree of it, next to it.
func clone(t *testing.T, shop string) string {
	t.Helper()
	c := filepath.Join(filepath.Dir(shop), "shop-c")
	gittest.Run(t, filepath.Dir(shop), "clone", "--quiet", shop, c)
	return c
}

// codexTrust is the field codex_trust of the JSON of project add and
// worktree add.
type codexTrust struct {
	Trusted    []string `json:"trusted"`
	Distrusted []string `json:"distrusted"`
	Error      *string  `json:"error"`
}

// addJSON runs the command with --json, which must succeed and match the
// schema, and returns its output, its action and its field codex_trust; nil
// if there is none.
func addJSON(t *testing.T, schema string, args ...string) (string, string, *codexTrust, map[string]json.RawMessage) {
	t.Helper()
	out := testkit.WantJSON(t, contract.ExitOK, schema, args...)
	var res struct {
		Action     string      `json:"action"`
		CodexTrust *codexTrust `json:"codex_trust"`
	}
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(out), &res)
	json.Unmarshal([]byte(out), &fields)
	var trust map[string]json.RawMessage
	json.Unmarshal(fields["codex_trust"], &trust)
	return out, res.Action, res.CodexTrust, trust
}

// samePaths reports whether the paths are those wanted, in any order, by
// the rules of Codex.
func samePaths(got []string, want ...string) bool {
	if got == nil || len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.ContainsFunc(got, func(g string) bool { return testkit.CodexSamePath(g, w) }) {
			return false
		}
	}
	return true
}

// trustOf returns the trust_level of the record of trust of dir in the
// settings of Codex; "" if there is none.
func trustOf(t *testing.T, dir string) string {
	t.Helper()
	_, level, _ := testkit.CodexTrust(testkit.ReadCodexConfig(t, testkit.CodexHome()), dir)
	return level
}

// TestWorktreeAddCodexTrust checks that worktree add trusts in Codex the
// worktree it adds, or has in the pool, without a record of trust.
func TestWorktreeAddCodexTrust(t *testing.T) {
	t.Run("repeat", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		testkit.MustRun(t, "worktree", "add", c)
		testkit.SetCodexTrust(t, testkit.CodexHome(), c, "")
		out, action, trust, _ := addJSON(t, "schemas/worktree-add.json", "worktree", "add", c)
		if action != "unchanged" || trust == nil || !samePaths(trust.Trusted, c) {
			t.Errorf("worktree add --json: %s\nwant action unchanged and codex_trust.trusted [%s]", out, c)
		}
		if got := trustOf(t, c); got != "trusted" {
			t.Errorf("the record of trust of %s: %q, want trusted", c, got)
		}
	})
	t.Run("clone", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		if code, _, stderr := testkit.Run("worktree", "add", c); code != contract.ExitOK {
			t.Fatalf("worktree add: exit code %d, stderr:\n%s", code, stderr)
		}
		if got := trustOf(t, c); got != "trusted" {
			t.Errorf("the record of trust of %s: %q, want trusted", c, got)
		}
	})
	t.Run("trusted_repository", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		testkit.SetCodexTrust(t, testkit.CodexHome(), a, "trusted")
		before := testkit.CodexProjects(testkit.ReadCodexConfig(t, testkit.CodexHome()))
		b := gittest.Worktree(t, a, filepath.Join(filepath.Dir(a), "shop-b"), "b")
		out, _, _, trust := addJSON(t, "schemas/worktree-add.json", "worktree", "add", b)
		if after := testkit.CodexProjects(testkit.ReadCodexConfig(t, testkit.CodexHome())); !testkit.JSONEqual(after, before) {
			t.Errorf("the records of trust of Codex: %v, were %v", after, before)
		}
		if string(trust["trusted"]) != "[]" || string(trust["distrusted"]) != "[]" {
			t.Errorf("worktree add --json: %s\nwant codex_trust.trusted [] and codex_trust.distrusted []", out)
		}
	})
	t.Run("without_codex", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, false)
		a := testkit.MustWd(t)
		config := testkit.CodexConfigFile(testkit.CodexHome())
		before, beforeErr := os.ReadFile(config)
		b := gittest.Worktree(t, a, filepath.Join(filepath.Dir(a), "shop-b"), "b")
		out, _, _, _ := addJSON(t, "schemas/worktree-add.json", "worktree", "add", b)
		after, afterErr := os.ReadFile(config)
		if string(after) != string(before) || (beforeErr == nil) != (afterErr == nil) {
			t.Errorf("worktree add without Codex changed %s", config)
		}
		if strings.Contains(out, `"codex_trust"`) {
			t.Errorf("worktree add --json: %s\nwant no field codex_trust", out)
		}
	})
}

// TestProjectAddCodexTrust checks that project add trusts in Codex both
// worktrees it adds by the record of the main repository.
func TestProjectAddCodexTrust(t *testing.T) {
	root := testkit.ToolsShopDir(t, false, true)
	a, b := filepath.Join(root, "shop"), filepath.Join(root, "shop-fix")
	t.Chdir(b)
	out, _, trust, _ := addJSON(t, "schemas/project-add.json", "project", "add", "shop", "--knowledge", "../shop-knowledge")
	if got := trustOf(t, a); got != "trusted" {
		t.Errorf("the record of trust of %s: %q, want trusted", a, got)
	}
	if trust == nil || !samePaths(trust.Trusted, a, b) {
		t.Errorf("project add --json: %s\nwant codex_trust.trusted [%s, %s]", out, a, b)
	}
}

// TestWorktreeAddCodexDistrusted checks that worktree add leaves a worktree
// of a record the operator set untrusted as it is and names it.
func TestWorktreeAddCodexDistrusted(t *testing.T) {
	testkit.ToolsShopFlow(t, false, true)
	a := testkit.MustWd(t)
	testkit.SetCodexTrust(t, testkit.CodexHome(), a, "untrusted")
	before := testkit.CodexProjects(testkit.ReadCodexConfig(t, testkit.CodexHome()))
	b := gittest.Worktree(t, a, filepath.Join(filepath.Dir(a), "shop-b"), "b")
	code, stdout, _ := testkit.Run("worktree", "add", b)
	if code != contract.ExitOK {
		t.Fatalf("worktree add: exit code %d", code)
	}
	if after := testkit.CodexProjects(testkit.ReadCodexConfig(t, testkit.CodexHome())); !testkit.JSONEqual(after, before) {
		t.Errorf("the records of trust of Codex: %v, were %v", after, before)
	}
	if !strings.Contains("\n"+stdout, "\n\n"+distrustedHeading+"\n  "+b+"\n") {
		t.Errorf("worktree add:\n%s\nwant the block «%s» with %s", stdout, distrustedHeading, b)
	}
	hint := `Доверить рабочие копии в настройках Codex: projects."` + a + `".trust_level = "trusted"`
	ls := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	i := len(ls) - 1
	for i >= 0 && ls[i] != "" {
		i--
	}
	if !slices.Contains(ls[i+1:], hint) {
		t.Errorf("worktree add:\n%s\nwant «%s» in the last block", stdout, hint)
	}
}

// TestWorktreeAddCodexJSON checks the field codex_trust of the JSON of
// worktree add.
func TestWorktreeAddCodexJSON(t *testing.T) {
	t.Run("added", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		out, _, trust, fields := addJSON(t, "schemas/worktree-add.json", "worktree", "add", c)
		if trust == nil || !samePaths(trust.Trusted, c) || string(fields["distrusted"]) != "[]" {
			t.Errorf("worktree add --json: %s\nwant codex_trust.trusted [%s], codex_trust.distrusted []", out, c)
		}
		if _, ok := fields["error"]; ok {
			t.Errorf("worktree add --json: %s\nwant no codex_trust.error", out)
		}
	})
	t.Run("repeat", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		testkit.MustRun(t, "worktree", "add", c)
		if got := trustOf(t, c); got != "trusted" {
			t.Fatalf("stand: the record of trust of %s: %q, want trusted", c, got)
		}
		out, action, _, fields := addJSON(t, "schemas/worktree-add.json", "worktree", "add", c)
		if action != "unchanged" || string(fields["trusted"]) != "[]" {
			t.Errorf("worktree add --json: %s\nwant action unchanged, codex_trust.trusted []", out)
		}
	})
}

// TestProjectAddCodexText checks the place of the lines of Codex in the
// output of project add: a project of a flow, connected on a machine whose
// settings of Codex have no record of trust, with a worktree of the
// repository outside the pool. The flow comes to the machine from the
// remote repository of the process of another one.
func TestProjectAddCodexText(t *testing.T) {
	testkit.ToolsShopFlow(t, false, true)
	a := testkit.MustWd(t)
	root := filepath.Dir(a)
	d := filepath.Join(root, "shop-fix")
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)
	testkit.MustRun(t, "process", "remote", "set", remote)
	// The other machine: a data root and settings of Codex of its own.
	t.Setenv(testkit.HomeEnv, filepath.Join(root, "home-2"))
	t.Setenv(testkit.CodexHomeEnv, filepath.Join(root, "codex-2"))
	testkit.ConnectCodex(t)
	testkit.MustRun(t, "process", "remote", "set", remote)
	os.RemoveAll(filepath.Join(a, ".codex"))
	code, stdout, _ := testkit.Run("project", "add", "--knowledge", "../shop-knowledge")
	if code != contract.ExitOK {
		t.Fatalf("project add: exit code %d, stdout:\n%s", code, stdout)
	}
	laidOut := testkit.Lines(msg.Text(msg.AgentsFreeSynced, "shop"), msg.Text(msg.AgentsNextSession))
	tail := "\n" + laidOut + "\n" + msg.Text(msg.ProjectUnpooled) + "\n  " + d + "\n\n" + cli.HintText(msg.HintProjectUnpooled) + "\n"
	if !strings.HasPrefix(stdout, msg.Text(msg.ProjectAdded, "shop")+"\n") || !strings.HasSuffix(stdout, tail) {
		t.Errorf("project add:\n%s\nwant the lines of the project, then:\n%s", stdout, tail)
	}
	if strings.Contains(stdout, "Codex") {
		t.Errorf("project add names Codex:\n%s", stdout)
	}
}

// TestWorktreeAddCodexText checks the output of worktree add with Codex
// connected.
func TestWorktreeAddCodexText(t *testing.T) {
	changed := msg.Text(msg.AgentsWorktreeChanged, msg.Text(msg.AgentAdded, "reviewer"))
	t.Run("full", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		c := clone(t, testkit.MustWd(t))
		testkit.WantRun(t, contract.ExitOK, testkit.Lines(
			msg.Text(msg.WorktreeAdded, "shop", c),
			"",
			changed,
			msg.Text(msg.AgentsNextSession),
		), "", "worktree", "add", c)
		if got := trustOf(t, c); got != "trusted" {
			t.Errorf("the record of trust of %s: %q, want trusted", c, got)
		}
	})
	t.Run("settings_warning", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes a file closed for writing")
		}
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		c := clone(t, testkit.MustWd(t))
		readOnly(t, testkit.CodexConfigFile(testkit.CodexHome()))
		testkit.WantRun(t, contract.ExitOK, testkit.Lines(
			msg.Text(msg.WorktreeAdded, "shop", c),
			"",
			changed,
			msg.Text(msg.AgentsNextSession),
			"",
			trustUnwritten,
			"",
			trustFixHint,
		), "", "worktree", "add", c)
	})
}

// readOnly makes the file read-only until the test ends.
func readOnly(t *testing.T, p string) {
	t.Helper()
	if !testkit.FileExists(p) {
		t.Fatalf("stand: no %s", p)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
}

// TestWorktreeAddCodexSettings checks that worktree add with settings of
// Codex it cannot read or write adds the worktree and warns.
func TestWorktreeAddCodexSettings(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		const broken = "[features\nhooks = true\n"
		testkit.WriteFiles(t, testkit.CodexHome(), map[string]string{"config.toml": broken})
		code, stdout, _ := testkit.Run("worktree", "add", c)
		if code != contract.ExitOK || !slices.Contains(strings.Split(stdout, "\n"), trustUnread) || !strings.HasSuffix(stdout, "\n\n"+trustFixHint+"\n") {
			t.Errorf("worktree add: exit code %d, stdout:\n%s\nwant 0, «%s» and the last block «%s»", code, stdout, trustUnread, trustFixHint)
		}
		if _, list, _ := testkit.Run("worktree", "list"); !strings.Contains(list, c) {
			t.Errorf("%s is not in the pool:\n%s", c, list)
		}
		out, _, trust, _ := addJSON(t, "schemas/worktree-add.json", "worktree", "add", c)
		if trust == nil || trust.Error == nil || *trust.Error != "unreadable" {
			t.Errorf("worktree add --json: %s\nwant codex_trust.error unreadable", out)
		}
		if b, _ := os.ReadFile(testkit.CodexConfigFile(testkit.CodexHome())); string(b) != broken {
			t.Errorf("the settings of Codex changed:\n%s", b)
		}
	})
	t.Run("unwritable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes a file closed for writing")
		}
		testkit.ToolsShopFlow(t, false, true)
		c := clone(t, testkit.MustWd(t))
		readOnly(t, testkit.CodexConfigFile(testkit.CodexHome()))
		code, stdout, _ := testkit.Run("worktree", "add", c)
		if code != contract.ExitOK || !slices.Contains(strings.Split(stdout, "\n"), trustUnwritten) {
			t.Errorf("worktree add: exit code %d, stdout:\n%s\nwant 0 and «%s»", code, stdout, trustUnwritten)
		}
	})
}
