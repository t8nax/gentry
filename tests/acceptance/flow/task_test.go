package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// takeInFix takes a task in shop-fix and moves the test there. It returns
// the flow of the task as task show gives it.
func takeInFix(t *testing.T) (shop, fix string, taken contract.Applied) {
	t.Helper()
	shop, fix = testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	var out contract.TaskShowOutput
	if err := json.Unmarshal([]byte(testkit.MustRun(t, "task", "show", "SHOP-1", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	t.Chdir(fix)
	return shop, fix, out.Task.Flow
}

// showJSON runs flow show with args and --json, which must succeed by the
// schema, and returns its output.
func showJSON(t *testing.T, args ...string) contract.FlowShowOutput {
	t.Helper()
	stdout := testkit.WantJSON(t, contract.ExitOK, "schemas/flow-show.json", append([]string{"flow", "show"}, args...)...)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// applyJSON runs apply of group kind with --json and returns the commit it
// applied.
func applyJSON(t *testing.T, kind string) contract.Applied {
	t.Helper()
	var out struct {
		Applied contract.Applied `json:"applied"`
	}
	if err := json.Unmarshal([]byte(testkit.MustRun(t, kind, "apply", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	return out.Applied
}

// sameApplied reports whether a names the commit of want.
func sameApplied(a *contract.Applied, want contract.Applied) bool {
	return a != nil && a.Commit == want.Commit && a.Time.Equal(want.Time)
}

// taskFlowJSON runs task flow show with args and --json, which must succeed
// by the schema of flow show, and returns its output.
func taskFlowJSON(t *testing.T, args ...string) contract.FlowShowOutput {
	t.Helper()
	stdout := testkit.WantJSON(t, contract.ExitOK, "schemas/flow-show.json", append([]string{"task", "flow", "show"}, args...)...)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// stageHint is the hint of task flow show to a stage of the flow of the
// task, with the args after the placeholder of the stage.
func stageHint(args ...string) string {
	return msg.Text(msg.HintLine, msg.Text(msg.HintFlowStage),
		strings.Join(append([]string{"gentry task flow show --stage", msg.Text(msg.ArgStage)}, args...), " "))
}

// TestTaskFlowShow checks task flow show: the flow of the task in its
// worktree and from another directory by --task, with the change of the
// active flow marked once a flow is applied after the take; flow show shows
// the active flow in the worktree of the task as elsewhere.
func TestTaskFlowShow(t *testing.T) {
	shop, _, taken := takeInFix(t)
	head := testkit.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowTask, "SHOP-1"), msg.Text(msg.TaskFlowApplied, "<время>"))
	want := head + "\n" + testkit.ShopTables() + "\n" + stageHint() + "\n"
	if code, stdout, stderr := testkit.Run("task", "flow", "show"); code != contract.ExitOK || testkit.Masked(stdout) != want {
		t.Errorf("before a change: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	out := taskFlowJSON(t)
	if out.Task == nil || out.Task.Id != "SHOP-1" || !sameApplied(&out.Task.Flow, taken) || out.Task.ActiveFlow != nil ||
		out.Applied != nil || out.Dir != nil || out.Draft != nil || len(out.Stages) != 6 {
		t.Errorf("before a change: %+v", out)
	}

	// The merge stage changes and a stage is added; a draft stays open.
	p := testkit.ShopPlaces()
	testkit.WriteDraft(t, p, map[string]string{
		"stages/merge.md":      "Влить ветку задачи в main после ревью.\n",
		"stages/security.yaml": "title: Безопасность\nexit: проверка пройдена\nexecutor: orchestrator\n",
		"stages/security.md":   "Проверить уязвимости.\n",
	})
	applied := applyJSON(t, "flow")
	testkit.WriteDraft(t, p, map[string]string{"stages/branch.md": "Создать ветку от main.\n"})

	want = testkit.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowTask, "SHOP-1"), msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskActiveFlowChanged, "<время>")) + "\n" + testkit.ShopTables() + "\n" +
		testkit.Lines(stageHint(), cli.HintText(msg.HintFlowActive))
	if code, stdout, stderr := testkit.Run("task", "flow", "show"); code != contract.ExitOK || testkit.Masked(stdout) != want {
		t.Errorf("after a change: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	out = taskFlowJSON(t)
	if out.Task == nil || !sameApplied(out.Task.ActiveFlow, applied) || !sameApplied(&out.Task.Flow, taken) || len(out.Stages) != 6 {
		t.Errorf("after a change: %+v", out)
	}

	// An object is looked for in the flow of the task.
	merge := testkit.Lines(
		msg.Text(msg.FlowTask, "SHOP-1"),
		msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskActiveFlowChanged, "<время>"),
		"",
		msg.Text(msg.FlowStage, "merge"),
		msg.Text(msg.FlowTitle, "Слияние"),
		msg.Text(msg.FlowExecutor, "operator"),
		msg.Text(msg.FlowAgents, testkit.None),
		msg.Text(msg.FlowExit, "ветка задачи влита в main"),
		msg.Text(msg.FlowParts, testkit.None),
		msg.Text(msg.FlowScenarios, "bug, feature"),
		"",
		msg.Text(msg.FlowInstruction),
		"  Влить ветку задачи в main.",
		"",
		cli.HintText(msg.HintFlowActive),
	)
	if code, stdout, stderr := testkit.Run("task", "flow", "show", "--stage", "merge"); code != contract.ExitOK || testkit.Masked(stdout) != merge {
		t.Errorf("--stage merge: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, merge)
	}
	if code, errCode := testkit.ErrorCode(t, "task", "flow", "show", "--stage", "security"); code != contract.ExitError || errCode != contract.CodeFlowObjectNotFound {
		t.Errorf("a stage added after the take: exit code %d, %s", code, errCode)
	}
	want = testkit.Lines(msg.Text(msg.ErrTaskFlowObjectNotFound, "SHOP-1", msg.Text(msg.FlowKindStage), "security"), "",
		msg.Text(msg.HintLine, msg.Text(msg.HintFlowObjects), "gentry task flow show"))
	if code, _, stderr := testkit.Run("task", "flow", "show", "--stage", "security"); code != contract.ExitError || stderr != want {
		t.Errorf("a stage added after the take: exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}

	// flow show shows the active flow in the worktree of the task, as
	// elsewhere; the flag --active is no more.
	activeMerge := testkit.Lines(
		msg.Text(msg.FlowStage, "merge"),
		msg.Text(msg.FlowTitle, "Слияние"),
		msg.Text(msg.FlowExecutor, "operator"),
		msg.Text(msg.FlowAgents, testkit.None),
		msg.Text(msg.FlowExit, "ветка задачи влита в main"),
		msg.Text(msg.FlowParts, testkit.None),
		msg.Text(msg.FlowScenarios, "bug, feature"),
		"",
		msg.Text(msg.FlowInstruction),
		"  Влить ветку задачи в main после ревью.",
	)
	if code, stdout, stderr := testkit.Run("flow", "show", "--stage", "merge"); code != contract.ExitOK || stdout != activeMerge {
		t.Errorf("flow show --stage merge: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, activeMerge)
	}
	out = showJSON(t)
	if out.Task != nil || out.Applied == nil || out.Applied.Commit != applied.Commit || out.Dir == nil || *out.Dir != p.Dir ||
		out.Draft == nil || len(out.Stages) != 7 {
		t.Errorf("flow show: %+v", out)
	}
	_, inFix, _ := testkit.Run("flow", "show", "--json")
	if code, errCode := testkit.ErrorCode(t, "flow", "show", "--active", "--json"); code != contract.ExitUsage || errCode != contract.CodeUnknownFlag {
		t.Errorf("--active: exit code %d, %s", code, errCode)
	}

	// From another directory the task is named by --task, and so are the
	// hints.
	t.Chdir(shop)
	if _, elsewhere, _ := testkit.Run("flow", "show", "--json"); elsewhere != inFix {
		t.Errorf("the active flow differs: in the worktree of the task\n%s\nelsewhere\n%s", inFix, elsewhere)
	}
	want = testkit.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowTask, "SHOP-1"), msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskActiveFlowChanged, "<время>")) + "\n" + testkit.ShopTables() + "\n" +
		testkit.Lines(stageHint("--task", "SHOP-1"), msg.Text(msg.HintLine, msg.Text(msg.HintFlowActive), "gentry flow show --project shop"))
	if code, stdout, stderr := testkit.Run("task", "flow", "show", "--task", "SHOP-1"); code != contract.ExitOK || testkit.Masked(stdout) != want {
		t.Errorf("--task SHOP-1: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	if code, errCode := testkit.ErrorCode(t, "task", "flow", "show"); code != contract.ExitError || errCode != contract.CodeTaskUndetermined {
		t.Errorf("no task in the directory: exit code %d, %s", code, errCode)
	}

	// A cancelled task shows its flow too, without the mark of a change:
	// it follows no flow.
	testkit.MustRun(t, "task", "cancel", "SHOP-1")
	out = taskFlowJSON(t, "--task", "SHOP-1")
	if out.Task == nil || out.Task.Id != "SHOP-1" || out.Task.ActiveFlow != nil || len(out.Stages) != 6 {
		t.Errorf("a cancelled task: %+v", out)
	}
}

// TestTaskFlowShowShared checks a shared subagent of the flow of a task
// after a change of it is applied: as the task has it, with the hint to the
// active flow. Which changes of the shared subagents count, TestActiveFlow of
// package task checks.
func TestTaskFlowShowShared(t *testing.T) {
	_, _, _ = takeInFix(t)
	lib := testkit.ShopPlaces().Shared
	text, err := os.ReadFile(filepath.Join(lib, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFiles(t, lib, map[string]string{"reviewer.md": string(text) + "Проверить логи.\n"})
	testkit.MustRun(t, "flow", "apply")
	_, stdout, _ := testkit.Run("task", "flow", "show", "--agent", "reviewer")
	if strings.Contains(stdout, "Проверить логи.") || !strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintFlowActive)+"\n") {
		t.Errorf("--agent reviewer:\n%s", stdout)
	}
}
