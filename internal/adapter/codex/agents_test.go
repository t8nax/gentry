package codex

import (
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// TestAgentFile checks the file of a subagent: TOML with the mark first, the
// name, the description, the model and the instruction with the lines of
// the capabilities, the special characters kept.
func TestAgentFile(t *testing.T) {
	a := flow.Agent{
		ID:           "reviewer",
		Purpose:      `ревью "заказа" и C:\shop`,
		Capabilities: []string{"read", "search", "task"},
		Model:        "strong",
		Instruction:  "Первый \"\"\" и ''' абзац.\n\nВторой: \\n.",
	}
	b := Agents{}.File(a)
	if !strings.HasPrefix(string(b), agents.MarkPrefix+" "+msg.Text(msg.AgentFileMark)+"\n") || !(Agents{}).Marked(b) {
		t.Errorf("the file has no mark:\n%s", b)
	}
	d, err := parseTOML(string(b))
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	// The lines of the capabilities in the words of the catalog: the test of
	// the output of the file states them.
	can := "Возможности: читать файлы, искать по коду, смотреть задачу инструментами Gentry."
	cannot := "Не делать: править файлы, запускать команды в оболочке, обращаться к сети, вести ход работы задачи (шаги, заметки, артефакты)."
	want := map[string]any{
		"name":                   "reviewer",
		"description":            a.Purpose,
		"model":                  "gpt-6-astra",
		"developer_instructions": can + "\n" + cannot + "\n\n" + a.Instruction + "\n",
	}
	for k, v := range want {
		if d.root[k] != v {
			t.Errorf("%s = %q, want %q", k, d.root[k], v)
		}
	}
	if len(d.root) != len(want) {
		t.Errorf("keys: %v", d.root)
	}
	if (Agents{}).Marked([]byte("name = \"reviewer\"\n")) {
		t.Error("a file without the mark is marked")
	}
	if id, ok := (Agents{}).ID("reviewer.toml"); !ok || id != "reviewer" {
		t.Errorf("ID = %q %v", id, ok)
	}
}

// TestAgentCapabilities checks the lines of what a subagent may do and of
// what it does not: all the capabilities, none, and progress without task.
func TestAgentCapabilities(t *testing.T) {
	for _, tt := range []struct {
		caps []string
		want string
	}{
		{flow.Capabilities, "Возможности: читать файлы, искать по коду, править файлы, запускать команды в оболочке, обращаться к сети, смотреть задачу и вести ход работы инструментами Gentry (шаги, заметки, артефакты).\n"},
		{nil, "Не делать: читать файлы, искать по коду, править файлы, запускать команды в оболочке, обращаться к сети, вызывать инструменты Gentry.\n"},
		{[]string{"run", "progress"}, "Возможности: запускать команды в оболочке, смотреть задачу и вести ход работы инструментами Gentry (шаги, заметки, артефакты).\n" +
			"Не делать: читать файлы, искать по коду, править файлы, обращаться к сети.\n"},
	} {
		got := instruction(flow.Agent{Capabilities: tt.caps, Instruction: "Сделать."})
		if want := tt.want + "\nСделать.\n"; got != want {
			t.Errorf("%v: %q, want %q", tt.caps, got, want)
		}
	}
}

// TestModels checks that the table of models has each model level of the
// flow, and only those, and that a subagent without a level has no model.
func TestModels(t *testing.T) {
	var levels []string
	for l := range models {
		levels = append(levels, l)
	}
	slices.Sort(levels)
	want := slices.Clone(flow.Models)
	slices.Sort(want)
	if !slices.Equal(levels, want) {
		t.Errorf("the levels of the table: %v, want those of flow.Models %v", levels, want)
	}
	if strings.Contains(string(Agents{}.File(flow.Agent{ID: "a", Instruction: "x"})), "model") {
		t.Error("a subagent without a level has a model")
	}
}
