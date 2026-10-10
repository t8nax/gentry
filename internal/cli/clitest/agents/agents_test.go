package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
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
	if files, err := git.Changed(w); err != nil || len(files) > 0 {
		t.Errorf("git status of %s: %v, %v; want clean", w, files, err)
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

// changeLibrary rewrites the instruction of reviewer in the library and
// applies it.
func changeLibrary(t *testing.T, text string) string {
	t.Helper()
	clitest.WriteFiles(t, clitest.ShopPlaces().Library, map[string]string{"reviewer.md": text})
	_, stdout, _ := clitest.Run("library", "apply")
	return stdout
}

func TestPoolIsLaidOut(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	for _, w := range []string{shop, fix} {
		text := read(t, reviewerFile(w))
		if !strings.HasPrefix(text, "---\n# Gentry: ") || !strings.Contains(text, "Проверить изменения задачи") {
			t.Errorf("reviewer in %s:\n%s", w, text)
		}
		clean(t, w)
	}
}

func TestWorktreeAddLayout(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-extra"), "extra")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.WorktreeAdded, "shop", extra),
		"",
		changed(msg.Text(msg.AgentAdded, "reviewer")),
		nextSession,
	), "", "worktree", "add", extra)
	clean(t, extra)

	other := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-other"), "other")
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", other)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if a := res.Agents; a == nil || len(a.Worktrees) != 1 || a.Worktrees[0].Path != other || strings.Join(a.Worktrees[0].Added, ",") != "reviewer" {
		t.Errorf("JSON: %s", out)
	}
}

func TestWorktreeAddWarning(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	// A branch of the project tracks a file of the name of reviewer.
	gittest.Run(t, shop, "switch", "--quiet", "-c", "own-agents")
	os.WriteFile(reviewerFile(shop), []byte("проектный\n"), 0o644)
	gittest.Run(t, shop, "add", "--force", ".claude/agents/reviewer.md")
	gittest.Run(t, shop, "commit", "--quiet", "-m", "reviewer")
	gittest.Run(t, shop, "switch", "--quiet", "-")
	extra := filepath.Join(filepath.Dir(shop), "shop-own")
	gittest.Run(t, shop, "worktree", "add", "--quiet", extra, "own-agents")

	code, stdout, stderr := clitest.Run("worktree", "add", extra)
	want := clitest.Lines(
		msg.Text(msg.WorktreeAdded, "shop", extra),
		"",
		msg.Text(msg.AgentsConflictWarning),
		"",
	) + clitest.Table(
		conflictHeader,
		[]string{extra, "reviewer", ".claude/agents/reviewer.md", msg.Text(msg.AgentsReasonTracked)},
	) + clitest.Lines("", cli.HintText(msg.HintAgentsConflictWarning))
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Fatalf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	if read(t, reviewerFile(extra)) != "проектный\n" {
		t.Error("the tracked file is changed")
	}

	// A task is not taken there.
	if code, c := clitest.ErrorCode(t, clitest.TakeArgs("--worktree", extra)...); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("take: exit code %d, code %q", code, c)
	}
}

func TestTakeConflict(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)

	code, stdout, stderr := clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	want := clitest.Lines(msg.Text(msg.ErrAgentsConflict), "") + clitest.Table(
		conflictHeader,
		[]string{fix, "reviewer", ".claude/agents/reviewer.md", msg.Text(msg.AgentsReasonForeign)},
	) + clitest.Lines("", msg.Text(msg.HintAgentsConflict))
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Fatalf("exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	if _, out, _ := clitest.Run("task", "list"); strings.Contains(out, "SHOP-1") {
		t.Errorf("the task is taken:\n%s", out)
	}
	if read(t, reviewerFile(fix)) != "личный\n" {
		t.Error("the file of the operator is changed")
	}

	out := clitest.WantJSON(t, contract.ExitError, "schemas/error.json", clitest.TakeArgs("--worktree", fix)...)
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
	code, stdout, _ = clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\n"+clitest.Lines(changed(msg.Text(msg.AgentAdded, "reviewer")), nextSession, "")) {
		t.Errorf("take after the file is removed: exit code %d, output:\n%s", code, stdout)
	}
	clean(t, fix)
}

func TestApplyLaysOutFreeWorktreesOnly(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	stdout := changeLibrary(t, "Проверить изменения и тексты.\n")
	if !strings.HasSuffix(clitest.Masked(stdout), "\n\n"+clitest.Lines(msg.Text(msg.AgentsFreeSyncedAll), nextSession)) {
		t.Errorf("library apply:\n%s", stdout)
	}
	if !strings.Contains(read(t, reviewerFile(shop)), "Проверить изменения и тексты.") {
		t.Error("the free worktree keeps the old reviewer")
	}
	if read(t, reviewerFile(fix)) != before {
		t.Error("the worktree of the task is changed")
	}

	// The worktree the task releases gets the active flow.
	code, stdout, _ := clitest.Run("task", "cancel", "SHOP-1")
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\n"+clitest.Lines(changed(msg.Text(msg.AgentUpdated, "reviewer")), nextSession)) {
		t.Errorf("cancel: exit code %d, output:\n%s", code, stdout)
	}
	if !strings.Contains(read(t, reviewerFile(fix)), "Проверить изменения и тексты.") {
		t.Error("the released worktree keeps the reviewer of the snapshot")
	}
	clean(t, fix)
}

