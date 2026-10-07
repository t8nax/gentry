package stage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/msg"
)

// sbpInput is a decision with a question and options of the answer.
const sbpInput = `{
  "question": "Делать частичный возврат и для СБП?",
  "options": [
    {"label": "Только карта", "description": "СБП требует другого API банка.", "recommended": true},
    {"label": "Карта и СБП", "description": "Задача вырастет примерно вдвое.\nСроки сдвинутся."},
    {"label": "Отложить"}
  ],
  "answer": "Давай первый."
}`

// TestOperatorRecord records decisions of the operator with and without a
// question and shows them with the statement.
func TestOperatorRecord(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)

	// Without decisions the statement has no block of them.
	_, stdout, _ := clitest.Run("task", "show", "--statement")
	if strings.Contains(stdout, "Решения оператора") {
		t.Errorf("task show --statement without decisions:\n%s", stdout)
	}
	input := filepath.Join(t.TempDir(), "decision.json")
	if err := os.WriteFile(input, []byte(sbpInput), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WantRun(t, contract.ExitOK, "Решение оператора записано.\n", "", "operator", "record", "--input", input)
	closeStage(t, "Создать ветку")
	// The agent records the words of the operator: two lines, no question.
	t.Setenv(caller.SessionEnv, "drv-1")
	code, stdout, stderr := clitest.RunWith(`{"answer": "Сумму возврата писать в лог.\nУровень — info."}`, "operator", "record", "--input", "-")
	if code != contract.ExitOK || stdout != "Решение оператора записано.\n" {
		t.Errorf("operator record --input -: exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	t.Setenv(caller.SessionEnv, "")

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		"Постановка задачи SHOP-1:",
		"  "+clitest.Statement,
		"",
		"Постановка записана: оператором",
		"",
		"Решения оператора:",
		"  1. Ветка (branch), круг 1, записано оператором:",
		"     Вопрос: Делать частичный возврат и для СБП?",
		"     Варианты:",
		"       1. Только карта (рекомендован): СБП требует другого API банка.",
		"       2. Карта и СБП:",
		"            Задача вырастет примерно вдвое.",
		"            Сроки сдвинутся.",
		"       3. Отложить",
		"     Ответ: Давай первый.",
		"",
		"  2. План фичи (plan-feature), круг 1, записано агентом со слов оператора:",
		"     Ответ:",
		"       Сумму возврата писать в лог.",
		"       Уровень — info.",
	), "", "task", "show", "--statement")
	_, stdout, _ = clitest.Run("task", "show")
	if !strings.HasSuffix(stdout, "\n\nПосмотреть постановку и решения оператора: gentry task show --statement\n") {
		t.Errorf("task show has no hint to the decisions:\n%s", stdout)
	}
	t.Chdir(t.TempDir())
	_, stdout, _ = clitest.Run("task", "show", "SHOP-1")
	if !strings.Contains(stdout, "Посмотреть постановку и решения оператора: gentry task show SHOP-1 --statement\n") {
		t.Errorf("task show outside the worktree:\n%s", stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/operator-record.json",
		"operator", "record", "--task", "SHOP-1", "--answer", "Сначала карта.", "--json")
	if !strings.Contains(out, `"task":"SHOP-1"`) || !strings.Contains(out, `"number":3`) || strings.Contains(out, `"return"`) ||
		strings.Contains(out, `"question"`) || !strings.Contains(out, `"source":"operator"`) {
		t.Errorf("operator record --json: %s", out)
	}
	t.Chdir(fix)

	out = clitest.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "--statement", "--json")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if ds := shown.OperatorDecisions; len(ds) != 3 || ds[0].Question == nil || len(ds[0].Options) != 3 ||
		ds[0].Options[0].Recommended == nil || ds[0].Options[2].Description != nil || ds[0].Node != "branch" ||
		ds[1].Question != nil || ds[1].Source != "agent" || ds[1].Stage != "plan-feature" || ds[2].Number != 3 {
		t.Errorf("task show --json: %s", out)
	}

	_, events, _ := clitest.Run("events")
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(events), "\n") {
		var e struct {
			Type string          `json:"type"`
			Task string          `json:"task"`
			Data json.RawMessage `json:"data"`
		}
		json.Unmarshal([]byte(line), &e)
		if e.Type == "operator_decision.recorded" {
			clitest.Validate(t, "schemas/events/operator_decision.recorded.json", string(e.Data))
			if e.Task != "SHOP-1" {
				t.Errorf("event of task %q", e.Task)
			}
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d events of decisions, want 3:\n%s", n, events)
	}
}

