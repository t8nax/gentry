package task_test

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

// passScenario takes the task of the current worktree through the feature
// scenario by skips: the review goes on to the merge, which the operator
// closes.
func passScenario(t *testing.T) {
	t.Helper()
	for range 3 {
		testkit.MustRun(t, "task", "stage", "skip", "--reason", "Не нужен")
	}
	testkit.MustRun(t, "task", "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	testkit.MustRun(t, "task", "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
}

// lastEvent returns the type and the data of the last event of the journal.
func lastEvent(t *testing.T) (string, string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(testkit.MustRun(t, "events")), "\n")
	var e struct {
		Type string          `json:"type"`
		Task string          `json:"task"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &e); err != nil {
		t.Fatal(err)
	}
	return e.Type, string(e.Data)
}

// The words are those of the catalog: the tests of the output in package
// cli state them.

// listHeader is the header of task list in a project.
var listHeader = []string{msg.Text(msg.ColNumber), msg.Text(msg.ColTitle), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
	msg.Text(msg.ColStage), msg.Text(msg.ColWorktree)}

// taskEnded is the refusal of a command that writes the closed task key.
func taskEnded(key string) string {
	return testkit.Lines(msg.Text(msg.TaskClosed, key), "", cli.HintFor(msg.HintTaskShow, "task", key))
}

func TestTaskClose(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	_, stdout, _ := testkit.Run("task", "show")
	if !strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintStatement)+"\n") || strings.Contains(stdout, "task close") {
		t.Errorf("task show of a passed scenario:\n%s", stdout)
	}

	testkit.WantRun(t, contract.ExitOK, testkit.Lines(
		msg.Text(msg.TaskClosed, "SHOP-1"),
		msg.Text(msg.WorktreeReleased, fix),
	), "", "task", "close")
	typ, data := lastEvent(t)
	testkit.Validate(t, "schemas/events/task.closed.json", data)
	if want := `{"attempt":1,"source":"agent","worktree":` + testkit.JSONString(fix) + `}`; typ != "task.closed" || data != want {
		t.Errorf("event %s %s, want task.closed %s", typ, data, want)
	}

	t.Chdir(shop)
	_, stdout, _ = testkit.Run("task", "show", "SHOP-1")
	if !strings.Contains(testkit.Masked(stdout), msg.Text(msg.TaskState, msg.Text(msg.TaskStateClosed))+"\n") ||
		!strings.Contains(testkit.Masked(stdout), testkit.Lines(msg.Text(msg.TaskTakenAt, "<время>"), msg.Text(msg.TaskClosedByAgent, "<время>"),
			msg.Text(msg.TaskFlowApplied, "<время>"), "")) ||
		strings.Contains(stdout, fix) ||
		!strings.HasSuffix(stdout, "\n\n"+cli.HintFor(msg.HintStatement, "task", "SHOP-1")+"\n") {
		t.Errorf("task show of a closed task:\n%s", stdout)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "SHOP-1")
	if !strings.Contains(out, `"ended":{"source":"agent","time":"`) || strings.Contains(out, `"worktree"`) {
		t.Errorf("task show --json of a closed task: %s", out)
	}
	testkit.WantSameTime(t, "the time of closing in task show --json", testkit.JSONAt(t, out, "task.ended.time"))
	// Closed, the task is not in the list of open ones; the worktree takes
	// the next task.
	if _, stdout, _ := testkit.Run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("task list:\n%s", stdout)
	}
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	_, stdout, _ = testkit.Run("task", "list", "--all")
	if stdout != testkit.Table(listHeader,
		[]string{"SHOP-1", "Частичный возврат по карте", msg.Text(msg.TaskStateClosed), "Фича", msg.Text(msg.StageFinished), testkit.None},
		[]string{"SHOP-2", "Частичный возврат по карте", msg.Text(msg.TaskStateActive), "Фича", "Ветка", fix}) {
		t.Errorf("task list --all:\n%s", stdout)
	}

	// A closed task is closed once and is not cancelled.
	closed := taskEnded("SHOP-1")
	testkit.WantRun(t, contract.ExitError, "", closed, "task", "close", "SHOP-1")
	testkit.WantRun(t, contract.ExitError, "", closed, "task", "cancel", "SHOP-1")
	if code, c := testkit.ErrorCode(t, "task", "close", "SHOP-1"); code != contract.ExitError || c != contract.CodeTaskEnded {
		t.Errorf("close a closed task: exit code %d, code %s", code, c)
	}
}

