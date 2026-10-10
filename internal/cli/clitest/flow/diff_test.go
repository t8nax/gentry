package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
)

// reviewerRuns is the change of the library of the example of the plan: the
// reviewer runs the tests.
func reviewerRuns(t *testing.T, p flow.Places) {
	t.Helper()
	clitest.WriteFiles(t, p.Library, map[string]string{
		"reviewer.yaml": "purpose: ревью изменений задачи — поведение и текст\ncapabilities: [read, search, run]\n",
		"reviewer.md":   "Проверить изменения задачи: поведение, тесты и тексты для оператора.\nЗапустить тесты проекта и приложить результат к замечаниям.\n",
	})
}

// reportOf returns the file of changes of the project at p, its times
// masked.
func reportOf(t *testing.T, p flow.Places) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(p.Dir), "changes.md"))
	if err != nil {
		t.Fatal(err)
	}
	return clitest.Masked(string(b))
}

// The words of flow diff are those of the catalog: the tests of the output in
// package cli state them.

// diffHead is the head of the summary of flow diff of the shop: when the flow
// and, with library, the library were applied, and the file of changes.
func diffHead(applied string, library bool, report string) string {
	h := clitest.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, applied))
	if library {
		h += msg.Text(msg.LibraryAppliedAt, "<время>") + "\n"
	}
	return h + msg.Text(msg.DiffReportFile, report) + "\n\n"
}

// legend is the legend of the summary with its blank line.
var legend = msg.Text(msg.DiffLegend) + "\n\n"

// change is a changed object in the summary: its mark and its name.
func change(mark, name string) string { return "  " + mark + " " + name }

// stage names a stage in the summary of flow diff.
func stage(title, id string) string { return msg.Text(msg.DiffStage, title, id) }

// returnLine is the line of a return of a scenario in the summary.
func returnLine(mark, from, to, details string) string {
	return "    " + mark + " " + msg.Text(msg.DiffReturn, from, to) + ": " + details
}

