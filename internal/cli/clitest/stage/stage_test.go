package stage_test

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
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// closeStage adds a step to the current stage, marks it done and closes the
// stage by a result with the flags given.
func closeStage(t *testing.T, step string, flags ...string) {
	t.Helper()
	clitest.MustRun(t, "step", "add", step)
	clitest.MustRun(t, "step", "done", "1")
	clitest.MustRun(t, append([]string{"stage", "exit", "--kind", "result", "--text", "Готово"}, flags...)...)
}

// The words of the outputs are those of the catalog: the tests of the output
// in package cli state them.
var (
	none   = msg.Text(msg.ValueNone)
	result = msg.Text(msg.ExitKindResult)
)

// round names a stage of the shop by its title and identifier with the
// round of its pass.
func round(title, stage string, n int) string {
	return msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, title, stage), n)
}

// progress is the line of the progress of a task of the feature scenario,
// which has five stages.
func progress(passed int) string {
	return msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, passed, 5))
}

// returnLimit is the refusal of a return to the node to when its limit of n
// returns is used up.
func returnLimit(to string, n int) string {
	return clitest.Lines(
		msg.Text(msg.ErrReturnLimit, to),
		msg.Text(msg.ReturnsLine, n, n),
		"",
		cli.HintText(msg.HintOtherTransitions),
		cli.HintFor(msg.HintAllowReturn, "allow_return", to),
	)
}

