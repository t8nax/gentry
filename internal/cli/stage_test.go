package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// lines joins lines of an output, each ended by a newline.
func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// wantRun runs a command and checks its exit code, stdout and stderr.
func wantRun(t *testing.T, exit int, stdout, stderr string, args ...string) {
	t.Helper()
	code, out, errOut := run(args...)
	if code != exit || out != stdout || errOut != stderr {
		t.Errorf("%v: exit code %d, stdout:\n%s\nstderr:\n%s\nwant %d, stdout:\n%s\nstderr:\n%s", args, code, out, errOut, exit, stdout, stderr)
	}
}

// wantJSON runs a command with --json, checks its exit code and the output
// against a schema, and returns the output.
func wantJSON(t *testing.T, exit int, schema string, args ...string) string {
	t.Helper()
	code, out, _ := run(append(args, "--json")...)
	if code != exit {
		t.Fatalf("%v --json: exit code %d, output %s", args, code, out)
	}
	validate(t, schema, out)
	return out
}

// errorCode returns the code of the failure of a --json command.
func errorCode(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, out, _ := run(append(args, "--json")...)
	validate(t, "schemas/error.json", out)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(out), &e)
	return code, e.Error.Code
}

// closeStage adds a step to the current stage, marks it done and closes the
// stage by a result with the flags given.
func closeStage(t *testing.T, step string, flags ...string) {
	t.Helper()
	mustRun(t, "step", "add", step)
	mustRun(t, "step", "done", "1")
	mustRun(t, append([]string{"stage", "exit", "--kind", "result", "--text", "Готово"}, flags...)...)
}

