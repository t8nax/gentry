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

// The output of a decision recorded, and who recorded it.
var (
	recorded = msg.Text(msg.DecisionRecorded) + "\n"
	operator = msg.Text(msg.TaskSourceOperator)
)

// TestOperatorRecord records decisions of the operator with and without a
// question and shows them with the statement.
func TestOperatorRecord(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)

	// Without decisions the statement has no block of them.
	_, stdout, _ := clitest.Run("task", "show", "--statement")
	if strings.Contains(stdout, msg.Text(msg.DecisionsHeading)) {
		t.Errorf("task show --statement without decisions:\n%s", stdout)
	}
	input := filepath.Join(t.TempDir(), "decision.json")
	if err := os.WriteFile(input, []byte(sbpInput), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WantRun(t, contract.ExitOK, recorded, "", "operator", "record", "--input", input)
	closeStage(t, "Создать ветку")
	// The agent records the words of the operator: two lines, no question.
	t.Setenv(caller.SessionEnv, "drv-1")
	code, stdout, stderr := clitest.RunWith(`{"answer": "Сумму возврата писать в лог.\nУровень — info."}`, "operator", "record", "--input", "-")
	if code != contract.ExitOK || stdout != recorded {
		t.Errorf("operator record --input -: exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	t.Setenv(caller.SessionEnv, "")

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StatementHeading, "SHOP-1"),
		"  "+clitest.Statement,
		"",
		msg.Text(msg.TaskSource, operator),
		"",
		msg.Text(msg.DecisionsHeading),
		"  "+msg.Text(msg.DecisionHeading, 1, clitest.Round("Ветка", "branch", 1), operator),
		"     "+msg.Text(msg.DecisionQuestion)+": Делать частичный возврат и для СБП?",
		"     "+msg.Text(msg.DecisionOptions),
		"       1. "+msg.Text(msg.OptionRecommended, "Только карта")+": СБП требует другого API банка.",
		"       2. Карта и СБП:",
		"            Задача вырастет примерно вдвое.",
		"            Сроки сдвинутся.",
		"       3. Отложить",
		"     "+msg.Text(msg.DecisionAnswer)+": Давай первый.",
		"",
		"  "+msg.Text(msg.DecisionHeading, 2, clitest.Round("План фичи", "plan-feature", 1), msg.Text(msg.TaskSourceAgent)),
		"     "+msg.Text(msg.DecisionAnswer)+":",
		"       Сумму возврата писать в лог.",
		"       Уровень — info.",
	), "", "task", "show", "--statement")
	_, stdout, _ = clitest.Run("task", "show")
	if !strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintStatementDecisions)+"\n") {
		t.Errorf("task show has no hint to the decisions:\n%s", stdout)
	}
	t.Chdir(t.TempDir())
	_, stdout, _ = clitest.Run("task", "show", "SHOP-1")
	if !strings.Contains(stdout, cli.HintFor(msg.HintStatementDecisions, "task", "SHOP-1")+"\n") {
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
	help := cli.HintText(msg.HintCommandHelp, "operator record")

	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrAnswerMissing), "", help),
		"operator", "record", "--question", "Делать для СБП?")
	tests := []struct {
		name, input, code, stderr string
	}{
		{"options without a question", `{"options": [{"label": "А"}, {"label": "Б"}], "answer": "А"}`,
			contract.CodeMissingField, clitest.Lines(msg.Text(msg.ErrOptionsQuestionMissing), "", help)},
		{"one option", `{"question": "Как?", "options": [{"label": "А"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines(msg.Text(msg.ErrOptionsTooFew), "", msg.Text(msg.HintOptionsTooFew))},
		{"two recommended", `{"question": "Как?", "options": [{"label": "А", "recommended": true}, {"label": "Б", "recommended": true}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines(msg.Text(msg.ErrOptionsRecommendedMany), "", msg.Text(msg.HintOptionsRecommended))},
		{"no label", `{"question": "Как?", "options": [{"label": "А"}, {"description": "Б"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines(msg.Text(msg.ErrOptionLabelEmpty, 2), "", msg.Text(msg.HintOptionLabel))},
		{"label of two lines", `{"question": "Как?", "options": [{"label": "А\nБ"}, {"label": "В"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines(msg.Text(msg.ErrOptionLabelMultiline, 1), "", msg.Text(msg.HintOptionLabel))},
		{"long label", `{"question": "Как?", "options": [{"label": "А"}, {"label": "` + strings.Repeat("б", 121) + `"}], "answer": "А"}`,
			contract.CodeFieldInvalid, clitest.Lines(msg.Text(msg.ErrOptionLabelTooLong, 2), "", msg.Text(msg.HintOptionLabel))},
		{"options not an array", `{"question": "Как?", "options": "А, Б", "answer": "А"}`,
			contract.CodeInputInvalid, clitest.Lines(msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotObjects, "options")), "", help)},
		{"unknown field of an option", `{"question": "Как?", "options": [{"label": "А"}, {"label": "Б", "weight": 2}], "answer": "А"}`,
			contract.CodeInputInvalid, clitest.Lines(msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputBadOption, 2)), "", help)},
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
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrReturnNotFound, "Ветка", "plan"), "", cli.HintText(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "plan")
	_, out, _ := clitest.Run("operator", "record", "--answer", "Да", "--allow-return", "plan", "--json")
	if !strings.Contains(out, `"code":"return_not_found"`) || !strings.Contains(out, `"returns":[]`) {
		t.Errorf("return_not_found --json: %s", out)
	}
	// Nothing refused was recorded.
	if _, stdout, _ := clitest.Run("task", "show", "--statement"); strings.Contains(stdout, msg.Text(msg.DecisionsHeading)) {
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
	clitest.WantRun(t, contract.ExitError, "", returnLimit("implementation", 1), exit...)

	// Only a return of the current stage can be allowed.
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrReturnNotFound, "Ревью", "merge"), "", cli.HintText(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "merge")
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrReturnNotFound, "Ревью", "plan"), "", cli.HintText(msg.HintStageTransitions)),
		"operator", "record", "--answer", "Да", "--allow-return", "plan")
	_, out, _ := clitest.Run("operator", "record", "--answer", "Да", "--allow-return", "merge", "--json")
	if !strings.Contains(out, `"details":{"node":"review","returns":["implementation"],"to":"merge"}`) {
		t.Errorf("return_not_found --json: %s", out)
	}

	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.DecisionRecorded),
		msg.Text(msg.AllowedReturnLine, msg.Text(msg.TaskNamed, "Реализация", "implementation")),
		msg.Text(msg.ReturnsLine, 1, 2),
	), "", "operator", "record", "--question", "Вернуть на реализацию сверх предела?", "--answer", "Да.", "--allow-return", "implementation")
	_, stdout, _ := clitest.Run("stage", "show")
	if !strings.Contains(stdout, "ревью выявило существенные замечания  "+msg.Text(msg.StageReturns, 1, 2)+"\n") {
		t.Errorf("stage show after the allowance:\n%s", stdout)
	}
	out = clitest.WantJSON(t, contract.ExitOK, "schemas/stage-show.json", "stage", "show")
	if !strings.Contains(out, `"allowed_returns":1,"if":"ревью выявило существенные замечания","max_returns":2,"returns":1`) {
		t.Errorf("stage show --json: %s", out)
	}
	clitest.MustRun(t, exit...)
	_, stdout, _ = clitest.Run("task", "show")
	if !strings.Contains(stdout, msg.Text(msg.TaskStage, clitest.Round("Реализация", "implementation", 3))+"\n") {
		t.Errorf("task show after the return allowed:\n%s", stdout)
	}

	// The next return is refused again, until the next allowance.
	closeStage(t, "Исправить")
	clitest.MustRun(t, "step", "add", "Провести ревью")
	clitest.MustRun(t, "step", "done", "1")
	_, _, stderr := clitest.Run(exit...)
	if !strings.Contains(stderr, msg.Text(msg.ReturnsLine, 2, 2)+"\n") {
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
	clitest.WantRun(t, contract.ExitError, "", clitest.Lines(msg.Text(msg.ErrScenarioFinished, "SHOP-1"), "", cli.HintText(msg.HintTaskClose)),
		"operator", "record", "--answer", "Да", "--allow-return", "implementation")
	clitest.WantRun(t, contract.ExitOK, recorded, "", "operator", "record", "--answer", "Задачу закрыть.")
	_, stdout, _ = clitest.Run("task", "show", "--statement")
	for _, want := range []string{
		clitest.Lines(
			"  "+msg.Text(msg.DecisionHeading, 1, clitest.Round("Ревью", "review", 2), operator),
			"     "+msg.Text(msg.DecisionQuestion)+": Вернуть на реализацию сверх предела?",
			"     "+msg.Text(msg.DecisionAnswer)+": Да.",
			"     "+msg.Text(msg.AllowedReturnLine, msg.Text(msg.TaskNamed, "Реализация", "implementation")),
		),
		clitest.Lines(
			"  "+msg.Text(msg.DecisionHeading, 3, clitest.Round("Слияние", "merge", 1), operator),
			"     "+msg.Text(msg.DecisionAnswer)+": Задачу закрыть.",
		),
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
