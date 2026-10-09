package agenttext

import (
	"strings"
	"testing"
)

// TestNeutral checks that the texts name skills only by neutral references
// to known skills: each tool names skills its own way.
func TestNeutral(t *testing.T) {
	known := map[string]bool{WorkingOnTask: true, CancelingTask: true, EditingFlow: true}
	for name := range known {
		text := Skill(name)
		if strings.Contains(text, "gentry:") {
			t.Errorf("%s names a skill the way of a tool:\n%s", name, text)
		}
		for _, m := range skillRef.FindAllStringSubmatch(text, -1) {
			if !known[m[1]] {
				t.Errorf("%s refers to an unknown skill %s", name, m[1])
			}
		}
		if s := commandOf(text); s != "" {
			t.Errorf("%s names a command of the program, not a tool: %s", name, s)
		}
		if strings.Contains(text, "${") {
			t.Errorf("%s names the directory of the skill the way of a tool", name)
		}
	}
}

func TestResolveSkills(t *testing.T) {
	got := ResolveSkills("по скиллу `"+SkillRef(CancelingTask)+"`.", func(n string) string { return "x:" + n })
	if want := "по скиллу `x:canceling-task`."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFlowGuideNeutral checks that the guide names neither a tool nor a place
// to fill: it lies beside the skill as it is.
func TestFlowGuideNeutral(t *testing.T) {
	text := FlowGuide()
	if s := commandOf(text); s != "" {
		t.Errorf("the guide names a command of the program, not a tool: %s", s)
	}
	for _, s := range []string{"gentry:", "<gentry>", "<skill:", SkillDir} {
		if strings.Contains(text, s) {
			t.Errorf("the guide contains %q", s)
		}
	}
}

// commandOf returns what in text names the program instead of a tool: a
// command of gentry, a flag of --input or a help; empty if nothing.
func commandOf(text string) string {
	for _, s := range []string{"gentry project", "gentry worktree", "gentry flow", "gentry library", "gentry process", "gentry task", "gentry stage", "gentry step", "gentry note", "gentry artifact", "gentry operator", "gentry setup", "--input", "--help"} {
		if strings.Contains(text, s) {
			return s
		}
	}
	return ""
}
