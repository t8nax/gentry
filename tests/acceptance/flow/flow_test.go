package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// security is the change of the example of the plan: a stage security after
// the review, carried out by the project subagent auditor, and a new
// instruction of the review.
func security(t *testing.T, p flow.Places) {
	t.Helper()
	feature, err := os.ReadFile(filepath.Join(p.Dir, "scenarios", "feature.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(feature), "      - to: merge\n", "      - to: security\n", 1)
	text = strings.Replace(text, "  merge:", "  security:        { stage: security, next: merge }\n  merge:", 1)
	testkit.WriteDraft(t, p, map[string]string{
		"scenarios/feature.yaml": text,
		"stages/review.md":       "## Замечания\nЗаписать замечания ревью и проверить, что каждое исправлено\nили отклонено с обоснованием.\n",
		"stages/security.yaml":   "title: Безопасность\nexit: проверка безопасности пройдена\nexecutor: auditor\n",
		"stages/security.md":     "Проверить изменения на уязвимости.\n",
		"agents/auditor.yaml":    "purpose: проверка безопасности изменений\ncapabilities: [read, search]\n",
		"agents/auditor.md":      "Найти уязвимости в изменениях задачи.\n",
	})
}

// flowHead is the head of flow show of the shop: its project, when its flow
// was applied, and the flow directory. Its words are those of the catalog:
// the tests of the output in package cli state them.
func flowHead(applied, dir string) string {
	return testkit.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, applied), msg.Text(msg.FlowDir, dir))
}

func TestFlowShow(t *testing.T) {
	p := testkit.ShopFlow(t)
	code, stdout, stderr := testkit.Run("flow", "show")
	want := flowHead("<время>", p.Dir) + "\n" + testkit.ShopTables() + "\n" + cli.HintText(msg.HintFlowStage) + "\n"
	if stdout = testkit.Masked(stdout); code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--scenario", "feature"}, testkit.Lines(
			msg.Text(msg.FlowScenario, "feature"),
			msg.Text(msg.FlowTitle, "Фича"),
			"",
			msg.Text(msg.FlowDefaultPath),
			"  branch → plan → implementation → review → merge → "+msg.Text(msg.FlowEnd),
			"",
			msg.Text(msg.FlowConditional),
			"  "+msg.Text(msg.FlowTransitionLimit, "review", "implementation", msg.Count(msg.FlowRounds, 3), "ревью выявило существенные замечания"),
			"",
		) + testkit.Table(
			[]string{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)},
			[]string{"branch", "branch", "orchestrator"},
			[]string{"plan", "plan-feature", "orchestrator"},
			[]string{"implementation", "implementation", "orchestrator"},
			[]string{"review", "review", "reviewer"},
			[]string{"merge", "merge", "operator"},
		)},
		{[]string{"--stage", "review"}, testkit.Lines(
			msg.Text(msg.FlowStage, "review"),
			msg.Text(msg.FlowTitle, "Ревью"),
			msg.Text(msg.FlowExecutor, "reviewer"),
			msg.Text(msg.FlowExit, "замечания ревью записаны и разобраны"),
			msg.Text(msg.FlowParts, "review-checklist"),
			msg.Text(msg.FlowScenarios, "bug, feature"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Проверить изменения задачи по списку.",
		)},
		{[]string{"--stage=merge"}, testkit.Lines(
			msg.Text(msg.FlowStage, "merge"),
			msg.Text(msg.FlowTitle, "Слияние"),
			msg.Text(msg.FlowExecutor, "operator"),
			msg.Text(msg.FlowExit, "ветка задачи влита в main"),
			msg.Text(msg.FlowParts, testkit.None),
			msg.Text(msg.FlowScenarios, "bug, feature"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Влить ветку задачи в main.",
		)},
		{[]string{"--agent", "reviewer"}, testkit.Lines(
			msg.Text(msg.FlowAgent, "reviewer"),
			msg.Text(msg.FlowSource, msg.Text(msg.FlowSourceShared)),
			msg.Text(msg.FlowPurpose, "ревью изменений задачи — поведение и текст"),
			msg.Text(msg.FlowCapabilities, "read, search"),
			msg.Text(msg.FlowModel, testkit.None),
			msg.Text(msg.FlowStages, "review"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Проверить изменения задачи: поведение, тесты и тексты для оператора.",
		)},
		{[]string{"--part", "review-checklist"}, testkit.Lines(
			msg.Text(msg.FlowPart, "review-checklist"),
			msg.Text(msg.FlowStages, "review"),
			"",
			msg.Text(msg.FlowText),
			"  ## Список ревью",
			"  - поведение",
			"  - тесты",
			"  - тексты",
		)},
	}
	for _, tt := range tests {
		code, stdout, stderr := testkit.Run(append([]string{"flow", "show"}, tt.args...)...)
		if code != contract.ExitOK || stdout != tt.want {
			t.Errorf("%v: exit code %d, stderr %q, output:\n%s\nwant:\n%s", tt.args, code, stderr, stdout, tt.want)
		}
	}
}

func TestFlowShowConditional(t *testing.T) {
	p := testkit.ShopFlow(t)
	testkit.WriteDraft(t, p, map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: triage\nnodes:\n" +
		"  triage:\n    stage: plan-bug\n    next:\n      - to: merge\n        if: срочно\n      - to: finish\n        if: |\n          не воспроизводится\n          на main\n" +
		"  merge: { stage: merge, next: finish }\n"})
	_, stdout, stderr := testkit.Run("flow", "show", "--draft", "--scenario", "bug")
	// At a fork the default path stops; the end is named in words.
	want := testkit.Lines(msg.Text(msg.FlowDefaultPath), "  triage", "", msg.Text(msg.FlowConditional),
		"  "+msg.Text(msg.FlowTransition, "triage", "merge", "срочно"),
		"  "+msg.Text(msg.FlowTransition, "triage", msg.Text(msg.FlowEnd), "не воспроизводится на main"), "")
	if !strings.Contains(stdout, want) {
		t.Errorf("stderr %q, output:\n%s\nwant within:\n%s", stderr, stdout, want)
	}
}

