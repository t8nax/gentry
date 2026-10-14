package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// reviewerFile is the file of the subagent reviewer of the shop in worktree w.
func reviewerFile(w string) string { return filepath.Join(w, ".claude", "agents", "reviewer.md") }

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func clean(t *testing.T, w string) {
	t.Helper()
	if status := gittest.Run(t, w, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Errorf("git status of %s:\n%swant clean", w, status)
	}
}

// The words of the layout are those of the catalog: the tests of the output
// in package cli state them.
var (
	nextSession = msg.Text(msg.AgentsNextSession)
	// conflictHeader is the header of the table of conflicts.
	conflictHeader = []string{msg.Text(msg.ColWorktree), msg.Text(msg.ColAgent), msg.Text(msg.ColFile), msg.Text(msg.ColReason)}
)

// changed is the line of the subagents of one worktree changed.
func changed(change string) string { return msg.Text(msg.AgentsWorktreeChanged, change) }

// changeShared rewrites the instruction of the shared reviewer and applies
// it by flow apply of the shop.
func changeShared(t *testing.T, text string) string {
	t.Helper()
	testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{"reviewer.md": text})
	_, stdout, _ := testkit.Run("flow", "apply")
	return stdout
}

func TestPoolIsLaidOut(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	for _, w := range []string{shop, fix} {
		text := read(t, reviewerFile(w))
		if !strings.HasPrefix(text, "---\n# Gentry: ") || !strings.Contains(text, "Проверить изменения задачи") {
			t.Errorf("reviewer in %s:\n%s", w, text)
		}
		clean(t, w)
	}
}

func TestWorktreeAddLayout(t *testing.T) {
	shop, _ := testkit.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-extra"), "extra")
	testkit.WantRun(t, contract.ExitOK, testkit.Lines(
		msg.Text(msg.WorktreeAdded, "shop", extra),
		"",
		changed(msg.Text(msg.AgentAdded, "reviewer")),
		nextSession,
	), "", "worktree", "add", extra)
	clean(t, extra)

	other := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-other"), "other")
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", other)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if a := res.Agents; a == nil || len(a.Worktrees) != 1 || a.Worktrees[0].Path != other || strings.Join(a.Worktrees[0].Added, ",") != "reviewer" {
		t.Errorf("JSON: %s", out)
	}
}

func TestWorktreeAddWarning(t *testing.T) {
	shop, _ := testkit.TaskShop(t)
	// A branch of the project tracks a file of the name of reviewer.
	gittest.Run(t, shop, "switch", "--quiet", "-c", "own-agents")
	os.WriteFile(reviewerFile(shop), []byte("проектный\n"), 0o644)
	gittest.Run(t, shop, "add", "--force", ".claude/agents/reviewer.md")
	gittest.Run(t, shop, "commit", "--quiet", "-m", "reviewer")
	gittest.Run(t, shop, "switch", "--quiet", "-")
	extra := filepath.Join(filepath.Dir(shop), "shop-own")
	gittest.Run(t, shop, "worktree", "add", "--quiet", extra, "own-agents")

	code, stdout, stderr := testkit.Run("worktree", "add", extra)
	want := testkit.Lines(
		msg.Text(msg.WorktreeAdded, "shop", extra),
		"",
		msg.Text(msg.AgentsConflictWarning),
		"",
	) + testkit.Table(
		conflictHeader,
		[]string{extra, "reviewer", ".claude/agents/reviewer.md", msg.Text(msg.AgentsReasonTracked)},
	) + testkit.Lines("", cli.HintText(msg.HintAgentsConflictWarning))
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Fatalf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	if read(t, reviewerFile(extra)) != "проектный\n" {
		t.Error("the tracked file is changed")
	}

	// A task is not taken there.
	if code, c := testkit.ErrorCode(t, testkit.TakeArgs("--worktree", extra)...); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("take: exit code %d, code %q", code, c)
	}
}