func TestFlowDiffReport(t *testing.T) {
	p := clitest.ShopFlow(t)
	security(t, p)
	clitest.WriteDraft(t, p, map[string]string{"scenarios/bug.yaml": strings.Replace(readFlowFile(t, p, "scenarios/bug.yaml"), "max_rounds: 2", "max_rounds: 3", 1)})
	reviewerRuns(t, p)
	report := filepath.Join(filepath.Dir(p.Dir), "changes.md")

	_, stdout, _ := clitest.Run("flow", "diff")
	want := diffHead("<время>", true, report) + clitest.Lines(
		msg.Text(msg.DiffScenarios),
		"  "+msg.Text(msg.DiffItem, "Баг", "Ветка → План бага → Реализация → ~ Ревью → Слияние"),
		returnLine("~", "Ревью", "Реализация", msg.Text(msg.DiffInstead, msg.Count(msg.DiffRounds, 3), 2)),
		"  "+msg.Text(msg.DiffItem, "Фича", "Ветка → План фичи → Реализация → ~ Ревью → + Безопасность → Слияние"),
		"",
		msg.Text(msg.DiffStages),
		change("~", stage("Ревью", "review")),
		change("+", stage("Безопасность", "security")),
		"",
		msg.Text(msg.DiffAgents),
		change("+", "auditor"),
		"",
		msg.Text(msg.DiffLibraryAgents),
		change("~", msg.Text(msg.DiffItem, "reviewer", msg.Text(msg.DiffUsedBy, "shop"))),
		"",
	) + legend + clitest.Lines(cli.HintText(msg.HintDiffLibraryApply), cli.HintText(msg.HintFlowApply))
	if clitest.Masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}

	// The file of changes, which TestDiffReportText of package cli states
	// for the same change, as the command writes it.
	scenario := func(title, id string) string { return msg.Text(msg.ReportObjScenario, title, id) }
	stageObj := func(title, id string) string { return msg.Text(msg.ReportObjStage, title, id) }
	modified, added := msg.Text(msg.FlowChangeModified), msg.Text(msg.FlowChangeAdded)
	executor := func(name string) string { return "<br/>" + msg.Text(msg.SchemaExecutor, name) }
	end := msg.Text(msg.FlowEnd)
	condition := "ревью выявило существенные замечания<br/>"
	want = msg.Text(msg.ReportTitle, "shop") + "\n\n" +
		msg.Text(msg.FlowProject, "shop") + "  \n" + msg.Text(msg.FlowAppliedAt, "<время>") + "  \n" +
		msg.Text(msg.LibraryAppliedAt, "<время>") + "  \n" + msg.Text(msg.ReportProblemsNone) + "  \n" +
		msg.Text(msg.ReportCreated, "<время>") + "\n\n" +
		cli.HintText(msg.HintDiffLibraryApply) + "  \n" + cli.HintText(msg.HintFlowApply) + "\n" +
		"\n" + msg.Text(msg.ReportSummary) + "\n\n" +
		"| " + msg.Text(msg.ColObject) + " | " + msg.Text(msg.ColChange) + " |\n" +
		"| --- | --- |\n" +
		"| " + scenario("Баг", "bug") + " | " + modified + " |\n" +
		"| " + scenario("Фича", "feature") + " | " + modified + " |\n" +
		"| " + stageObj("Ревью", "review") + " | " + modified + " |\n" +
		"| " + stageObj("Безопасность", "security") + " | " + added + " |\n" +
		"| " + msg.Text(msg.FlowObjAgent, "auditor") + " | " + added + " |\n" +
		"| " + msg.Text(msg.FlowObjLibraryAgent, "reviewer") + " | " + modified + "; " + msg.Text(msg.DiffUsedBy, "shop") + " |\n" +
		"\n" + msg.Text(msg.ReportLegend) + "\n" +
		"\n" + msg.Text(msg.ReportHeading, scenario("Баг", "bug")) + "\n\n" + "```mermaid" + `
flowchart TD
    n1["Ветка"]
    n2["План бага"]
    n3["Реализация"]
    n4["Ревью` + executor("reviewer") + `"]
    n5["Слияние` + executor(msg.Text(msg.SchemaOperator)) + `"]
    fin(("` + end + `"))
    n1 --> n2
    n2 --> n3
    n3 --> n4
    n4 -.->|"` + condition + msg.Count(msg.DiffRounds, 3) + " " + msg.Text(msg.SchemaWas, 2) + `"| n3
    n4 --> n5
    n5 --> fin

    classDef changed fill:#fff4c2,stroke:#b8860b,color:#1b1b1b
    class n4 changed
    linkStyle 3 stroke:#b8860b,stroke-width:2px
` + "```" + "\n\n" + msg.Text(msg.ReportChanges) + "\n\n" +
		"- " + msg.Text(msg.ReportNodeStageChanged, "Ревью") + "\n" +
		"- " + msg.Text(msg.ReportModified, msg.Text(msg.DiffReturn, "Ревью", "Реализация"), msg.Text(msg.DiffInstead, msg.Count(msg.DiffRounds, 3), 2)) + "\n" +
		"\n" + msg.Text(msg.ReportHeading, scenario("Фича", "feature")) + "\n\n" + "```mermaid" + `
flowchart TD
    n1["Ветка"]
    n2["План фичи"]
    n3["Реализация"]
    n4["Ревью` + executor("reviewer") + `"]
    n5["Безопасность` + executor("auditor") + `"]
    n6["Слияние` + executor(msg.Text(msg.SchemaOperator)) + `"]
    fin(("` + end + `"))
    n1 --> n2
    n2 --> n3
    n3 --> n4
    n4 -.->|"` + condition + msg.Count(msg.DiffRounds, 3) + `"| n3
    n4 --> n5
    n5 --> n6
    n6 --> fin
    n4 --> n6

    classDef added fill:#d4f4dd,stroke:#2e7d32,color:#1b1b1b
    class n5 added
    classDef changed fill:#fff4c2,stroke:#b8860b,color:#1b1b1b
    class n4 changed
    linkStyle 4,5 stroke:#2e7d32,stroke-width:2px
    linkStyle 7 stroke:#c62828,stroke-width:2px,stroke-dasharray:5 5
` + "```" + "\n\n" + msg.Text(msg.ReportChanges) + "\n\n" +
		"- " + msg.Text(msg.ReportNodeStageChanged, "Ревью") + "\n" +
		"- " + msg.Text(msg.ReportNodeAdded, "Безопасность") + "\n" +
		"- " + msg.Text(msg.ReportAdded, msg.Text(msg.DiffTransition, "Ревью", "Безопасность")) + "\n" +
		"- " + msg.Text(msg.ReportAdded, msg.Text(msg.DiffTransition, "Безопасность", "Слияние")) + "\n" +
		"- " + msg.Text(msg.ReportRemoved, msg.Text(msg.DiffTransition, "Ревью", "Слияние")) + "\n" +
		"\n" + msg.Text(msg.ReportHeadingChange, stageObj("Ревью", "review"), modified) + "\n\n" +
		msg.Text(msg.FlowScenarios, "bug, feature") + "\n\n" + msg.Text(msg.FlowInstruction) + "\n\n" + "```diff" + `
-Проверить изменения задачи по списку.
+## Замечания
+Записать замечания ревью и проверить, что каждое исправлено
+или отклонено с обоснованием.
` + "```" + "\n" +
		"\n" + msg.Text(msg.ReportHeadingChange, stageObj("Безопасность", "security"), added) + "\n\n" +
		clitest.Lines(msg.Text(msg.FlowExit, "проверка безопасности пройдена")+"  ", msg.Text(msg.FlowExecutor, "auditor")+"  ",
			msg.Text(msg.FlowParts, clitest.None)+"  ", msg.Text(msg.FlowScenarios, "feature")) +
		"\n" + msg.Text(msg.FlowInstruction) + "\n\n> Проверить изменения на уязвимости.\n" +
		"\n" + msg.Text(msg.ReportHeadingChange, msg.Text(msg.FlowObjAgent, "auditor"), added) + "\n\n" +
		clitest.Lines(msg.Text(msg.FlowPurpose, "проверка безопасности изменений")+"  ", msg.Text(msg.FlowCapabilities, "read, search")+"  ",
			msg.Text(msg.FlowStages, "security")) +
		"\n" + msg.Text(msg.FlowInstruction) + "\n\n> Найти уязвимости в изменениях задачи.\n" +
		"\n" + msg.Text(msg.ReportLibrary) + "\n" +
		"\n" + msg.Text(msg.ReportLibraryAgent, "reviewer", modified) + "\n\n" +
		msg.Text(msg.ReportProjects, "shop") + "\n\n" +
		"| " + msg.Text(msg.ColField) + " | " + msg.Text(msg.ColWas) + " | " + msg.Text(msg.ColNow) + " |\n" +
		"| --- | --- | --- |\n" +
		"| " + msg.Text(msg.ReportFieldCapabilities) + " | read, search | read, search, run |\n" +
		"\n" + msg.Text(msg.FlowInstruction) + "\n\n" + "```diff" + `
 Проверить изменения задачи: поведение, тесты и тексты для оператора.
+Запустить тесты проекта и приложить результат к замечаниям.
` + "```" + "\n"
	if got := reportOf(t, p); got != want {
		t.Errorf("the file of changes:\n%s\nwant:\n%s", got, want)
	}

	// The file is not a part of the process.
	if status := gittest.Run(t, filepath.Dir(p.Library), "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "changes.md") {
		t.Errorf("git sees the file of changes:\n%s", status)
	}
	// The library goes first; its apply removes the file.
	clitest.MustRun(t, "library", "apply")
	if clitest.FileExists(report) {
		t.Error("the file of changes after library apply")
	}
}

