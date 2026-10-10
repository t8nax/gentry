package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
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
	clitest.WriteDraft(t, p, map[string]string{
		"scenarios/feature.yaml": text,
		"stages/review.md":       "## Замечания\nЗаписать замечания ревью и проверить, что каждое исправлено\nили отклонено с обоснованием.\n",
		"stages/security.yaml":   "title: Безопасность\nexit: проверка безопасности пройдена\nexecutor: auditor\n",
		"stages/security.md":     "Проверить изменения на уязвимости.\n",
		"agents/auditor.yaml":    "purpose: проверка безопасности изменений\ncapabilities: [read, search]\n",
		"agents/auditor.md":      "Найти уязвимости в изменениях задачи.\n",
	})
}

// The words of the flow are those of the catalog: the tests of the output in
// package cli state them.

// none is the mark of a missing value.
var none = msg.Text(msg.ValueNone)

// flowHead is the head of flow show of the shop: its project, when its flow
// was applied, and the flow directory.
func flowHead(applied, dir string) string {
	return clitest.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, applied), msg.Text(msg.FlowDir, dir))
}

func TestFlowShow(t *testing.T) {
	p := clitest.ShopFlow(t)
	code, stdout, stderr := clitest.Run("flow", "show")
	want := flowHead("<время>", p.Dir) + "\n" + clitest.ShopTables + "\n" + cli.HintText(msg.HintFlowStage) + "\n"
	if stdout = clitest.Masked(stdout); code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--scenario", "feature"}, clitest.Lines(
			msg.Text(msg.FlowScenario, "feature"),
			msg.Text(msg.FlowTitle, "Фича"),
			"",
			msg.Text(msg.FlowDefaultPath),
			"  branch → plan → implementation → review → merge → "+msg.Text(msg.FlowEnd),
			"",
			msg.Text(msg.FlowConditional),
			"  "+msg.Text(msg.FlowTransitionLimit, "review", "implementation", msg.Count(msg.FlowRounds, 3), "ревью выявило существенные замечания"),
			"",
		) + clitest.Table(
			[]string{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)},
			[]string{"branch", "branch", "orchestrator"},
			[]string{"plan", "plan-feature", "orchestrator"},
			[]string{"implementation", "implementation", "orchestrator"},
			[]string{"review", "review", "reviewer"},
			[]string{"merge", "merge", "operator"},
		)},
		{[]string{"--stage", "review"}, clitest.Lines(
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
		{[]string{"--stage=merge"}, clitest.Lines(
			msg.Text(msg.FlowStage, "merge"),
			msg.Text(msg.FlowTitle, "Слияние"),
			msg.Text(msg.FlowExecutor, "operator"),
			msg.Text(msg.FlowExit, "ветка задачи влита в main"),
			msg.Text(msg.FlowParts, msg.Text(msg.ValueNone)),
			msg.Text(msg.FlowScenarios, "bug, feature"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Влить ветку задачи в main.",
		)},
		{[]string{"--agent", "reviewer"}, clitest.Lines(
			msg.Text(msg.FlowAgent, "reviewer"),
			msg.Text(msg.FlowSource, msg.Text(msg.FlowSourceLibrary)),
			msg.Text(msg.FlowPurpose, "ревью изменений задачи — поведение и текст"),
			msg.Text(msg.FlowCapabilities, "read, search"),
			msg.Text(msg.FlowStages, "review"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Проверить изменения задачи: поведение, тесты и тексты для оператора.",
		)},
		{[]string{"--part", "review-checklist"}, clitest.Lines(
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
		code, stdout, stderr := clitest.Run(append([]string{"flow", "show"}, tt.args...)...)
		if code != contract.ExitOK || stdout != tt.want {
			t.Errorf("%v: exit code %d, stderr %q, output:\n%s\nwant:\n%s", tt.args, code, stderr, stdout, tt.want)
		}
	}
}

func TestFlowShowConditional(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteDraft(t, p, map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: triage\nnodes:\n" +
		"  triage:\n    stage: plan-bug\n    next:\n      - to: merge\n        if: срочно\n      - to: finish\n        if: |\n          не воспроизводится\n          на main\n" +
		"  merge: { stage: merge, next: finish }\n"})
	_, stdout, stderr := clitest.Run("flow", "show", "--draft", "--scenario", "bug")
	// At a fork the default path stops; the end is named in words.
	want := clitest.Lines(msg.Text(msg.FlowDefaultPath), "  triage", "", msg.Text(msg.FlowConditional),
		"  "+msg.Text(msg.FlowTransition, "triage", "merge", "срочно"),
		"  "+msg.Text(msg.FlowTransition, "triage", msg.Text(msg.FlowEnd), "не воспроизводится на main"), "")
	if !strings.Contains(stdout, want) {
		t.Errorf("stderr %q, output:\n%s\nwant within:\n%s", stderr, stdout, want)
	}
}

func TestFlowDraft(t *testing.T) {
	p := clitest.EmptyShopFlow(t)
	// The flow directory is there from the connection, with the directories
	// of a flow as a prompt.
	for _, d := range []string{"scenarios", "stages", "parts", "agents"} {
		if fi, err := os.Stat(filepath.Join(p.Dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", d, err)
		}
	}
	clitest.CopyShopFlow(t, p)

	report := filepath.Join(filepath.Dir(p.Dir), "changes.md")
	_, stdout, _ := clitest.Run("flow", "diff")
	condition := msg.Text(msg.DiffCondition, "ревью выявило существенные замечания")
	want := diffHead(none, false, report) + clitest.Lines(
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
	) + legend + clitest.Lines(cli.HintText(msg.HintFlowApply))
	if stdout != want {
		t.Errorf("diff from nothing:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = clitest.Run("flow", "show", "--draft")
	want = flowHead(none, p.Dir) + "\n" + clitest.ShopTables + "\n" + cli.HintText(msg.HintFlowApply) + "\n"
	if stdout != want {
		t.Errorf("show --draft:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := clitest.Run("flow", "apply"); clitest.Masked(stdout) != clitest.Lines(msg.Text(msg.FlowApplied), msg.Text(msg.FlowProject, "shop"),
		msg.Text(msg.FlowAppliedAt, "<время>"), "", msg.Text(msg.AgentsFreeSynced, "shop"), msg.Text(msg.AgentsNextSession)) {
		t.Errorf("apply:\n%s", stdout)
	}
	process := filepath.Dir(p.Library)
	if log := gittest.Run(t, process, "log", "-1", "--format=%s|%b"); log != "Apply the flow of shop|Gentry: flow apply shop\n\n" {
		t.Errorf("commit: %q", log)
	}

	// Editing the files makes a draft.
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	_, stdout, _ = clitest.Run("flow", "show")
	if want := flowHead("<время>", p.Dir) + msg.Text(msg.FlowDraftOpened) + "\n\n" + clitest.ShopTables + "\n" + cli.HintText(msg.HintFlowShowDraft) + "\n"; clitest.Masked(stdout) != want {
		t.Errorf("show with a draft:\n%s\nwant:\n%s", stdout, want)
	}
	// Line ends of another editor are no change.
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main.\r\n"})
	code, _, stderr := clitest.Run("flow", "diff")
	if want := clitest.Lines(msg.Text(msg.ErrNoDrafts, "shop"), msg.Text(msg.FlowDir, p.Dir)); code != contract.ExitError || stderr != want {
		t.Errorf("diff without changes: exit code %d, stderr:\n%s", code, stderr)
	}

	security(t, p)
	_, stdout, _ = clitest.Run("flow", "diff")
	want = diffHead("<время>", false, report) + clitest.Lines(
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
	) + legend + clitest.Lines(cli.HintText(msg.HintFlowApply))
	if clitest.Masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}
	if _, err := os.Stat(report); err != nil {
		t.Errorf("the file of changes: %v", err)
	}
	clitest.MustRun(t, "flow", "apply")
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Errorf("the file of changes after apply: %v", err)
	}
	_, stdout, _ = clitest.Run("flow", "show")
	if !strings.Contains(stdout, "\n"+clitest.Table([]string{msg.Text(msg.ColAgent), msg.Text(msg.ColSource), msg.Text(msg.ColStages)},
		[]string{"auditor", msg.Text(msg.FlowSourceProject), "security"},
		[]string{"reviewer", msg.Text(msg.FlowSourceLibrary), "review"})) {
		t.Errorf("show after apply:\n%s", stdout)
	}

	clitest.WriteDraft(t, p, map[string]string{"stages/security.md": "", "parts/extra.md": "Лишнее.\n"})
	clitest.MustRun(t, "flow", "diff")
	if _, stdout, _ := clitest.Run("flow", "discard"); stdout != clitest.Lines(msg.Text(msg.FlowDiscarded), msg.Text(msg.FlowProject, "shop")) {
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
	if code, _, _ := clitest.Run("flow", "show", "--draft"); code != contract.ExitError {
		t.Errorf("a draft after discard")
	}
}

func TestFlowJSON(t *testing.T) {
	p := clitest.EmptyShopFlow(t)
	clitest.CopyShopFlow(t, p)
	_, stdout, _ := clitest.Run("flow", "apply", "--json")
	clitest.Validate(t, "schemas/flow-apply.json", stdout)
	var applied contract.FlowApplyOutput
	if err := json.Unmarshal([]byte(stdout), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Project != "shop" || applied.Sent || len(applied.Applied.Commit) != 40 || applied.Sync != nil ||
		!regexp.MustCompile(`"time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.000Z"`).MatchString(stdout) {
		t.Errorf("apply: %s", stdout)
	}

	security(t, p)
	_, stdout, _ = clitest.Run("flow", "diff", "--json")
	clitest.Validate(t, "schemas/flow-diff.json", stdout)
	want := `{"applied":{"commit":"` + applied.Applied.Commit + `",`
	tail := `"changes":[{"change":"modified","id":"feature","object":"scenario"},` +
		`{"change":"modified","id":"review","object":"stage"},{"change":"added","id":"security","object":"stage"},` +
		`{"change":"added","id":"auditor","object":"agent"}],"project":"shop","report":`
	var diff contract.FlowDiffOutput
	if err := json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, want) || !strings.Contains(stdout, tail) || diff.Library != nil ||
		diff.Report != filepath.Join(filepath.Dir(p.Dir), "changes.md") {
		t.Errorf("diff: %s", stdout)
	}

	code, stdout, _ := clitest.Run("flow", "show", "--json")
	if code != contract.ExitOK {
		t.Fatalf("show: exit code %d: %s", code, stdout)
	}
	clitest.Validate(t, "schemas/flow-show.json", stdout)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Project != "shop" || out.Applied == nil || out.Applied.Commit != applied.Applied.Commit || out.Dir != p.Dir ||
		out.Draft == nil || out.Draft.ConflictDir != nil || len(out.Scenarios) != 2 || len(out.Stages) != 6 || len(out.Parts) != 2 || len(out.Agents) != 1 {
		t.Errorf("show: %s", stdout)
	}
	for _, part := range []string{
		`{"id":"review","next":[{"if":"ревью выявило существенные замечания","max_rounds":3,"to":"implementation"},{"to":"merge"}],"stage":"review"},`,
		`{"executor":"operator","exit":"ветка задачи влита в main","id":"merge","include":[],"instruction":"Влить ветку задачи в main.\n","title":"Слияние"}`,
		`"parts":[{"id":"plan-format","text":`,
		`"agents":[{"capabilities":["read","search"],"id":"reviewer","instruction":"Проверить изменения задачи: поведение, тесты и тексты для оператора.\n","purpose":"ревью изменений задачи — поведение и текст","source":"library"}]`,
		`"draft":{}`,
	} {
		if !strings.Contains(stdout, part) {
			t.Errorf("show: no %s in\n%s", part, stdout)
		}
	}

	// An object narrows the arrays to it and what it refers to.
	_, stdout, _ = clitest.Run("flow", "show", "--draft", "--stage", "security", "--json")
	clitest.Validate(t, "schemas/flow-show.json", stdout)
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Scenarios) != 0 || len(out.Stages) != 1 || len(out.Parts) != 0 || len(out.Agents) != 1 || out.Agents[0].Source != "project" {
		t.Errorf("show --draft --stage security: %s", stdout)
	}
	_, stdout, _ = clitest.Run("flow", "show", "--scenario", "feature", "--json")
	if strings.Contains(stdout, `"plan-bug"`) || !strings.Contains(stdout, `"id":"reviewer"`) {
		t.Errorf("show --scenario feature: %s", stdout)
	}

	_, stdout, _ = clitest.Run("flow", "discard", "--json")
	clitest.Validate(t, "schemas/flow-discard.json", stdout)
	if want := `{"dir":` + clitest.JSONString(p.Dir) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("discard: %s, want %s", stdout, want)
	}

	// Every event of the flow and the library matches its schema.
	_, stdout, _ = clitest.Run("events")
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var e contract.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(e.Type, "flow.") && !strings.HasPrefix(e.Type, "library.") {
			continue
		}
		data, _ := json.Marshal(e.Data)
		clitest.Validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type)
	}
	if got := strings.Join(types, " "); got != "library.applied flow.applied flow.draft_discarded" {
		t.Errorf("events: %s", got)
	}
}

func TestFlowDraftInvalid(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteDraft(t, p, map[string]string{
		"stages/security.yaml": "title: Безопасность\nexecutor: auditor\n",
		"stages/security.md":   "Проверить безопасность.\n",
		"agents/auditor.yaml":  "purpose: проверка безопасности изменений\ncapabilities: [read]\n",
	})
	noExit := msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "security"), "exit")
	noInstruction := msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjAgent, "auditor"))
	want := clitest.Lines(msg.Text(msg.ErrDraftInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems),
		"  "+noExit, "  "+noInstruction)
	for _, args := range [][]string{{"flow", "show", "--draft"}, {"flow", "apply"}, {"flow", "show", "--draft", "--stage", "security"}} {
		code, stdout, stderr := clitest.Run(args...)
		if code != contract.ExitError || stdout != "" || stderr != want {
			t.Errorf("%v: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", args, code, stdout, stderr, want)
		}
	}
	// Nothing changed: the draft is there.
	if _, stdout, _ := clitest.Run("flow", "show"); !strings.Contains(stdout, "\n"+msg.Text(msg.FlowDraftOpened)+"\n") {
		t.Errorf("show after a refused apply:\n%s", stdout)
	}

	code, stdout, _ := clitest.Run("flow", "apply", "--json")
	clitest.Validate(t, "schemas/error.json", stdout)
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	clitest.Validate(t, "schemas/flow-draft-invalid.json", string(out.Error.Details))
	wantDetails := `{"dir":` + clitest.JSONString(p.Dir) + `,"problems":[` +
		`{"code":"missing_field","file":"stages/security.yaml","message":` + clitest.JSONString(noExit) + `},` +
		`{"code":"missing_instruction","file":"agents/auditor.yaml","message":` + clitest.JSONString(noInstruction) + `}],"project":"shop"}`
	if code != contract.ExitError || out.Error.Code != contract.CodeFlowDraftInvalid || string(out.Error.Details) != wantDetails {
		t.Errorf("--json: exit code %d, details:\n%s\nwant:\n%s", code, out.Error.Details, wantDetails)
	}
}

