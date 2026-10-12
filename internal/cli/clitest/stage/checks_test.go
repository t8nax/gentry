package stage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// TestStageChecks takes a task whose first stage has two commands of checks:
// git, and the test binary, which does what the variable helperEnv names. It
// checks the refusals of a command that fails, of checks out of time and of
// a program not found, that the refusals without commands come first, and
// the exit once the commands pass.
func TestStageChecks(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := `"` + exe + `" check`
	git := "git rev-parse --is-inside-work-tree"
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{
		"stages/branch.yaml": "title: Ветка\nexit: создана ветка задачи\nexecutor: orchestrator\n" +
			"checks:\n  - " + git + "\n  - '" + helper + "'\ntimeout: 8s\n",
	})
	clitest.MustRun(t, "flow", "apply")
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	logs := filepath.Join(os.Getenv(home.EnvVar), "state", "shop", "tasks", "SHOP-1", "checks")
	log1, log2 := filepath.Join(logs, "branch-1-1.log"), filepath.Join(logs, "branch-1-2.log")

	stdout := clitest.MustRun(t, "stage", "show")
	if want := clitest.Lines(msg.Text(msg.FlowParts, clitest.None), msg.Text(msg.FlowCheckTimeout, "8s"), msg.Text(msg.FlowChecks),
		"  "+git, "  "+helper, ""); !strings.Contains(stdout, want) {
		t.Errorf("stage show:\n%s", stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/stage-show.json", "stage", "show")
	if want := `"checks":["git rev-parse --is-inside-work-tree",` + clitest.JSONString(helper) + `],`; !strings.Contains(out, want) ||
		!strings.Contains(out, `"timeout":"8s"`) {
		t.Errorf("stage show --json: %s", out)
	}

	// Without steps the exit is refused before the commands run.
	t.Setenv(helperEnv, "sleep")
	start := time.Now()
	if code, c := clitest.ErrorCode(t, "stage", "exit", "--kind", "result", "--text", "Готово"); code != contract.ExitError || c != contract.CodeStepsEmpty {
		t.Errorf("exit without steps: exit code %d, code %s", code, c)
	}
	if d := time.Since(start); d > 2*time.Second || clitest.FileExists(logs) {
		t.Errorf("exit without steps ran the commands: %v", d)
	}
	clitest.MustRun(t, "step", "add", "Создать ветку")
	clitest.MustRun(t, "step", "done", "1")

	t.Setenv(helperEnv, "fail")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrCheckFailed, helper),
		msg.Text(msg.CheckExitCodeLine, 3),
		msg.Text(msg.CheckLogLine, log2),
		"",
		"итог: 1 ошибка",
		"cart_test.go:7: Total = 5, want 6",
	), "stage", "exit", "--kind", "result", "--text", "Готово")
	if got := read(t, log1); got != "true\n" {
		t.Errorf("the output of git: %q", got)
	}
	code, out, _ := clitest.Run("stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	clitest.Validate(t, "schemas/error.json", out)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "command": helper, "exit_code": 3.0, "log": log2}; code != contract.ExitError ||
		e.Error.Code != contract.CodeChecksFailed || !clitest.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with a command failed: exit code %d: %s", code, out)
	}

	t.Setenv(helperEnv, "sleep")
	start = time.Now()
	code, out, _ = clitest.Run("stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "timeout": "8s", "command": helper, "log": log2}; code != contract.ExitError ||
		e.Error.Code != contract.CodeChecksTimeout || !clitest.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with checks out of time: exit code %d: %s", code, out)
	}
	if d := time.Since(start); d > 16*time.Second {
		t.Errorf("checks of 8s ended after %v", d)
	}

	// Without git on PATH the first command finds no program, and the files
	// of the last attempt are gone.
	path := os.Getenv("PATH")
	t.Setenv("PATH", "")
	code, out, _ = clitest.Run("stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "command": git, "program": "git"}; code != contract.ExitError ||
		e.Error.Code != contract.CodeCheckProgramNotFound || !clitest.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with a program not found: exit code %d: %s", code, out)
	}
	if clitest.FileExists(log1) || clitest.FileExists(log2) {
		t.Error("the files of the output of the last attempt are left")
	}
	t.Setenv("PATH", path)

	t.Setenv(helperEnv, "pass")
	stdout = clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Готово")
	if want := clitest.Lines(msg.Text(msg.ExitTextLine, "Готово"), msg.Text(msg.ChecksPassedLine, 2), msg.Text(msg.TransitionLine, "plan")); !strings.Contains(stdout, want) {
		t.Errorf("stage exit:\n%s", stdout)
	}
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "--path")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if len(shown.Path) != 2 || !clitest.JSONEqual(shown.Path[0].ChecksLogs, []string{log1, log2}) || shown.Path[1].ChecksLogs != nil {
		t.Errorf("task show --path --json: %s", out)
	}
}

// read returns the text of a file with line ends of \n.
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}