// readFlowFile returns file rel of the flow at p.
func readFlowFile(t *testing.T, p flow.Places, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFlowDiffScenarios(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteDraft(t, p, map[string]string{
		"scenarios/bug.yaml":   "",
		"stages/plan-bug.yaml": "",
		"stages/plan-bug.md":   "",
		"scenarios/incident.yaml": "title: Инцидент\nstart: branch\nnodes:\n" +
			"  branch: { stage: branch, next: repro }\n" +
			"  repro:\n    stage: implementation\n    next:\n" +
			"      - to: plan\n        if: ошибка воспроизводится\n" +
			"      - to: merge\n        if: ошибка не воспроизводится\n" +
			"  plan: { stage: plan-feature, next: merge }\n" +
			"  merge: { stage: merge, next: finish }\n",
		"parts/plan-format.md": "## План\nШаги, проверка и риски.\nСрок.\n",
	})
	_, stdout, _ := clitest.Run("flow", "diff")
	transition := func(from, to, condition string) string {
		return "    + " + msg.Text(msg.DiffTransition, from, to) + ": " + msg.Text(msg.DiffCondition, condition)
	}
	want := clitest.Lines(
		msg.Text(msg.DiffScenarios),
		change("−", "Баг"),
		change("+", msg.Text(msg.DiffItem, "Инцидент", "Ветка → Реализация")),
		transition("Реализация", "План фичи", "ошибка воспроизводится"),
		transition("Реализация", "Слияние", "ошибка не воспроизводится"),
		"",
		msg.Text(msg.DiffStages),
		change("−", stage("План бага", "plan-bug")),
		"",
		msg.Text(msg.DiffParts),
		change("~", "plan-format"),
	)
	if !strings.Contains(stdout, "\n\n"+want+"\n") {
		t.Errorf("diff:\n%s\nwant within:\n%s", stdout, want)
	}
	report := reportOf(t, p)
	removed, added, modified := msg.Text(msg.FlowChangeRemoved), msg.Text(msg.FlowChangeAdded), msg.Text(msg.FlowChangeModified)
	for _, want := range []string{
		"\n" + msg.Text(msg.ReportHeadingChange, msg.Text(msg.ReportObjScenario, "Баг", "bug"), removed) + "\n\n```mermaid\n",
		"\n" + msg.Text(msg.ReportHeadingChange, msg.Text(msg.ReportObjScenario, "Инцидент", "incident"), added) + "\n\n```mermaid\n",
		"    n2 -.->|\"ошибка воспроизводится\"| n3\n",
		"\n" + msg.Text(msg.ReportHeadingChange, msg.Text(msg.ReportObjStage, "План бага", "plan-bug"), removed) + "\n\n" + msg.Text(msg.FlowExit, ""),
		"\n" + msg.Text(msg.ReportHeadingChange, msg.Text(msg.FlowObjPart, "plan-format"), modified) + "\n\n" + msg.Text(msg.FlowStages, "plan-feature") +
			"\n\n" + msg.Text(msg.FlowText) + "\n\n```diff\n ## План\n Шаги, проверка и риски.\n+Срок.\n```\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the file of changes:\n%s\nwant within:\n%s", report, want)
		}
	}
}

