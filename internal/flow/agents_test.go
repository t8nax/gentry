package flow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// auditor is a valid subagent of the project and a stage it carries out.
var auditor = map[string]string{
	"agents/auditor.yaml":  "purpose: проверка безопасности изменений\ncapabilities: [read, search, run]\n",
	"agents/auditor.md":    "Проверить изменения на уязвимости.\n",
	"stages/security.yaml": "title: Безопасность\nexit: проверка безопасности пройдена\nexecutor: auditor\n",
	"stages/security.md":   "Проверить безопасность.\n",
}

// with returns the changes of base and more together.
func with(base map[string]string, more map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

func TestProjectAgents(t *testing.T) {
	p := shop(t, auditor)
	res := readShop(t, p)
	if len(res.Problems) > 0 {
		t.Fatal(dump(res.Problems))
	}
	a, ok := res.Flow.Agent("auditor")
	want := Agent{ID: "auditor", Purpose: "проверка безопасности изменений", Capabilities: []string{"read", "search", "run"},
		Instruction: "Проверить изменения на уязвимости.\n"}
	if !ok || !reflect.DeepEqual(a, want) {
		t.Errorf("auditor: %+v, want %+v", a, want)
	}
	// The project subagents are files of the flow; only the library ones the
	// stages name are copied.
	if res.Snapshot.Files["agents/auditor.md"] != auditor["agents/auditor.md"] {
		t.Errorf("snapshot files: %v", res.Snapshot.Files)
	}
	if want := (map[string]string{
		"reviewer.yaml": "purpose: ревью изменений задачи — поведение и текст\ncapabilities: [read, search]\n",
		"reviewer.md":   "Проверить изменения задачи: поведение, тесты и тексты для оператора.\n",
	}); !reflect.DeepEqual(res.Snapshot.Library, want) {
		t.Errorf("snapshot library: %v, want %v", res.Snapshot.Library, want)
	}

	// A subagent of the project replaces the library one of the same name.
	res = readShop(t, shop(t, with(auditor, map[string]string{
		"agents/reviewer.yaml": "purpose: ревью проекта\ncapabilities: []\n",
		"agents/reviewer.md":   "Ревью по правилам проекта.\n",
	})))
	if r, _ := res.Flow.Agent("reviewer"); r.Library || r.Purpose != "ревью проекта" || len(res.Snapshot.Library) != 0 {
		t.Errorf("reviewer: %+v, library %v", r, res.Snapshot.Library)
	}

	// A subagent no stage names is part of the flow too; an unused library one
	// is not.
	if err := os.WriteFile(filepath.Join(p.Library, "linter.yaml"), []byte("purpose: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = readShop(t, shop(t, with(auditor, map[string]string{"stages/security.yaml": "title: Безопасность\nexit: пройдена\nexecutor: orchestrator\n"})))
	if _, ok := res.Flow.Agent("auditor"); !ok || len(res.Flow.Agents) != 2 {
		t.Errorf("agents: %+v", res.Flow.Agents)
	}
}

func TestAgentProblems(t *testing.T) {
	tests := []struct {
		name    string
		changes map[string]string
		library map[string]string
		want    []Problem
	}{
		{"fields", map[string]string{
			"agents/auditor.yaml": "capabilities: read\nmodel: x\n",
		}, nil, []Problem{
			{contract.ProblemMissingField, "agents/auditor.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjAgent, "auditor"), "purpose")},
			{contract.ProblemInvalidValue, "agents/auditor.yaml", 1, msg.Text(msg.ProblemCapabilities, msg.Text(msg.FlowObjAgent, "auditor"), "read, search, edit, run, web, task, progress")},
			{contract.ProblemUnknownField, "agents/auditor.yaml", 2, msg.Text(msg.ProblemUnknownField, msg.Text(msg.FlowObjAgent, "auditor"), "model")},
		}},
		{"unknown capability", map[string]string{
			"agents/auditor.yaml": "purpose: x\ncapabilities: [read, write]\n",
		}, nil, []Problem{
			{contract.ProblemInvalidValue, "agents/auditor.yaml", 2, msg.Text(msg.ProblemCapabilities, msg.Text(msg.FlowObjAgent, "auditor"), "read, search, edit, run, web, task, progress")},
		}},
		{"no capabilities", map[string]string{
			"agents/auditor.yaml": "purpose: x\n",
		}, nil, []Problem{
			{contract.ProblemMissingField, "agents/auditor.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjAgent, "auditor"), "capabilities")},
		}},
		{"instructions", map[string]string{
			"agents/auditor.md": "",
			"agents/old.md":     "x",
		}, nil, []Problem{
			{contract.ProblemMissingInstruction, "agents/auditor.yaml", 0, msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjAgent, "auditor"))},
			{contract.ProblemOrphanInstruction, "agents/old.md", 0, msg.Text(msg.ProblemOrphanAgentInstruction, msg.Text(msg.FlowObjAgent, "old"))},
		}},
		{"invalid id", map[string]string{"agents/Auditor_2.yaml": "x"}, nil, []Problem{
			{contract.ProblemInvalidID, "agents/Auditor_2.yaml", 0, msg.Text(msg.ProblemInvalidID, msg.Text(msg.FlowObjAgent, "Auditor_2"))},
		}},
		// A library subagent has no file in the flow: its problems name it and
		// come before files that do not belong to the flow.
		{"library", map[string]string{"README.md": "x"}, map[string]string{
			"reviewer.yaml": "purpose: x\ncapabilities: [all]\n",
			"reviewer.md":   "",
		}, []Problem{
			{contract.ProblemInvalidValue, "", 0, msg.Text(msg.ProblemCapabilities, msg.Text(msg.FlowObjLibraryAgent, "reviewer"), "read, search, edit, run, web, task, progress")},
			{contract.ProblemMissingInstruction, "", 0, msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjLibraryAgent, "reviewer"))},
			{contract.ProblemExtraFile, "README.md", 0, msg.Text(msg.ProblemExtraFile, "README.md")},
		}},
		{"library without fields", nil, map[string]string{"reviewer.yaml": ""}, []Problem{
			{contract.ProblemUnknownExecutor, "stages/review.yaml", 3, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewer")},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := shop(t, with(auditor, tt.changes))
			for name, text := range tt.library {
				path := filepath.Join(p.Library, name)
				var err error
				if text == "" {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte(text), 0o644)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if got := problems(t, p); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("problems:\n%s\nwant:\n%s", dump(got), dump(tt.want))
			}
		})
	}
}

// TestAgentGentryCapabilities checks that a subagent may see the task and
// lead its progress.
func TestAgentGentryCapabilities(t *testing.T) {
	res := readShop(t, shop(t, with(auditor, map[string]string{
		"agents/auditor.yaml": "purpose: код по плану задачи\ncapabilities: [read, edit, task, progress]\n",
	})))
	if len(res.Problems) > 0 {
		t.Fatal(dump(res.Problems))
	}
	if a, _ := res.Flow.Agent("auditor"); !reflect.DeepEqual(a.Capabilities, []string{"read", "edit", "task", "progress"}) {
		t.Errorf("capabilities %v", a.Capabilities)
	}
}