// TestFlowInvalid checks an active flow that this Gentry finds problems in,
// as one applied by a later Gentry that knows more fields.
func TestFlowInvalid(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteDraft(t, p, map[string]string{"flow.yaml": "on_take: x\n"})
	gittest.Run(t, filepath.Dir(p.Library), "add", "--all")
	gittest.Run(t, filepath.Dir(p.Library), "commit", "--quiet", "-am", "Apply the flow of shop\n\nGentry: flow apply shop")
	code, stdout, stderr := clitest.Run("flow", "show")
	want := clitest.Lines(msg.Text(msg.ErrFlowInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems),
		"  "+msg.Text(msg.ProblemUnsupportedField, msg.Text(msg.FlowObjCommon), "on_take"))
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ = clitest.Run("flow", "show", "--json")
	var out struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	clitest.Validate(t, "schemas/flow-invalid.json", string(out.Error.Details))
	if !strings.Contains(string(out.Error.Details), `"project":"shop"`) {
		t.Errorf("details: %s", out.Error.Details)
	}
}

// TestFlowBypass checks a change of the flow committed around Gentry.
func TestFlowBypass(t *testing.T) {
	p := clitest.ShopFlow(t)
	process := filepath.Dir(p.Library)
	// Without problems it is taken as it is.
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main вручную.\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand")
	if code, _, stderr := clitest.Run("flow", "show", "--stage", "merge"); code != contract.ExitOK || stderr != "" {
		t.Errorf("a valid change: exit code %d, stderr:\n%s", code, stderr)
	}
	// With problems it becomes a draft, and the checked flow is active.
	clitest.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand again")
	code, stdout, stderr := clitest.Run("flow", "show", "--stage", "review")
	want := clitest.Lines(msg.Text(msg.SyncFlowRestored, "shop"), "", msg.Text(msg.FlowProblems),
		"  "+msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "review"), "exit"), "",
		cli.HintFor(msg.HintFlowDiffProject, "project", "shop"), "")
	if code != contract.ExitOK || stderr != want || !strings.Contains(stdout, msg.Text(msg.FlowExit, "замечания ревью записаны и разобраны")+"\n") {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s\noutput:\n%s", code, stderr, want, stdout)
	}
	if log := gittest.Run(t, process, "log", "-1", "--format=%s"); log != "Restore the flow of shop\n" {
		t.Errorf("commit: %q", log)
	}
	if _, stdout, _ := clitest.Run("flow", "diff"); !strings.Contains(stdout, "\n"+clitest.Lines(msg.Text(msg.DiffStages), change("~", stage("Ревью", "review")))) {
		t.Errorf("diff:\n%s", stdout)
	}
}