// TestStageFeature takes a task of the shop through the feature scenario
// with returns from review to implementation up to the limit.
func TestStageFeature(t *testing.T) {
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(fix)

	wantRun(t, contract.ExitOK, lines(
		"Этап: Ветка (branch), круг 1",
		"Задача: SHOP-1",
		"Исполнитель: orchestrator",
		"Выход: создана ветка задачи",
		"Фрагменты: —",
		"",
		"ПЕРЕХОД  ЭТАП       УСЛОВИЕ  ВОЗВРАТЫ",
		"plan     План фичи  —        —",
		"",
		"Инструкция:",
		"  Создать ветку задачи от main.",
	), "", "stage", "show")
	out := wantJSON(t, contract.ExitOK, "schemas/stage-show.json", "stage", "show")
	if !strings.Contains(out, `"transitions":[{"stage":"plan-feature","title":"План фичи","to":"plan"}]`) {
		t.Errorf("stage show --json: %s", out)
	}

	// A stage without steps is not closed.
	wantRun(t, contract.ExitError, "", lines(msg.Text(msg.ErrStepsEmpty, "Ветка"), "", msg.Text(msg.HintStepAdd)),
		"stage", "exit", "--kind", "result", "--text", "Создана ветка")
	wantRun(t, contract.ExitOK, lines(
		"К этапу добавлено шагов: 1.",
		"",
		"№  СОСТОЯНИЕ     ШАГ                           ПОЯСНЕНИЕ",
		"1  запланирован  Создать ветку feature/shop-1  —",
	), "", "step", "add", "Создать ветку feature/shop-1")
	wantRun(t, contract.ExitOK, lines(
		"Шаг 1 выполнен.",
		"",
		"№  СОСТОЯНИЕ  ШАГ                           ПОЯСНЕНИЕ",
		"1  выполнен   Создать ветку feature/shop-1  git branch --show-current",
		"",
		"Закрыть этап: gentry stage exit --kind <вид> --text <текст>",
	), "", "step", "done", "1", "--check", "git branch --show-current")
	wantRun(t, contract.ExitOK, lines(
		"Этап «Ветка» закрыт.",
		"Вид выхода: результат",
		"Выход: Создана ветка feature/shop-1",
		"Переход: plan",
		"Этап: План фичи (plan-feature), круг 1",
		"Прогресс: 1 из 5",
		"",
		"Посмотреть этап: gentry stage show",
	), "", "stage", "exit", "--kind", "result", "--text", "Создана ветка feature/shop-1")

	// An exit by an artifact needs the artifact saved.
	mustRun(t, "step", "add", "Согласовать план")
	mustRun(t, "step", "done", "1")
	wantRun(t, contract.ExitError, "", lines(msg.Text(msg.ErrArtifactNotFound, "plan"), "", "Сохранить артефакт: gentry artifact save plan --file <путь>"),
		"stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован с оператором")
	wantRun(t, contract.ExitOK, lines("Артефакт plan сохранён.", "Адрес: https://claude.ai/code/artifact/plan"), "",
		"artifact", "save", "plan", "--url", "https://claude.ai/code/artifact/plan")
	out = wantJSON(t, contract.ExitOK, "schemas/stage-close.json",
		"stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован с оператором")
	var closed contract.StageCloseOutput
	json.Unmarshal([]byte(out), &closed)
	if c := closed.Closed; c.Node != "plan" || c.Exit == nil || c.Exit.Kind != "artifact" || *c.Exit.Artifact != "plan" ||
		*c.To != "implementation" || closed.Task.Stage.Node != "implementation" || closed.Task.Progress.Passed != 2 {
		t.Errorf("stage exit --json: %s", out)
	}

	// Implementation: a step not done is listed; a dropped step needs a reason.
	mustRun(t, "step", "add", "Добавить расчёт суммы", "Покрыть расчёт тестами")
	mustRun(t, "step", "done", "1", "--check", "go test ./backend/payments/...")
	wantRun(t, contract.ExitError, "", lines(
		msg.Text(msg.ErrStepsOpen),
		"",
		"Невыполненные шаги:",
		"  2. Покрыть расчёт тестами",
		"",
		"Отметить шаг выполненным: gentry step done <номер>",
		"Снять шаг: gentry step drop <номер> --reason <обоснование>",
	), "stage", "exit", "--kind", "result", "--text", "Изменения сделаны")
	if code, c := errorCode(t, "step", "drop", "2"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("step drop without a reason: exit code %d, code %s", code, c)
	}
	mustRun(t, "step", "drop", "2", "--reason", "Тесты уже есть в шаге 1")
	wantRun(t, contract.ExitOK, "Заметка 1 добавлена.\n", "", "note", "add", "На ревью проверить, что возврат по СБП не задет")
	mustRun(t, "stage", "exit", "--kind", "result", "--text", "Изменения сделаны, тесты проходят")

	// Review is a fork: the transition and its reason are required.
	mustRun(t, "step", "add", "Провести ревью")
	mustRun(t, "step", "done", "1")
	wantRun(t, contract.ExitUsage, "", lines(msg.Text(msg.ErrForkToMissing), "", msg.Text(msg.HintStageTransitions)),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны")
	wantRun(t, contract.ExitUsage, "", lines(msg.Text(msg.ErrForkReasonMissing), "", msg.Text(msg.HintCommandHelp, "stage exit")),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "implementation")
	wantRun(t, contract.ExitError, "", lines("У этапа «Ревью» нет перехода к узлу deploy.", "", msg.Text(msg.HintStageTransitions)),
		"stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "deploy", "--reason", "Выкатить")
	wantRun(t, contract.ExitOK, lines(
		"Этап «Ревью» закрыт.",
		"Вид выхода: результат",
		"Выход: Замечания записаны",
		"Переход: implementation",
		"Обоснование: Две ошибки в расчёте суммы",
		"Этап: Реализация (implementation), круг 2",
		"Прогресс: 4 из 5",
		"",
		"Посмотреть этап: gentry stage show",
	), "", "stage", "exit", "--kind", "result", "--text", "Замечания записаны", "--to", "implementation", "--reason", "Две ошибки в расчёте суммы")
	// The new pass starts without steps.
	if code, c := errorCode(t, "stage", "exit", "--kind", "result", "--text", "Исправлено"); code != contract.ExitError || c != contract.CodeStepsEmpty {
		t.Errorf("a new round with the steps of the last: exit code %d, code %s", code, c)
	}
	// Two more returns are allowed, the fourth is not.
	for range 2 {
		closeStage(t, "Исправить замечания")
		closeStage(t, "Провести ревью", "--to", "implementation", "--reason", "Есть замечания")
	}
	closeStage(t, "Исправить замечания")
	mustRun(t, "step", "add", "Провести ревью")
	mustRun(t, "step", "done", "1")
	_, stdout, _ := run("stage", "show")
	if !strings.Contains(stdout, "Этап: Ревью (review), круг 4\n") || !strings.Contains(stdout, "ревью выявило существенные замечания  3 из 3\n") {
		t.Errorf("stage show at the limit:\n%s", stdout)
	}
	wantRun(t, contract.ExitError, "", lines(
		"Возвраты к узлу implementation исчерпаны.",
		"Возвратов: 3 из 3",
		"",
		"Посмотреть другие переходы: gentry stage show",
	), "stage", "exit", "--kind", "result", "--text", "Замечания", "--to", "implementation", "--reason", "Ещё замечания")
	mustRun(t, "stage", "exit", "--kind", "result", "--text", "Замечаний нет", "--to", "merge", "--reason", "Существенных замечаний нет")

	// Merge is the operator's: no steps needed. Its exit passes the scenario.
	wantRun(t, contract.ExitOK, lines(
		"Этап «Слияние» закрыт.",
		"Вид выхода: результат",
		"Выход: Ветка влита в main",
		"Переход: конец",
		"Этап: сценарий пройден",
		"Прогресс: 5 из 5",
		"",
		"Посмотреть задачу: gentry task show",
	), "", "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
	finished := lines(msg.Text(msg.ErrScenarioFinished, "SHOP-1"), "", "Посмотреть задачу: gentry task show SHOP-1")
	wantRun(t, contract.ExitError, "", finished, "stage", "show")
	wantRun(t, contract.ExitError, "", finished, "step", "add", "Ещё шаг")
	wantRun(t, contract.ExitError, "", finished, "stage", "skip", "--reason", "Не нужен")
	if code, c := errorCode(t, "stage", "exit", "--kind", "result", "--text", "Ещё"); code != contract.ExitError || c != contract.CodeScenarioFinished {
		t.Errorf("stage exit after the end: exit code %d, code %s", code, c)
	}
	// Notes and artifacts are taken until the task is closed.
	mustRun(t, "note", "add", "Первая строка.\nВторая строка.")

	_, stdout, _ = run("task", "show")
	for _, want := range []string{
		"Этап: сценарий пройден\nПрогресс: 5 из 5\n",
		lines(
			"ЭТАП        КРУГ  ИТОГ           ПЕРЕХОД",
			"Ветка       1     результат      plan",
			"План фичи   1     артефакт plan  implementation",
			"Реализация  1     результат      review",
			"Ревью       1     результат      implementation",
			"Реализация  2     результат      review",
		),
		"Ревью       4     результат      merge\nСлияние     1     результат      конец\n\nЗаметки:\n",
		"  1. На ревью проверить, что возврат по СБП не задет\n  2. Первая строка.\n     Вторая строка.\n",
		"АРТЕФАКТ  ВИД     СОХРАНЁН          МЕСТО\nplan      ссылка  ",
	} {
		if !strings.Contains(masked(stdout), masked(want)) {
			t.Errorf("task show has no\n%s\noutput:\n%s", want, stdout)
		}
	}
	_, stdout, _ = run("task", "show", "--path")
	for _, want := range []string{
		lines(
			"Реализация (implementation), круг 1",
			"Итог: результат",
			"Выход: Изменения сделаны, тесты проходят",
			"Переход: review",
			"Записано: оператором",
			"Закрыт: <время>",
			"",
			"№  СОСТОЯНИЕ  ШАГ                     ПОЯСНЕНИЕ",
			"1  выполнен   Добавить расчёт суммы   go test ./backend/payments/...",
			"2  снят       Покрыть расчёт тестами  Тесты уже есть в шаге 1",
		),
		"Переход: implementation\nОбоснование: Две ошибки в расчёте суммы\n",
	} {
		if !strings.Contains(masked(stdout), want) {
			t.Errorf("task show --path has no\n%s\noutput:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "ЭТАП        КРУГ") {
		t.Errorf("task show --path has the table of the path:\n%s", stdout)
	}

	out = wantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if !shown.Task.Finished || shown.Task.Stage != nil || len(shown.Path) != 11 || len(shown.Notes) != 2 || len(shown.Artifacts) != 1 ||
		shown.Task.Progress != (contract.TaskProgress{Passed: 5, TotalMin: 5, TotalMax: 5}) {
		t.Errorf("task show --json: %s", out)
	}
	list := wantJSON(t, contract.ExitOK, "schemas/task-list.json", "task", "list")
	if !strings.Contains(list, `"finished":true`) || strings.Contains(list, `"stage":`) {
		t.Errorf("task list --json: %s", list)
	}

	// Every event of the way matches its schema.
	_, events, _ := run("events")
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
			validate(t, "schemas/events/"+e.Type+".json", string(e.Data))
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
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(fix)
	wantRun(t, contract.ExitUsage, "", lines(msg.Text(msg.ErrSkipReasonMissing), "", msg.Text(msg.HintCommandHelp, "stage skip")),
		"stage", "skip")
	wantRun(t, contract.ExitOK, lines(
		"Этап «Ветка» пропущен.",
		"Обоснование: Ветка уже создана оператором",
		"Переход: plan",
		"Этап: План фичи (plan-feature), круг 1",
		"Прогресс: 1 из 5",
		"",
		"Посмотреть этап: gentry stage show",
	), "", "stage", "skip", "--reason", "Ветка уже создана оператором")
	mustRun(t, "step", "add", "Согласовать план")
	if code, c := errorCode(t, "stage", "skip", "--reason", "Не нужен"); code != contract.ExitError || c != contract.CodeStepsOpen {
		t.Errorf("skip with a step not done: exit code %d, code %s", code, c)
	}
	mustRun(t, "step", "drop", "1", "--reason", "План не нужен")
	input := filepath.Join(t.TempDir(), "skip.json")
	writeFiles(t, filepath.Dir(input), map[string]string{"skip.json": `{"reason":"План не нужен"}`})
	out := wantJSON(t, contract.ExitOK, "schemas/stage-close.json", "stage", "skip", "--input", input)
	if !strings.Contains(out, `"outcome":"skip","reason":"План не нужен"`) {
		t.Errorf("stage skip --json: %s", out)
	}
	closeStage(t, "Сделать")
	if code, c := errorCode(t, "stage", "skip", "--reason", "Нечего смотреть"); code != contract.ExitUsage || c != contract.CodeMissingField {
		t.Errorf("skip at a fork without the transition: exit code %d, code %s", code, c)
	}
	_, stdout, _ := run("stage", "skip", "--reason", "Нечего смотреть", "--to", "merge")
	if !strings.Contains(stdout, "Переход: merge\nЭтап: Слияние (merge), круг 1\nПрогресс: 4 из 5\n") {
		t.Errorf("skip at a fork:\n%s", stdout)
	}
	_, stdout, _ = run("task", "show")
	if !strings.Contains(stdout, "Ветка       1     пропуск    plan\n") {
		t.Errorf("task show:\n%s", stdout)
	}
}

// TestStageByAgent checks the source of what the agent records: the stage
// of the operator closed by the agent is recorded from the operator's words.
func TestStageByAgent(t *testing.T) {
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(fix)
	t.Setenv(caller.SessionEnv, "drv-1")
	closeStage(t, "Создать ветку")
	mustRun(t, "artifact", "save", "plan", "--url", "https://example.com/plan")
	mustRun(t, "step", "add", "Согласовать план")
	mustRun(t, "step", "done", "1")
	mustRun(t, "stage", "exit", "--kind", "artifact", "--artifact", "plan", "--text", "План согласован")
	closeStage(t, "Сделать")
	closeStage(t, "Проверить", "--to", "merge", "--reason", "Замечаний нет")
	mustRun(t, "stage", "exit", "--kind", "result", "--text", "Влито оператором")
	_, stdout, _ := run("task", "show", "--path")
	for _, want := range []string{
		"Переход: plan\nЗаписано: агентом\n",
		"Переход: конец\nЗаписано: агентом со слов оператора\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("task show --path has no %q:\n%s", want, stdout)
		}
	}
	_, out, _ := run("task", "show", "--json")
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
	shop, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(shop)
	wantRun(t, contract.ExitError, "", lines(msg.Text(msg.ErrStepsEmpty, "Ветка"), "", "Добавить шаги: gentry step add <шаг> --task SHOP-1"),
		"stage", "exit", "--kind", "result", "--text", "Готово", "--task", "SHOP-1")
	mustRun(t, "step", "add", "Создать ветку", "--task", "shop-1")
	_, stdout, _ := run("step", "done", "1", "--task", "SHOP-1")
	if !strings.HasSuffix(stdout, "\nЗакрыть этап: gentry stage exit --kind <вид> --text <текст> --task SHOP-1\n") {
		t.Errorf("step done:\n%s", stdout)
	}
	_, stdout, _ = run("stage", "exit", "--kind", "result", "--text", "Готово", "--task", "SHOP-1")
	if !strings.HasSuffix(stdout, "\nПосмотреть этап: gentry stage show --task SHOP-1\n") {
		t.Errorf("stage exit:\n%s", stdout)
	}
	// In the main worktree, which holds no task, a command without --task finds none.
	if code, c := errorCode(t, "stage", "show"); code != contract.ExitError || c != contract.CodeTaskUndetermined {
		t.Errorf("stage show without a task: exit code %d, code %s", code, c)
	}
	if code, c := errorCode(t, "stage", "show", "--task", "SHOP-9"); code != contract.ExitError || c != contract.CodeTaskNotFound {
		t.Errorf("stage show of no task: exit code %d, code %s", code, c)
	}
	if code, c := errorCode(t, "stage", "show", "--task", "shop"); code != contract.ExitUsage || c != contract.CodeFlagValue {
		t.Errorf("stage show --task shop: exit code %d, code %s", code, c)
	}
}

// TestWayRefusals checks the refusals of the fields of the commands.
func TestWayRefusals(t *testing.T) {
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(fix)
	mustRun(t, "step", "add", "Создать ветку")
	mustRun(t, "step", "done", "1")
	input := func(text string) string {
		p := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exitHelp := msg.Text(msg.HintCommandHelp, "stage exit")
	long := strings.Repeat("ш", 121)
	tests := []struct {
		name   string
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{"no kind", []string{"stage", "exit", "--text", "Т"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrExitKindMissing), "", exitHelp)},
		{"bad kind", []string{"stage", "exit", "--kind", "fact", "--text", "Т"}, contract.ExitUsage, contract.CodeFlagValue,
			lines(msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact"), "", exitHelp)},
		{"bad kind in input", []string{"stage", "exit", "--input", input(`{"kind":"fact","text":"Т"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			lines("Не удалось прочитать поля из --input: недопустимое значение поля «kind».", "", exitHelp)},
		{"no text", []string{"stage", "exit", "--kind", "result"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrExitTextMissing), "", exitHelp)},
		{"no artifact", []string{"stage", "exit", "--kind", "artifact", "--text", "Т"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrExitArtifactMissing), "", exitHelp)},
		{"artifact of a result", []string{"stage", "exit", "--kind", "result", "--artifact", "plan", "--text", "Т"}, contract.ExitUsage, contract.CodeConflictingFlags,
			lines(msg.Text(msg.ErrArtifactForKind), "", exitHelp)},
		{"flag with input", []string{"stage", "exit", "--kind", "result", "--input", "x.json"}, contract.ExitUsage, contract.CodeConflictingFlags,
			lines(msg.Text(msg.ErrConflictingFlags, "--kind", "--input"), "", exitHelp)},
		{"no step", []string{"step", "add"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrStepsMissing), "", msg.Text(msg.HintCommandHelp, "step add"))},
		{"long step", []string{"step", "add", long}, contract.ExitUsage, contract.CodeFieldInvalid,
			lines(msg.Text(msg.ErrStepTooLong), "", msg.Text(msg.HintStep))},
		{"step of two lines", []string{"step", "add", "А\nБ"}, contract.ExitUsage, contract.CodeFieldInvalid,
			lines(msg.Text(msg.ErrStepMultiline), "", msg.Text(msg.HintStep))},
		{"steps not a list", []string{"step", "add", "--input", input(`{"steps":"Шаг"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			lines("Не удалось прочитать поля из --input: поле «steps» должно быть массивом строк.", "", msg.Text(msg.HintCommandHelp, "step add"))},
		{"arguments with input", []string{"step", "add", "Шаг", "--input", input(`{"steps":["Шаг"]}`)}, contract.ExitUsage, contract.CodeConflictingFlags,
			lines(msg.Text(msg.ErrInputWithArgs), "", msg.Text(msg.HintCommandHelp, "step add"))},
		{"step number", []string{"step", "done", "первый"}, contract.ExitUsage, contract.CodeInvalidArgument,
			lines(msg.Text(msg.ErrStepNumberInvalid, "первый"), "", msg.Text(msg.HintSteps))},
		{"step number not an integer", []string{"step", "done", "--input", input(`{"step":"1"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			lines("Не удалось прочитать поля из --input: поле «step» должно быть целым числом.", "", msg.Text(msg.HintCommandHelp, "step done"))},
		{"no step number", []string{"step", "done"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrStepNumberMissing), "", msg.Text(msg.HintCommandHelp, "step done"))},
		{"step not found", []string{"step", "done", "7"}, contract.ExitError, contract.CodeStepNotFound,
			lines(msg.Text(msg.ErrStepNotFound, 7), "", msg.Text(msg.HintSteps))},
		{"step done", []string{"step", "done", "1"}, contract.ExitError, contract.CodeStepClosed,
			lines(msg.Text(msg.ErrStepDoneAlready, 1), "", msg.Text(msg.HintSteps))},
		{"no note", []string{"note", "add"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrNoteMissing), "", msg.Text(msg.HintCommandHelp, "note add"))},
		{"no artifact name", []string{"artifact", "save", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrArtifactNameMissing), "", msg.Text(msg.HintCommandHelp, "artifact save"))},
		{"bad artifact name", []string{"artifact", "save", "план", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeFieldInvalid,
			lines(msg.Text(msg.ErrArtifactNameInvalid, "план"), "", msg.Text(msg.HintArtifactName))},
		{"no file or address", []string{"artifact", "save", "plan"}, contract.ExitUsage, contract.CodeMissingField,
			lines(msg.Text(msg.ErrArtifactSourceMissing), "", msg.Text(msg.HintCommandHelp, "artifact save"))},
		{"file and address", []string{"artifact", "save", "plan", "--file", "plan.md", "--url", "https://example.com"}, contract.ExitUsage, contract.CodeConflictingFlags,
			lines(msg.Text(msg.ErrConflictingFlags, "--file", "--url"), "", msg.Text(msg.HintCommandHelp, "artifact save"))},
		{"bad address", []string{"artifact", "save", "plan", "--url", "claude.ai/code/artifact/1"}, contract.ExitUsage, contract.CodeFieldInvalid,
			lines(msg.Text(msg.ErrArtifactURLInvalid, "claude.ai/code/artifact/1"), "", msg.Text(msg.HintArtifactURL))},
		{"no file", []string{"artifact", "save", "plan", "--file", "plan.md"}, contract.ExitUsage, contract.CodeFieldInvalid,
			lines(msg.Text(msg.ErrArtifactFileNotFound, "plan.md"), "", msg.Text(msg.HintArtifactFile))},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr {
			t.Errorf("%s: exit code %d, stdout %q, stderr:\n%s\nwant %d:\n%s", tt.name, code, stdout, stderr, tt.exit, tt.stderr)
		}
		if code, c := errorCode(t, tt.args...); code != tt.exit || c != tt.code {
			t.Errorf("%s in JSON: exit code %d, code %q; want %d, %q", tt.name, code, c, tt.exit, tt.code)
		}
	}
	mustRun(t, "step", "add", "Проверить ветку")
	mustRun(t, "step", "drop", "2", "--reason", "Не нужно")
	wantRun(t, contract.ExitError, "", lines(msg.Text(msg.ErrStepDroppedAlready, 2), "", msg.Text(msg.HintSteps)), "step", "drop", "2", "--reason", "Ещё раз")
}

// TestStepsInput adds steps, marks and drops them through --input.
func TestStepsInput(t *testing.T) {
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	t.Chdir(fix)
	out := wantJSON(t, contract.ExitOK, "schemas/step-list.json", "step", "add", "Первый", "Второй")
	if !strings.Contains(out, `"added":[1,2]`) || !strings.Contains(out, `"node":"branch","round":1`) {
		t.Errorf("step add --json: %s", out)
	}
	code, out, _ := runWith(`{"steps":["Третий"]}`, "step", "add", "--input", "-", "--json")
	if code != contract.ExitOK || !strings.Contains(out, `"added":[3]`) {
		t.Errorf("step add --input: exit code %d, %s", code, out)
	}
	code, out, _ = runWith(`{"step":2,"check":"git status"}`, "step", "done", "--input", "-", "--json")
	validate(t, "schemas/step-list.json", out)
	if code != contract.ExitOK || !strings.Contains(out, `"check":"git status"`) || strings.Contains(out, `"added":[`) {
		t.Errorf("step done --input: exit code %d, %s", code, out)
	}
	code, out, _ = runWith(`{"step":3,"reason":"Лишний"}`, "step", "drop", "--input", "-", "--json")
	if code != contract.ExitOK || !strings.Contains(out, `"reason":"Лишний"`) || !strings.Contains(out, `"closed_source":"operator"`) {
		t.Errorf("step drop --input: exit code %d, %s", code, out)
	}
	code, out, _ = runWith(`{"text":"Строка 1.\nСтрока 2."}`, "note", "add", "--input", "-", "--json")
	validate(t, "schemas/note-add.json", out)
	if code != contract.ExitOK || !strings.Contains(out, `"node":"branch"`) || !strings.Contains(out, `"number":1`) {
		t.Errorf("note add --input: exit code %d, %s", code, out)
	}
}

// TestArtifactFile keeps a copy of a file, replaces it, and replaces it by a
// link, which removes the copy.
func TestArtifactFile(t *testing.T) {
	_, fix := taskShop(t)
	mustRun(t, takeArgs("--worktree", fix)...)
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"plan.md": "# План\n"})
	_, stdout, _ := run("artifact", "save", "plan.md", "--file", "plan.md", "--task", "SHOP-1")
	copyPath := filepath.Join(os.Getenv("GENTRY_HOME"), "state", "shop", "tasks", "SHOP-1", "artifacts", "plan.md")
	if want := lines("Артефакт plan.md сохранён.", "Файл: "+copyPath); stdout != want {
		t.Errorf("artifact save:\n%s\nwant:\n%s", stdout, want)
	}
	writeFiles(t, dir, map[string]string{"plan.md": "# План, вторая версия\n"})
	out := wantJSON(t, contract.ExitOK, "schemas/artifact-save.json", "artifact", "save", "plan.md", "--file", "plan.md", "--task", "SHOP-1")
	if !strings.Contains(out, `"replaced":true`) || !strings.Contains(out, `"kind":"file"`) {
		t.Errorf("artifact save --json: %s", out)
	}
	if b, err := os.ReadFile(copyPath); err != nil || string(b) != "# План, вторая версия\n" {
		t.Errorf("copy %q, %v", b, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(copyPath)); len(entries) != 1 {
		t.Errorf("files left in the artifacts: %v", entries)
	}
	_, stdout, _ = run("artifact", "save", "plan.md", "--url", "https://example.com/plan", "--task", "SHOP-1")
	if stdout != lines("Артефакт plan.md заменён.", "Адрес: https://example.com/plan") {
		t.Errorf("replaced by a link:\n%s", stdout)
	}
	if fileExists(copyPath) {
		t.Error("the copy of the file is left after the link replaced it")
	}
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, 10<<20+1), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRun(t, contract.ExitUsage, "", lines(msg.Text(msg.ErrArtifactFileTooLarge, "big.bin"), "", msg.Text(msg.HintArtifactLink)+" --task SHOP-1"),
		"artifact", "save", "big", "--file", "big.bin", "--task", "SHOP-1")
}

// TestWayWithoutStore checks that the commands of the way of a task find no
// task without a state store and do not create one.
func TestWayWithoutStore(t *testing.T) {
	emptyHome(t)
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"stage", "show"}, {"step", "add", "Шаг"}, {"note", "add", "Заметка"}, {"stage", "exit", "--kind", "result", "--text", "Т", "--task", "SHOP-1"}} {
		if code, _, _ := run(args...); code != contract.ExitError {
			t.Errorf("%v: exit code %d", args, code)
		}
	}
	if p, _ := state.Path(); fileExists(p) {
		t.Errorf("the commands must not create the state store %s", p)
	}
}
