package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

func TestOperatorRecordText(t *testing.T) {
	decision := contract.OperatorDecision{Number: 1, Answer: "Да.", Node: "review", Stage: "review", Round: 2,
		Source: contract.OperatorDecisionSourceOperator, Recorded: shopTime}
	wantText(t, "recorded", func(p *page) {
		operatorRecordText(p, contract.OperatorRecordOutput{Task: "SHOP-1", Decision: decision}, shopNames())
	}, "Решение оператора записано.\n", "")
	allowed := decision
	allowed.AllowReturn = ptr("implementation")
	wantText(t, "return allowed", func(p *page) {
		operatorRecordText(p, contract.OperatorRecordOutput{Task: "SHOP-1", Decision: allowed, Return: &contract.OperatorRecordOutputReturn{
			Node: "review", To: "implementation", Returns: 1, MaxReturns: 2, AllowedReturns: 1}}, shopNames())
	}, lines(
		"Решение оператора записано.",
		"Разрешён возврат: "+titled("Реализация", "implementation"),
		msg.Text(msg.ReturnsLine, 1, 2),
	), "")
}

func TestCheckDecisionText(t *testing.T) {
	two := []state.Option{{Label: "А"}, {Label: "Б"}}
	long := strings.Repeat("б", 121)
	tests := []struct {
		name       string
		req        task.Decision
		cli, agent string
	}{
		{"no answer", task.Decision{Question: "Делать для СБП?"}, lines(
			"Не указан ответ оператора.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry operator record --help"),
		), "Не указан ответ оператора.\n"},
		{"options without a question", task.Decision{Options: two, Answer: "А"}, lines(
			"Не указан вопрос, к которому относятся варианты.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry operator record --help"),
		), "Не указан вопрос, к которому относятся варианты.\n"},
		{"one option", task.Decision{Question: "Как?", Options: two[:1], Answer: "А"}, lines(
			"Вариантов меньше двух.",
			"",
			msg.Text(msg.HintOptionsTooFew),
		), ""},
		{"two recommended", task.Decision{Question: "Как?", Options: []state.Option{{Label: "А", Recommended: true}, {Label: "Б", Recommended: true}}, Answer: "А"}, lines(
			"Рекомендовано больше одного варианта.",
			"",
			msg.Text(msg.HintOptionsRecommended),
		), ""},
		{"no label", task.Decision{Question: "Как?", Options: []state.Option{{Label: "А"}, {Description: "Б"}}, Answer: "А"}, lines(
			"Не указано название варианта 2.",
			"",
			msg.Text(msg.HintOptionLabel),
		), ""},
		{"label of two lines", task.Decision{Question: "Как?", Options: []state.Option{{Label: "А\nБ"}, {Label: "В"}}, Answer: "А"}, lines(
			"Название варианта 1 состоит из нескольких строк.",
			"",
			msg.Text(msg.HintOptionLabel),
		), ""},
		{"long label", task.Decision{Question: "Как?", Options: []state.Option{{Label: "А"}, {Label: long}}, Answer: "А"}, lines(
			"Название варианта 2 длиннее 120 знаков.",
			"",
			msg.Text(msg.HintOptionLabel),
		), ""},
	}
	for _, tt := range tests {
		f := checkDecision("operator record", tt.req)
		if f == nil {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, *f, tt.cli, tt.agent)
	}
}

// TestReadOptionsText checks the refusals of the options of an answer: the
// words of the refusal of --input are those of TestReadFieldsText.
func TestReadOptionsText(t *testing.T) {
	tests := []struct {
		name, options, cause string
	}{
		{"options not an array", `"А, Б"`, "поле «options» должно быть массивом объектов"},
		{"unknown field of an option", `[{"label": "А"}, {"label": "Б", "weight": 2}]`,
			"вариант 2 должен быть объектом с полями label, description и recommended"},
	}
	for _, tt := range tests {
		_, f := readOptions("operator record", "-", json.RawMessage(tt.options))
		if f == nil {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, *f, lines(
			msg.Text(msg.ErrInputInvalid, tt.cause),
			"",
			hintLineOf(msg.HintCommandHelp, "gentry operator record --help"),
		), msg.Text(msg.ErrToolFields, tt.cause)+"\n")
	}
}
