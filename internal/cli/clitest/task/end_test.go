package task_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
)

// passScenario takes the task of the current worktree through the feature
// scenario by skips: the review goes on to the merge, which the operator
// closes.
func passScenario(t *testing.T) {
	t.Helper()
	for range 3 {
		clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен")
	}
	clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
}

// lastEvent returns the type and the data of the last event of the journal.
func lastEvent(t *testing.T) (string, string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(clitest.MustRun(t, "events")), "\n")
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

// round names a stage of the shop by its title and identifier with the
// round of its pass.
func round(title, stage string, n int) string {
	return msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, title, stage), n)
}

// taskEnded is the refusal of a command that writes the closed task key.
func taskEnded(key string) string {
	return clitest.Lines(msg.Text(msg.TaskClosed, key), "", cli.HintFor(msg.HintTaskShow, "task", key))
}

func TestTaskClose(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	_, stdout, _ := clitest.Run("task", "show")
	if !strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintStatement)+"\n") || strings.Contains(stdout, "task close") {
		t.Errorf("task show of a passed scenario:\n%s", stdout)
	}

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.TaskClosed, "SHOP-1"),
		msg.Text(msg.WorktreeReleased, fix),
	), "", "task", "close")
	typ, data := lastEvent(t)
	clitest.Validate(t, "schemas/events/task.closed.json", data)
	if want := `{"attempt":1,"source":"agent","worktree":` + clitest.JSONString(fix) + `}`; typ != "task.closed" || data != want {
		t.Errorf("event %s %s, want task.closed %s", typ, data, want)
	}

	t.Chdir(shop)
	_, stdout, _ = clitest.Run("task", "show", "SHOP-1")
	if !strings.Contains(clitest.Masked(stdout), msg.Text(msg.TaskState, msg.Text(msg.TaskStateClosed))+"\n") ||
		!strings.Contains(clitest.Masked(stdout), clitest.Lines(msg.Text(msg.TaskTakenAt, "<время>"), msg.Text(msg.TaskClosedByAgent, "<время>"),
			msg.Text(msg.TaskFlowApplied, "<время>"), "")) ||
		strings.Contains(stdout, msg.Text(msg.TaskWorktree, "")) ||
		!strings.HasSuffix(stdout, "\n\n"+cli.HintFor(msg.HintStatement, "task", "SHOP-1")+"\n") {
		t.Errorf("task show of a closed task:\n%s", stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "SHOP-1")
	if !strings.Contains(out, `"ended":{"source":"agent","time":"`) || strings.Contains(out, `"worktree"`) {
		t.Errorf("task show --json of a closed task: %s", out)
	}
	// Closed, the task is not in the list of open ones; the worktree takes
	// the next task.
	if _, stdout, _ := clitest.Run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("task list:\n%s", stdout)
	}
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	_, stdout, _ = clitest.Run("task", "list", "--all")
	if stdout != clitest.Table(listHeader,
		[]string{"SHOP-1", "Частичный возврат по карте", msg.Text(msg.TaskStateClosed), "Фича", msg.Text(msg.StageFinished), msg.Text(msg.ValueNone)},
		[]string{"SHOP-2", "Частичный возврат по карте", msg.Text(msg.TaskStateActive), "Фича", "Ветка", fix}) {
		t.Errorf("task list --all:\n%s", stdout)
	}

	// A closed task is closed once and is not cancelled.
	closed := taskEnded("SHOP-1")
	clitest.WantRun(t, contract.ExitError, "", closed, "task", "close", "SHOP-1")
	clitest.WantRun(t, contract.ExitError, "", closed, "task", "cancel", "SHOP-1")
	if code, c := clitest.ErrorCode(t, "task", "close", "SHOP-1"); code != contract.ExitError || c != contract.CodeTaskEnded {
		t.Errorf("close a closed task: exit code %d, code %s", code, c)
	}
}