// TestFlowExecutorInDraftLibrary checks a stage carried out by a subagent of
// the library that is not applied yet.
func TestFlowExecutorInDraftLibrary(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteFiles(t, p.Library, map[string]string{
		"tester.yaml": "purpose: тесты\ncapabilities: [read, run]\n",
		"tester.md":   "Прогнать тесты.\n",
	})
	clitest.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: tester\n"})
	code, _, stderr := clitest.Run("flow", "apply")
	if want := "  " + msg.Text(msg.ProblemExecutorInDraftLibrary, msg.Text(msg.FlowObjStage, "review"), "tester") + "\n"; code != contract.ExitError || !strings.HasSuffix(stderr, want) {
		t.Errorf("exit code %d, stderr:\n%s", code, stderr)
	}
	clitest.MustRun(t, "library", "apply")
	clitest.MustRun(t, "flow", "apply")
}

// TestFlowShowByFlowDir checks that the project is found by its flow
// directory, so the operator can edit the flow and show it in one place.
func TestFlowShowByFlowDir(t *testing.T) {
	p := clitest.ShopFlow(t)
	t.Chdir(filepath.Join(p.Dir, "stages"))
	if code, stdout, stderr := clitest.Run("flow", "show"); code != contract.ExitOK || !strings.HasPrefix(stdout, msg.Text(msg.FlowProject, "shop")+"\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
}

func TestFlowRefusals(t *testing.T) {
	p := clitest.EmptyShopFlow(t)
	dir := "\n" + msg.Text(msg.FlowDir, p.Dir)
	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no flow", []string{"flow", "show"}, contract.ExitError, msg.Text(msg.ErrFlowNotFound, "shop") + dir},
		{"no draft to show", []string{"flow", "show", "--draft"}, contract.ExitError, msg.Text(msg.ErrDraftNotFound, "shop") + dir},
		// flow diff builds the refusal of no drafts from the process
		// repository: no test of package cli reaches it, this test states its
		// words.
		{"no draft to compare", []string{"flow", "diff"}, contract.ExitError, "Ни у флоу проекта shop, ни у библиотеки субагентов нет черновика." + dir},
		{"no draft to apply", []string{"flow", "apply"}, contract.ExitError, msg.Text(msg.ErrDraftNotFound, "shop") + dir},
		{"no draft to discard", []string{"flow", "discard"}, contract.ExitError, msg.Text(msg.ErrDraftNotFound, "shop") + dir},
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
		code, stdout, stderr := clitest.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	code, stdout, _ := clitest.Run("flow", "show", "--json")
	clitest.Validate(t, "schemas/error.json", stdout)
	if want := `{"error":{"code":"flow_not_found","details":{"dir":` + clitest.JSONString(p.Dir) + `,"project":"shop"},` +
		`"message":` + clitest.JSONString(msg.Text(msg.ErrFlowNotFound, "shop")) + `}}` + "\n"; code != contract.ExitError || stdout != want {
		t.Errorf("no flow --json: exit code %d, %s, want %s", code, stdout, want)
	}

	// Objects the flow or the draft does not have.
	clitest.CopyShopFlow(t, p)
	clitest.MustRun(t, "flow", "apply")
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку.\n"})
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
		code, stdout, stderr := clitest.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	// Outside any project, without a state store.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"flow", "show"}, {"flow", "diff"}} {
		if code, _, stderr := clitest.Run(args...); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectUndetermined, "")) {
			t.Errorf("%v outside a project: exit code %d, stderr %q", args, code, stderr)
		}
	}
	if entries, _ := os.ReadDir(os.Getenv(home.EnvVar)); len(entries) != 0 {
		t.Errorf("reading must not create the store: %v", entries)
	}
}

func TestFlowHelp(t *testing.T) {
	_, stdout, _ := clitest.Run("flow", "--help")
	want := `Показать или изменить флоу проекта.
Изменения в папке флоу — черновик; действующим он становится после применения.

Использование:
  gentry flow <действие> [аргументы] [флаги]

Действия:
  show      Показать флоу проекта
  diff      Показать изменения черновика
  apply     Применить черновик флоу
  discard   Отменить изменения флоу

Посмотреть описание действия: gentry flow <действие> --help
`
	if stdout != want {
		t.Errorf("flow --help:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = clitest.Run("flow", "show", "--help")
	if usage := "  gentry flow show [--scenario <сценарий> | --stage <этап> | --agent <субагент> | --part <фрагмент>] [--draft] [--project <идентификатор>] [--json]\n"; !strings.Contains(stdout, usage) {
		t.Errorf("flow show --help:\n%s\nwant within:\n%s", stdout, usage)
	}
}
