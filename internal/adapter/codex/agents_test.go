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
	can := msg.Text(msg.AgentCodexCan, strings.Join([]string{msg.Text(msg.CapabilityRead), msg.Text(msg.CapabilitySearch), msg.Text(msg.CapabilityTask)}, ", "))
	cannot := msg.Text(msg.AgentCodexCannot, strings.Join([]string{msg.Text(msg.CapabilityEdit), msg.Text(msg.CapabilityRun), msg.Text(msg.CapabilityWeb), msg.Text(msg.CapabilityNoProgress)}, ", "))
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
// what it does not.
func TestAgentCapabilities(t *testing.T) {
	text := func(ks ...msg.Key) string {
		var parts []string
		for _, k := range ks {
			parts = append(parts, msg.Text(k))
		}
		return strings.Join(parts, ", ")
	}
	for _, tt := range []struct {
		caps []string
		want string
	}{
		{flow.Capabilities, msg.Text(msg.AgentCodexCan, text(msg.CapabilityRead, msg.CapabilitySearch, msg.CapabilityEdit, msg.CapabilityRun, msg.CapabilityWeb, msg.CapabilityProgress)) + "\n"},
		{nil, msg.Text(msg.AgentCodexCannot, text(msg.CapabilityRead, msg.CapabilitySearch, msg.CapabilityEdit, msg.CapabilityRun, msg.CapabilityWeb, msg.CapabilityNoGentry)) + "\n"},
		{[]string{"run", "progress"}, msg.Text(msg.AgentCodexCan, text(msg.CapabilityRun, msg.CapabilityProgress)) + "\n" +
			msg.Text(msg.AgentCodexCannot, text(msg.CapabilityRead, msg.CapabilitySearch, msg.CapabilityEdit, msg.CapabilityWeb)) + "\n"},
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
