package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/msg"
)

// takeInFix takes a task in shop-fix and moves the test there. It returns
// the flow of the task as task show gives it.
func takeInFix(t *testing.T) (shop, fix string, taken contract.Applied) {
	t.Helper()
	shop, fix = clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	var out contract.TaskShowOutput
	if err := json.Unmarshal([]byte(clitest.MustRun(t, "task", "show", "SHOP-1", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	t.Chdir(fix)
	return shop, fix, out.Task.Flow
}

// showJSON runs flow show with args and --json, which must succeed by the
// schema, and returns its output.
func showJSON(t *testing.T, args ...string) contract.FlowShowOutput {
	t.Helper()
	stdout := clitest.WantJSON(t, contract.ExitOK, "schemas/flow-show.json", append([]string{"flow", "show"}, args...)...)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// applyJSON runs flow apply or library apply with --json and returns the
// commit it applied.
func applyJSON(t *testing.T, kind string) contract.Applied {
	t.Helper()
	var out struct {
		Applied contract.Applied `json:"applied"`
	}
	if err := json.Unmarshal([]byte(clitest.MustRun(t, kind, "apply", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	return out.Applied
}

// sameApplied reports whether a names the commit of want.
func sameApplied(a *contract.Applied, want contract.Applied) bool {
	return a != nil && a.Commit == want.Commit && a.Time.Equal(want.Time)
}

// TestFlowShowTask checks flow show in the worktree of a task: the flow of
// the task, with the change of the active flow marked once a flow is applied
// after the take, and the active flow with --active and elsewhere.
func TestFlowShowTask(t *testing.T) {
	shop, _, taken := takeInFix(t)
	head := clitest.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowTask, "SHOP-1"), msg.Text(msg.TaskFlowApplied, "<время>"))
	want := head + "\n" + clitest.ShopTables() + "\n" + cli.HintText(msg.HintFlowStage) + "\n"
	if code, stdout, stderr := clitest.Run("flow", "show"); code != contract.ExitOK || clitest.Masked(stdout) != want {
		t.Errorf("before a change: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	out := showJSON(t)
	if out.Task == nil || out.Task.Id != "SHOP-1" || !sameApplied(&out.Task.Flow, taken) || out.Task.ActiveFlow != nil ||
		out.Applied != nil || out.Dir != nil || out.Draft != nil || len(out.Stages) != 6 {
		t.Errorf("before a change: %+v", out)
	}

	// The merge stage changes and a stage is added; a draft stays open.
	p := clitest.ShopPlaces()
	clitest.WriteDraft(t, p, map[string]string{
		"stages/merge.md":      "Влить ветку задачи в main после ревью.\n",
		"stages/security.yaml": "title: Безопасность\nexit: проверка пройдена\nexecutor: orchestrator\n",
		"stages/security.md":   "Проверить уязвимости.\n",
	})
	applied := applyJSON(t, "flow")
	clitest.WriteDraft(t, p, map[string]string{"stages/branch.md": "Создать ветку от main.\n"})

	want = clitest.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowTask, "SHOP-1"), msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskActiveFlowChanged, "<время>")) + "\n" + clitest.ShopTables() + "\n" +
		clitest.Lines(cli.HintText(msg.HintFlowStage), cli.HintText(msg.HintFlowActive))
	if code, stdout, stderr := clitest.Run("flow", "show"); code != contract.ExitOK || clitest.Masked(stdout) != want {
		t.Errorf("after a change: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	out = showJSON(t)
	if out.Task == nil || !sameApplied(out.Task.ActiveFlow, applied) || !sameApplied(&out.Task.Flow, taken) || len(out.Stages) != 6 {
		t.Errorf("after a change: %+v", out)
	}

	// An object is looked for in the flow of the task: the project of the
	// task named by --project changes nothing.
	merge := clitest.Lines(
		msg.Text(msg.FlowTask, "SHOP-1"),
		msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskActiveFlowChanged, "<время>"),
		"",
		msg.Text(msg.FlowStage, "merge"),
		msg.Text(msg.FlowTitle, "Слияние"),
		msg.Text(msg.FlowExecutor, "operator"),
		msg.Text(msg.FlowExit, "ветка задачи влита в main"),
		msg.Text(msg.FlowParts, clitest.None),
		msg.Text(msg.FlowScenarios, "bug, feature"),
		"",
		msg.Text(msg.FlowInstruction),
		"  Влить ветку задачи в main.",
		"",
		cli.HintText(msg.HintFlowActive),
	)
	for _, args := range [][]string{{"--stage", "merge"}, {"--project", "shop", "--stage", "merge"}} {
		if code, stdout, stderr := clitest.Run(append([]string{"flow", "show"}, args...)...); code != contract.ExitOK || clitest.Masked(stdout) != merge {
			t.Errorf("%v: exit code %d, stderr %q, output:\n%s\nwant:\n%s", args, code, stderr, stdout, merge)
		}
	}
	if code, errCode := clitest.ErrorCode(t, "flow", "show", "--stage", "security"); code != contract.ExitError || errCode != contract.CodeFlowObjectNotFound {
		t.Errorf("a stage added after the take: exit code %d, %s", code, errCode)
	}

	// --active shows the active flow, as flow show does in a worktree with
	// no task.
	activeMerge := clitest.Lines(
		msg.Text(msg.FlowStage, "merge"),
		msg.Text(msg.FlowTitle, "Слияние"),
		msg.Text(msg.FlowExecutor, "operator"),
		msg.Text(msg.FlowExit, "ветка задачи влита в main"),
		msg.Text(msg.FlowParts, clitest.None),
		msg.Text(msg.FlowScenarios, "bug, feature"),
		"",
		msg.Text(msg.FlowInstruction),
		"  Влить ветку задачи в main после ревью.",
	)
	if code, stdout, stderr := clitest.Run("flow", "show", "--active", "--stage", "merge"); code != contract.ExitOK || stdout != activeMerge {
		t.Errorf("--active --stage merge: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, activeMerge)
	}
	out = showJSON(t, "--active")
	if out.Task != nil || out.Applied == nil || out.Applied.Commit != applied.Commit || out.Dir == nil || *out.Dir != p.Dir ||
		out.Draft == nil || len(out.Stages) != 7 {
		t.Errorf("--active: %+v", out)
	}
	_, inFix, _ := clitest.Run("flow", "show", "--active", "--json")
	t.Chdir(shop)
	_, active, _ := clitest.Run("flow", "show", "--active", "--json")
	_, free, _ := clitest.Run("flow", "show", "--json")
	if active != free || active != inFix {
		t.Errorf("the active flow differs: in the worktree of the task\n%s\nwith --active\n%s\nwithout\n%s", inFix, active, free)
	}
	if code, errCode := clitest.ErrorCode(t, "flow", "show", "--active", "--draft"); code != contract.ExitUsage || errCode != contract.CodeConflictingFlags {
		t.Errorf("--active --draft: exit code %d, %s", code, errCode)
	}
}

// TestFlowShowTaskLibrary checks a library subagent of the flow of a task
// after the library changed it: as the task has it, with the hint to the
// active flow. Which changes of the library count, TestActiveFlow of
// package task checks.
func TestFlowShowTaskLibrary(t *testing.T) {
	_, _, _ = takeInFix(t)
	lib := clitest.ShopPlaces().Library
	text, err := os.ReadFile(filepath.Join(lib, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteFiles(t, lib, map[string]string{"reviewer.md": string(text) + "Проверить логи.\n"})
	clitest.MustRun(t, "library", "apply")
	_, stdout, _ := clitest.Run("flow", "show", "--agent", "reviewer")
	if strings.Contains(stdout, "Проверить логи.") || !strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintFlowActive)+"\n") {
		t.Errorf("--agent reviewer:\n%s", stdout)
	}
}