func TestTakeConflict(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)

	code, stdout, stderr := testkit.Run(testkit.TakeArgs("--worktree", fix)...)
	want := testkit.Lines(msg.Text(msg.ErrAgentsConflict), "") + testkit.Table(
		conflictHeader,
		[]string{fix, "reviewer", ".claude/agents/reviewer.md", msg.Text(msg.AgentsReasonForeign)},
	) + testkit.Lines("", msg.Text(msg.HintAgentsConflict))
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Fatalf("exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	if _, out, _ := testkit.Run("task", "list"); strings.Contains(out, "SHOP-1") {
		t.Errorf("the task is taken:\n%s", out)
	}
	if read(t, reviewerFile(fix)) != "личный\n" {
		t.Error("the file of the operator is changed")
	}

	out := testkit.WantJSON(t, contract.ExitError, "schemas/error.json", testkit.TakeArgs("--worktree", fix)...)
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Conflicts []contract.AgentsConflict `json:"conflicts"`
			} `json:"details"`
		} `json:"error"`
	}
	json.Unmarshal([]byte(out), &e)
	if c := e.Error.Details.Conflicts; e.Error.Code != contract.CodeAgentsConflict || len(c) != 1 || c[0].Reason != "foreign" || c[0].Worktree != fix {
		t.Errorf("JSON: %s", out)
	}

	// Once the file is gone, the task is taken and the subagent laid out.
	os.Remove(reviewerFile(fix))
	code, stdout, _ = testkit.Run(testkit.TakeArgs("--worktree", fix)...)
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\n"+testkit.Lines(changed(msg.Text(msg.AgentAdded, "reviewer")), nextSession, "")) {
		t.Errorf("take after the file is removed: exit code %d, output:\n%s", code, stdout)
	}
	clean(t, fix)
}

func TestApplyLaysOutFreeWorktreesOnly(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	stdout := changeShared(t, "Проверить изменения и тексты.\n")
	if !strings.HasSuffix(testkit.Masked(stdout), "\n\n"+testkit.Lines(msg.Text(msg.AgentsFreeSynced, "shop"), nextSession)) {
		t.Errorf("flow apply:\n%s", stdout)
	}
	if !strings.Contains(read(t, reviewerFile(shop)), "Проверить изменения и тексты.") {
		t.Error("the free worktree keeps the old reviewer")
	}
	if read(t, reviewerFile(fix)) != before {
		t.Error("the worktree of the task is changed")
	}

	// The worktree the task releases gets the active flow.
	code, stdout, _ := testkit.Run("task", "cancel", "SHOP-1")
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\n"+testkit.Lines(changed(msg.Text(msg.AgentUpdated, "reviewer")), nextSession)) {
		t.Errorf("cancel: exit code %d, output:\n%s", code, stdout)
	}
	if !strings.Contains(read(t, reviewerFile(fix)), "Проверить изменения и тексты.") {
		t.Error("the released worktree keeps the reviewer of the snapshot")
	}
	clean(t, fix)
}

func TestFlowApplyRemoves(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	_, stdout, _ := testkit.Run("flow", "apply")
	if !strings.HasSuffix(testkit.Masked(stdout), "\n\n"+testkit.Lines(msg.Text(msg.AgentsFreeSynced, "shop"), nextSession)) {
		t.Errorf("flow apply:\n%s", stdout)
	}
	for _, w := range []string{shop, fix} {
		if testkit.FileExists(reviewerFile(w)) {
			t.Errorf("reviewer is still in %s", w)
		}
		clean(t, w)
	}
}