func TestTaskCloseRefusals(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)

	// The scenario is not passed: in the worktree and outside it.
	t.Chdir(fix)
	notFinished := msg.Text(msg.TaskStage, msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, "Ветка", "branch"), 1))
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		notFinished,
		"",
		cli.HintText(msg.HintStageShow),
	), "task", "close")
	t.Chdir(shop)
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		notFinished,
		"",
		cli.HintFor(msg.HintStageShow, "task", "SHOP-1"),
	), "task", "close", "shop-1")
	out := clitest.WantJSON(t, contract.ExitError, "schemas/error.json", "task", "close", "SHOP-1")
	if !strings.Contains(out, `"code":"scenario_not_finished"`) || !strings.Contains(out, `"details":{"node":"branch","task":"SHOP-1"}`) {
		t.Errorf("task close --json: %s", out)
	}

	// Changes in the worktree are not touched.
	t.Chdir(fix)
	passScenario(t)
	if err := os.WriteFile(filepath.Join(fix, "refund.go"), []byte("package payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrWorktreeDirty, fix),
		"",
		msg.Text(msg.WorktreeChangedFiles),
		"  refund.go",
		"",
		msg.Text(msg.HintWorktreeDirty),
	), "task", "close")
	if code, c := clitest.ErrorCode(t, "task", "cancel"); code != contract.ExitError || c != contract.CodeWorktreeDirty {
		t.Errorf("cancel with changes: exit code %d, code %s", code, c)
	}
	if _, stdout, _ := clitest.Run("task", "show"); !strings.Contains(stdout, msg.Text(msg.TaskState, msg.Text(msg.TaskStateActive))+"\n") {
		t.Errorf("the task changed:\n%s", stdout)
	}

	// No task here; no such task; not a number of a task.
	t.Chdir(filepath.Dir(shop))
	if code, c := clitest.ErrorCode(t, "task", "close"); code != contract.ExitError || c != contract.CodeTaskUndetermined {
		t.Errorf("close outside a worktree: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "task", "cancel", "SHOP-9"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("cancel SHOP-9: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "task", "close", "9"); code != contract.ExitUsage || c != contract.CodeInvalidArgument {
		t.Errorf("close 9: exit code %d, code %s", code, c)
	}
}

