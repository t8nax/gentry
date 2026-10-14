package flow

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// shop copies the process of testdata — the flow of the shop and the
// shared subagents — into a temporary directory, applies changes and
// returns the places of the flow of the shop. A change is the new text of a
// file by its path inside the flow; an empty text removes the file or
// directory.
func shop(t *testing.T, changes map[string]string) Places {
	t.Helper()
	process := t.TempDir()
	if err := os.CopyFS(process, os.DirFS("testdata/process")); err != nil {
		t.Fatal(err)
	}
	p := Places{Project: "shop", Dir: filepath.Join(process, "shop", "flow"), Shared: filepath.Join(process, "agents")}
	for rel, text := range changes {
		full := filepath.Join(p.Dir, filepath.FromSlash(rel))
		if text == "" {
			if err := os.RemoveAll(full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// readShop reads the flow of p.
func readShop(t *testing.T, p Places) *Result {
	t.Helper()
	res, err := ReadDir(p.Dir, p.Shared)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// load reads the flow of p, which must have no problems.
func load(t *testing.T, p Places) *Flow {
	t.Helper()
	res := readShop(t, p)
	if len(res.Problems) > 0 {
		t.Fatalf("problems:\n%s", dump(res.Problems))
	}
	return res.Flow
}

// problems reads the flow of p and returns its problems.
func problems(t *testing.T, p Places) []Problem {
	t.Helper()
	res := readShop(t, p)
	if len(res.Problems) == 0 || res.Flow != nil {
		t.Fatalf("no problems, flow %+v", res.Flow)
	}
	return res.Problems
}

func TestLoadShop(t *testing.T) {
	f := load(t, shop(t, nil))
	var ids []string
	for _, s := range f.Scenarios {
		ids = append(ids, s.ID)
	}
	if want := []string{"bug", "feature"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("scenarios %v, want %v", ids, want)
	}
	feature, ok := f.Scenario("feature")
	want := Scenario{ID: "feature", Title: "Фича", Start: "branch", Nodes: []Node{
		{ID: "branch", Stage: "branch", Next: []Transition{{To: "plan"}}},
		{ID: "plan", Stage: "plan-feature", Next: []Transition{{To: "implementation"}}},
		{ID: "implementation", Stage: "implementation", Next: []Transition{{To: "review"}}},
		{ID: "review", Stage: "review", Next: []Transition{
			{To: "implementation", If: "ревью выявило существенные замечания", MaxRounds: 3},
			{To: "merge"},
		}},
		{ID: "merge", Stage: "merge", Next: []Transition{{To: Finish}}},
	}}
	if !ok || !reflect.DeepEqual(feature, want) {
		t.Errorf("feature:\n%+v\nwant\n%+v", feature, want)
	}
	ids = nil
	for _, s := range f.Stages {
		ids = append(ids, s.ID)
	}
	if want := []string{"branch", "implementation", "merge", "plan-bug", "plan-feature", "review"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("stages %v, want %v", ids, want)
	}
	review, _ := f.Stage("review")
	if want := (Stage{ID: "review", Title: "Ревью", Exit: "замечания ревью записаны и разобраны", Executor: "reviewer",
		Include: []string{"review-checklist"}, Agents: []string{}, Instruction: "Проверить изменения задачи по списку.\n"}); !reflect.DeepEqual(review, want) {
		t.Errorf("review: %+v, want %+v", review, want)
	}
	if merge, _ := f.Stage("merge"); merge.Executor != Operator || merge.Include == nil {
		t.Errorf("merge: %+v; want the operator and an empty include", merge)
	}
	if p, _ := f.Part("review-checklist"); len(f.Parts) != 2 || p.Text != "## Список ревью\n- поведение\n- тесты\n- тексты\n" {
		t.Errorf("parts %+v", f.Parts)
	}
	// The shared subagent a stage names is part of the flow.
	if want := []Agent{{ID: "reviewer", Shared: true, Purpose: "ревью изменений задачи — поведение и текст",
		Capabilities: []string{"read", "search"}, Instruction: "Проверить изменения задачи: поведение, тесты и тексты для оператора.\n"}}; !reflect.DeepEqual(f.Agents, want) {
		t.Errorf("agents %+v, want %+v", f.Agents, want)
	}
	if got, want := feature.DefaultPath(), []string{"branch", "plan", "implementation", "review", "merge", Finish}; !reflect.DeepEqual(got, want) {
		t.Errorf("default path %v, want %v", got, want)
	}
	if got := f.ScenariosOf("review"); !reflect.DeepEqual(got, []string{"bug", "feature"}) {
		t.Errorf("scenarios of review: %v", got)
	}
	if got := f.StagesOf("review-checklist"); !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("stages of review-checklist: %v", got)
	}
	if got := f.StagesBy("reviewer"); !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("stages by reviewer: %v", got)
	}

	only := f.OnlyScenario(feature)
	ids = nil
	for _, s := range only.Stages {
		ids = append(ids, s.ID)
	}
	if len(only.Scenarios) != 1 || !reflect.DeepEqual(ids, []string{"branch", "implementation", "merge", "plan-feature", "review"}) ||
		len(only.Parts) != 2 || len(only.Agents) != 1 {
		t.Errorf("only feature: stages %v, parts %+v, agents %+v", ids, only.Parts, only.Agents)
	}
	if only := f.OnlyStage(review); len(only.Scenarios) != 0 || len(only.Stages) != 1 || len(only.Parts) != 1 || len(only.Agents) != 1 {
		t.Errorf("only review: %+v", only)
	}
	if only := f.OnlyAgent(f.Agents[0]); len(only.Stages) != 0 || len(only.Agents) != 1 || only.Parts == nil {
		t.Errorf("only reviewer: %+v", only)
	}
}

// TestCapabilitiesInContract checks that the schema of flow show allows the
// capabilities a subagent may have, in the same order: a capability added to
// the flow and not to the schema makes the output of flow show fail the
// contract for the panel.
func TestCapabilitiesInContract(t *testing.T) {
	const name = "schemas/flow-show.json"
	data, err := contract.Schemas.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Agents struct {
				Items struct {
					Properties struct {
						Capabilities struct {
							Items struct {
								Enum []string `json:"enum"`
							} `json:"items"`
						} `json:"capabilities"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"agents"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if enum := schema.Properties.Agents.Items.Properties.Capabilities.Items.Enum; !slices.Equal(enum, Capabilities) {
		t.Errorf("%s allows the capabilities %v of a subagent, the flow accepts %v", name, enum, Capabilities)
	}
}

// TestModelsInContract checks that the schema of flow show allows the model
// levels a subagent may have, in the same order.
func TestModelsInContract(t *testing.T) {
	const name = "schemas/flow-show.json"
	data, err := contract.Schemas.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Agents struct {
				Items struct {
					Properties struct {
						Model struct {
							Enum []string `json:"enum"`
						} `json:"model"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"agents"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if enum := schema.Properties.Agents.Items.Properties.Model.Enum; !slices.Equal(enum, Models) {
		t.Errorf("%s allows the model levels %v of a subagent, the flow accepts %v", name, enum, Models)
	}
}

func TestReadDirMissing(t *testing.T) {
	p := shop(t, nil)
	var pe *fs.PathError
	if _, err := ReadDir(filepath.Join(p.Dir, "none"), p.Shared); !errors.As(err, &pe) {
		t.Errorf("got %v, want a path error", err)
	}
}

func TestLoadIgnoresHidden(t *testing.T) {
	res := readShop(t, shop(t, map[string]string{
		".git/config":         "x",
		"stages/.draft.yaml":  "not: [yaml",
		"scenarios/.old.yaml": "x",
	}))
	if len(res.Problems) > 0 {
		t.Error(dump(res.Problems))
	}
	// Hidden files are not part of the snapshot either.
	for path := range res.Snapshot.Files {
		if strings.Contains(path, "/.") || strings.HasPrefix(path, ".") {
			t.Errorf("hidden file %s in the snapshot", path)
		}
	}
}

func TestLoadAcceptsYAMLForms(t *testing.T) {
	// Anchors, aliases, block text, a byte order mark and line ends of Windows
	// are plain YAML.
	f := load(t, shop(t, map[string]string{
		"stages/branch.yaml":         "\xEF\xBB\xBFtitle: &t Ветка\nexit: *t\nexecutor: orchestrator\ninclude: []\n",
		"stages/implementation.yaml": "title: Реализация\nexit: |\n  изменения сделаны,\n  тесты проходят\nexecutor: orchestrator\n",
		"stages/merge.yaml":          "title: Слияние\r\nexit: ветка задачи влита в main\r\nexecutor: operator\r\n",
		"stages/merge.md":            "Влить ветку.\r\nУдалить ветку.\r\n",
		"flow.yaml":                  "# общие правила\n",
	}))
	if b, _ := f.Stage("branch"); b.Title != "Ветка" || b.Exit != "Ветка" {
		t.Errorf("branch: %+v", b)
	}
	if i, _ := f.Stage("implementation"); i.Exit != "изменения сделаны,\nтесты проходят\n" {
		t.Errorf("implementation: %+v", i)
	}
	// Line ends of Windows are not part of the values and texts.
	if m, _ := f.Stage("merge"); m.Title != "Слияние" || m.Executor != Operator || m.Instruction != "Влить ветку.\nУдалить ветку.\n" {
		t.Errorf("merge: %+v", m)
	}
}

// TestProblemOrder checks the example of the plan: problems by file and
// line, files that do not belong to the flow last.
func TestProblemOrder(t *testing.T) {
	d := shop(t, map[string]string{
		"stages/merge.yml":     "x",
		"stages/review.yaml":   "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: reviewr\n",
		"stages/plan-bug.yaml": "title: План бага\nexecutor: orchestrator\ninclude: [plan-format]\n",
		"scenarios/bug.yaml":   strings.Replace(read(t, d0(t), "scenarios/bug.yaml"), "start: branch", "start: brnch", 1),
		"scenarios/feature.yaml": strings.Replace(read(t, d0(t), "scenarios/feature.yaml"),
			"        max_rounds: 3\n", "", 1),
	})
	var got []string
	for _, p := range problems(t, d) {
		got = append(got, p.Message)
	}
	want := []string{
		msg.Text(msg.ProblemUnknownStart, msg.Text(msg.FlowObjScenario, "bug"), "brnch"),
		msg.Text(msg.ProblemUnlimitedLoop, msg.Text(msg.FlowObjScenario, "feature"), "implementation → review → implementation"),
		msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "plan-bug"), "exit"),
		msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewr"),
		msg.Text(msg.ProblemExtraFile, "stages/merge.yml"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// d0 returns the places of an unchanged copy of the shop.
func d0(t *testing.T) Places { return shop(t, nil) }

// read returns the text of file rel of the flow of p.
func read(t *testing.T, p Places, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFileProblems(t *testing.T) {
	stage := func(fields string) string {
		return "title: План бага\nexit: план согласован\nexecutor: orchestrator\n" + fields
	}
	tests := []struct {
		name    string
		changes map[string]string
		want    []Problem
	}{
		{"not utf-8", map[string]string{"stages/review.md": "\xff\xfe"}, []Problem{
			{contract.ProblemSyntax, "stages/review.md", 0, msg.Text(msg.ProblemEncoding, msg.Text(msg.FlowObjStage, "review"))},
		}},
		{"part not utf-8", map[string]string{"parts/plan-format.md": "\xff"}, []Problem{
			{contract.ProblemSyntax, "parts/plan-format.md", 0, msg.Text(msg.ProblemEncoding, msg.Text(msg.FlowObjPart, "plan-format"))},
		}},
		{"two documents", map[string]string{"stages/plan-bug.yaml": stage("---\ntitle: x\n")}, []Problem{
			{contract.ProblemSyntax, "stages/plan-bug.yaml", 5, msg.Text(msg.ProblemSyntax, msg.Text(msg.FlowObjStage, "plan-bug"))},
		}},
		{"unknown field", map[string]string{"stages/plan-bug.yaml": "title: План бага\nexits: x\nexit: y\nexecutor: orchestrator\n"}, []Problem{
			{contract.ProblemUnknownField, "stages/plan-bug.yaml", 2, msg.Text(msg.ProblemUnknownField, msg.Text(msg.FlowObjStage, "plan-bug"), "exits")},
		}},
		{"later fields", map[string]string{
			"flow.yaml":          "on_take:\n  tracker:\n    - set_state: In Progress\non_enter: x\n",
			"stages/branch.yaml": "title: Ветка\nexit: создана ветка\nexecutor: orchestrator\nprocedures: []\non_enter: x\n",
		}, []Problem{
			{contract.ProblemUnsupportedField, "flow.yaml", 1, msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjCommon), "on_take")},
			{contract.ProblemUnknownField, "flow.yaml", 4, msg.Text(msg.ProblemUnknownField, msg.Text(msg.FlowObjCommon), "on_enter")},
			{contract.ProblemUnsupportedField, "stages/branch.yaml", 4, msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjStage, "branch"), "procedures")},
			{contract.ProblemUnsupportedField, "stages/branch.yaml", 5, msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjStage, "branch"), "on_enter")},
		}},
		{"scenario event", map[string]string{"scenarios/bug.yaml": "on_close:\n  tracker: []\n" + read(t, d0(t), "scenarios/bug.yaml")}, []Problem{
			{contract.ProblemUnsupportedField, "scenarios/bug.yaml", 1, msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjScenario, "bug"), "on_close")},
		}},
		{"missing fields", map[string]string{
			"stages/plan-bug.yaml": "title: План бага\nexit: \"\"\n",
			"stages/merge.yaml":    "# пусто\n",
		}, []Problem{
			{contract.ProblemMissingField, "stages/merge.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "merge"), "title")},
			{contract.ProblemMissingField, "stages/merge.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "merge"), "exit")},
			{contract.ProblemMissingField, "stages/merge.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "merge"), "executor")},
			{contract.ProblemMissingField, "stages/plan-bug.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "plan-bug"), "executor")},
			{contract.ProblemMissingField, "stages/plan-bug.yaml", 2, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "plan-bug"), "exit")},
		}},
		{"invalid values", map[string]string{
			"stages/plan-bug.yaml": stage("include: plan-format\n"),
			"stages/review.yaml":   "title: 5\nexit: [a]\nexecutor: reviewer\ninclude: [Review-Checklist]\n",
			"stages/merge.yaml":    "- title\n",
		}, []Problem{
			{contract.ProblemInvalidValue, "stages/merge.yaml", 1, msg.Text(msg.ProblemNotMapping, msg.Text(msg.FlowObjStage, "merge"))},
			{contract.ProblemInvalidValue, "stages/plan-bug.yaml", 4, msg.Text(msg.ProblemInclude, msg.Text(msg.FlowObjStage, "plan-bug"))},
			{contract.ProblemInvalidValue, "stages/review.yaml", 1, msg.Text(msg.ProblemNotString, msg.Text(msg.FlowObjStage, "review"), "title")},
			{contract.ProblemInvalidValue, "stages/review.yaml", 2, msg.Text(msg.ProblemNotString, msg.Text(msg.FlowObjStage, "review"), "exit")},
			{contract.ProblemInvalidValue, "stages/review.yaml", 4, msg.Text(msg.ProblemInclude, msg.Text(msg.FlowObjStage, "review"))},
		}},
		// A command of checks is a string with a command; the time is whole
		// numbers with units above zero.
		{"checks", map[string]string{
			"stages/plan-bug.yaml":       stage("checks: go test ./...\ntimeout: 600\n"),
			"stages/review.yaml":         "title: Ревью\nexit: x\nexecutor: reviewer\nchecks: [go vet, 5]\ntimeout: 1.5m\n",
			"stages/merge.yaml":          "title: Слияние\nexit: x\nexecutor: orchestrator\nchecks:\n  - \"  \"\ntimeout: 0s\n",
			"stages/implementation.yaml": "title: Реализация\nexit: x\nexecutor: orchestrator\nchecks:\n  - go vet ./...\n  - go test -run \"TestTotal ./...\ntimeout: 500ms\n",
		}, []Problem{
			{contract.ProblemInvalidValue, "stages/implementation.yaml", 6, msg.Text(msg.ProblemCheckQuote, msg.Text(msg.FlowObjStage, "implementation"), `go test -run "TestTotal ./...`)},
			{contract.ProblemInvalidValue, "stages/implementation.yaml", 7, msg.Text(msg.ProblemTimeout, msg.Text(msg.FlowObjStage, "implementation"))},
			{contract.ProblemInvalidValue, "stages/merge.yaml", 4, msg.Text(msg.ProblemChecks, msg.Text(msg.FlowObjStage, "merge"))},
			{contract.ProblemInvalidValue, "stages/merge.yaml", 6, msg.Text(msg.ProblemTimeout, msg.Text(msg.FlowObjStage, "merge"))},
			{contract.ProblemInvalidValue, "stages/plan-bug.yaml", 4, msg.Text(msg.ProblemChecks, msg.Text(msg.FlowObjStage, "plan-bug"))},
			{contract.ProblemInvalidValue, "stages/plan-bug.yaml", 5, msg.Text(msg.ProblemTimeout, msg.Text(msg.FlowObjStage, "plan-bug"))},
			{contract.ProblemInvalidValue, "stages/review.yaml", 4, msg.Text(msg.ProblemChecks, msg.Text(msg.FlowObjStage, "review"))},
			{contract.ProblemInvalidValue, "stages/review.yaml", 5, msg.Text(msg.ProblemTimeout, msg.Text(msg.FlowObjStage, "review"))},
		}},
		{"invalid ids", map[string]string{
			"stages/Plan_Bug.yaml":   stage(""),
			"stages/Plan_Bug.md":     "x",
			"parts/Big.md":           "x",
			"scenarios/Bug_Fix.yaml": "x",
		}, []Problem{
			{contract.ProblemInvalidID, "scenarios/Bug_Fix.yaml", 0, msg.Text(msg.ProblemInvalidID, msg.Text(msg.FlowObjScenario, "Bug_Fix"))},
			{contract.ProblemInvalidID, "stages/Plan_Bug.md", 0, msg.Text(msg.ProblemInvalidID, msg.Text(msg.FlowObjStage, "Plan_Bug"))},
			{contract.ProblemInvalidID, "parts/Big.md", 0, msg.Text(msg.ProblemInvalidID, msg.Text(msg.FlowObjPart, "Big"))},
		}},
		{"extra files", map[string]string{
			"stages/merge.yml":   "x",
			"templates/close.md": "x",
			"README.md":          "x",
			"scenarios/old/x":    "x",
			"parts/note.txt":     "x",
		}, []Problem{
			{contract.ProblemExtraFile, "README.md", 0, msg.Text(msg.ProblemExtraFile, "README.md")},
			{contract.ProblemExtraFile, "parts/note.txt", 0, msg.Text(msg.ProblemExtraFile, "parts/note.txt")},
			{contract.ProblemExtraFile, "scenarios/old", 0, msg.Text(msg.ProblemExtraDir, "scenarios/old")},
			{contract.ProblemExtraFile, "stages/merge.yml", 0, msg.Text(msg.ProblemExtraFile, "stages/merge.yml")},
			{contract.ProblemExtraFile, "templates", 0, msg.Text(msg.ProblemExtraDir, "templates")},
		}},
		{"instructions", map[string]string{"stages/merge.md": "", "stages/old.md": "x"}, []Problem{
			{contract.ProblemMissingInstruction, "stages/merge.yaml", 0, msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjStage, "merge"))},
			{contract.ProblemOrphanInstruction, "stages/old.md", 0, msg.Text(msg.ProblemOrphanInstruction, msg.Text(msg.FlowObjStage, "old"))},
		}},
		{"no scenarios", map[string]string{"scenarios": ""}, []Problem{
			{contract.ProblemNoScenarios, "", 0, msg.Text(msg.ProblemNoScenarios)},
		}},
		{"unknown references", map[string]string{
			"stages/plan-bug.yaml": stage("include: [plan-formt]\n"),
			"stages/review.yaml":   "title: Ревью\nexit: замечания разобраны\nexecutor: reviewr\n",
			"scenarios/bug.yaml":   strings.Replace(read(t, d0(t), "scenarios/bug.yaml"), "stage: plan-bug", "stage: plan-bugg", 1),
		}, []Problem{
			{contract.ProblemUnknownStage, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemUnknownStage, msg.Text(msg.FlowObjNode, "bug", "plan"), "plan-bugg")},
			{contract.ProblemUnknownPart, "stages/plan-bug.yaml", 4, msg.Text(msg.ProblemUnknownPart, msg.Text(msg.FlowObjStage, "plan-bug"), "plan-formt")},
			{contract.ProblemUnknownExecutor, "stages/review.yaml", 3, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewr")},
		}},
		// The field agents is a list of identifiers without repeats, at a
		// stage the main session or the operator carries out; the subagents
		// of a field that is not allowed are not looked up.
		{"agents", map[string]string{
			"stages/branch.yaml":       stage("agents: [Reviewer]\n"),
			"stages/plan-bug.yaml":     stage("agents: reviewer\n"),
			"stages/plan-feature.yaml": stage("agents: [reviewer, reviewer]\n"),
			"stages/review.yaml":       "title: Ревью\nexit: x\nexecutor: reviewer\nagents: [auditor]\n",
		}, []Problem{
			{contract.ProblemInvalidValue, "stages/branch.yaml", 4, msg.Text(msg.ProblemAgents, msg.Text(msg.FlowObjStage, "branch"))},
			{contract.ProblemInvalidValue, "stages/plan-bug.yaml", 4, msg.Text(msg.ProblemAgents, msg.Text(msg.FlowObjStage, "plan-bug"))},
			{contract.ProblemInvalidValue, "stages/plan-feature.yaml", 4, msg.Text(msg.ProblemAgents, msg.Text(msg.FlowObjStage, "plan-feature"))},
			{contract.ProblemAgentsNotAllowed, "stages/review.yaml", 4, msg.Text(msg.ProblemAgentsNotAllowed, msg.Text(msg.FlowObjStage, "review"))},
		}},
		// A subagent of agents that does not exist is told in the words of
		// the executor, on its own line; the main session and the operator
		// are not subagents.
		{"unknown agents", map[string]string{
			"stages/merge.yaml": "title: Слияние\nexit: x\nexecutor: operator\nagents:\n  - revewer\n  - orchestrator\n  - operator\n  - reviewer\n",
		}, []Problem{
			{contract.ProblemUnknownAgent, "stages/merge.yaml", 5, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "merge"), "revewer")},
			{contract.ProblemUnknownAgent, "stages/merge.yaml", 6, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "merge"), "orchestrator")},
			{contract.ProblemUnknownAgent, "stages/merge.yaml", 7, msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "merge"), "operator")},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := problems(t, shop(t, tt.changes))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("problems:\n%s\nwant:\n%s", dump(got), dump(tt.want))
			}
		})
	}
}

