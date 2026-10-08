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
		text := Skill(name, "/opt/gentry")
		if strings.Contains(text, "gentry:") {
			t.Errorf("%s names a skill the way of a tool:\n%s", name, text)
		}
		for _, m := range skillRef.FindAllStringSubmatch(text, -1) {
			if !known[m[1]] {
				t.Errorf("%s refers to an unknown skill %s", name, m[1])
			}
		}
		if strings.Contains(text, "<gentry>") {
			t.Errorf("%s keeps the place of the program", name)
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
	for _, s := range []string{"gentry:", "<gentry>", "<skill:", SkillDir} {
		if strings.Contains(text, s) {
			t.Errorf("the guide contains %q", s)
		}
	}
}