func TestTaskCancel(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	clitest.MustRun(t, "note", "add", "Промокоды в таблице promo.")

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.TaskCancelled, "SHOP-1"),
		msg.Text(msg.TaskStage, round("Ветка", "branch", 1)),
		msg.Text(msg.ReasonLine, "Отложено до релиза каталога."),
		msg.Text(msg.WorktreeReleased, fix),
		"",
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-1"),
	), "", "task", "cancel", "--reason", "Отложено до релиза каталога.")
	typ, data := lastEvent(t)
	clitest.Validate(t, "schemas/events/task.cancelled.json", data)
	if want := `{"attempt":1,"node":"branch","reason":"Отложено до релиза каталога.","source":"operator","worktree":` + clitest.JSONString(fix) + `}`; typ != "task.cancelled" || data != want {
		t.Errorf("event %s %s, want task.cancelled %s", typ, data, want)
	}

	t.Chdir(shop)
	_, stdout, _ := clitest.Run("task", "show", "SHOP-1")
	none := msg.Text(msg.ValueNone)
	want := clitest.Lines(
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskState, msg.Text(msg.TaskStateCancelled)),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
		msg.Text(msg.TaskStage, round("Ветка", "branch", 1)),
		msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)),
		msg.Text(msg.TaskTakenAt, "<время>"),
		msg.Text(msg.TaskCancelledByOperator, "<время>"),
		msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.CancelReasonLine, "Отложено до релиза каталога."),
		"",
	) + clitest.Table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", none, none},
	) + clitest.Lines(
		"",
		msg.Text(msg.StepsNone),
		"",
		cli.HintFor(msg.HintStatement, "task", "SHOP-1"),
		cli.HintFor(msg.HintNotes, "task", "SHOP-1"),
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-1"),
	)
	if clitest.Masked(stdout) != want {
		t.Errorf("task show of a cancelled task:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := clitest.Run("task", "list", "--state", "cancelled"); stdout != clitest.Table(listHeader,
		[]string{"SHOP-1", "Частичный возврат по карте", msg.Text(msg.TaskStateCancelled), "Фича", "Ветка", none}) {
		t.Errorf("task list --state cancelled:\n%s", stdout)
	}

	// A passed scenario is cancelled too, without a reason, by the agent
	// and with the fields in --input.
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	t.Setenv(caller.SessionEnv, "s1")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.TaskCancelled, "SHOP-2"),
		msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
		msg.Text(msg.WorktreeReleased, fix),
		"",
		cli.HintFor(msg.HintTaskAgain, "task", "SHOP-2"),
	), "", "task", "cancel")
	t.Setenv(caller.SessionEnv, "")
	if _, stdout, _ := clitest.Run("task", "show", "SHOP-2"); !strings.Contains(clitest.Masked(stdout), msg.Text(msg.TaskCancelledByAgent, "<время>")+"\n") ||
		strings.Contains(stdout, msg.Text(msg.CancelReasonLine, "")) {
		t.Errorf("task show of a task cancelled by the agent:\n%s", stdout)
	}
	_, data = lastEvent(t)
	if want := `{"attempt":1,"node":"finish","source":"agent","worktree":` + clitest.JSONString(fix) + `}`; data != want {
		t.Errorf("event %s, want %s", data, want)
	}

	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	input := filepath.Join(t.TempDir(), "cancel.json")
	if err := os.WriteFile(input, []byte(`{"reason":"Первая строка.\nВторая строка."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/task-cancel.json", "task", "cancel", "SHOP-3", "--input", input)
	if !strings.Contains(out, `"reason":"Первая строка.\nВторая строка."`) || !strings.Contains(out, `"worktree":`+clitest.JSONString(fix)+`}`) {
		t.Errorf("task cancel --json: %s", out)
	}
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--reason", "--input"), "", cli.HintText(msg.HintCommandHelp, "task cancel")),
		"task", "cancel", "SHOP-3", "--reason", "Причина", "--input", input)
}

// TestEndedRefusesWrites checks that every command that writes a task
// refuses a closed and a cancelled one, and the commands that read do not.
func TestEndedRefusesWrites(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	clitest.MustRun(t, "task", "close")
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	clitest.MustRun(t, "task", "cancel")
	t.Chdir(shop)

	file := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(file, []byte("План"), 0o644); err != nil {
		t.Fatal(err)
	}
	writes := [][]string{
		{"stage", "exit", "--kind", "result", "--text", "Готово"},
		{"stage", "skip", "--reason", "Не нужен"},
		{"step", "add", "Шаг"},
		{"step", "done", "1"},
		{"step", "drop", "1", "--reason", "Не нужен"},
		{"note", "add", "Проверить логи."},
		{"artifact", "save", "plan.md", "--file", file},
		{"artifact", "save", "plan", "--url", "https://example.com/plan"},
		{"operator", "record", "--answer", "Да."},
	}
	for _, task := range []struct{ key, text string }{
		{"SHOP-1", taskEnded("SHOP-1")},
		{"SHOP-2", clitest.Lines(msg.Text(msg.TaskCancelled, "SHOP-2"), "", cli.HintFor(msg.HintTaskAgain, "task", "SHOP-2"))},
	} {
		for _, w := range writes {
			clitest.WantRun(t, contract.ExitError, "", task.text, append(w, "--task", task.key)...)
			if code, c := clitest.ErrorCode(t, append(w, "--task", task.key)...); code != contract.ExitError || c != contract.CodeTaskEnded {
				t.Errorf("%v of %s: exit code %d, code %s", w, task.key, code, c)
			}
		}
		clitest.MustRun(t, "note", "list", "--task", task.key)
	}
	// A cancelled task shows the stage it stopped at; a closed one has none.
	clitest.MustRun(t, "stage", "show", "--task", "SHOP-2")
	clitest.WantRun(t, contract.ExitError, "", taskEnded("SHOP-1"), "stage", "show", "--task", "SHOP-1")
	if clitest.FileExists(filepath.Join(os.Getenv("GENTRY_HOME"), "state", "shop", "tasks", "SHOP-2", "artifacts")) {
		t.Error("the artifact of a cancelled task is saved")
	}
}

func TestTaskTakeAgain(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	clitest.MustRun(t, "note", "add", "Промокоды в таблице promo.")
	clitest.MustRun(t, "operator", "record", "--answer", "Только для карт.")
	plan := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(plan, []byte("План 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.MustRun(t, "artifact", "save", "plan.md", "--file", plan)
	clitest.MustRun(t, "step", "add", "Создать ветку")
	clitest.MustRun(t, "step", "done", "1")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Ветка создана")
	clitest.MustRun(t, "task", "cancel", "--reason", "Отложено.")

	// Taken anew in the main worktree by the agent: the title and the
	// statement stay those of the operator.
	t.Chdir(shop)
	t.Setenv(caller.SessionEnv, "s1")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.TaskTakenAnew, "SHOP-1"),
		msg.Text(msg.AttemptLine, 2),
		msg.Text(msg.TaskTitle, "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, "Баг"),
		msg.Text(msg.TaskStage, "Ветка"),
		msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
		msg.Text(msg.TaskWorktree, shop),
		"",
		cli.HintText(msg.HintTaskShow),
	), "", "task", "take", "--task", "shop-1", "--scenario", "bug")
	t.Setenv(caller.SessionEnv, "")
	typ, data := lastEvent(t)
	clitest.Validate(t, "schemas/events/task.taken.json", data)
	if typ != "task.taken" || !strings.HasPrefix(data, `{"attempt":2,`) || !strings.Contains(data, `"source":"operator"`) {
		t.Errorf("event %s %s", typ, data)
	}

	// The new attempt starts anew: no path, notes, decisions or artifacts
	// of the first.
	_, stdout, _ := clitest.Run("task", "show")
	if !strings.Contains(stdout, clitest.Lines(msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Баг", "bug")),
		msg.Text(msg.TaskStage, round("Ветка", "branch", 1)), msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)))) ||
		strings.Contains(stdout, msg.Text(msg.HintNotes)) || strings.Contains(stdout, "plan.md") ||
		!strings.HasSuffix(stdout, "\n\n"+clitest.Lines(cli.HintText(msg.HintStatement), cli.HintText(msg.HintAttempts))) {
		t.Errorf("task show of the second attempt:\n%s", stdout)
	}
	if _, stdout, _ := clitest.Run("task", "show", "--statement"); strings.Contains(stdout, "Только для карт") {
		t.Errorf("the decisions of the first attempt are in the second:\n%s", stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show")
	if !strings.Contains(out, `"attempt":2,`) || strings.Contains(out, `"ended"`) {
		t.Errorf("task show --json: %s", out)
	}

	// An artifact of the same name lies apart from the one of the first.
	if err := os.WriteFile(plan, []byte("План 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.MustRun(t, "artifact", "save", "plan.md", "--file", plan)
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
	_, stdout, _ = clitest.Run("task", "attempts", "SHOP-1")
	// Any time of the width of the times printed: the table is compared masked.
	const at = "2026-01-01 00:00"
	none := msg.Text(msg.ValueNone)
	want := clitest.Table([]string{msg.Text(msg.ColAttempt), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
		msg.Text(msg.ColTaken), msg.Text(msg.ColEnded), msg.Text(msg.ColCancelReason)},
		[]string{"1", msg.Text(msg.TaskStateCancelled), "Фича", at, at, "Отложено."},
		[]string{"2", msg.Text(msg.TaskStateActive), "Баг", at, none, none}) + "\n" + cli.HintFor(msg.HintAttempt, "task", "SHOP-1") + "\n"
	if clitest.Masked(stdout) != clitest.Masked(want) {
		t.Errorf("task attempts:\n%s\nwant:\n%s", stdout, want)
	}
	clitest.WantJSON(t, contract.ExitOK, "schemas/task-attempts.json", "task", "attempts", "SHOP-1")
	_, stdout, _ = clitest.Run("task", "attempts", "SHOP-1", "1")
	for _, part := range []string{
		clitest.Lines(msg.Text(msg.AttemptHeading, "SHOP-1", 1, "Частичный возврат по карте"), "", msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.TaskState, msg.Text(msg.TaskStateCancelled))),
		clitest.Lines(msg.Text(msg.CancelReasonLine, "Отложено."), "", round("Ветка", "branch", 1),
			msg.Text(msg.OutcomeLine, msg.Text(msg.ExitKindResult)), msg.Text(msg.ExitTextLine, "Ветка создана")),
		"\n" + clitest.Lines(round("План фичи", "plan-feature", 1), msg.Text(msg.OutcomeLine, msg.Text(msg.ValueNone))),
		"\nplan.md   " + msg.Text(msg.ArtifactKindFile),
		"\n\n" + clitest.Lines(msg.Text(msg.NotesHeading), "  "+msg.Text(msg.NoteHeading, 1, round("Ветка", "branch", 1)), "     Промокоды в таблице promo."),
		"\n\n" + clitest.Lines(msg.Text(msg.DecisionsHeading),
			"  "+msg.Text(msg.DecisionHeading, 1, round("Ветка", "branch", 1), msg.Text(msg.TaskSourceOperator))),
	} {
		if !strings.Contains(stdout, part) {
			t.Errorf("task attempts SHOP-1 1 has no %q:\n%s", part, stdout)
		}
	}
	if strings.Contains(stdout, ": gentry ") {
		t.Errorf("task attempts SHOP-1 1 has hints:\n%s", stdout)
	}
	clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "attempts", "SHOP-1", "1")
	// The refusal of an attempt that the task has not is built from the
	// attempts of the store: no test of package cli reaches it, this test
	// states its words.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		"У задачи SHOP-1 нет попытки 5.",
		"Попыток: 2",
		"",
		cli.HintFor(msg.HintAttemptsList, "task", "SHOP-1"),
	), "task", "attempts", "SHOP-1", "5")
	if code, c := clitest.ErrorCode(t, "task", "attempts", "SHOP-1", "5"); code != contract.ExitError || c != contract.CodeAttemptNotFound {
		t.Errorf("attempt 5: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "task", "attempts", "SHOP-1", "первая"); code != contract.ExitUsage || c != contract.CodeInvalidArgument {
		t.Errorf("attempt «первая»: exit code %d, code %s", code, c)
	}
	// In the worktree of the task the attempt alone is enough.
	t.Chdir(shop)
	if _, stdout, _ := clitest.Run("task", "attempts", "2"); !strings.HasPrefix(stdout, msg.Text(msg.AttemptHeading, "SHOP-1", 2, "Частичный возврат по карте")+"\n") {
		t.Errorf("task attempts 2:\n%s", stdout)
	}
	if _, stdout, _ := clitest.Run("task", "list", "--all"); strings.Count(stdout, "SHOP-1") != 1 || !strings.Contains(stdout, "SHOP-1  Частичный возврат по карте  "+msg.Text(msg.TaskStateActive)) {
		t.Errorf("task list --all:\n%s", stdout)
	}
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/task-list.json", "task", "list", "--all")
	if !strings.Contains(out, `"attempt":2,`) {
		t.Errorf("task list --json: %s", out)
	}
}

func TestTaskTakeAgainRefusals(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	passScenario(t)
	clitest.MustRun(t, "task", "close")
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	clitest.MustRun(t, "task", "cancel")
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(shop)

	clitest.WantRun(t, contract.ExitError, "", taskEnded("SHOP-1"), "task", "take", "--task", "SHOP-1", "--scenario", "feature")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrTaskInWork, "SHOP-3"),
		msg.Text(msg.TaskWorktree, fix),
		"",
		cli.HintFor(msg.HintTaskShow, "task", "SHOP-3"),
	), "task", "take", "--task", "SHOP-3", "--scenario", "feature")
	if code, c := clitest.ErrorCode(t, "task", "take", "--task", "SHOP-3", "--scenario", "feature"); code != contract.ExitError || c != contract.CodeTaskInWork {
		t.Errorf("take a task in work: exit code %d, code %s", code, c)
	}
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--task", "--title"), "", cli.HintText(msg.HintCommandHelp, "task take")),
		"task", "take", "--task", "SHOP-2", "--scenario", "feature", "--title", "Скидка")
	input := filepath.Join(t.TempDir(), "task.json")
	if err := os.WriteFile(input, []byte(`{"task":"SHOP-2","scenario":"feature","statement":"Текст."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--task", "--statement"), "", cli.HintText(msg.HintCommandHelp, "task take")),
		"task", "take", "--input", input)
	if code, c := clitest.ErrorCode(t, "task", "take", "--task", "SHOP-2"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("take without a scenario: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "task", "take", "--task", "SHOP-9", "--scenario", "feature"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("take SHOP-9: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "task", "take", "--task", "9", "--scenario", "feature"); code != contract.ExitUsage || c != contract.CodeFlagValue {
		t.Errorf("take 9: exit code %d, code %s", code, c)
	}
	if _, stdout, _ := clitest.Run("task", "attempts", "SHOP-2"); strings.Count(stdout, "\n") != 4 {
		t.Errorf("a refused take made an attempt:\n%s", stdout)
	}

	// A worktree of another project.
	blog := gittest.Repo(t, filepath.Join(filepath.Dir(shop), "blog"))
	t.Chdir(blog)
	clitest.MustRun(t, "project", "add", "blog", "--knowledge", "../blog-knowledge")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrTaskProjectMismatch, "SHOP-2"),
		msg.Text(msg.TaskProjectLine, "shop"),
		msg.Text(msg.WorktreeProjectLine, "blog"),
		"",
		cli.HintFor(msg.HintWorktreeListProject, "project", "shop"),
	), "task", "take", "--task", "SHOP-2", "--scenario", "feature")
	out := clitest.WantJSON(t, contract.ExitError, "schemas/error.json", "task", "take", "--task", "SHOP-2", "--scenario", "feature")
	if !strings.Contains(out, `"code":"task_project_mismatch"`) || !strings.Contains(out, `"worktree_project":"blog"`) {
		t.Errorf("take in the blog --json: %s", out)
	}
}