func TestTaskCloseRefusals(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)

	// The scenario is not passed: in the worktree and outside it.
	t.Chdir(fix)
	stageLine := msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1))
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		stageLine,
		"",
		cli.HintText(msg.HintStageShow),
	), "task", "close")
	t.Chdir(shop)
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		stageLine,
		"",
		cli.HintFor(msg.HintStageShow, "task", "SHOP-1"),
	), "task", "close", "shop-1")
	out := testkit.WantJSON(t, contract.ExitError, "schemas/error.json", "task", "close", "SHOP-1")
	if !strings.Contains(out, `"code":"scenario_not_finished"`) || !strings.Contains(out, `"details":{"node":"branch","task":"SHOP-1"}`) {
		t.Errorf("task close --json: %s", out)
	}

	// Changes in the worktree are not touched.
	t.Chdir(fix)
	passScenario(t)
	if err := os.WriteFile(filepath.Join(fix, "refund.go"), []byte("package payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrWorktreeDirty, fix),
		"",
		msg.Text(msg.WorktreeChangedFiles),
		"  refund.go",
		"",
		msg.Text(msg.HintWorktreeDirty),
	), "task", "close")
	if code, c := testkit.ErrorCode(t, "task", "cancel"); code != contract.ExitError || c != contract.CodeWorktreeDirty {
		t.Errorf("cancel with changes: exit code %d, code %s", code, c)
	}
	if _, stdout, _ := testkit.Run("task", "show"); !strings.Contains(stdout, msg.Text(msg.TaskState, msg.Text(msg.TaskStateActive))+"\n") {
		t.Errorf("the task changed:\n%s", stdout)
	}

	// No task here; no such task; not a number of a task.
	t.Chdir(filepath.Dir(shop))
	if code, c := testkit.ErrorCode(t, "task", "close"); code != contract.ExitError || c != contract.CodeTaskUndetermined {
		t.Errorf("close outside a worktree: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "cancel", "SHOP-9"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("cancel SHOP-9: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "close", "9"); code != contract.ExitUsage || c != contract.CodeInvalidArgument {
		t.Errorf("close 9: exit code %d, code %s", code, c)
	}
}