// TestFlowOperatorReturn applies a return without a limit from the merge,
// a stage of the operator, and refuses it once the stage is the
// orchestrator's.
func TestFlowOperatorReturn(t *testing.T) {
	p := testkit.ShopFlow(t)
	feature, err := os.ReadFile(filepath.Join(p.Dir, "scenarios", "feature.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	unlimited := strings.Replace(string(feature), "  merge:           { stage: merge, next: finish }\n",
		"  merge:\n    stage: merge\n    next:\n      - to: implementation\n        if: оператор назвал, что не так\n      - to: finish\n", 1)
	testkit.WriteDraft(t, p, map[string]string{
		"scenarios/feature.yaml": unlimited,
		"stages/merge.yaml":      "title: Слияние\nexit: ветка задачи влита в main\nexecutor: orchestrator\n",
	})
	code, stdout, _ := testkit.Run("flow", "apply", "--json")
	if code != contract.ExitError || !strings.Contains(stdout, `"code":"flow_draft_invalid"`) || !strings.Contains(stdout, `"code":"unlimited_loop"`) {
		t.Errorf("apply of a return of the orchestrator without a limit: exit code %d, %s", code, stdout)
	}

	testkit.WriteDraft(t, p, map[string]string{"stages/merge.yaml": "title: Слияние\nexit: ветка задачи влита в main\nexecutor: operator\n"})
	testkit.MustRun(t, "flow", "apply")
	_, stdout, _ = testkit.Run("flow", "show", "--scenario", "feature")
	if want := "  " + msg.Text(msg.FlowTransition, "merge", "implementation", "оператор назвал, что не так") + "\n"; !strings.Contains(stdout, want) {
		t.Errorf("flow show --scenario feature:\n%s\nwant within:\n%s", stdout, want)
	}
	stdout = testkit.WantJSON(t, contract.ExitOK, "schemas/flow-show.json", "flow", "show", "--scenario", "feature")
	if want := `{"id":"merge","next":[{"if":"оператор назвал, что не так","return":true,"to":"implementation"},{"to":"finish"}],"stage":"merge"}`; !strings.Contains(stdout, want) {
		t.Errorf("flow show --scenario feature --json:\n%s\nwant within:\n%s", stdout, want)
	}
}

func TestFlowDraft(t *testing.T) {
	p := testkit.EmptyShopFlow(t)
	// The flow directory is there from the connection, with the directories
	// of a flow as a prompt.
	for _, d := range []string{"scenarios", "stages", "parts", "agents"} {
		if fi, err := os.Stat(filepath.Join(p.Dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", d, err)
		}
	}
	testkit.CopyShopFlow(t, p)

	report := filepath.Join(filepath.Dir(p.Dir), "changes.md")
	_, stdout, _ := testkit.Run("flow", "diff")
	condition := msg.Text(msg.DiffCondition, "ревью выявило существенные замечания")
	want := diffHead(testkit.None, false, report) + testkit.Lines(
		msg.Text(msg.DiffScenarios),
		change("+", msg.Text(msg.DiffItem, "Баг", "Ветка → План бага → Реализация → Ревью → Слияние")),
		returnLine("+", "Ревью", "Реализация", condition+", "+msg.Count(msg.DiffRounds, 2)),
		change("+", msg.Text(msg.DiffItem, "Фича", "Ветка → План фичи → Реализация → Ревью → Слияние")),
		returnLine("+", "Ревью", "Реализация", condition+", "+msg.Count(msg.DiffRounds, 3)),
		"",
		msg.Text(msg.DiffStages),
		change("+", stage("Ветка", "branch")),
		change("+", stage("Реализация", "implementation")),
		change("+", stage("Слияние", "merge")),
		change("+", stage("План бага", "plan-bug")),
		change("+", stage("План фичи", "plan-feature")),
		change("+", stage("Ревью", "review")),
		"",
		msg.Text(msg.DiffParts),
		change("+", "plan-format"),
		change("+", "review-checklist"),
		"",
	) + legend + testkit.Lines(cli.HintText(msg.HintFlowApply))
	if stdout != want {
		t.Errorf("diff from nothing:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = testkit.Run("flow", "show", "--draft")
	want = flowHead(testkit.None, p.Dir) + "\n" + testkit.ShopTables() + "\n" + cli.HintText(msg.HintFlowApply) + "\n"
	if stdout != want {
		t.Errorf("show --draft:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := testkit.Run("flow", "apply"); testkit.Masked(stdout) != testkit.Lines(msg.Text(msg.FlowApplied), msg.Text(msg.FlowProject, "shop"),
		msg.Text(msg.FlowAppliedAt, "<время>"), "", msg.Text(msg.AgentsFreeSynced, "shop"), msg.Text(msg.AgentsNextSession)) {
		t.Errorf("apply:\n%s", stdout)
	}
	process := filepath.Dir(p.Shared)
	if log := gittest.Run(t, process, "log", "-1", "--format=%s|%b"); log != "Apply the flow of shop|Gentry: flow apply shop\n\n" {
		t.Errorf("commit: %q", log)
	}

	// Editing the files makes a draft.
	testkit.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	_, stdout, _ = testkit.Run("flow", "show")
	if want := flowHead("<время>", p.Dir) + msg.Text(msg.FlowDraftOpened) + "\n\n" + testkit.ShopTables() + "\n" + cli.HintText(msg.HintFlowShowDraft) + "\n"; testkit.Masked(stdout) != want {
		t.Errorf("show with a draft:\n%s\nwant:\n%s", stdout, want)
	}
	// Line ends of another editor are no change.
	testkit.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main.\r\n"})
	code, _, stderr := testkit.Run("flow", "diff")
	if want := testkit.Lines(msg.Text(msg.ErrNoDrafts, "shop"), msg.Text(msg.FlowDir, p.Dir)); code != contract.ExitError || stderr != want {
		t.Errorf("diff without changes: exit code %d, stderr:\n%s", code, stderr)
	}

	security(t, p)
	_, stdout, _ = testkit.Run("flow", "diff")
	want = diffHead("<время>", false, report) + testkit.Lines(
		msg.Text(msg.DiffScenarios),
		"  "+msg.Text(msg.DiffItem, "Фича", "Ветка → План фичи → Реализация → ~ Ревью → + Безопасность → Слияние"),
		"",
		msg.Text(msg.DiffStages),
		change("~", stage("Ревью", "review")),
		change("+", stage("Безопасность", "security")),
		"",
		msg.Text(msg.DiffAgents),
		change("+", "auditor"),
		"",
	) + legend + testkit.Lines(cli.HintText(msg.HintFlowApply))
	if testkit.Masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}
	if _, err := os.Stat(report); err != nil {
		t.Errorf("the file of changes: %v", err)
	}
	testkit.MustRun(t, "flow", "apply")
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Errorf("the file of changes after apply: %v", err)
	}
	_, stdout, _ = testkit.Run("flow", "show")
	if !strings.Contains(stdout, "\n"+testkit.Table([]string{msg.Text(msg.ColAgent), msg.Text(msg.ColSource), msg.Text(msg.ColStages)},
		[]string{"auditor", msg.Text(msg.FlowSourceProject), "security"},
		[]string{"reviewer", msg.Text(msg.FlowSourceShared), "review"})) {
		t.Errorf("show after apply:\n%s", stdout)
	}

	testkit.WriteDraft(t, p, map[string]string{"stages/security.md": "", "parts/extra.md": "Лишнее.\n"})
	testkit.MustRun(t, "flow", "diff")
	if _, stdout, _ := testkit.Run("flow", "discard"); stdout != testkit.Lines(msg.Text(msg.FlowDiscarded), msg.Text(msg.FlowProject, "shop")) {
		t.Errorf("discard:\n%s", stdout)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Errorf("the file of changes after discard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "parts", "extra.md")); !os.IsNotExist(err) {
		t.Errorf("an added file after discard: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "stages", "security.md")); err != nil || string(b) != "Проверить изменения на уязвимости.\n" {
		t.Errorf("a removed file after discard: %q, %v", b, err)
	}
	if code, _, _ := testkit.Run("flow", "show", "--draft"); code != contract.ExitError {
		t.Errorf("a draft after discard")
	}
}

func TestFlowJSON(t *testing.T) {
	p := testkit.EmptyShopFlow(t)
	testkit.CopyShopFlow(t, p)
	_, stdout, _ := testkit.Run("flow", "apply", "--json")
	testkit.Validate(t, "schemas/flow-apply.json", stdout)
	var applied contract.FlowApplyOutput
	if err := json.Unmarshal([]byte(stdout), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Project != "shop" || applied.Sent || len(applied.Applied.Commit) != 40 || applied.Sync != nil ||
		!regexp.MustCompile(`"time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.000Z"`).MatchString(stdout) {
		t.Errorf("apply: %s", stdout)
	}

	security(t, p)
	_, stdout, _ = testkit.Run("flow", "diff", "--json")
	testkit.Validate(t, "schemas/flow-diff.json", stdout)
	want := `{"applied":{"commit":"` + applied.Applied.Commit + `",`
	tail := `"changes":[{"change":"modified","id":"feature","object":"scenario"},` +
		`{"change":"modified","id":"review","object":"stage"},{"change":"added","id":"security","object":"stage"},` +
		`{"change":"added","id":"auditor","object":"agent"}],"project":"shop","report":`
	var diff contract.FlowDiffOutput
	if err := json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, want) || !strings.Contains(stdout, tail) || diff.Shared != nil ||
		diff.Report != filepath.Join(filepath.Dir(p.Dir), "changes.md") {
		t.Errorf("diff: %s", stdout)
	}

	code, stdout, _ := testkit.Run("flow", "show", "--json")
	if code != contract.ExitOK {
		t.Fatalf("show: exit code %d: %s", code, stdout)
	}
	testkit.Validate(t, "schemas/flow-show.json", stdout)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Project != "shop" || out.Applied == nil || out.Applied.Commit != applied.Applied.Commit || out.Dir == nil || *out.Dir != p.Dir || out.Task != nil ||
		out.Draft == nil || out.Draft.ConflictDir != nil || len(out.Scenarios) != 2 || len(out.Stages) != 6 || len(out.Parts) != 2 || len(out.Agents) != 1 {
		t.Errorf("show: %s", stdout)
	}
	for _, part := range []string{
		`{"id":"review","next":[{"if":"ревью выявило существенные замечания","max_rounds":3,"return":true,"to":"implementation"},{"to":"merge"}],"stage":"review"},`,
		`{"executor":"operator","exit":"ветка задачи влита в main","id":"merge","include":[],"instruction":"Влить ветку задачи в main.\n","title":"Слияние"}`,
		`"parts":[{"id":"plan-format","text":`,
		`"agents":[{"capabilities":["read","search"],"id":"reviewer","instruction":"Проверить изменения задачи: поведение, тесты и тексты для оператора.\n","purpose":"ревью изменений задачи — поведение и текст","source":"shared"}]`,
		`"draft":{}`,
	} {
		if !strings.Contains(stdout, part) {
			t.Errorf("show: no %s in\n%s", part, stdout)
		}
	}

	// An object narrows the arrays to it and what it refers to.
	_, stdout, _ = testkit.Run("flow", "show", "--draft", "--stage", "security", "--json")
	testkit.Validate(t, "schemas/flow-show.json", stdout)
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Scenarios) != 0 || len(out.Stages) != 1 || len(out.Parts) != 0 || len(out.Agents) != 1 || out.Agents[0].Source != "project" {
		t.Errorf("show --draft --stage security: %s", stdout)
	}
	_, stdout, _ = testkit.Run("flow", "show", "--scenario", "feature", "--json")
	if strings.Contains(stdout, `"plan-bug"`) || !strings.Contains(stdout, `"id":"reviewer"`) {
		t.Errorf("show --scenario feature: %s", stdout)
	}

	// A subagent that sees the task and leads its progress passes the
	// contract too.
	testkit.WriteDraft(t, p, map[string]string{
		"agents/auditor.yaml": "purpose: проверка безопасности изменений\ncapabilities: [read, edit, task, progress]\n",
	})
	code, stdout, _ = testkit.Run("flow", "show", "--draft", "--agent", "auditor", "--json")
	if code != contract.ExitOK {
		t.Fatalf("show --draft --agent auditor: exit code %d: %s", code, stdout)
	}
	testkit.Validate(t, "schemas/flow-show.json", stdout)
	out = contract.FlowShowOutput{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if want := []contract.FlowShowOutputAgentsElemCapabilitiesElem{"read", "edit", "task", "progress"}; len(out.Agents) != 1 ||
		!slices.Equal(out.Agents[0].Capabilities, want) {
		t.Errorf("show --draft --agent auditor: %s", stdout)
	}

	// The model level of a subagent passes the contract; the model of the AI
	// tool is not shown.
	testkit.WriteDraft(t, p, map[string]string{
		"agents/auditor.yaml": "purpose: проверка безопасности изменений\ncapabilities: [read, search]\nmodel: strong\n",
	})
	code, stdout, _ = testkit.Run("flow", "show", "--draft", "--agent", "auditor", "--json")
	if code != contract.ExitOK {
		t.Fatalf("show --draft --agent auditor: exit code %d: %s", code, stdout)
	}
	testkit.Validate(t, "schemas/flow-show.json", stdout)
	if !strings.Contains(stdout, `"instruction":"Найти уязвимости в изменениях задачи.\n","model":"strong","purpose":`) {
		t.Errorf("show --draft --agent auditor with a model: %s", stdout)
	}

	// The commands of checks of a stage and their time pass the contract;
	// without the field timeout the time is the default. A stage without
	// commands has neither field.
	testkit.WriteDraft(t, p, map[string]string{
		"stages/implementation.yaml": "title: Реализация\nexit: изменения сделаны, тесты проходят\nexecutor: orchestrator\n" +
			"checks:\n  - go vet ./...\n  - go test ./...\n",
	})
	for stage, want := range map[string]string{
		"implementation": `"checks":["go vet ./...","go test ./..."],"executor":"orchestrator",`,
		"merge":          `[{"executor":"operator",`,
	} {
		code, stdout, _ = testkit.Run("flow", "show", "--draft", "--stage", stage, "--json")
		if code != contract.ExitOK {
			t.Fatalf("show --draft --stage %s: exit code %d: %s", stage, code, stdout)
		}
		testkit.Validate(t, "schemas/flow-show.json", stdout)
		if !strings.Contains(stdout, want) || strings.Contains(stdout, `"timeout"`) != (stage == "implementation") ||
			stage == "implementation" && !strings.Contains(stdout, `"timeout":"10m","title":"Реализация"`) {
			t.Errorf("show --draft --stage %s: %s", stage, stdout)
		}
	}

	_, stdout, _ = testkit.Run("flow", "discard", "--json")
	testkit.Validate(t, "schemas/flow-discard.json", stdout)
	if want := `{"dir":` + testkit.JSONString(p.Dir) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("discard: %s, want %s", stdout, want)
	}

	// Every event of the flow matches its schema: the shared subagents of
	// the shop were applied by flow apply too.
	_, stdout, _ = testkit.Run("events")
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var e contract.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(e.Type, "flow.") {
			continue
		}
		data, _ := json.Marshal(e.Data)
		testkit.Validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type)
	}
	if got := strings.Join(types, " "); got != "flow.applied flow.applied flow.draft_discarded" {
		t.Errorf("events: %s", got)
	}
}

func TestFlowDraftInvalid(t *testing.T) {
	p := testkit.ShopFlow(t)
	testkit.WriteDraft(t, p, map[string]string{
		"stages/security.yaml": "title: Безопасность\nexecutor: auditor\n",
		"stages/security.md":   "Проверить безопасность.\n",
		"agents/auditor.yaml":  "purpose: проверка безопасности изменений\ncapabilities: [read]\n",
	})
	noExit := msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "security"), "exit")
	noInstruction := msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjAgent, "auditor"))
	want := testkit.Lines(msg.Text(msg.ErrDraftInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems),
		"  "+noExit, "  "+noInstruction)
	for _, args := range [][]string{{"flow", "show", "--draft"}, {"flow", "apply"}, {"flow", "show", "--draft", "--stage", "security"}} {
		code, stdout, stderr := testkit.Run(args...)
		if code != contract.ExitError || stdout != "" || stderr != want {
			t.Errorf("%v: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", args, code, stdout, stderr, want)
		}
	}
	// Nothing changed: the draft is there.
	if _, stdout, _ := testkit.Run("flow", "show"); !strings.Contains(stdout, "\n"+msg.Text(msg.FlowDraftOpened)+"\n") {
		t.Errorf("show after a refused apply:\n%s", stdout)
	}

	code, stdout, _ := testkit.Run("flow", "apply", "--json")
	testkit.Validate(t, "schemas/error.json", stdout)
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	testkit.Validate(t, "schemas/flow-draft-invalid.json", string(out.Error.Details))
	wantDetails := `{"dir":` + testkit.JSONString(p.Dir) + `,"problems":[` +
		`{"code":"missing_field","file":"stages/security.yaml","message":` + testkit.JSONString(noExit) + `},` +
		`{"code":"missing_instruction","file":"agents/auditor.yaml","message":` + testkit.JSONString(noInstruction) + `}],"project":"shop"}`
	if code != contract.ExitError || out.Error.Code != contract.CodeFlowDraftInvalid || string(out.Error.Details) != wantDetails {
		t.Errorf("--json: exit code %d, details:\n%s\nwant:\n%s", code, out.Error.Details, wantDetails)
	}
}