func TestAgentsDeploy(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	gone := filepath.Join(filepath.Dir(shop), "shop-gone")
	gittest.Worktree(t, shop, gone, "gone")
	testkit.MustRun(t, "worktree", "add", gone)
	os.RemoveAll(gone)

	header := []string{msg.Text(msg.ColWorktree), msg.Text(msg.ColTask), msg.Text(msg.ColAgents), msg.Text(msg.ColChanges)}
	missing := msg.Text(msg.AgentsWorktreeMissing)
	testkit.WantRun(t, contract.ExitOK, testkit.Lines(msg.Text(msg.AgentsInPlace, "shop"), "")+testkit.Table(header,
		[]string{shop, testkit.None, "reviewer", testkit.None},
		[]string{fix, "SHOP-1", "reviewer", testkit.None},
		[]string{gone, testkit.None, testkit.None, missing},
	), "", "agents", "deploy")

	os.Remove(reviewerFile(shop))
	testkit.WantRun(t, contract.ExitOK, testkit.Lines(msg.Text(msg.AgentsDeployed, "shop"), nextSession, "")+testkit.Table(header,
		[]string{shop, testkit.None, "reviewer", msg.Text(msg.AgentAdded, "reviewer")},
		[]string{fix, "SHOP-1", "reviewer", testkit.None},
		[]string{gone, testkit.None, testkit.None, missing},
	), "", "agents", "deploy")

	out := testkit.WantJSON(t, contract.ExitOK, "schemas/agents-deploy.json", "agents", "deploy")
	var res contract.AgentsDeployOutput
	json.Unmarshal([]byte(out), &res)
	if res.Project != "shop" || res.Changed || len(res.Worktrees) != 3 || res.Worktrees[1].Task == nil || *res.Worktrees[1].Task != "SHOP-1" {
		t.Errorf("JSON: %s", out)
	}

	// A conflict in any worktree refuses the command and changes nothing.
	os.Remove(reviewerFile(shop))
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)
	if code, c := testkit.ErrorCode(t, "agents", "deploy"); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("conflict: exit code %d, code %q", code, c)
	}
	if testkit.FileExists(reviewerFile(shop)) {
		t.Error("a worktree is laid out despite the conflict")
	}
}

// passScenario takes the task of the current worktree through the feature
// scenario by skips.
func passScenario(t *testing.T) {
	t.Helper()
	for range 3 {
		testkit.MustRun(t, "task", "stage", "skip", "--reason", "Не нужен")
	}
	testkit.MustRun(t, "task", "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	testkit.MustRun(t, "task", "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
}

func TestCloseAndFlowApplyWithTask(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	// flow apply leaves the worktree of the task to its snapshot.
	testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	testkit.MustRun(t, "flow", "apply")
	if read(t, reviewerFile(fix)) != before {
		t.Error("flow apply changed the worktree of the task")
	}

	t.Chdir(fix)
	passScenario(t)
	code, stdout, _ := testkit.Run("task", "close")
	if code != contract.ExitOK || !strings.HasSuffix(stdout, "\n\n"+testkit.Lines(changed(msg.Text(msg.AgentRemoved, "reviewer")), nextSession)) {
		t.Errorf("close: exit code %d, output:\n%s", code, stdout)
	}
	if testkit.FileExists(reviewerFile(fix)) {
		t.Error("the released worktree keeps reviewer")
	}
	clean(t, fix)
}

func TestLayoutFailure(t *testing.T) {
	shop, _ := testkit.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-broken"), "broken")
	// A file where the directory of subagents must be.
	os.WriteFile(filepath.Join(extra, ".claude"), []byte("файл\n"), 0o644)
	gittest.Run(t, extra, "add", ".claude")
	gittest.Run(t, extra, "commit", "--quiet", "-m", "file")

	code, stdout, _ := testkit.Run("worktree", "add", extra)
	head := testkit.Lines(msg.Text(msg.WorktreeAdded, "shop", extra), "", msg.Text(msg.AgentsSyncFailed))
	tail := testkit.Lines("", cli.HintText(msg.HintAgentsDeployFailed))
	if code != contract.ExitOK || !strings.HasPrefix(stdout, head+msg.Text(msg.AgentsSyncFailedReason, "")) || !strings.HasSuffix(stdout, tail) {
		t.Errorf("exit code %d, output:\n%s", code, stdout)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", extra)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if res.Agents == nil || res.Agents.Error == nil || *res.Agents.Error == "" {
		t.Errorf("JSON: %s", out)
	}
}