// TestStageFeature takes a task of the shop through the feature scenario
// with returns from review to implementation up to the limit.
func TestStageFeature(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.FlowStage, round("Ветка", "branch", 1)),
		msg.Text(msg.StageTask, "SHOP-1"),
		msg.Text(msg.FlowExecutor, "orchestrator"),
		msg.Text(msg.FlowExit, "создана ветка задачи"),
		msg.Text(msg.FlowParts, none),
		"",
	)+clitest.Table(
		[]string{msg.Text(msg.ColTransition), msg.Text(msg.ColStage), msg.Text(msg.ColCondition), msg.Text(msg.ColReturns)},
		[]string{"plan", "План фичи", none, none},
	)+clitest.Lines(
		"",
		msg.Text(msg.FlowInstruction),
		"  Создать ветку задачи от main.",
	), "", "stage", "show")
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/stage-show.json", "stage", "show")
	if !strings.Contains(out, `"transitions":[{"stage":"plan-feature","title":"План фичи","to":"plan"}]`) {
		t.Errorf("stage show --json: %s", out)
	}

	// A stage without steps is not closed.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrStepsEmpty, "Ветка"), "", cli.HintText(msg.HintStepAdd)),
		"stage", "exit", "--kind", "result", "--text", "Создана ветка")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StepsAdded, 1),
		msg.Text(msg.StepNumber, 1),
	), "", "step", "add", "Создать ветку feature/shop-1")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StepMarkedDone, 1),
		msg.Text(msg.StepsLeft, 0),
		"",
		cli.HintText(msg.HintStageExit),
	), "", "step", "done", "1", "--check", "git branch --show-current")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StageExited, "Ветка"),
		msg.Text(msg.ExitKindLine, result),
		msg.Text(msg.ExitTextLine, "Создана ветка feature/shop-1"),
		msg.Text(msg.TransitionLine, "plan"),
		msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
		progress(1),
		"",
		cli.HintText(msg.HintStageShow),
	), "", "stage", "exit", "--kind", "result", "--text", "Создана ветка feature/shop-1")

	// An exit by an artifact needs the artifact saved.
	clitest.MustRun(t, "step", "add", "Согласовать план")
	clitest.MustRun(t, "step", "done", "1")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrArtifactNotFound, "plan"), "", cli.HintFor(msg.HintArtifactSave, "name", "plan")),
		"stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован с оператором")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(msg.Text(msg.ArtifactSaved, "plan"), msg.Text(msg.ArtifactURLLine, "https://claude.ai/code/artifact/plan")), "",
		"artifact", "save", "plan", "--url", "https://claude.ai/code/artifact/plan")
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/stage-close.json",
		"stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован с оператором")
	var closed contract.StageCloseOutput
	json.Unmarshal([]byte(out), &closed)
	if c := closed.Closed; c.Node != "plan" || c.Exit == nil || c.Exit.Kind != "artifact" || *c.Exit.Artifact != "plan" ||
		*c.To != "implementation" || closed.Task.Stage.Node != "implementation" || closed.Task.Progress.Passed != 2 {
		t.Errorf("stage exit --json: %s", out)
	}

	// Implementation: a step not done is listed; a dropped step needs a reason.
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StepsAdded, 2),
		msg.Text(msg.StepNumbers, 1, 2),
	), "", "step", "add", "Добавить расчёт суммы", "Покрыть расчёт тестами")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StepMarkedDone, 1),
		msg.Text(msg.StepsLeft, 1),
	), "", "step", "done", "1", "--check", "go test ./backend/payments/...")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		msg.Text(msg.ErrStepsOpen),
		"",
		msg.Text(msg.StepsOpenHeading),
		"  2. Покрыть расчёт тестами",
		"",
		cli.HintText(msg.HintStepDone),
		cli.HintText(msg.HintStepDrop),
	), "stage", "exit", "--kind", "result", "--text", "Изменения сделаны")
	if code, c := clitest.ErrorCode(t, "step", "drop", "2"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("step drop without a reason: exit code %d, code %s", code, c)
	}
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StepMarkedDropped, 2),
		msg.Text(msg.StepsLeft, 0),
		"",
		cli.HintText(msg.HintStageExit),
	), "", "step", "drop", "2", "--reason", "Тесты уже есть в шаге 1")
	clitest.WantRun(t, contract.ExitOK, msg.Text(msg.NoteAdded, 1)+"\n", "", "note", "add", "На ревью проверить, что возврат по СБП не задет")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Изменения сделаны, тесты проходят")

	// Review is a fork: the transition and its reason are required.
	clitest.MustRun(t, "step", "add", "Провести ревью")
	clitest.MustRun(t, "step", "done", "1")
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrForkToMissing), "", cli.HintText(msg.HintStageTransitions)),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны")
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrForkReasonMissing), "", cli.HintText(msg.HintCommandHelp, "stage exit")),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "implementation")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrTransitionNotFound, "Ревью", "deploy"), "", cli.HintText(msg.HintStageTransitions)),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "deploy", "--reason", "Выкатить")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StageExited, "Ревью"),
		msg.Text(msg.ExitKindLine, result),
		msg.Text(msg.ExitTextLine, "Замечания записаны"),
		msg.Text(msg.TransitionLine, "implementation"),
		msg.Text(msg.ReasonLine, "Две ошибки в расчёте суммы"),
		msg.Text(msg.TaskStage, round("Реализация", "implementation", 2)),
		progress(4),
		"",
		cli.HintText(msg.HintStageShow),
	), "", "stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "implementation", "--reason", "Две ошибки в расчёте суммы")
	// The new pass starts without steps.
	if code, c := clitest.ErrorCode(t, "stage", "exit", "--kind", "result", "--text", "Исправлено"); code != contract.ExitError || c != contract.CodeStepsEmpty {
		t.Errorf("a new round with the steps of the last: exit code %d, code %s", code, c)
	}
	// Two more returns are allowed, the fourth is not.
	for range 2 {
		closeStage(t, "Исправить замечания")
		closeStage(t, "Провести ревью", "--to", "implementation", "--reason", "Есть замечания")
	}
	closeStage(t, "Исправить замечания")
	clitest.MustRun(t, "step", "add", "Провести ревью")
	clitest.MustRun(t, "step", "done", "1")
	_, stdout, _ := clitest.Run("stage", "show")
	if !strings.Contains(stdout, msg.Text(msg.FlowStage, round("Ревью", "review", 4))+"\n") ||
		!strings.Contains(stdout, "ревью выявило существенные замечания  "+msg.Text(msg.StageReturns, 3, 3)+"\n") {
		t.Errorf("stage show at the limit:\n%s", stdout)
	}
	clitest.WantRun(t, contract.ExitError, "", returnLimit("implementation", 3),
		"stage", "exit", "--kind", "result", "--text", "Замечания", "--to", "implementation", "--reason", "Ещё замечания")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Замечаний нет", "--to", "merge", "--reason", "Существенных замечаний нет")

	// Merge is the operator's: no steps needed. Its exit passes the scenario.
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StageExited, "Слияние"),
		msg.Text(msg.ExitKindLine, result),
		msg.Text(msg.ExitTextLine, "Ветка влита в main"),
		msg.Text(msg.TransitionLine, msg.Text(msg.FlowEnd)),
		msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
		progress(5),
		"",
		cli.HintText(msg.HintTaskClose),
	), "", "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
	finished := clitest.Lines(msg.Text(msg.ErrScenarioFinished, "SHOP-1"), "", cli.HintText(msg.HintTaskClose))
	clitest.WantRun(t, contract.ExitError, "", finished, "stage", "show")
	clitest.WantRun(t, contract.ExitError, "", finished, "step", "add", "Ещё шаг")
	clitest.WantRun(t, contract.ExitError, "", finished, "stage", "skip", "--reason", "Не нужен")
	if code, c := clitest.ErrorCode(t, "stage", "exit", "--kind", "result", "--text", "Ещё"); code != contract.ExitError || c != contract.CodeScenarioFinished {
		t.Errorf("stage exit after the end: exit code %d, code %s", code, c)
	}
	// Notes and artifacts are taken until the task is closed.
	clitest.MustRun(t, "note", "add", "Первая строка.\nВторая строка.")

	_, stdout, _ = clitest.Run("task", "show")
	// The table of the path: the columns are as wide as the longest cell.
	pathTable := clitest.Table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", result, "plan"},
		[]string{"План фичи", "1", msg.Text(msg.ExitKindArtifact, "plan"), "implementation"},
		[]string{"Реализация", "1", result, "review"},
		[]string{"Ревью", "1", result, "implementation"},
		[]string{"Реализация", "2", result, "review"},
		[]string{"Ревью", "2", result, "implementation"},
		[]string{"Реализация", "3", result, "review"},
		[]string{"Ревью", "3", result, "implementation"},
		[]string{"Реализация", "4", result, "review"},
		[]string{"Ревью", "4", result, "merge"},
		[]string{"Слияние", "1", result, msg.Text(msg.FlowEnd)},
	)
	for _, want := range []string{
		msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)) + "\n" + progress(5) + "\n",
		pathTable + "\n" + msg.Text(msg.ColArtifact),
		clitest.Table(
			[]string{msg.Text(msg.ColArtifact), msg.Text(msg.ColKind), msg.Text(msg.ColSaved), msg.Text(msg.ColPlace)},
			[]string{"plan", msg.Text(msg.ArtifactKindLink), "2006-01-02 15:04", "https://claude.ai/code/artifact/plan"},
		),
		"\n\n" + cli.HintText(msg.HintStatement) + "\n" + cli.HintText(msg.HintNotes) + "\n",
	} {
		if !strings.Contains(clitest.Masked(stdout), clitest.Masked(want)) {
			t.Errorf("task show has no\n%s\noutput:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "На ревью") || strings.Contains(stdout, msg.Text(msg.TaskSource, "")) {
		t.Errorf("task show has the notes or the statement:\n%s", stdout)
	}
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.NoteHeading, 1, round("Реализация", "implementation", 1)),
		"   На ревью проверить, что возврат по СБП не задет",
		"",
		msg.Text(msg.NoteHeading, 2, round("Слияние", "merge", 1)),
		"   Первая строка.",
		"   Вторая строка.",
	), "", "note", "list")
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/note-list.json", "note", "list")
	if !strings.Contains(out, `"task":"SHOP-1"`) || strings.Count(out, `"number":`) != 2 {
		t.Errorf("note list --json: %s", out)
	}
	_, stdout, _ = clitest.Run("task", "show", "--path")
	for _, want := range []string{
		clitest.Lines(
			round("Реализация", "implementation", 1),
			msg.Text(msg.OutcomeLine, result),
			msg.Text(msg.ExitTextLine, "Изменения сделаны, тесты проходят"),
			msg.Text(msg.TransitionLine, "review"),
			msg.Text(msg.RecordedLine, msg.Text(msg.RecordedByAgent)),
			msg.Text(msg.ClosedLine, "<время>"),
			"",
		) + clitest.Table(
			[]string{msg.Text(msg.ColStepNumber), msg.Text(msg.ColState), msg.Text(msg.ColStep), msg.Text(msg.ColComment)},
			[]string{"1", msg.Text(msg.StepStateDone), "Добавить расчёт суммы", "go test ./backend/payments/..."},
			[]string{"2", msg.Text(msg.StepStateDropped), "Покрыть расчёт тестами", "Тесты уже есть в шаге 1"},
		),
		msg.Text(msg.TransitionLine, "implementation") + "\n" + msg.Text(msg.ReasonLine, "Две ошибки в расчёте суммы") + "\n",
	} {
		if !strings.Contains(clitest.Masked(stdout), want) {
			t.Errorf("task show --path has no\n%s\noutput:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, msg.Text(msg.ColRound)) {
		t.Errorf("task show --path has the table of the path:\n%s", stdout)
	}

	out = clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if !shown.Task.Finished || shown.Task.Stage != nil || len(shown.Path) != 11 || len(shown.Notes) != 2 || len(shown.Artifacts) != 1 ||
		shown.Task.Progress != (contract.TaskProgress{Passed: 5, TotalMin: 5, TotalMax: 5}) {
		t.Errorf("task show --json: %s", out)
	}
	list := clitest.WantJSON(t, contract.ExitOK, "schemas/task-list.json", "task", "list")
	if !strings.Contains(list, `"finished":true`) || strings.Contains(list, `"stage":`) {
		t.Errorf("task list --json: %s", list)
	}

	// Every event of the way matches its schema.
	_, events, _ := clitest.Run("events")
	seen := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(events), "\n") {
		var e struct {
			Type string          `json:"type"`
			Task string          `json:"task"`
			Data json.RawMessage `json:"data"`
		}
		json.Unmarshal([]byte(line), &e)
		switch e.Type {
		case "stage.exited", "stage.skipped", "step.added", "step.done", "step.dropped", "note.added", "artifact.saved":
			clitest.Validate(t, "schemas/events/"+e.Type+".json", string(e.Data))
			if e.Task != "SHOP-1" {
				t.Errorf("event %s of task %q", e.Type, e.Task)
			}
			seen[e.Type]++
		}
	}
	if seen["stage.exited"] != 11 || seen["step.dropped"] != 1 || seen["note.added"] != 2 || seen["artifact.saved"] != 1 {
		t.Errorf("events %v", seen)
	}
	if !strings.Contains(events, `"returns":3`) || !strings.Contains(events, `"finished":true`) {
		t.Errorf("events have no third return or no end:\n%s", events)
	}
}