// TestFlowInvalid checks an active flow that this Gentry finds problems in,
// as one applied by a later Gentry that knows more fields.
func TestFlowInvalid(t *testing.T) {
	p := testkit.ShopFlow(t)
	testkit.WriteDraft(t, p, map[string]string{"flow.yaml": "on_take: x\n"})
	gittest.Run(t, filepath.Dir(p.Shared), "add", "--all")
	gittest.Run(t, filepath.Dir(p.Shared), "commit", "--quiet", "-am", "Apply the flow of shop\n\nGentry: flow apply shop")
	code, stdout, stderr := testkit.Run("flow", "show")
	want := testkit.Lines(msg.Text(msg.ErrFlowInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems),
		"  "+msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjCommon), "on_take"))
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ = testkit.Run("flow", "show", "--json")
	var out struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	testkit.Validate(t, "schemas/flow-invalid.json", string(out.Error.Details))
	if !strings.Contains(string(out.Error.Details), `"project":"shop"`) {
		t.Errorf("details: %s", out.Error.Details)
	}
}

// TestFlowBypass checks a change of the flow committed around Gentry.
func TestFlowBypass(t *testing.T) {
	p := testkit.ShopFlow(t)
	process := filepath.Dir(p.Shared)
	// Without problems it is taken as it is.
	testkit.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main вручную.\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand")
	if code, _, stderr := testkit.Run("flow", "show", "--stage", "merge"); code != contract.ExitOK || stderr != "" {
		t.Errorf("a valid change: exit code %d, stderr:\n%s", code, stderr)
	}
	// With problems it becomes a draft, and the checked flow is active.
	testkit.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand again")
	code, stdout, stderr := testkit.Run("flow", "show", "--stage", "review")
	want := testkit.Lines(msg.Text(msg.SyncFlowRestored, "shop"), "", msg.Text(msg.FlowProblems),
		"  "+msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "review"), "exit"), "",
		cli.HintFor(msg.HintFlowDiffProject, "project", "shop"), "")
	if code != contract.ExitOK || stderr != want || !strings.Contains(stdout, msg.Text(msg.FlowExit, "замечания ревью записаны и разобраны")+"\n") {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s\noutput:\n%s", code, stderr, want, stdout)
	}
	if log := gittest.Run(t, process, "log", "-1", "--format=%s"); log != "Restore the flow of shop\n" {
		t.Errorf("commit: %q", log)
	}
	if _, stdout, _ := testkit.Run("flow", "diff"); !strings.Contains(stdout, "\n"+testkit.Lines(msg.Text(msg.DiffStages), change("~", stage("Ревью", "review")))) {
		t.Errorf("diff:\n%s", stdout)
	}
}