func TestTaskCancel(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	testkit.MustRun(t, "task", "note", "add", "Промокоды в таблице promo.")

	testkit.WantRun(t, contract.ExitOK, testkit.Lines(
		msg.Text(msg.TaskCancelled, "SHOP-1"),
		msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1)),
		msg.Text(msg.ReasonLine, "Отложено до релиза каталога."),
		msg.Text(msg.WorktreeReleased, fix),
		"",
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-1"),
	), "", "task", "cancel", "--reason", "Отложено до релиза каталога.")
	typ, data := lastEvent(t)
	testkit.Validate(t, "schemas/events/task.cancelled.json", data)
	if want := `{"attempt":1,"node":"branch","reason":"Отложено до релиза каталога.","source":"operator","worktree":` + testkit.JSONString(fix) + `}`; typ != "task.cancelled" || data != want {
		t.Errorf("event %s %s, want task.cancelled %s", typ, data, want)
	}

	t.Chdir(shop)
	_, stdout, _ := testkit.Run("task", "show", "SHOP-1")
	want := testkit.Lines(
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskState, msg.Text(msg.TaskStateCancelled)),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
		msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1)),
		msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)),
		msg.Text(msg.TaskTakenAt, "<время>"),
		msg.Text(msg.TaskCancelledByOperator, "<время>"),
		msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.CancelReasonLine, "Отложено до релиза каталога."),
		"",
	) + testkit.Table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", testkit.None, testkit.None},
	) + testkit.Lines(
		"",
		msg.Text(msg.StepsNone),
		"",
		cli.HintFor(msg.HintStatement, "task", "SHOP-1"),
		cli.HintFor(msg.HintNotes, "task", "SHOP-1"),
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-1"),
	)
	if testkit.Masked(stdout) != want {
		t.Errorf("task show of a cancelled task:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := testkit.Run("task", "list", "--state", "cancelled"); stdout != testkit.Table(listHeader,
		[]string{"SHOP-1", "Частичный возврат по карте", msg.Text(msg.TaskStateCancelled), "Фича", "Ветка", testkit.None}) {
		t.Errorf("task list --state cancelled:\n%s", stdout)
	}

	// A passed scenario is cancelled too, without a reason, by the agent
	// and with the fields in --input.
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	t.Setenv(testkit.SessionEnv, "s1")
	testkit.WantRun(t, contract.ExitOK, testkit.Lines(
		msg.Text(msg.TaskCancelled, "SHOP-2"),
		msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
		msg.Text(msg.WorktreeReleased, fix),
		"",
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-2"),
	), "", "task", "cancel")
	t.Setenv(testkit.SessionEnv, "")
	if _, stdout, _ := testkit.Run("task", "show", "SHOP-2"); !strings.Contains(testkit.Masked(stdout), msg.Text(msg.TaskCancelledByAgent, "<время>")+"\n") ||
		strings.Contains(stdout, msg.Text(msg.CancelReasonLine, "")) {
		t.Errorf("task show of a task cancelled by the agent:\n%s", stdout)
	}
	_, data = lastEvent(t)
	if want := `{"attempt":1,"node":"finish","source":"agent","worktree":` + testkit.JSONString(fix) + `}`; data != want {
		t.Errorf("event %s, want %s", data, want)
	}

	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	input := filepath.Join(t.TempDir(), "cancel.json")
	if err := os.WriteFile(input, []byte(`{"reason":"Первая строка.\nВторая строка."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-cancel.json", "task", "cancel", "SHOP-3", "--input", input)
	if !strings.Contains(out, `"reason":"Первая строка.\nВторая строка."`) || !strings.Contains(out, `"worktree":`+testkit.JSONString(fix)+`}`) {
		t.Errorf("task cancel --json: %s", out)
	}
	// The times of the cancelling are read back as written.
	show := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "SHOP-3")
	attempts := testkit.WantJSON(t, contract.ExitOK, "schemas/task-attempt-list.json", "task", "attempt", "list", "--task", "SHOP-3")
	testkit.WantSameTime(t, "the time of cancelling in task cancel, task show and task attempt list --json",
		testkit.JSONAt(t, out, "task.ended.time"), testkit.JSONAt(t, show, "task.ended.time"), testkit.JSONAt(t, attempts, "attempts.0.ended.time"))
	testkit.WantSameTime(t, "the time of taking in task cancel and task attempt list --json",
		testkit.JSONAt(t, out, "task.taken"), testkit.JSONAt(t, attempts, "attempts.0.taken"))
	testkit.WantRun(t, contract.ExitUsage, "", testkit.Lines(msg.Text(msg.ErrConflictingFlags, "--reason", "--input"), "", cli.HintText(msg.HintCommandHelp, "task cancel")),
		"task", "cancel", "SHOP-3", "--reason", "Причина", "--input", input)
}

// TestEndedRefusesWrites checks that every command that writes a task
// refuses a closed and a cancelled one, and the commands that read do not.
func TestEndedRefusesWrites(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	testkit.MustRun(t, "task", "close")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	testkit.MustRun(t, "task", "cancel")
	t.Chdir(shop)

	file := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(file, []byte("План"), 0o644); err != nil {
		t.Fatal(err)
	}
	writes := [][]string{
		{"task", "stage", "exit", "--kind", "result", "--text", "Готово"},
		{"task", "stage", "skip", "--reason", "Не нужен"},
		{"task", "step", "add", "Шаг"},
		{"task", "step", "done", "1"},
		{"task", "step", "drop", "1", "--reason", "Не нужен"},
		{"task", "note", "add", "Проверить логи."},
		{"task", "artifact", "save", "plan.md", "--file", file},
		{"task", "artifact", "save", "plan", "--url", "https://example.com/plan"},
		{"task", "decision", "add", "--answer", "Да."},
	}
	for _, task := range []struct{ key, text string }{
		{"SHOP-1", taskEnded("SHOP-1")},
		{"SHOP-2", testkit.Lines(msg.Text(msg.TaskCancelled, "SHOP-2"), "", cli.HintFor(msg.HintTaskAgain, "task", "SHOP-2"))},
	} {
		for _, w := range writes {
			testkit.WantRun(t, contract.ExitError, "", task.text, append(w, "--task", task.key)...)
			if code, c := testkit.ErrorCode(t, append(w, "--task", task.key)...); code != contract.ExitError || c != contract.CodeTaskEnded {
				t.Errorf("%v of %s: exit code %d, code %s", w, task.key, code, c)
			}
		}
		testkit.MustRun(t, "task", "note", "list", "--task", task.key)
	}
	// A cancelled task shows the stage it stopped at; a closed one has none.
	testkit.MustRun(t, "task", "stage", "show", "--task", "SHOP-2")
	testkit.WantRun(t, contract.ExitError, "", taskEnded("SHOP-1"), "task", "stage", "show", "--task", "SHOP-1")
	if testkit.FileExists(filepath.Join(os.Getenv("GENTRY_HOME"), "state", "shop", "tasks", "SHOP-2", "artifacts")) {
		t.Error("the artifact of a cancelled task is saved")
	}
}

func TestTaskTakeAgain(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	testkit.MustRun(t, "task", "note", "add", "Промокоды в таблице promo.")
	testkit.MustRun(t, "task", "decision", "add", "--answer", "Только для карт.")
	plan := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(plan, []byte("План 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.MustRun(t, "task", "artifact", "save", "plan.md", "--file", plan)
	testkit.MustRun(t, "task", "step", "add", "Создать ветку")
	testkit.MustRun(t, "task", "step", "done", "1")
	testkit.MustRun(t, "task", "stage", "exit", "--kind", "result", "--text", "Ветка создана")
	testkit.MustRun(t, "task", "cancel", "--reason", "Отложено.")

	// Taken anew in the main worktree by the agent: the title and the
	// statement stay those of the operator.
	t.Chdir(shop)
	t.Setenv(testkit.SessionEnv, "s1")
	testkit.WantRun(t, contract.ExitOK, testkit.Lines(
		msg.Text(msg.TaskTakenAnew, "SHOP-1"),
		msg.Text(msg.AttemptLine, 2),
		msg.Text(msg.TaskTitle, "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, "Баг"),
		msg.Text(msg.TaskStage, "Ветка"),
		msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
		msg.Text(msg.TaskWorktree, shop),
		"",
		cli.HintText(msg.HintTaskShow),
	), "", "task", "take", "shop-1", "--scenario", "bug")
	t.Setenv(testkit.SessionEnv, "")
	typ, data := lastEvent(t)
	testkit.Validate(t, "schemas/events/task.taken.json", data)
	if typ != "task.taken" || !strings.HasPrefix(data, `{"attempt":2,`) || !strings.Contains(data, `"source":"operator"`) {
		t.Errorf("event %s %s", typ, data)
	}

	// The new attempt starts anew: no path, notes, decisions or artifacts
	// of the first.
	_, stdout, _ := testkit.Run("task", "show")
	if !strings.Contains(stdout, testkit.Lines(msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Баг", "bug")),
		msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1)), msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)))) ||
		strings.Contains(stdout, msg.Text(msg.HintNotes)) || strings.Contains(stdout, "plan.md") ||
		!strings.HasSuffix(stdout, "\n\n"+testkit.Lines(cli.HintText(msg.HintStatement), cli.HintText(msg.HintAttempts))) {
		t.Errorf("task show of the second attempt:\n%s", stdout)
	}
	if _, stdout, _ := testkit.Run("task", "show", "--statement"); strings.Contains(stdout, "Только для карт") {
		t.Errorf("the decisions of the first attempt are in the second:\n%s", stdout)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show")
	if !strings.Contains(out, `"attempt":2,`) || strings.Contains(out, `"ended"`) {
		t.Errorf("task show --json: %s", out)
	}
	taken := testkit.JSONAt(t, out, "task.taken")

	// An artifact of the same name lies apart from the one of the first.
	if err := os.WriteFile(plan, []byte("План 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.MustRun(t, "task", "artifact", "save", "plan.md", "--file", plan)
	tasks := filepath.Join(os.Getenv("GENTRY_HOME"), "state", "shop", "tasks", "SHOP-1")
	for p, want := range map[string]string{
		filepath.Join(tasks, "artifacts", "plan.md"):                  "План 1",
		filepath.Join(tasks, "attempts", "2", "artifacts", "plan.md"): "План 2",
	} {
		if b, err := os.ReadFile(p); err != nil || string(b) != want {
			t.Errorf("%s: %q, %v; want %q", p, b, err, want)
		}
	}

	// The attempts.
	t.Chdir(filepath.Dir(shop))
	_, stdout, _ = testkit.Run("task", "attempt", "list", "--task", "SHOP-1")
	// Any time of the width of the times printed: the table is compared masked.
	const at = "2026-01-01 00:00"
	want := testkit.Table([]string{msg.Text(msg.ColAttempt), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
		msg.Text(msg.ColTaken), msg.Text(msg.ColEnded), msg.Text(msg.ColCancelReason)},
		[]string{"1", msg.Text(msg.TaskStateCancelled), "Фича", at, at, "Отложено."},
		[]string{"2", msg.Text(msg.TaskStateActive), "Баг", at, testkit.None, testkit.None}) + "\n" + cli.HintFor(msg.HintAttempt, "task", "SHOP-1") + "\n"
	if testkit.Masked(stdout) != testkit.Masked(want) {
		t.Errorf("task attempt list:\n%s\nwant:\n%s", stdout, want)
	}
	attempts := testkit.WantJSON(t, contract.ExitOK, "schemas/task-attempt-list.json", "task", "attempt", "list", "--task", "SHOP-1")
	testkit.WantSameTime(t, "the time of taking the second attempt in task show and task attempt list --json", taken, testkit.JSONAt(t, attempts, "attempts.1.taken"))
	_, stdout, _ = testkit.Run("task", "attempt", "show", "1", "--task", "SHOP-1")
	for _, part := range []string{
		testkit.Lines(msg.Text(msg.AttemptHeading, "SHOP-1", 1, "Частичный возврат по карте"), "", msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.TaskState, msg.Text(msg.TaskStateCancelled))),
		testkit.Lines(msg.Text(msg.CancelReasonLine, "Отложено."), "", testkit.Round("Ветка", "branch", 1),
			msg.Text(msg.OutcomeLine, msg.Text(msg.ExitKindResult)), msg.Text(msg.ExitTextLine, "Ветка создана")),
		"\n" + testkit.Lines(testkit.Round("План фичи", "plan-feature", 1), msg.Text(msg.OutcomeLine, testkit.None)),
		"\nplan.md   " + msg.Text(msg.ArtifactKindFile),
		"\n\n" + testkit.Lines(msg.Text(msg.NotesHeading), "  "+msg.Text(msg.NoteHeading, 1, testkit.Round("Ветка", "branch", 1)), "     Промокоды в таблице promo."),
		"\n\n" + testkit.Lines(msg.Text(msg.DecisionsHeading),
			"  "+msg.Text(msg.DecisionHeading, 1, testkit.Round("Ветка", "branch", 1), msg.Text(msg.TaskSourceOperator))),
	} {
		if !strings.Contains(stdout, part) {
			t.Errorf("task attempt show 1 --task SHOP-1 has no %q:\n%s", part, stdout)
		}
	}
	if strings.Contains(stdout, ": gentry ") {
		t.Errorf("task attempt show 1 --task SHOP-1 has hints:\n%s", stdout)
	}
	first := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "attempt", "show", "1", "--task", "SHOP-1")
	testkit.WantSameTime(t, "the time of cancelling the first attempt in task attempt list --json",
		testkit.JSONAt(t, attempts, "attempts.0.ended.time"), testkit.JSONAt(t, first, "task.ended.time"))
	testkit.WantSameTime(t, "the time of taking the first attempt in task attempt list --json",
		testkit.JSONAt(t, attempts, "attempts.0.taken"), testkit.JSONAt(t, first, "task.taken"))
	// The refusal of an attempt that the task has not is built from the
	// attempts of the store: no test of package cli reaches it, this test
	// states its words.
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		"У задачи SHOP-1 нет попытки 5.",
		"Попыток: 2",
		"",
		cli.HintFor(msg.HintAttemptsList, "task", "SHOP-1"),
	), "task", "attempt", "show", "5", "--task", "SHOP-1")
	if code, c := testkit.ErrorCode(t, "task", "attempt", "show", "5", "--task", "SHOP-1"); code != contract.ExitError || c != contract.CodeAttemptNotFound {
		t.Errorf("attempt 5: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "attempt", "show", "первая", "--task", "SHOP-1"); code != contract.ExitUsage || c != contract.CodeInvalidArgument {
		t.Errorf("attempt «первая»: exit code %d, code %s", code, c)
	}
	// In the worktree of the task the attempt alone is enough.
	t.Chdir(shop)
	if _, stdout, _ := testkit.Run("task", "attempt", "show", "2"); !strings.HasPrefix(stdout, msg.Text(msg.AttemptHeading, "SHOP-1", 2, "Частичный возврат по карте")+"\n") {
		t.Errorf("task attempt show 2:\n%s", stdout)
	}
	if _, stdout, _ := testkit.Run("task", "list", "--all"); strings.Count(stdout, "SHOP-1") != 1 || !strings.Contains(stdout, "SHOP-1  Частичный возврат по карте  "+msg.Text(msg.TaskStateActive)) {
		t.Errorf("task list --all:\n%s", stdout)
	}
	out = testkit.WantJSON(t, contract.ExitOK, "schemas/task-list.json", "task", "list", "--all")
	if !strings.Contains(out, `"attempt":2,`) {
		t.Errorf("task list --json: %s", out)
	}
}

func TestTaskTakeAgainRefusals(t *testing.T) {
	shop, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	testkit.MustRun(t, "task", "close")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	testkit.MustRun(t, "task", "cancel")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(shop)

	testkit.WantRun(t, contract.ExitError, "", taskEnded("SHOP-1"), "task", "take", "SHOP-1", "--scenario", "feature")
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrTaskInWork, "SHOP-3"),
		msg.Text(msg.TaskWorktree, fix),
		"",
		cli.HintFor(msg.HintTaskShow, "task", "SHOP-3"),
	), "task", "take", "SHOP-3", "--scenario", "feature")
	if code, c := testkit.ErrorCode(t, "task", "take", "SHOP-3", "--scenario", "feature"); code != contract.ExitError || c != contract.CodeTaskInWork {
		t.Errorf("take a task in work: exit code %d, code %s", code, c)
	}
	testkit.WantRun(t, contract.ExitUsage, "", testkit.Lines(msg.Text(msg.ErrTaskKeyWithFlag, "--title"), "", cli.HintText(msg.HintCommandHelp, "task take")),
		"task", "take", "SHOP-2", "--scenario", "feature", "--title", "Скидка")
	refused := testkit.WantJSON(t, contract.ExitUsage, "schemas/error.json", "task", "take", "SHOP-2", "--scenario", "feature", "--title", "Скидка", "--json")
	if !strings.Contains(refused, `"code":"conflicting_flags"`) || !strings.Contains(refused, `"flags":["`+msg.Text(msg.ArgTask)+`","--title"]`) {
		t.Errorf("take SHOP-2 --title --json: %s", refused)
	}
	input := filepath.Join(t.TempDir(), "task.json")
	if err := os.WriteFile(input, []byte(`{"scenario":"feature","statement":"Текст."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.WantRun(t, contract.ExitUsage, "", testkit.Lines(msg.Text(msg.ErrTaskKeyWithFlag, "--statement"), "", cli.HintText(msg.HintCommandHelp, "task take")),
		"task", "take", "SHOP-2", "--input", input)
	if code, c := testkit.ErrorCode(t, "task", "take", "--task", "SHOP-2", "--scenario", "feature"); code != contract.ExitUsage || c != contract.CodeUnknownFlag {
		t.Errorf("take --task: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "take", "SHOP-2"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("take without a scenario: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "take", "SHOP-9", "--scenario", "feature"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("take SHOP-9: exit code %d, code %s", code, c)
	}
	if code, c := testkit.ErrorCode(t, "task", "take", "9", "--scenario", "feature"); code != contract.ExitUsage || c != contract.CodeInvalidArgument {
		t.Errorf("take 9: exit code %d, code %s", code, c)
	}
	if _, stdout, _ := testkit.Run("task", "attempt", "list", "--task", "SHOP-2"); strings.Count(stdout, "\n") != 4 {
		t.Errorf("a refused take made an attempt:\n%s", stdout)
	}

	// A worktree of another project.
	blog := gittest.Repo(t, filepath.Join(filepath.Dir(shop), "blog"))
	t.Chdir(blog)
	testkit.MustRun(t, "project", "add", "blog", "--knowledge", "../blog-knowledge")
	testkit.WantRun(t, contract.ExitError, "", testkit.Lines(
		msg.Text(msg.ErrTaskProjectMismatch, "SHOP-2"),
		msg.Text(msg.TaskProjectLine, "shop"),
		msg.Text(msg.WorktreeProjectLine, "blog"),
		"",
		cli.HintFor(msg.HintWorktreeListProject, "project", "shop"),
	), "task", "take", "SHOP-2", "--scenario", "feature")
	out := testkit.WantJSON(t, contract.ExitError, "schemas/error.json", "task", "take", "SHOP-2", "--scenario", "feature")
	if !strings.Contains(out, `"code":"task_project_mismatch"`) || !strings.Contains(out, `"worktree_project":"blog"`) {
		t.Errorf("take in the blog --json: %s", out)
	}
}