// TestStageSkip skips stages: the reason is required, at a fork the
// transition too, and steps not done refuse a skip as an exit.
func TestStageSkip(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrSkipReasonMissing), "", cli.HintText(msg.HintCommandHelp, "stage skip")),
		"stage", "skip")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StageSkipped, "Ветка"),
		msg.Text(msg.ReasonLine, "Ветка уже создана оператором"),
		msg.Text(msg.TransitionLine, "plan"),
		msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
		progress(1),
		"",
		cli.HintText(msg.HintStageShow),
	), "", "stage", "skip", "--reason", "Ветка уже создана оператором")
	clitest.MustRun(t, "step", "add", "Согласовать план")
	if code, c := clitest.ErrorCode(t, "stage", "skip", "--reason", "Не нужен"); code != contract.ExitError || c != contract.CodeStepsOpen {
		t.Errorf("skip with a step not done: exit code %d, code %s", code, c)
	}
	clitest.MustRun(t, "step", "drop", "1", "--reason", "План не нужен")
	input := filepath.Join(t.TempDir(), "skip.json")
	clitest.WriteFiles(t, filepath.Dir(input), map[string]string{"skip.json": `{"reason":"План не нужен"}`})
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/stage-close.json", "stage", "skip", "--input", input)
	if !strings.Contains(out, `"outcome":"skip","reason":"План не нужен"`) {
		t.Errorf("stage skip --json: %s", out)
	}
	closeStage(t, "Сделать")
	if code, c := clitest.ErrorCode(t, "stage", "skip", "--reason", "Нечего смотреть"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("skip at a fork without the transition: exit code %d, code %s", code, c)
	}
	_, stdout, _ := clitest.Run("stage", "skip", "--reason", "Нечего смотреть", "--to", "merge")
	if !strings.Contains(stdout, clitest.Lines(msg.Text(msg.TransitionLine, "merge"), msg.Text(msg.TaskStage, round("Слияние", "merge", 1)), progress(4))) {
		t.Errorf("skip at a fork:\n%s", stdout)
	}
	_, stdout, _ = clitest.Run("task", "show")
	skip := msg.Text(msg.OutcomeSkip)
	if !strings.Contains(stdout, clitest.Table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", skip, "plan"},
		[]string{"План фичи", "1", skip, "implementation"},
		[]string{"Реализация", "1", result, "review"},
		[]string{"Ревью", "1", skip, "merge"},
		[]string{"Слияние", "1", msg.Text(msg.OutcomeCurrent), msg.Text(msg.ValueNone)},
	)) {
		t.Errorf("task show:\n%s", stdout)
	}
}