// TestFlowExecutorInDraftShared checks a stage of the draft carried out by
// a shared subagent there is only in the draft: the draft shows it and is
// applied with it at once.
func TestFlowExecutorInDraftShared(t *testing.T) {
	p := testkit.ShopFlow(t)
	testkit.WriteFiles(t, p.Shared, map[string]string{
		"tester.yaml": "purpose: тесты\ncapabilities: [read, run]\n",
		"tester.md":   "Прогнать тесты.\n",
	})
	testkit.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: tester\n"})
	want := testkit.Lines(msg.Text(msg.FlowAgent, "tester"), msg.Text(msg.FlowSource, msg.Text(msg.FlowSourceShared)))
	if code, stdout, stderr := testkit.Run("flow", "show", "--draft", "--agent", "tester"); code != contract.ExitOK || !strings.HasPrefix(stdout, want) {
		t.Errorf("show --draft --agent tester: exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	testkit.MustRun(t, "flow", "apply")
	if code, stdout, stderr := testkit.Run("flow", "show", "--agent", "tester"); code != contract.ExitOK || !strings.HasPrefix(stdout, want) {
		t.Errorf("show --agent tester: exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	if _, stdout, _ := testkit.Run("process", "show"); strings.Contains(stdout, msg.Text(msg.ProcessDrafts)) {
		t.Errorf("drafts after apply:\n%s", stdout)
	}
}

// TestFlowShowByFlowDir checks that the project is found by its flow
// directory, so the operator can edit the flow and show it in one place.
func TestFlowShowByFlowDir(t *testing.T) {
	p := testkit.ShopFlow(t)
	t.Chdir(filepath.Join(p.Dir, "stages"))
	if code, stdout, stderr := testkit.Run("flow", "show"); code != contract.ExitOK || !strings.HasPrefix(stdout, msg.Text(msg.FlowProject, "shop")+"\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
}

func TestFlowRefusals(t *testing.T) {
	p := testkit.EmptyShopFlow(t)
	dir := "\n" + msg.Text(msg.FlowDir, p.Dir)
	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no flow", []string{"flow", "show"}, contract.ExitError, msg.Text(msg.ErrFlowNotFound, "shop") + dir},
		// Without a draft of the flow or of the shared subagents every command
		// of the draft refuses alike.
		{"no draft to show", []string{"flow", "show", "--draft"}, contract.ExitError, msg.Text(msg.ErrNoDrafts, "shop") + dir},
		{"no draft to compare", []string{"flow", "diff"}, contract.ExitError, msg.Text(msg.ErrNoDrafts, "shop") + dir},
		{"no draft to apply", []string{"flow", "apply"}, contract.ExitError, msg.Text(msg.ErrNoDrafts, "shop") + dir},
		{"no draft to discard", []string{"flow", "discard"}, contract.ExitError, msg.Text(msg.ErrNoDrafts, "shop") + dir},
		{"edit", []string{"flow", "edit"}, contract.ExitUsage,
			msg.Text(msg.ErrActionUnknown, "edit", "flow") + "\n\n" + cli.HintText(msg.HintActions, "flow")},
		{"two objects", []string{"flow", "show", "--stage", "review", "--agent", "reviewer"}, contract.ExitUsage,
			msg.Text(msg.ErrConflictingFlags, "--stage", "--agent") + "\n\n" + cli.HintText(msg.HintCommandHelp, "flow show")},
		{"empty object", []string{"flow", "show", "--stage="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--stage")},
		{"argument", []string{"flow", "show", "feature"}, contract.ExitUsage, msg.Text(msg.ErrUnexpectedArgs, "flow show")},
		{"unknown project", []string{"flow", "diff", "--project", "cart"}, contract.ExitError,
			msg.Text(msg.ErrProjectNotFound, "cart") + "\n\n" + cli.HintText(msg.HintProjectNotFound)},
		{"empty project", []string{"flow", "diff", "--project="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--project")},
		{"no action", []string{"flow"}, contract.ExitUsage, msg.Text(msg.ErrActionMissing, "flow") + "\n\n" + cli.HintText(msg.HintActions, "flow")},
	}
	for _, tt := range tests {
		code, stdout, stderr := testkit.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	code, stdout, _ := testkit.Run("flow", "show", "--json")
	testkit.Validate(t, "schemas/error.json", stdout)
	if want := `{"error":{"code":"flow_not_found","details":{"dir":` + testkit.JSONString(p.Dir) + `,"project":"shop"},` +
		`"message":` + testkit.JSONString(msg.Text(msg.ErrFlowNotFound, "shop")) + `}}` + "\n"; code != contract.ExitError || stdout != want {
		t.Errorf("no flow --json: exit code %d, %s, want %s", code, stdout, want)
	}

	// Objects the flow or the draft does not have.
	testkit.CopyShopFlow(t, p)
	testkit.MustRun(t, "flow", "apply")
	testkit.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку.\n"})
	tests = []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"unknown stage", []string{"flow", "show", "--stage", "revew"}, contract.ExitError,
			msg.Text(msg.ErrFlowObjectNotFound, "shop", msg.Text(msg.FlowKindStage), "revew") + "\n\n" + cli.HintText(msg.HintFlowObjects)},
		{"unknown agent in the draft", []string{"flow", "show", "--draft", "--agent", "auditor"}, contract.ExitError,
			msg.Text(msg.ErrDraftObjectNotFound, "shop", msg.Text(msg.FlowKindAgent), "auditor") + "\n\n" + cli.HintText(msg.HintDraftObjects)},
	}
	for _, tt := range tests {
		code, stdout, stderr := testkit.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	// Outside any project, without a state store.
	t.Setenv(testkit.HomeEnv, t.TempDir())
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"flow", "show"}, {"flow", "diff"}} {
		if code, _, stderr := testkit.Run(args...); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectUndetermined, "")) {
			t.Errorf("%v outside a project: exit code %d, stderr %q", args, code, stderr)
		}
	}
	if entries, _ := os.ReadDir(os.Getenv(testkit.HomeEnv)); len(entries) != 0 {
		t.Errorf("reading must not create the store: %v", entries)
	}
}

func TestFlowHelp(t *testing.T) {
	_, stdout, _ := testkit.Run("flow", "--help")
	want := testkit.Lines(
		msg.Text(msg.CmdFlowDesc),
		"",
		msg.Text(msg.HelpUsageTitle),
		"  "+msg.Text(msg.HelpGroupUsage, "flow"),
		"",
		msg.Text(msg.HelpActions),
		"  show      "+msg.Text(msg.CmdFlowShowSummary),
		"  diff      "+msg.Text(msg.CmdFlowDiffSummary),
		"  apply     "+msg.Text(msg.CmdFlowApplySummary),
		"  discard   "+msg.Text(msg.CmdFlowDiscardSummary),
		"",
		msg.Text(msg.HelpGroupMore, "flow"),
	)
	if stdout != want {
		t.Errorf("flow --help:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = testkit.Run("flow", "show", "--help")
	if usage := "  gentry flow show [--scenario " + msg.Text(msg.ArgScenario) + " | --stage " + msg.Text(msg.ArgStage) + " | --agent " + msg.Text(msg.ArgAgent) +
		" | --part " + msg.Text(msg.ArgPart) + "] [--draft] [--project " + msg.Text(msg.ArgProjectID) + "] [--json]\n"; !strings.Contains(stdout, usage) {
		t.Errorf("flow show --help:\n%s\nwant within:\n%s", stdout, usage)
	}
}