// TestStageChecks checks that a stage reads its commands of checks in order
// and the time of them as written, and that a stage without them, with an
// empty list or with null fields has none and the default time.
func TestStageChecks(t *testing.T) {
	res := readShop(t, shop(t, map[string]string{
		"stages/implementation.yaml": "title: Реализация\nexit: тесты проходят\nexecutor: orchestrator\n" +
			"checks:\n  - go vet ./...\n  - go test -run 'Total|Sum' ./...\ntimeout: 1h30m\n",
		"stages/review.yaml": "title: Ревью\nexit: x\nexecutor: reviewer\nchecks: []\ntimeout: 90s\n",
		"stages/merge.yaml":  "title: Слияние\nexit: x\nexecutor: orchestrator\nchecks:\ntimeout:\n",
	}))
	if len(res.Problems) > 0 {
		t.Fatal(dump(res.Problems))
	}
	st, _ := res.Flow.Stage("implementation")
	if want := []string{"go vet ./...", "go test -run 'Total|Sum' ./..."}; !slices.Equal(st.Checks, want) || st.CheckTimeout() != "1h30m" {
		t.Errorf("implementation: checks %q, timeout %q", st.Checks, st.CheckTimeout())
	}
	st, _ = res.Flow.Stage("review")
	if st.Checks != nil || st.Timeout != "90s" {
		t.Errorf("review: checks %q, timeout %q", st.Checks, st.Timeout)
	}
	for _, id := range []string{"merge", "branch"} {
		st, _ = res.Flow.Stage(id)
		if st.Checks != nil || st.Timeout != "" || st.CheckTimeout() != "10m" {
			t.Errorf("%s: checks %q, timeout %q, time %q", id, st.Checks, st.Timeout, st.CheckTimeout())
		}
	}
}

func TestSyntaxProblem(t *testing.T) {
	got := problems(t, shop(t, map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: [branch\n"}))
	if len(got) != 1 || got[0].Code != contract.ProblemSyntax || got[0].File != "scenarios/bug.yaml" || got[0].Line < 1 ||
		got[0].Message != msg.Text(msg.ProblemSyntax, msg.Text(msg.FlowObjScenario, "bug")) {
		t.Errorf("problems:\n%s", dump(got))
	}
	// A key given twice is a syntax error of YAML too.
	got = problems(t, shop(t, map[string]string{"stages/merge.yaml": "title: a\ntitle: b\nexit: c\nexecutor: operator\n"}))
	if len(got) != 1 || got[0].Code != contract.ProblemSyntax {
		t.Errorf("duplicate key:\n%s", dump(got))
	}
}

func dump(ps []Problem) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString("  " + p.Code + " " + p.File + ":" + strconv.Itoa(p.Line) + " " + p.Message + "\n")
	}
	return b.String()
}
