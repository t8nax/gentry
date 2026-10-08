package flow

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/agenttext"
)

// guideExamples returns the examples of files in the guide to the format of
// the flow: blocks indented by four spaces whose first line names the file,
// by its path inside the flow.
func guideExamples(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	var name string
	var body []string
	flush := func() {
		if name != "" {
			out[name] = strings.Join(body, "\n") + "\n"
		}
		name, body = "", nil
	}
	for _, line := range strings.Split(agenttext.FlowGuide(), "\n") {
		code, ok := strings.CutPrefix(line, "    ")
		switch {
		case !ok && strings.TrimSpace(line) != "":
			flush()
		case !ok:
		case strings.HasPrefix(code, "# ") && strings.Contains(code, "/"):
			flush()
			name = strings.TrimPrefix(code, "# ")
		case strings.HasPrefix(code, "<!-- ") && strings.HasSuffix(code, " -->"):
			flush()
			name = strings.TrimSuffix(strings.TrimPrefix(code, "<!-- "), " -->")
		case name != "":
			body = append(body, code)
		}
	}
	flush()
	return out
}

// TestGuideExamples checks that the examples of the guide make a flow that
// passes the checks, once the stages and the part they name are there.
func TestGuideExamples(t *testing.T) {
	files := guideExamples(t)
	for _, want := range []string{"scenarios/feature.yaml", "stages/review.yaml", "stages/review.md", "agents/reviewer.yaml"} {
		if files[want] == "" {
			t.Fatalf("the guide has no example of %s: %v", want, files)
		}
	}
	files["agents/reviewer.md"] = "Проверить изменения задачи.\n"
	files["parts/review-checklist.md"] = "Поведение, тесты, тексты.\n"
	for _, st := range []string{"branch", "plan-feature", "implementation", "merge"} {
		files["stages/"+st+".yaml"] = "title: " + st + "\nexit: этап пройден\nexecutor: orchestrator\n"
		files["stages/"+st+".md"] = "Пройти этап.\n"
	}
	res, err := Read(files, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) > 0 {
		t.Errorf("the examples of the guide have problems: %v", res.Problems)
	}
}

// TestGuideFields checks that the guide names every field the flow is read
// with, every executor and capability, and the fields of later versions.
func TestGuideFields(t *testing.T) {
	guide := agenttext.FlowGuide()
	words := []string{
		"title", "start", "nodes", "stage", "next", "to", "if", "max_rounds", "exit", "executor", "include",
		"purpose", "capabilities", Finish, Orchestrator, Operator, commonFile,
		scenariosDir + "/", stagesDir + "/", partsDir + "/", agentsDir + "/",
	}
	words = append(words, Capabilities...)
	words = append(words, laterTaskFields...)
	words = append(words, laterStageFields...)
	for _, w := range words {
		if !strings.Contains(guide, w) {
			t.Errorf("the guide does not name %s", w)
		}
	}
}

// TestGuideContents checks that the list of sections at the top of the guide
// names its sections in order: the agent reads a part of a long file by it.
func TestGuideContents(t *testing.T) {
	var sections []string
	var contents string
	for _, line := range strings.Split(agenttext.FlowGuide(), "\n") {
		if s, ok := strings.CutPrefix(line, "## "); ok {
			sections = append(sections, s)
		}
		if s, ok := strings.CutPrefix(line, "Разделы: "); ok && contents == "" {
			contents = s
		}
	}
	if want := strings.Join(sections, " · ") + "."; contents != want {
		t.Errorf("contents %q, want %q", contents, want)
	}
}