// TestOperatorRecordRefusals refuses a decision without an answer and
// options of an answer that cannot be taken.
func TestOperatorRecordRefusals(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	help := msg.Text(msg.HintCommandHelp, "operator record")

	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines("Не указан ответ оператора.", "", help),
		"operator", "record", "--question", "Делать для СБП?")
	tests := []struct {
		name, input, code, stderr string
	}{
		{"options without a question", `{"options": [{"label": "А"}, {"label": "Б"}], "answer": "А"}`,
			contract.CodeMissingField, clitest.Lines("Не указан вопрос, к которому относятся варианты.", "", help)},
		{"one option", `{"question": "Как?", "options": [{"label": "А"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines("Вариантов меньше двух.", "", "Укажите не меньше двух вариантов или ни одного.")},
		{"two recommended", `{"question": "Как?", "options": [{"label": "А", "recommended": true}, {"label": "Б", "recommended": true}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines("Рекомендовано больше одного варианта.", "", "Отметьте рекомендованным не больше одного варианта.")},
		{"no label", `{"question": "Как?", "options": [{"label": "А"}, {"description": "Б"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines("Не указано название варианта 2.", "", "Укажите название варианта одной строкой не длиннее 120 знаков.")},
		{"label of two lines", `{"question": "Как?", "options": [{"label": "А\nБ"}, {"label": "В"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines("Название варианта 1 состоит из нескольких строк.", "", "Укажите название варианта одной строкой не длиннее 120 знаков.")},
		{"long label", `{"question": "Как?", "options": [{"label": "А"}, {"label": "` + strings.Repeat("б", 121) + `"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines("Название варианта 2 длиннее 120 знаков.", "", "Укажите название варианта одной строкой не длиннее 120 знаков.")},
		{"options not an array", `{"question": "Как?", "options": "А, Б", "answer": "А"}`,
			contract.CodeInputInvalid, clitest.Lines("Не удалось прочитать поля из --input: поле «options» должно быть массивом объектов.", "", help)},
		{"unknown field of an option", `{"question": "Как?", "options": [{"label": "А"}, {"label": "Б", "weight": 2}], "answer": "А"}`,
			contract.CodeInputInvalid, clitest.Lines("Не удалось прочитать поля из --input: вариант 2 должен быть объектом с полями label, description и recommended.", "", help)},
	}
	for _, tt := range tests {
		code, stdout, stderr := clitest.RunWith(tt.input, "operator", "record", "--input", "-")
		if code != contract.ExitUsage || stdout != "" || stderr != tt.stderr {
			t.Errorf("%s: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", tt.name, code, stdout, stderr, tt.stderr)
		}
		_, out, _ := clitest.RunWith(tt.input, "operator", "record", "--input", "-", "--json")
		clitest.Validate(t, "schemas/error.json", out)
		if !strings.Contains(out, `"code":"`+tt.code+`"`) {
			t.Errorf("%s: %s, want code %s", tt.name, out, tt.code)
		}
	}
	if code, c := clitest.ErrorCode(t, "operator", "record", "--answer", "Да", "--input", "-"); code != contract.ExitUsage || c != contract.CodeConflictingFlags {
		t.Errorf("--answer with --input: exit code %d, code %s", code, c)
	}
	if code, c := clitest.ErrorCode(t, "operator", "record", "--answer", "Да", "--allow-return", ""); code != contract.ExitUsage || c != contract.CodeFlagValue {
		t.Errorf("--allow-return without a value: exit code %d, code %s", code, c)
	}
	// The branch has no returns at all.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines("У этапа «Ветка» нет возврата к узлу plan.", "", msg.Text(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "plan")
	_, out, _ := clitest.Run("operator", "record", "--answer", "Да", "--allow-return", "plan", "--json")
	if !strings.Contains(out, `"code":"return_not_found"`) || !strings.Contains(out, `"returns":[]`) {
		t.Errorf("return_not_found --json: %s", out)
	}
	// Nothing refused was recorded.
	if _, stdout, _ := clitest.Run("task", "show", "--statement"); strings.Contains(stdout, "Решения оператора") {
		t.Errorf("a refused decision is recorded:\n%s", stdout)
	}
}

// TestOperatorAllowReturn raises the limit of a return by decisions of the
// operator: a limit of 1, the second return refused, allowed, made, refused
// again and allowed again.
func TestOperatorAllowReturn(t *testing.T) {
	feature := strings.Replace(readTestdata(t, "scenarios/feature.yaml"), "max_rounds: 3", "max_rounds: 1", 1)
	_, fix := clitest.TaskShop(t)
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{"scenarios/feature.yaml": feature})
	clitest.MustRun(t, "flow", "apply")
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	closeStage(t, "Создать ветку")
	closeStage(t, "Согласовать план")
	closeStage(t, "Сделать")
	closeStage(t, "Провести ревью", "--to", "implementation", "--reason", "Есть замечания")
	closeStage(t, "Исправить")
	clitest.MustRun(t, "step", "add", "Провести ревью")
	clitest.MustRun(t, "step", "done", "1")
	exit := []string{"stage", "exit", "--kind", "result", "--text", "Замечания", "--to", "implementation", "--reason", "Ещё замечания"}
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(
		"Возвраты к узлу implementation исчерпаны.",
		"Возвратов: 1 из 1",
		"",
		"Посмотреть другие переходы: gentry stage show",
		"Записать разрешение оператора: gentry operator record --answer <ответ> --allow-return implementation",
	), exit...)

	// Only a return of the current stage can be allowed.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines("У этапа «Ревью» нет возврата к узлу merge.", "", msg.Text(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "merge")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines("У этапа «Ревью» нет возврата к узлу plan.", "", msg.Text(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "plan")
	_, out, _ := clitest.Run("operator", "record", "--answer", "Да", "--allow-return", "merge", "--json")
	if !strings.Contains(out, `"details":{"node":"review","returns":["implementation"],"to":"merge"}`) {
		t.Errorf("return_not_found --json: %s", out)
	}

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		"Решение оператора записано.",
		"Разрешён возврат: Реализация (implementation)",
		"Возвратов: 1 из 2",
	), "", "operator", "record", "--question", "Вернуть на реализацию сверх предела?", "--answer", "Да.", "--allow-return", "implementation")
	_, stdout, _ := clitest.Run("stage", "show")
	if !strings.Contains(stdout, "ревью выявило существенные замечания  1 из 2\n") {
		t.Errorf("stage show after the allowance:\n%s", stdout)
	}
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/stage-show.json", "stage", "show")
	if !strings.Contains(out, `"allowed_returns":1,"if":"ревью выявило существенные замечания","max_returns":2,"returns":1`) {
		t.Errorf("stage show --json: %s", out)
	}
	clitest.MustRun(t, exit...)
	_, stdout, _ = clitest.Run("task", "show")
	if !strings.Contains(stdout, "Этап: Реализация (implementation), круг 3\n") {
		t.Errorf("task show after the return allowed:\n%s", stdout)
	}

	// The next return is refused again, until the next allowance.
	closeStage(t, "Исправить")
	clitest.MustRun(t, "step", "add", "Провести ревью")
	clitest.MustRun(t, "step", "done", "1")
	_, _, stderr := clitest.Run(exit...)
	if !strings.Contains(stderr, "Возвратов: 2 из 2\n") {
		t.Errorf("the third return:\n%s", stderr)
	}
	_, out, _ = clitest.Run(append(exit, "--json")...)
	if !strings.Contains(out, `"details":{"allowed":1,"limit":2,"node":"review","to":"implementation"}`) {
		t.Errorf("return_limit --json: %s", out)
	}
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/operator-record.json",
		"operator", "record", "--answer", "Да, ещё раз.", "--allow-return", "implementation", "--json")
	if !strings.Contains(out, `"return":{"allowed_returns":2,"max_returns":3,"node":"review","returns":2,"to":"implementation"}`) ||
		!strings.Contains(out, `"allow_return":"implementation"`) {
		t.Errorf("operator record --json: %s", out)
	}
	clitest.MustRun(t, exit...)
	closeStage(t, "Исправить")
	closeStage(t, "Провести ревью", "--to", "merge", "--reason", "Замечаний нет")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Влито")

	// Once the scenario is passed, a return is not allowed; a decision is
	// still recorded, at the last stage.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrScenarioFinished, "SHOP-1"), "", "Закрыть задачу: gentry task close"),
		"operator", "record", "--answer", "Да", "--allow-return", "implementation")
	clitest.WantRun(t, contract.ExitOK, "Решение оператора записано.\n", "", "operator", "record", "--answer", "Задачу закрыть.")
	_, stdout, _ = clitest.Run("task", "show", "--statement")
	for _, want := range []string{
		clitest.Lines(
			"  1. Ревью (review), круг 2, записано оператором:",
			"     Вопрос: Вернуть на реализацию сверх предела?",
			"     Ответ: Да.",
			"     Разрешён возврат: Реализация (implementation)",
		),
		"  3. Слияние (merge), круг 1, записано оператором:\n     Ответ: Задачу закрыть.\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("task show --statement has no\n%s\noutput:\n%s", want, stdout)
		}
	}
}

// readTestdata reads a file of the flow of the flow testdata.
func readTestdata(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "flow", "testdata", "process", "shop", "flow", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