// TestStageByAgent checks the source of what the agent records: the stage
// of the operator closed by the agent is recorded from the operator's words.
func TestStageByAgent(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	t.Setenv(caller.SessionEnv, "drv-1")
	closeStage(t, "Создать ветку")
	clitest.MustRun(t, "artifact", "save", "plan", "--url", "https://example.com/plan")
	clitest.MustRun(t, "step", "add", "Согласовать план")
	clitest.MustRun(t, "step", "done", "1")
	clitest.MustRun(t, "stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован")
	closeStage(t, "Сделать")
	closeStage(t, "Проверить", "--to", "merge", "--reason", "Замечаний нет")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Влито оператором")
	_, stdout, _ := clitest.Run("task", "show", "--path")
	for _, want := range []string{
		clitest.Lines(msg.Text(msg.TransitionLine, "plan"), msg.Text(msg.RecordedLine, msg.Text(msg.RecordedByAgent))),
		clitest.Lines(msg.Text(msg.TransitionLine, msg.Text(msg.FlowEnd)), msg.Text(msg.RecordedLine, msg.Text(msg.TaskSourceAgent))),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("task show --path has no %q:\n%s", want, stdout)
		}
	}
	_, out, _ := clitest.Run("task", "show", "--json")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	for _, p := range shown.Path {
		if p.Source == nil || *p.Source != "agent" {
			t.Errorf("a pass closed by the agent has the source %v: %s", p.Source, out)
		}
		for _, s := range p.Steps {
			if s.Source != "agent" || s.ClosedSource == nil || *s.ClosedSource != "agent" {
				t.Errorf("a step of the agent has the source %s: %s", s.Source, out)
			}
		}
	}
	if len(shown.Artifacts) != 1 || shown.Artifacts[0].Source != "agent" {
		t.Errorf("artifacts: %s", out)
	}
}

