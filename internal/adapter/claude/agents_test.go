package claude

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/flow"
)

func TestAgentFile(t *testing.T) {
	a := flow.Agent{ID: "reviewer", Purpose: `ревью "корзины"`, Capabilities: []string{"read", "search", "run", "read"}, Instruction: "Проверить корзину.\n\n"}
	want := "---\n" +
		"# Gentry: файл разложен из флоу проекта и перезаписывается при раскладке субагентов.\n" +
		"name: reviewer\n" +
		"description: \"ревью \\\"корзины\\\"\"\n" +
		"tools: Read, Grep, Glob, Bash, PowerShell\n" +
		"---\n\nПроверить корзину.\n"
	if got := string(Agents{}.File(a)); got != want {
		t.Errorf("File =\n%s\nwant\n%s", got, want)
	}
	a.Capabilities = []string{"read", "task", "progress"}
	if got := string(Agents{}.File(a)); !strings.Contains(got, "\ntools: Read, mcp__plugin_gentry_core__stage_show, mcp__plugin_gentry_core__task_show, mcp__plugin_gentry_core__note_list, "+
		"mcp__plugin_gentry_core__step_add, mcp__plugin_gentry_core__step_done, mcp__plugin_gentry_core__step_drop, mcp__plugin_gentry_core__note_add, mcp__plugin_gentry_core__artifact_save\n") {
		t.Errorf("task and progress: %q", got)
	}
	a.Capabilities = []string{}
	if got := string(Agents{}.File(a)); !strings.Contains(got, "\ntools: []\n") {
		t.Errorf("no capabilities: %q", got)
	}
}

func TestAgentMarked(t *testing.T) {
	l := Agents{}
	for _, c := range []struct {
		content string
		want    bool
	}{
		{string(l.File(flow.Agent{ID: "tester"})), true},
		{"---\r\n# Gentry: перенесён\r\nname: tester\r\n---\r\n", true},
		{"---\nname: tester\n# Gentry: ниже\n---\n", false},
		{"# Gentry: без заголовка\n", false},
	} {
		if got := l.Marked([]byte(c.content)); got != c.want {
			t.Errorf("Marked(%q) = %v, want %v", c.content, got, c.want)
		}
	}
	if id, ok := l.ID("reviewer.md"); !ok || id != "reviewer" {
		t.Errorf("ID(reviewer.md) = %q, %v", id, ok)
	}
	if _, ok := l.ID("notes.txt"); ok {
		t.Error("ID(notes.txt) is a subagent")
	}
}
