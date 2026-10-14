package stage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// TestStageChecks takes a task whose first stage has two commands of checks:
// git, and the test binary, which does what the variable helperEnv names. It
// checks the refusals of a command that fails, of checks out of time and of
// a program not found, that the refusals without commands come first, and
// the exit once the commands pass.
func TestStageChecks(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := `"` + exe + `" check`
	git := "git rev-parse --is-inside-work-tree"
	testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
		"stages/branch.yaml": "title: Ветка\nexit: создана ветка задачи\nexecutor: orchestrator\n" +
			"checks:\n  - " + git + "\n  - '" + helper + "'\ntimeout: 8s\n",
	})
	testkit.MustRun(t, "flow", "apply")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	logs := filepath.Join(os.Getenv(home.EnvVar), "state", "shop", "tasks", "SHOP-1", "checks")
	log1, log2 := filepath.Join(logs, "branch-1-1.log"), filepath.Join(logs, "branch-1-2.log")

	stdout := testkit.MustRun(t, "task", "stage", "show")
	if want := testkit.Lines(msg.Text(msg.FlowParts, testkit.None), msg.Text(msg.FlowCheckTimeout, "8s"), msg.Text(msg.FlowChecks),
		"  "+git, "  "+helper, ""); !strings.Contains(stdout, want) {
		t.Errorf("task stage show:\n%s", stdout)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-stage-show.json", "task", "stage", "show")
	if want := `"checks":["git rev-parse --is-inside-work-tree",` + testkit.JSONString(helper) + `],`; !strings.Contains(out, want) ||
		!strings.Contains(out, `"timeout":"8s"`) {
		t.Errorf("task stage show --json: %s", out)
	}

	// Without steps the exit is refused before the commands run.
	t.Setenv(helperEnv, "sleep")
	start := time.Now()
	if code, c := testkit.ErrorCode(t, "task", "stage", "exit", "--kind", "result", "--text", "Готово"); code != contract.ExitError || c != contract.CodeStepsEmpty {
		t.Errorf("exit without steps: exit code %d, code %s", code, c)
	}
	if d := time.Since(start); d > 2*time.Second || testkit.FileExists(logs) {
		t.Errorf("exit without steps ran the commands: %v", d)
	}
	testkit.MustRun(t, "task", "step", "add", "Создать ветку")
	testkit.MustRun(t, "task", "step", "done", "1")

	t.Setenv(helperEnv, "fail")
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrCheckFailed, helper),
		msg.Text(msg.CheckExitCodeLine, 3),
		msg.Text(msg.CheckLogLine, log2),
		"",
		"итог: 1 ошибка",
		"cart_test.go:7: Total = 5, want 6",
	), "task", "stage", "exit", "--kind", "result", "--text", "Готово")
	if got := read(t, log1); got != "true\n" {
		t.Errorf("the output of git: %q", got)
	}
	code, out, _ := testkit.Run("task", "stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	testkit.Validate(t, "schemas/error.json", out)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "command": helper, "exit_code": 3.0, "log": log2}; code != contract.ExitError ||
		e.Error.Code != contract.CodeChecksFailed || !testkit.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with a command failed: exit code %d: %s", code, out)
	}

	t.Setenv(helperEnv, "sleep")
	start = time.Now()
	code, out, _ = testkit.Run("task", "stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "timeout": "8s", "command": helper, "log": log2}; code != contract.ExitError ||
		e.Error.Code != contract.CodeChecksTimeout || !testkit.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with checks out of time: exit code %d: %s", code, out)
	}
	if d := time.Since(start); d > 16*time.Second {
		t.Errorf("checks of 8s ended after %v", d)
	}

	// Without git on PATH the first command finds no program, and the files
	// of the last attempt are gone.
	path := os.Getenv("PATH")
	t.Setenv("PATH", "")
	code, out, _ = testkit.Run("task", "stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	json.Unmarshal([]byte(out), &e)
	if want := map[string]any{"node": "branch", "command": git, "program": "git"}; code != contract.ExitError ||
		e.Error.Code != contract.CodeCheckProgramNotFound || !testkit.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with a program not found: exit code %d: %s", code, out)
	}
	if testkit.FileExists(log1) || testkit.FileExists(log2) {
		t.Error("the files of the output of the last attempt are left")
	}
	t.Setenv("PATH", path)

	t.Setenv(helperEnv, "pass")
	stdout = testkit.MustRun(t, "task", "stage", "exit", "--kind", "result", "--text", "Готово")
	if want := testkit.Lines(msg.Text(msg.ExitTextLine, "Готово"), msg.Text(msg.ChecksPassedLine, 2), msg.Text(msg.TransitionLine, "plan")); !strings.Contains(stdout, want) {
		t.Errorf("task stage exit:\n%s", stdout)
	}
	out = testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "--path")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if len(shown.Path) != 2 || !testkit.JSONEqual(shown.Path[0].ChecksLogs, []string{log1, log2}) || shown.Path[1].ChecksLogs != nil {
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

// TestStageCheckNotStarted checks the refusal of a program of checks that
// is found, but is no program: the system does not start it.
func TestStageCheckNotStarted(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
		"stages/branch.yaml": "title: Ветка\nexit: создана ветка задачи\nexecutor: orchestrator\nchecks:\n  - ./bad.exe --all\n",
	})
	testkit.MustRun(t, "flow", "apply")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	if err := os.WriteFile(filepath.Join(fix, "bad.exe"), []byte("not a program\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.MustRun(t, "task", "step", "add", "Создать ветку")
	testkit.MustRun(t, "task", "step", "done", "1")
	code, out, _ := testkit.Run("task", "stage", "exit", "--kind", "result", "--text", "Готово", "--json")
	testkit.Validate(t, "schemas/error.json", out)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(out), &e)
	reason, _ := e.Error.Details["reason"].(string)
	if want := map[string]any{"node": "branch", "command": "./bad.exe --all", "program": "./bad.exe", "reason": reason}; code != contract.ExitError ||
		e.Error.Code != contract.CodeCheckProgramNotStarted || reason == "" || !testkit.JSONEqual(e.Error.Details, want) {
		t.Errorf("exit with a program not started: exit code %d: %s", code, out)
	}
	if logs := filepath.Join(os.Getenv(home.EnvVar), "state", "shop", "tasks", "SHOP-1", "checks", "branch-1-1.log"); testkit.FileExists(logs) {
		t.Error("a program not started has a file of output")
	}
}

// TestStageSkipChecks checks that a skip closes a stage whose command of
// checks fails without running it: no file of output is made.
func TestStageSkipChecks(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
		"stages/branch.yaml": "title: Ветка\nexit: создана ветка задачи\nexecutor: orchestrator\nchecks:\n  - '\"" + exe + "\" check'\n",
	})
	testkit.MustRun(t, "flow", "apply")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	t.Setenv(helperEnv, "fail")
	testkit.WantJSON(t, contract.ExitOK, "schemas/task-stage-close.json", "task", "stage", "skip", "--reason", "Ветка есть")
	if logs := filepath.Join(os.Getenv(home.EnvVar), "state", "shop", "tasks", "SHOP-1", "checks"); testkit.FileExists(logs) {
		t.Error("the skip ran the commands of checks")
	}
}