// TestStageOtherDirectory works with the task by --task from outside its
// worktree: hints name the task.
func TestStageOtherDirectory(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(shop)
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrStepsEmpty, "Ветка"), "", cli.HintFor(msg.HintStepAdd, "task", "SHOP-1")),
		"stage", "exit", "--kind", "result", "--text", "Готово", "--task", "SHOP-1")
	clitest.MustRun(t, "step", "add", "Создать ветку", "--task", "shop-1")
	_, stdout, _ := clitest.Run("step", "done", "1", "--task", "SHOP-1")
	// The commands are the agent's: the hints name tools.
	if want := clitest.InChannel(cli.HintFor(msg.HintStageExit, "task", "SHOP-1")+"\n", []string{"step", "done"}); !strings.HasSuffix(stdout, "\n"+want) {
		t.Errorf("step done:\n%s", stdout)
	}
	_, stdout, _ = clitest.Run("stage", "exit", "--kind", "result", "--text", "Готово", "--task", "SHOP-1")
	if want := clitest.InChannel(cli.HintFor(msg.HintStageShow, "task", "SHOP-1")+"\n", []string{"stage", "exit"}); !strings.HasSuffix(stdout, "\n"+want) {
		t.Errorf("stage exit:\n%s", stdout)
	}
	// In the main worktree, which holds no task, a command without --task finds none.
	if code, c := clitest.ErrorCode(t, "stage", "show"); code != contract.ExitError || c != contract.CodeTaskUndetermined {
		t.Errorf("stage show without a task: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "stage", "show", "--task", "SHOP-9"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("stage show of no task: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "stage", "show", "--task", "shop"); code != contract.ExitUsage || c != contract.CodeFlagValue {
		t.Errorf("stage show --task shop: exit code %d, code %s", code, c)
	}
}