func TestFlowApplyRemoves(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	_, stdout, _ := clitest.Run("flow", "apply")
	if !strings.HasSuffix(clitest.Masked(stdout), "\n\n"+clitest.Lines(msg.Text(msg.AgentsFreeSynced, "shop"), nextSession)) {
		t.Errorf("flow apply:\n%s", stdout)
	}
	for _, w := range []string{shop, fix} {
		if clitest.FileExists(reviewerFile(w)) {
			t.Errorf("reviewer is still in %s", w)
		}
		clean(t, w)
	}
}

func TestAgentsSync(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	gone := filepath.Join(filepath.Dir(shop), "shop-gone")
	gittest.Worktree(t, shop, gone, "gone")
	clitest.MustRun(t, "worktree", "add", gone)
	os.RemoveAll(gone)

	header := []string{msg.Text(msg.ColWorktree), msg.Text(msg.ColTask), msg.Text(msg.ColAgents), msg.Text(msg.ColChanges)}
	none, missing := msg.Text(msg.ValueNone), msg.Text(msg.AgentsWorktreeMissing)
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(msg.Text(msg.AgentsInPlace, "shop"), "")+clitest.Table(header,
		[]string{shop, none, "reviewer", none},
		[]string{fix, "SHOP-1", "reviewer", none},
		[]string{gone, none, none, missing},
	), "", "agents", "sync")

	os.Remove(reviewerFile(shop))
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(msg.Text(msg.AgentsSynced, "shop"), nextSession, "")+clitest.Table(header,
		[]string{shop, none, "reviewer", msg.Text(msg.AgentAdded, "reviewer")},
		[]string{fix, "SHOP-1", "reviewer", none},
		[]string{gone, none, none, missing},
	), "", "agents", "sync")

	out := clitest.WantJSON(t, contract.ExitOK, "schemas/agents-sync.json", "agents", "sync")
	var res contract.AgentsSyncOutput
	json.Unmarshal([]byte(out), &res)
	if res.Project != "shop" || res.Changed || len(res.Worktrees) != 3 || res.Worktrees[1].Task == nil || *res.Worktrees[1].Task != "SHOP-1" {
		t.Errorf("JSON: %s", out)
	}

	// A conflict in any worktree refuses the command and changes nothing.
	os.Remove(reviewerFile(shop))
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)
	if code, c := clitest.ErrorCode(t, "agents", "sync"); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("conflict: exit code %d, code %q", code, c)
	}
	if clitest.FileExists(reviewerFile(shop)) {
		t.Error("a worktree is laid out despite the conflict")
	}
}

// passScenario takes the task of the current worktree through the feature
// scenario by skips.
func passScenario(t *testing.T) {
	t.Helper()
	for range 3 {
		clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен")
	}
	clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
}

func TestCloseAndFlowApplyWithTask(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	// flow apply leaves the worktree of the task to its snapshot.
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	clitest.MustRun(t, "flow", "apply")
	if read(t, reviewerFile(fix)) != before {
		t.Error("flow apply changed the worktree of the task")
	}

	t.Chdir(fix)
	passScenario(t)
	code, stdout, _ := clitest.Run("task", "close")
	if code != contract.ExitOK || !strings.HasSuffix(stdout, "\n\n"+clitest.Lines(changed(msg.Text(msg.AgentRemoved, "reviewer")), nextSession)) {
		t.Errorf("close: exit code %d, output:\n%s", code, stdout)
	}
	if clitest.FileExists(reviewerFile(fix)) {
		t.Error("the released worktree keeps reviewer")
	}
	clean(t, fix)
}

func TestLayoutFailure(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-broken"), "broken")
	// A file where the directory of subagents must be.
	os.WriteFile(filepath.Join(extra, ".claude"), []byte("файл\n"), 0o644)
	gittest.Run(t, extra, "add", ".claude")
	gittest.Run(t, extra, "commit", "--quiet", "-m", "file")

	code, stdout, _ := clitest.Run("worktree", "add", extra)
	head := clitest.Lines(msg.Text(msg.WorktreeAdded, "shop", extra), "", msg.Text(msg.AgentsSyncFailed))
	tail := clitest.Lines("", cli.HintText(msg.HintAgentsSyncFailed))
	if code != contract.ExitOK || !strings.HasPrefix(stdout, head+msg.Text(msg.AgentsSyncFailedReason, "")) || !strings.HasSuffix(stdout, tail) {
		t.Errorf("exit code %d, output:\n%s", code, stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", extra)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if res.Agents == nil || res.Agents.Error == nil || *res.Agents.Error == "" {
		t.Errorf("JSON: %s", out)
	}
}
