package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// TestReadFieldsText checks the refusals of the fields of --input: the agent
// gets them as the fields of its tool.
func TestReadFieldsText(t *testing.T) {
	fields := []field{{name: "title"}, {name: "step", kind: numberField}, {name: "steps", kind: listField}}
	tests := []struct {
		name, input string
		cli, agent  string
	}{
		{"no file", "", lines(
			"Не удалось прочитать поля из --input: файл не найден.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: файл не найден.\n"},
		{"not an object", `["feature"]`, lines(
			"Не удалось прочитать поля из --input: текст не является объектом JSON.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: текст не является объектом JSON.\n"},
		{"more than an object", `{} {}`, lines(
			"Не удалось прочитать поля из --input: текст не является объектом JSON.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: текст не является объектом JSON.\n"},
		{"unknown field", `{"kind":"x"}`, lines(
			"Не удалось прочитать поля из --input: неизвестное поле «kind».",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: неизвестное поле «kind».\n"},
		{"not a string", `{"title":5}`, lines(
			"Не удалось прочитать поля из --input: поле «title» должно быть строкой.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: поле «title» должно быть строкой.\n"},
		{"not an integer", `{"step":"1"}`, lines(
			"Не удалось прочитать поля из --input: поле «step» должно быть целым числом.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: поле «step» должно быть целым числом.\n"},
		{"not a list", `{"steps":"Шаг"}`, lines(
			"Не удалось прочитать поля из --input: поле «steps» должно быть массивом строк.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не удалось прочитать поля инструмента: поле «steps» должно быть массивом строк.\n"},
	}
	for _, tt := range tests {
		value := "-"
		if tt.input == "" {
			value = filepath.Join(t.TempDir(), "none.json")
		}
		_, f := readFields("step add", value, strings.NewReader(tt.input), fields)
		if f == nil {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, *f, tt.cli, tt.agent)
	}

	input := &stringFlag{Value: "-", Set: true}
	_, f := commandFields("step add", input, nil, []string{"Шаг"}, strings.NewReader(`{}`), fields)
	if f == nil {
		t.Fatal("arguments with --input: no refusal")
	}
	wantFail(t, "arguments with --input", *f, lines(
		"Аргументы команды и --input нельзя указывать вместе.",
		"",
		hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
	), "Аргументы команды и --input нельзя указывать вместе.\n")
}