func TestFlowDiffProblems(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteDraft(t, p, map[string]string{
		"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n",
		"scenarios/bug.yaml": "title: [\n",
	})
	code, stdout, stderr := clitest.Run("flow", "diff")
	want := clitest.Lines(
		msg.Text(msg.DiffScenarios),
		"  "+msg.Text(msg.DiffItem, "bug", msg.Text(msg.DiffScenarioUnreadable)),
		"",
		msg.Text(msg.DiffStages),
		change("~", stage("Ревью", "review")),
		"",
		msg.Text(msg.FlowProblems),
	)
	if code != contract.ExitOK || stderr != "" || !strings.Contains(stdout, "\n\n"+want) ||
		!strings.HasSuffix(stdout, "\n\n"+legend+cli.HintText(msg.HintFlowShowDraft)+"\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	report := reportOf(t, p)
	for _, want := range []string{
		msg.Text(msg.ReportCreated, "<время>") + "\n\n" + msg.Text(msg.ReportProblems) + "\n\n- ",
		"\n" + msg.Text(msg.ReportHeading, msg.Text(msg.ReportObjScenario, "bug", "bug")) + "\n\n" + msg.Text(msg.ReportUnreadable) + "\n",
		"\n" + cli.HintText(msg.HintFlowShowDraft) + "\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the file of changes:\n%s\nwant within:\n%s", report, want)
		}
	}
}

func TestFlowDiffLibrary(t *testing.T) {
	p := clitest.ShopFlow(t)
	reviewerRuns(t, p)
	report := filepath.Join(filepath.Dir(p.Dir), "changes.md")
	_, stdout, _ := clitest.Run("flow", "diff")
	want := diffHead("<время>", true, report) + clitest.Lines(
		msg.Text(msg.DiffFlowUnchanged),
		"",
		msg.Text(msg.DiffLibraryAgents),
		change("~", msg.Text(msg.DiffItem, "reviewer", msg.Text(msg.DiffUsedBy, "shop"))),
		"",
	) + legend + clitest.Lines(cli.HintText(msg.HintDiffLibraryApply))
	if clitest.Masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}

	// A subagent of the project of the same name replaces the one of the
	// library: the change of the library does not concern the project.
	clitest.WriteDraft(t, p, map[string]string{
		"agents/reviewer.yaml": "purpose: ревью магазина\ncapabilities: [read]\n",
		"agents/reviewer.md":   "Проверить изменения магазина.\n",
	})
	stdout = clitest.WantJSON(t, contract.ExitOK, "schemas/flow-diff.json", "flow", "diff")
	var out contract.FlowDiffOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Report != report || out.Library == nil || out.Library.Applied == nil || len(out.Library.Changes) != 1 ||
		out.Library.Changes[0].Id != "reviewer" || !reflect.DeepEqual(out.Library.Changes[0].Projects, []string{}) ||
		len(out.Changes) != 1 {
		t.Errorf("diff: %s", stdout)
	}
	if _, stdout, _ := clitest.Run("flow", "diff"); !strings.Contains(stdout, "\n"+change("~", msg.Text(msg.DiffItem, "reviewer", msg.Text(msg.DiffUnused)))+"\n") {
		t.Errorf("diff:\n%s", stdout)
	}
	// Discarding the library removes the file.
	clitest.MustRun(t, "library", "discard")
	if clitest.FileExists(report) {
		t.Error("the file of changes after library discard")
	}
}