// TestWayRefusals checks the refusals of the fields of the commands.
func TestWayRefusals(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	clitest.MustRun(t, "step", "add", "Создать ветку")
	clitest.MustRun(t, "step", "done", "1")
	input := func(text string) string {
		p := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exitHelp := cli.HintText(msg.HintCommandHelp, "stage exit")
	long := strings.Repeat("ш", 121)
	tests := []struct {
		name   string
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{"no kind", []string{"stage", "exit", "--text", "Т"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrExitKindMissing), "", exitHelp)},
		{"bad kind", []string{"stage", "exit", "--kind", "fact", "--text", "Т"}, contract.ExitUsage, contract.CodeFlagValue,
			clitest.Lines(msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact"), "", exitHelp)},
		{"bad kind in input", []string{"stage", "exit", "--input", input(`{"kind":"fact","text":"Т"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			clitest.Lines(msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputBadValue, "kind")), "", exitHelp)},
		{"no text", []string{"stage", "exit", "--kind", "result"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrExitTextMissing), "", exitHelp)},
		{"no artifact", []string{"stage", "exit", "--kind", "artifact", "--text", "Т"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrExitArtifactMissing), "", exitHelp)},
		{"artifact of a result", []string{"stage", "exit", "--kind", "result", "--artifact", "plan", "--text", "Т"}, contract.ExitUsage, contract.CodeConflictingFlags,
			clitest.Lines(msg.Text(msg.ErrArtifactForKind), "", exitHelp)},
		{"flag with input", []string{"stage", "exit", "--kind", "result", "--input", "x.json"}, contract.ExitUsage, contract.CodeConflictingFlags,
			clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--kind", "--input"), "", exitHelp)},
		{"no step", []string{"step", "add"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrStepsMissing), "", cli.HintText(msg.HintCommandHelp, "step add"))},
		{"long step", []string{"step", "add", long}, contract.ExitUsage, contract.CodeFieldInvalid,
			clitest.Lines(msg.Text(msg.ErrStepTooLong), "", msg.Text(msg.HintStep))},
		{"step of two lines", []string{"step", "add", "А\nБ"}, contract.ExitUsage, contract.CodeFieldInvalid,
			clitest.Lines(msg.Text(msg.ErrStepMultiline), "", msg.Text(msg.HintStep))},
		{"steps not a list", []string{"step", "add", "--input", input(`{"steps":"Шаг"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			clitest.Lines(msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotList, "steps")), "", cli.HintText(msg.HintCommandHelp, "step add"))},
		{"arguments with input", []string{"step", "add", "Шаг", "--input", input(`{"steps":["Шаг"]}`)}, contract.ExitUsage, contract.CodeConflictingFlags,
			clitest.Lines(msg.Text(msg.ErrInputWithArgs), "", cli.HintText(msg.HintCommandHelp, "step add"))},
		{"step number", []string{"step", "done", "первый"}, contract.ExitUsage, contract.CodeInvalidArgument,
			clitest.Lines(msg.Text(msg.ErrStepNumberInvalid, "первый"), "", cli.HintText(msg.HintSteps))},
		{"step number not an integer", []string{"step", "done", "--input", input(`{"step":"1"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			clitest.Lines(msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotInteger, "step")), "", cli.HintText(msg.HintCommandHelp, "step done"))},
		{"no step number", []string{"step", "done"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrStepNumberMissing), "", cli.HintText(msg.HintCommandHelp, "step done"))},
		{"step not found", []string{"step", "done", "7"}, contract.ExitError, contract.CodeStepNotFound,
			clitest.Lines(msg.Text(msg.ErrStepNotFound, 7), "", cli.HintText(msg.HintSteps))},
		{"step done", []string{"step", "done", "1"}, contract.ExitError, contract.CodeStepClosed,
			clitest.Lines(msg.Text(msg.ErrStepDoneAlready, 1), "", cli.HintText(msg.HintSteps))},
		{"no note", []string{"note", "add"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrNoteMissing), "", cli.HintText(msg.HintCommandHelp, "note add"))},
		{"no artifact name", []string{"artifact", "save", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrArtifactNameMissing), "", cli.HintText(msg.HintCommandHelp, "artifact save"))},
		{"bad artifact name", []string{"artifact", "save", "план", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeFieldInvalid,
			clitest.Lines(msg.Text(msg.ErrArtifactNameInvalid, "план"), "", msg.Text(msg.HintArtifactName))},
		{"no file or address", []string{"artifact", "save", "plan"}, contract.ExitUsage, contract.CodeMissingField,
			clitest.Lines(msg.Text(msg.ErrArtifactSourceMissing), "", cli.HintText(msg.HintCommandHelp, "artifact save"))},
		{"file and address", []string{"artifact", "save", "plan", "--file", "plan.md", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeConflictingFlags,
			clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--file", "--url"), "", cli.HintText(msg.HintCommandHelp, "artifact save"))},
		{"bad address", []string{"artifact", "save", "plan", "--url", "claude.ai/code/artifact/1"}, contract.ExitUsage, contract.CodeFieldInvalid,
			clitest.Lines(msg.Text(msg.ErrArtifactURLInvalid, "claude.ai/code/artifact/1"), "", msg.Text(msg.HintArtifactURL))},
		{"no file", []string{"artifact", "save", "plan", "--file", "plan.md"}, contract.ExitUsage, contract.CodeFieldInvalid,
			clitest.Lines(msg.Text(msg.ErrArtifactFileNotFound, "plan.md"), "", msg.Text(msg.HintArtifactFile))},
	}
	for _, tt := range tests {
		code, stdout, stderr := clitest.Run(tt.args...)
		if want := clitest.InChannel(tt.stderr, tt.args); code != tt.exit || stdout != "" || stderr != want {
			tt.stderr = want
			t.Errorf("%s: exit code %d, stdout %q, stderr:\n%s\nwant %d:\n%s", tt.name, code, stdout, stderr, tt.exit, tt.stderr)
		}
		if code, c := clitest.ErrorCode(t, tt.args...); code != tt.exit || c != tt.code {
			t.Errorf("%s in JSON: exit code %d, code %q; want %d, %q", tt.name, code, c, tt.exit, tt.code)
		}
	}
	clitest.MustRun(t, "step", "add", "Проверить ветку")
	clitest.MustRun(t, "step", "drop", "2", "--reason", "Не нужно")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrStepDroppedAlready, 2), "", cli.HintText(msg.HintSteps)), "step", "drop", "2", "--reason", "Ещё раз")
}

// TestStepsInput adds steps, marks and drops them through --input.
func TestStepsInput(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/step-list.json", "step", "add", "Первый", "Второй")
	if !strings.Contains(out, `"added":[1,2]`) || !strings.Contains(out, `"node":"branch","round":1`) {
		t.Errorf("step add --json: %s", out)
	}
	code, out, _ := clitest.RunWith(`{"steps":["Третий"]}`, "step", "add", "--input", "-", "--json")
	if code != contract.ExitOK || !strings.Contains(out, `"added":[3]`) {
		t.Errorf("step add --input: exit code %d, %s", code, out)
	}
	code, out, _ = clitest.RunWith(`{"step":2,"check":"git status"}`, "step", "done", "--input", "-", "--json")
	clitest.Validate(t, "schemas/step-list.json", out)
	if code != contract.ExitOK || !strings.Contains(out, `"check":"git status"`) || strings.Contains(out, `"added":[`) {
		t.Errorf("step done --input: exit code %d, %s", code, out)
	}
	code, out, _ = clitest.RunWith(`{"step":3,"reason":"Лишний"}`, "step", "drop", "--input", "-", "--json")
	if code != contract.ExitOK || !strings.Contains(out, `"reason":"Лишний"`) || !strings.Contains(out, `"closed_source":"agent"`) {
		t.Errorf("step drop --input: exit code %d, %s", code, out)
	}
	code, out, _ = clitest.RunWith(`{"text":"Строка 1.\nСтрока 2."}`, "note", "add", "--input", "-", "--json")
	clitest.Validate(t, "schemas/note-add.json", out)
	if code != contract.ExitOK || !strings.Contains(out, `"node":"branch"`) || !strings.Contains(out, `"number":1`) {
		t.Errorf("note add --input: exit code %d, %s", code, out)
	}
}

// TestArtifactFile keeps a copy of a file, replaces it, and replaces it by a
// link, which removes the copy.
func TestArtifactFile(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.WriteFiles(t, dir, map[string]string{"plan.md": "# План\n"})
	_, stdout, _ := clitest.Run("artifact", "save", "plan.md", "--file", "plan.md", "--task", "SHOP-1")
	copyPath := filepath.Join(os.Getenv("GENTRY_HOME"), "state", "shop", "tasks", "SHOP-1", "artifacts", "plan.md")
	if want := clitest.Lines(msg.Text(msg.ArtifactSaved, "plan.md"), msg.Text(msg.ArtifactFileLine, copyPath)); stdout != want {
		t.Errorf("artifact save:\n%s\nwant:\n%s", stdout, want)
	}
	clitest.WriteFiles(t, dir, map[string]string{"plan.md": "# План, вторая версия\n"})
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/artifact-save.json", "artifact", "save", "plan.md", "--file", "plan.md", "--task", "SHOP-1")
	if !strings.Contains(out, `"replaced":true`) || !strings.Contains(out, `"kind":"file"`) {
		t.Errorf("artifact save --json: %s", out)
	}
	if b, err := os.ReadFile(copyPath); err != nil || string(b) != "# План, вторая версия\n" {
		t.Errorf("copy %q, %v", b, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(copyPath)); len(entries) != 1 {
		t.Errorf("files left in the artifacts: %v", entries)
	}
	_, stdout, _ = clitest.Run("artifact", "save", "plan.md", "--url", "https://example.com/plan", "--task", "SHOP-1")
	if stdout != clitest.Lines(msg.Text(msg.ArtifactReplaced, "plan.md"), msg.Text(msg.ArtifactURLLine, "https://example.com/plan")) {
		t.Errorf("replaced by a link:\n%s", stdout)
	}
	if clitest.FileExists(copyPath) {
		t.Error("the copy of the file is left after the link replaced it")
	}
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, 10<<20+1), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrArtifactFileTooLarge, "big.bin"), "", cli.HintFor(msg.HintArtifactLink, "task", "SHOP-1")),
		"artifact", "save", "big", "--file", "big.bin", "--task", "SHOP-1")
}

// TestWayWithoutStore checks that the commands of the way of a task find no
// task without a state store and do not create one.
func TestWayWithoutStore(t *testing.T) {
	clitest.EmptyHome(t)
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"stage", "show"}, {"step", "add", "Шаг"}, {"note", "add", "Заметка"}, {"stage", "exit", "--kind", "result", "--text", "Т", "--task", "SHOP-1"}} {
		if code, _, _ := clitest.Run(args...); code != contract.ExitError {
			t.Errorf("%v: exit code %d", args, code)
		}
	}
	if p, _ := state.Path(); clitest.FileExists(p) {
		t.Errorf("the commands must not create the state store %s", p)
	}
}

// TestNoteListEmpty shows a task without notes, from another directory.
func TestNoteListEmpty(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(shop)
	clitest.WantRun(t, contract.ExitOK, msg.Text(msg.NotesNone)+"\n", "", "note", "list", "--task", "SHOP-1")
	if out := clitest.WantJSON(t, contract.ExitOK, "schemas/note-list.json", "note", "list", "--task", "SHOP-1"); out != `{"notes":[],"task":"SHOP-1"}`+"\n" {
		t.Errorf("note list --json: %s", out)
	}
	clitest.MustRun(t, "note", "add", "Заметка", "--task", "SHOP-1")
	_, stdout, _ := clitest.Run("task", "show", "SHOP-1")
	if !strings.HasSuffix(stdout, "\n\n"+clitest.Lines(cli.HintFor(msg.HintStatement, "task", "SHOP-1"), cli.HintFor(msg.HintNotes, "task", "SHOP-1"))) {
		t.Errorf("task show from another directory:\n%s", stdout)
	}
}

// TestStageByTools passes a stage through the tools of the agent, as a
// session does: hints name tools, and the records are the agent's.
func TestStageByTools(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	// byTool is the text of lines as a tool gives it: the hints name tools,
	// and the text has no newline at its end.
	byTool := func(ls ...string) string {
		return strings.TrimSuffix(clitest.InChannel(clitest.Lines(ls...), []string{"stage", "exit"}), "\n")
	}
	for _, c := range []struct {
		tool, args, want string
		failed           bool
	}{
		{"stage_exit", `{"kind":"result","text":"Создана ветка"}`, byTool(msg.Text(msg.ErrStepsEmpty, "Ветка"), "", cli.HintText(msg.HintStepAdd)), true},
		{"step_add", `{"steps":["Создать ветку feature/shop-1"]}`, byTool(msg.Text(msg.StepsAdded, 1), msg.Text(msg.StepNumber, 1)), false},
		{"step_done", `{"step":1,"check":"git branch --show-current"}`,
			byTool(msg.Text(msg.StepMarkedDone, 1), msg.Text(msg.StepsLeft, 0), "", cli.HintText(msg.HintStageExit)), false},
		{"stage_exit", `{"kind":"artifact","artifact":"plan","text":"План"}`,
			byTool(msg.Text(msg.ErrArtifactNotFound, "plan"), "", cli.HintFor(msg.HintArtifactSave, "name", "plan")), true},
		{"stage_exit", `{"kind":"result","text":"Создана ветка"}`, byTool(
			msg.Text(msg.StageExited, "Ветка"),
			msg.Text(msg.ExitKindLine, result),
			msg.Text(msg.ExitTextLine, "Создана ветка"),
			msg.Text(msg.TransitionLine, "plan"),
			msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
			progress(1),
			"",
			cli.HintText(msg.HintStageShow),
		), false},
	} {
		text, failed := cli.CallTool(c.tool, []byte(c.args))
		if text != c.want || failed != c.failed {
			t.Errorf("%s %s: failed %v:\n%s\nwant failed %v:\n%s", c.tool, c.args, failed, text, c.failed, c.want)
		}
	}
	_, out, _ := clitest.Run("task", "show", "--path", "--json")
	if !strings.Contains(out, `"source":"agent"`) || strings.Contains(out, `"source":"operator","state"`) {
		t.Errorf("task show --path --json: %s", out)
	}
}
