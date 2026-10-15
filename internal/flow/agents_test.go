package flow

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	// The project subagents are files of the flow; only the shared ones the
	// stages name are copied.
	if res.Snapshot.Files["agents/auditor.md"] != auditor["agents/auditor.md"] {
		t.Errorf("snapshot files: %v", res.Snapshot.Files)
	}
	if want := (map[string]string{
		"reviewer.yaml": "purpose: ревью изменений задачи — поведение и текст\ncapabilities: [read, search]\n",
		"reviewer.md":   "Проверить изменения задачи: поведение, тесты и тексты для оператора.\n",
	}); !reflect.DeepEqual(res.Snapshot.Shared, want) {
		t.Errorf("snapshot shared: %v, want %v", res.Snapshot.Shared, want)
	}

	// A subagent of the project replaces the shared one of the same name.
	res = readShop(t, shop(t, with(auditor, map[string]string{
		"agents/reviewer.yaml": "purpose: ревью проекта\ncapabilities: []\n",
		"agents/reviewer.md":   "Ревью по правилам проекта.\n",
	})))
	if r, _ := res.Flow.Agent("reviewer"); r.Shared || r.Purpose != "ревью проекта" || len(res.Snapshot.Shared) != 0 {
		t.Errorf("reviewer: %+v, shared %v", r, res.Snapshot.Shared)
	}

	// A subagent no stage names is part of the flow too; an unused shared one
	// is not.
	if err := os.WriteFile(filepath.Join(p.Shared, "linter.yaml"), []byte("purpose: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = readShop(t, shop(t, with(auditor, map[string]string{"stages/security.yaml": "title: Безопасность\nexit: пройдена\nexecutor: orchestrator\n"})))
	if _, ok := res.Flow.Agent("auditor"); !ok || len(res.Flow.Agents) != 2 {
		t.Errorf("agents: %+v", res.Flow.Agents)
	}
}

// TestStageAgents checks the field agents of a stage: the subagents in the
// order of the field, an empty list and null as no field, a shared subagent
// it names in the flow and its snapshot, as an executor is, and the stages
// that name a subagent in one list.
func TestStageAgents(t *testing.T) {
	p := shop(t, with(auditor, map[string]string{
		"stages/plan-feature.yaml":   "title: План фичи\nexit: план согласован\nexecutor: orchestrator\nagents: [scout, reviewer]\n",
		"stages/plan-bug.yaml":       "title: План бага\nexit: план согласован\nexecutor: operator\nagents: [auditor]\n",
		"stages/implementation.yaml": "title: Реализация\nexit: тесты проходят\nexecutor: orchestrator\nagents: []\n",
		"stages/review.yaml":         "title: Ревью\nexit: замечания разобраны\nexecutor: reviewer\nagents: []\n",
		"stages/merge.yaml":          "title: Слияние\nexit: ветка влита\nexecutor: operator\nagents:\n",
	}))
	scout := map[string]string{"scout.yaml": "purpose: разведка кода\ncapabilities: [read, search]\n", "scout.md": "Найти места изменения.\n"}
	for name, text := range scout {
		if err := os.WriteFile(filepath.Join(p.Shared, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := readShop(t, p)
	if len(res.Problems) > 0 {
		t.Fatal(dump(res.Problems))
	}
	fl := res.Flow
	for id, want := range map[string][]string{
		"plan-feature": {"scout", "reviewer"}, "plan-bug": {"auditor"}, "implementation": {}, "review": {}, "merge": {}, "branch": {},
	} {
		if st, _ := fl.Stage(id); !slices.Equal(st.Agents, want) {
			t.Errorf("%s: agents %q, want %q", id, st.Agents, want)
		}
	}
	if a, ok := fl.Agent("scout"); !ok || !a.Shared || a.Purpose != "разведка кода" {
		t.Errorf("scout: %+v, %v", a, ok)
	}
	if res.Snapshot.Shared["scout.yaml"] != scout["scout.yaml"] || res.Snapshot.Shared["scout.md"] != scout["scout.md"] {
		t.Errorf("snapshot shared: %v", res.Snapshot.Shared)
	}
	for id, want := range map[string][]string{"reviewer": {"plan-feature", "review"}, "scout": {"plan-feature"}, "auditor": {"plan-bug", "security"}} {
		if got := fl.StagesBy(id); !slices.Equal(got, want) {
			t.Errorf("stages by %s: %q, want %q", id, got, want)
		}
	}
	st, _ := fl.Stage("plan-feature")
	var ids []string
	for _, a := range fl.OnlyStage(st).Agents {
		ids = append(ids, a.ID)
	}
	if want := []string{"reviewer", "scout"}; !slices.Equal(ids, want) {
		t.Errorf("agents of the flow of plan-feature: %q, want %q", ids, want)
	}

	// A subagent of the project replaces the shared one of the same name in
	// the field too.
	for name, text := range map[string]string{"scout.yaml": "purpose: разведка магазина\ncapabilities: [read]\n", "scout.md": "Найти места.\n"} {
		if err := os.WriteFile(filepath.Join(p.Dir, "agents", name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res = readShop(t, p)
	if a, _ := res.Flow.Agent("scout"); a.Shared || a.Purpose != "разведка магазина" || res.Snapshot.Shared["scout.yaml"] != "" {
		t.Errorf("scout: %+v, shared %v", a, res.Snapshot.Shared)
	}
}

func TestAgentProblems(t *testing.T) {
	tests := []struct {
		name    string
		changes map[string]string
		shared  map[string]string
		want    []Problem
	}{
		{"fields", map[string]string{
			"agents/auditor.yaml": "capabilities: read\ntier: x\n",
		}, nil, []Problem{
			{contract.ProblemMissingField, "agents/auditor.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjAgent, "auditor"), "purpose")},
			{contract.ProblemInvalidValue, "agents/auditor.yaml", 1, msg.Text(msg.ProblemCapabilities, msg.Text(msg.FlowObjAgent, "auditor"), "read, search, edit, run, web, task, progress")},
			{contract.ProblemUnknownField, "agents/auditor.yaml", 2, msg.Text(msg.ProblemUnknownField, msg.Text(msg.FlowObjAgent, "auditor"), "tier")},
		}},
		// The model is a level the adapter turns into a model of the AI
		// tool, not a model of the tool.
		{"unknown model", map[string]string{
			"agents/auditor.yaml": "purpose: x\ncapabilities: [read]\nmodel: opus\n",
		}, nil, []Problem{
			{contract.ProblemInvalidValue, "agents/auditor.yaml", 3, msg.Text(msg.ProblemModel, msg.Text(msg.FlowObjAgent, "auditor"), "light, standard, strong")},
		}},
		{"model not a level", map[string]string{
			"agents/auditor.yaml":  "purpose: x\ncapabilities: [read]\nmodel: [strong]\n",
			"agents/reviewer.yaml": "purpose: x\ncapabilities: [read]\nmodel: 1\n",
			"agents/reviewer.md":   "x",
			"agents/tester.yaml":   "purpose: x\ncapabilities: [read]\nmodel: \"\"\n",
			"agents/tester.md":     "x",
		}, nil, []Problem{
			{contract.ProblemInvalidValue, "agents/auditor.yaml", 3, msg.Text(msg.ProblemModel, msg.Text(msg.FlowObjAgent, "auditor"), "light, standard, strong")},
			{contract.ProblemInvalidValue, "agents/reviewer.yaml", 3, msg.Text(msg.ProblemModel, msg.Text(msg.FlowObjAgent, "reviewer"), "light, standard, strong")},
			{contract.ProblemInvalidValue, "agents/tester.yaml", 3, msg.Text(msg.ProblemModel, msg.Text(msg.FlowObjAgent, "tester"), "light, standard, strong")},
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
		// A shared subagent has no file in the flow: its problems name it and
		// come before files that do not belong to the flow.
		{"shared", map[string]string{"README.md": "x"}, map[string]string{
			"reviewer.yaml": "purpose: x\ncapabilities: [all]\nmodel: huge\n",
			"reviewer.md":   "",
		}, []Problem{
			{contract.ProblemInvalidValue, "", 0, msg.Text(msg.ProblemCapabilities, msg.Text(msg.FlowObjSharedAgent, "reviewer"), "read, search, edit, run, web, task, progress")},
			{contract.ProblemInvalidValue, "", 0, msg.Text(msg.ProblemModel, msg.Text(msg.FlowObjSharedAgent, "reviewer"), "light, standard, strong")},
			{contract.ProblemMissingInstruction, "", 0, msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjSharedAgent, "reviewer"))},
			{contract.ProblemExtraFile, "README.md", 0, msg.Text(msg.ProblemExtraFile, "README.md")},
		}},
		{"shared without fields", nil, map[string]string{"reviewer.yaml": ""}, []Problem{
			{contract.ProblemUnknownExecutor, "stages/review.yaml", 3, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewer")},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := shop(t, with(auditor, tt.changes))
			for name, text := range tt.shared {
				path := filepath.Join(p.Shared, name)
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

// TestAgentModel checks that a subagent of the project and one of the shared
// subagents may name the model they run on, and that an empty field names none.
func TestAgentModel(t *testing.T) {
	p := shop(t, with(auditor, map[string]string{
		"agents/auditor.yaml": "purpose: проверка безопасности изменений\ncapabilities: [read, search, run]\nmodel: strong\n",
	}))
	if err := os.WriteFile(filepath.Join(p.Shared, "reviewer.yaml"), []byte("purpose: ревью\ncapabilities: [read]\nmodel: light\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := readShop(t, p)
	if len(res.Problems) > 0 {
		t.Fatal(dump(res.Problems))
	}
	if a, _ := res.Flow.Agent("auditor"); a.Model != "strong" {
		t.Errorf("auditor: model %q", a.Model)
	}
	if a, _ := res.Flow.Agent("reviewer"); !a.Shared || a.Model != "light" {
		t.Errorf("reviewer: %+v", a)
	}
	res = readShop(t, shop(t, with(auditor, map[string]string{
		"agents/auditor.yaml": "purpose: проверка безопасности изменений\ncapabilities: [read]\nmodel:\n",
	})))
	if a, _ := res.Flow.Agent("auditor"); len(res.Problems) > 0 || a.Model != "" {
		t.Errorf("auditor with an empty model: %+v, problems %s", a, dump(res.Problems))
	}
}
