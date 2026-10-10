package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

// shopReport is the file of changes of the shop as the texts name it.
const shopReport = "/work/process/shop/changes.md"

// The results of flow diff of the tests are made by hand: the flows are read
// from the files of the flow testdata, the changes of their objects are
// listed, the scenarios are compared by flow.DiffScenario.

// readFlow reads the files of a flow of the shop with the library of the flow
// testdata and, if not nil, its draft draftLibrary.
func readFlow(t *testing.T, files, draftLibrary process.Files) *flow.Result {
	t.Helper()
	res, err := flow.Read(files, testdataFiles(t, "agents", nil), draftLibrary)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// scenarioDiff is how scenario id differs between the flows old and cur,
// with the changes of its stages; unreadable marks a scenario of the draft
// with problems of its own.
func scenarioDiff(old, cur *flow.Flow, id string, stages map[string]string, unreadable bool) flow.ScenarioDiff {
	var before, after *flow.Scenario
	if s, ok := old.Scenario(id); ok {
		before = &s
	}
	if s, ok := cur.Scenario(id); ok {
		after = &s
	}
	d := flow.DiffScenario(before, after, stages)
	d.Unreadable = unreadable
	return d
}

// libraryChange is the change of subagent id of the library from the files
// old to cur; projects are those whose flows name it.
func libraryChange(id, change string, old, cur process.Files, projects ...string) flow.LibraryChange {
	c := flow.LibraryChange{ID: id, Change: change, Projects: append([]string{}, projects...)}
	if a, ok := flow.LibraryAgents(old)[id]; ok {
		c.Old = &a
	}
	if a, ok := flow.LibraryAgents(cur)[id]; ok {
		c.New = &a
	}
	return c
}

// applied is the time a flow or the library was applied in the tests.
var applied = &process.Applied{Commit: "c0ffee", Time: shopTime}

// reviewerRuns is the change of the library of the example of the plan: the
// reviewer runs the tests.
var reviewerRuns = map[string]string{
	"reviewer.yaml": "purpose: ревью изменений задачи — поведение и текст\ncapabilities: [read, search, run]\n",
	"reviewer.md":   "Проверить изменения задачи: поведение, тесты и тексты для оператора.\nЗапустить тесты проекта и приложить результат к замечаниям.\n",
}

// reviewerChanged is the library with reviewerRuns, the subagent named by
// the projects given.
func reviewerChanged(t *testing.T, projects ...string) *flow.LibraryDiffResult {
	lib := testdataFiles(t, "agents", nil)
	return &flow.LibraryDiffResult{Applied: applied, Changes: []flow.LibraryChange{
		libraryChange("reviewer", flow.Modified, lib, testdataFiles(t, "agents", reviewerRuns), projects...),
	}}
}

// change is a change of an object of the flow.
func change(object, id, change string) flow.Change {
	return flow.Change{Object: object, ID: id, Change: change}
}

// diffFromNothing is the draft of the whole flow of the shop, never applied.
func diffFromNothing(t *testing.T) flow.DiffResult {
	old, cur := readFlow(t, process.Files{}, nil), readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	stages := map[string]string{}
	res := flow.DiffResult{Draft: true, Old: old.Read, New: cur.Read}
	res.Changes = append(res.Changes, change(flow.ObjectScenario, "bug", flow.Added), change(flow.ObjectScenario, "feature", flow.Added))
	for _, id := range []string{"branch", "implementation", "merge", "plan-bug", "plan-feature", "review"} {
		res.Changes = append(res.Changes, change(flow.ObjectStage, id, flow.Added))
		stages[id] = flow.Added
	}
	res.Changes = append(res.Changes, change(flow.ObjectPart, "plan-format", flow.Added), change(flow.ObjectPart, "review-checklist", flow.Added))
	res.Scenarios = []flow.ScenarioDiff{
		scenarioDiff(res.Old, res.New, "bug", stages, false),
		scenarioDiff(res.Old, res.New, "feature", stages, false),
	}
	return res
}

// diffExample is the example of the plan: security, a return of the bug
// scenario allowed three times and the reviewer of the library who runs the
// tests.
func diffExample(t *testing.T) flow.DiffResult {
	changes := security(t)
	bug := testdataFiles(t, "shop/flow", nil)["scenarios/bug.yaml"]
	changes["scenarios/bug.yaml"] = strings.Replace(bug, "max_rounds: 2", "max_rounds: 3", 1)
	old := readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	cur := readFlow(t, testdataFiles(t, "shop/flow", changes), testdataFiles(t, "agents", reviewerRuns))
	stages := map[string]string{"review": flow.Modified, "security": flow.Added}
	return flow.DiffResult{Applied: applied, Draft: true, Old: old.Read, New: cur.Read,
		Changes: []flow.Change{
			change(flow.ObjectScenario, "bug", flow.Modified), change(flow.ObjectScenario, "feature", flow.Modified),
			change(flow.ObjectStage, "review", flow.Modified), change(flow.ObjectStage, "security", flow.Added),
			change(flow.ObjectAgent, "auditor", flow.Added),
		},
		Scenarios: []flow.ScenarioDiff{
			scenarioDiff(old.Read, cur.Read, "bug", stages, false),
			scenarioDiff(old.Read, cur.Read, "feature", stages, false),
		},
		Library: reviewerChanged(t, "shop"),
	}
}

// diffIncident is a draft that removes the bug scenario with its stage, adds
// the incident scenario and changes a part.
func diffIncident(t *testing.T) flow.DiffResult {
	old := readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	cur := readFlow(t, testdataFiles(t, "shop/flow", map[string]string{
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
	}), nil)
	stages := map[string]string{"plan-bug": flow.Removed}
	return flow.DiffResult{Applied: applied, Draft: true, Old: old.Read, New: cur.Read,
		Changes: []flow.Change{
			change(flow.ObjectScenario, "bug", flow.Removed), change(flow.ObjectScenario, "incident", flow.Added),
			change(flow.ObjectStage, "plan-bug", flow.Removed), change(flow.ObjectPart, "plan-format", flow.Modified),
		},
		Scenarios: []flow.ScenarioDiff{
			scenarioDiff(old.Read, cur.Read, "bug", stages, false),
			scenarioDiff(old.Read, cur.Read, "incident", stages, false),
		},
	}
}

// diffProblems is a draft with problems: a scenario that is not YAML and a
// stage without its exit.
func diffProblems(t *testing.T) flow.DiffResult {
	old := readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	cur := readFlow(t, testdataFiles(t, "shop/flow", map[string]string{
		"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n",
		"scenarios/bug.yaml": "title: [\n",
	}), nil)
	return flow.DiffResult{Applied: applied, Draft: true, Old: old.Read, New: cur.Read, Problems: cur.Problems,
		Changes: []flow.Change{change(flow.ObjectScenario, "bug", flow.Modified), change(flow.ObjectStage, "review", flow.Modified)},
		Scenarios: []flow.ScenarioDiff{
			scenarioDiff(old.Read, cur.Read, "bug", map[string]string{"review": flow.Modified}, cur.Unreadable["bug"]),
		},
	}
}

// diffLibrary is the draft of the library alone.
func diffLibrary(t *testing.T) flow.DiffResult {
	old := readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	return flow.DiffResult{Applied: applied, Old: old.Read, New: old.Read, Library: reviewerChanged(t, "shop")}
}

// diffUnused is the draft of the library with a subagent of the project of
// the same name in the draft of the flow: the change of the library concerns
// no project.
func diffUnused(t *testing.T) flow.DiffResult {
	old := readFlow(t, testdataFiles(t, "shop/flow", nil), nil)
	cur := readFlow(t, testdataFiles(t, "shop/flow", map[string]string{
		"agents/reviewer.yaml": "purpose: ревью магазина\ncapabilities: [read]\n",
		"agents/reviewer.md":   "Проверить изменения магазина.\n",
	}), testdataFiles(t, "agents", reviewerRuns))
	return flow.DiffResult{Applied: applied, Draft: true, Old: old.Read, New: cur.Read,
		Changes: []flow.Change{change(flow.ObjectAgent, "reviewer", flow.Added)},
		Library: reviewerChanged(t),
	}
}

func TestDiffText(t *testing.T) {
	inUTC(t)
	head := func(applied, library bool) string {
		h := msg.Text(msg.FlowProject, "shop") + "\n"
		if applied {
			h += msg.Text(msg.FlowAppliedAt, "2026-10-09 12:30") + "\n"
		} else {
			h += msg.Text(msg.FlowAppliedAt, noValue) + "\n"
		}
		if library {
			h += msg.Text(msg.LibraryAppliedAt, "2026-10-09 12:30") + "\n"
		}
		return h + "Файл изменений: /work/process/shop/changes.md\n\n"
	}
	legend := "Обозначения: + добавлено, ~ изменено, − удалено.\n\n"
	tests := []struct {
		name       string
		res        flow.DiffResult
		body       string
		cli, agent []string // the hints
	}{
		{"from nothing", diffFromNothing(t), head(false, false) + lines(
			"Сценарии:",
			"  + Баг: Ветка → План бага → Реализация → Ревью → Слияние",
			"    + возврат «Ревью → Реализация»: если ревью выявило существенные замечания, до 2 раз",
			"  + Фича: Ветка → План фичи → Реализация → Ревью → Слияние",
			"    + возврат «Ревью → Реализация»: если ревью выявило существенные замечания, до 3 раз",
			"",
			"Этапы:",
			"  + Ветка (branch)",
			"  + Реализация (implementation)",
			"  + Слияние (merge)",
			"  + План бага (plan-bug)",
			"  + План фичи (plan-feature)",
			"  + Ревью (review)",
			"",
			"Фрагменты:",
			"  + plan-format",
			"  + review-checklist",
			"",
		) + legend,
			[]string{hintLineOf(msg.HintFlowApply, "gentry flow apply")},
			[]string{hintLineOf(msg.HintFlowApply, "flow_apply")}},
		{"example of the plan", diffExample(t), head(true, true) + lines(
			msg.Text(msg.DiffScenarios),
			"  Баг: Ветка → План бага → Реализация → ~ Ревью → Слияние",
			"    ~ возврат «Ревью → Реализация»: до 3 раз вместо 2",
			"  Фича: Ветка → План фичи → Реализация → ~ Ревью → + Безопасность → Слияние",
			"",
			msg.Text(msg.DiffStages),
			"  ~ Ревью (review)",
			"  + Безопасность (security)",
			"",
			"Субагенты проекта:",
			"  + auditor",
			"",
			"Субагенты библиотеки:",
			"  ~ reviewer: касается проектов shop",
			"",
		) + legend,
			[]string{hintLineOf(msg.HintDiffLibraryApply, "gentry library apply"), hintLineOf(msg.HintFlowApply, "gentry flow apply")},
			[]string{hintLineOf(msg.HintDiffLibraryApply, "library_apply"), hintLineOf(msg.HintFlowApply, "flow_apply")}},
		{"scenarios added and removed", diffIncident(t), head(true, false) + lines(
			msg.Text(msg.DiffScenarios),
			"  − Баг",
			"  + Инцидент: Ветка → Реализация",
			"    + переход «Реализация → План фичи»: если ошибка воспроизводится",
			"    + переход «Реализация → Слияние»: если ошибка не воспроизводится",
			"",
			msg.Text(msg.DiffStages),
			"  − План бага (plan-bug)",
			"",
			msg.Text(msg.DiffParts),
			"  ~ plan-format",
			"",
		) + legend,
			[]string{hintLineOf(msg.HintFlowApply, "gentry flow apply")},
			[]string{hintLineOf(msg.HintFlowApply, "flow_apply")}},
		{"draft with problems", diffProblems(t), head(true, false) + lines(
			msg.Text(msg.DiffScenarios),
			"  bug: в сценарии есть ошибки",
			"",
			msg.Text(msg.DiffStages),
			"  ~ Ревью (review)",
			"",
			msg.Text(msg.FlowProblems),
			"  "+msg.Text(msg.ProblemSyntax, msg.Text(msg.FlowObjScenario, "bug")),
			"  "+msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "review"), "exit"),
			"",
		) + legend,
			[]string{hintLineOf(msg.HintFlowShowDraft, "gentry flow show --draft")},
			[]string{hintLineOf(msg.HintFlowShowDraft, "flow_show (draft)")}},
		{"library alone", diffLibrary(t), head(true, true) + lines(
			"Флоу проекта не изменён.",
			"",
			msg.Text(msg.DiffLibraryAgents),
			"  ~ reviewer: "+msg.Text(msg.DiffUsedBy, "shop"),
			"",
		) + legend,
			[]string{hintLineOf(msg.HintDiffLibraryApply, "gentry library apply")},
			[]string{hintLineOf(msg.HintDiffLibraryApply, "library_apply")}},
		{"library unused", diffUnused(t), head(true, true) + lines(
			msg.Text(msg.DiffAgents),
			"  + reviewer",
			"",
			msg.Text(msg.DiffLibraryAgents),
			"  ~ reviewer: не используется ни одним проектом",
			"",
		) + legend,
			[]string{hintLineOf(msg.HintDiffLibraryApply, "gentry library apply"), hintLineOf(msg.HintFlowApply, "gentry flow apply")},
			[]string{hintLineOf(msg.HintDiffLibraryApply, "library_apply"), hintLineOf(msg.HintFlowApply, "flow_apply")}},
	}
	for _, tt := range tests {
		d := diffView{project: "shop", res: tt.res}
		x := d.extra()
		wantText(t, tt.name, func(p *page) { diffText(p, flowDiffOutput("shop", tt.res, shopReport, nil), x) }, tt.body+lines(tt.cli...), tt.body+lines(tt.agent...))
	}
}

// TestDiffReportText checks the file of changes of flow diff.
func TestDiffReportText(t *testing.T) {
	inUTC(t)
	d := diffView{project: "shop", res: diffExample(t)}
	added, modified := msg.Text(msg.FlowChangeAdded), msg.Text(msg.FlowChangeModified)
	want := "# Изменения флоу проекта shop\n\n" +
		msg.Text(msg.FlowProject, "shop") + "  \n" + msg.Text(msg.FlowAppliedAt, "2026-10-09 12:30") + "  \n" +
		msg.Text(msg.LibraryAppliedAt, "2026-10-09 12:30") + "  \nОшибки черновика: нет  \nФайл сформирован: 2026-10-09 12:30\n\n" +
		hintLineOf(msg.HintDiffLibraryApply, "gentry library apply") + "  \n" + hintLineOf(msg.HintFlowApply, "gentry flow apply") + "\n" + `
## Сводка

| ` + msg.Text(msg.ColObject) + ` | ` + msg.Text(msg.ColChange) + ` |
| --- | --- |
| Сценарий «Баг» (bug) | ` + modified + ` |
| Сценарий «Фича» (feature) | ` + modified + ` |
| Этап «Ревью» (review) | ` + modified + ` |
| Этап «Безопасность» (security) | ` + added + ` |
| ` + msg.Text(msg.FlowObjAgent, "auditor") + ` | ` + added + ` |
| ` + msg.Text(msg.FlowObjLibraryAgent, "reviewer") + ` | ` + modified + `; ` + msg.Text(msg.DiffUsedBy, "shop") + ` |

Обозначения на схемах: зелёный — добавлено, жёлтый — изменено, красный пунктир — удалено. Сплошная стрелка — путь по умолчанию, пунктирная — переход с условием.

## Сценарий «Баг» (bug)

` + "```mermaid" + `
flowchart TD
    n1["Ветка"]
    n2["План бага"]
    n3["Реализация"]
    n4["Ревью<br/>исполнитель: reviewer"]
    n5["Слияние<br/>исполнитель: ` + msg.Text(msg.SchemaOperator) + `"]
    fin(("` + msg.Text(msg.FlowEnd) + `"))
    n1 --> n2
    n2 --> n3
    n3 --> n4
    n4 -.->|"ревью выявило существенные замечания<br/>до 3 раз (было 2)"| n3
    n4 --> n5
    n5 --> fin

    classDef changed fill:#fff4c2,stroke:#b8860b,color:#1b1b1b
    class n4 changed
    linkStyle 3 stroke:#b8860b,stroke-width:2px
` + "```" + `

Изменения:

- Изменён этап «Ревью».
- Изменён ` + msg.Text(msg.DiffReturn, "Ревью", "Реализация") + `: до 3 раз вместо 2.

## Сценарий «Фича» (feature)

` + "```mermaid" + `
flowchart TD
    n1["Ветка"]
    n2["План фичи"]
    n3["Реализация"]
    n4["Ревью<br/>исполнитель: reviewer"]
    n5["Безопасность<br/>исполнитель: auditor"]
    n6["Слияние<br/>исполнитель: ` + msg.Text(msg.SchemaOperator) + `"]
    fin(("` + msg.Text(msg.FlowEnd) + `"))
    n1 --> n2
    n2 --> n3
    n3 --> n4
    n4 -.->|"ревью выявило существенные замечания<br/>до 3 раз"| n3
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
` + "```" + `

Изменения:

- Изменён этап «Ревью».
- Добавлен этап «Безопасность».
- Добавлен ` + msg.Text(msg.DiffTransition, "Ревью", "Безопасность") + `.
- Добавлен ` + msg.Text(msg.DiffTransition, "Безопасность", "Слияние") + `.
- Удалён ` + msg.Text(msg.DiffTransition, "Ревью", "Слияние") + `.

## Этап «Ревью» (review) — ` + modified + `

` + msg.Text(msg.FlowScenarios, "bug, feature") + `

` + msg.Text(msg.FlowInstruction) + `

` + "```diff" + `
-Проверить изменения задачи по списку.
+## Замечания
+Записать замечания ревью и проверить, что каждое исправлено
+или отклонено с обоснованием.
` + "```" + `

## Этап «Безопасность» (security) — ` + added + `

` + lines(msg.Text(msg.FlowExit, "проверка безопасности пройдена")+"  ", msg.Text(msg.FlowExecutor, "auditor")+"  ",
		msg.Text(msg.FlowParts, noValue)+"  ", msg.Text(msg.FlowScenarios, "feature")) + `
` + msg.Text(msg.FlowInstruction) + `

> Проверить изменения на уязвимости.

## ` + msg.Text(msg.FlowObjAgent, "auditor") + ` — ` + added + `

` + lines(msg.Text(msg.FlowPurpose, "проверка безопасности изменений")+"  ",
		msg.Text(msg.FlowCapabilities, "read, search")+"  ", msg.Text(msg.FlowStages, "security")) + `
` + msg.Text(msg.FlowInstruction) + `

> Найти уязвимости в изменениях задачи.

## Библиотека субагентов

### Субагент reviewer — ` + modified + `

Касается проектов: shop

| ` + msg.Text(msg.ColField) + ` | ` + msg.Text(msg.ColWas) + ` | ` + msg.Text(msg.ColNow) + ` |
| --- | --- | --- |
| Возможности | read, search | read, search, run |

` + msg.Text(msg.FlowInstruction) + `

` + "```diff" + `
 Проверить изменения задачи: поведение, тесты и тексты для оператора.
+Запустить тесты проекта и приложить результат к замечаниям.
` + "```" + `
`
	if got := d.report(shopTime); got != want {
		t.Errorf("the file of changes:\n%s\nwant:\n%s", got, want)
	}
}

// TestDiffReportParts checks the parts of the file of changes of scenarios
// added and removed, of a part changed and of a draft with problems.
func TestDiffReportParts(t *testing.T) {
	inUTC(t)
	for _, c := range []struct {
		name  string
		res   flow.DiffResult
		parts []string
	}{
		{"scenarios added and removed", diffIncident(t), []string{
			"\n## " + msg.Text(msg.ReportObjScenario, "Баг", "bug") + " — " + msg.Text(msg.FlowChangeRemoved) + "\n\n```mermaid\n",
			"\n## " + msg.Text(msg.ReportObjScenario, "Инцидент", "incident") + " — " + msg.Text(msg.FlowChangeAdded) + "\n\n```mermaid\n",
			"    n2 -.->|\"ошибка воспроизводится\"| n3\n",
			"\n## " + msg.Text(msg.ReportObjStage, "План бага", "plan-bug") + " — " + msg.Text(msg.FlowChangeRemoved) + "\n\n" + msg.Text(msg.FlowExit, ""),
			"\n## " + msg.Text(msg.FlowObjPart, "plan-format") + " — " + msg.Text(msg.FlowChangeModified) + "\n\n" + msg.Text(msg.FlowStages, "plan-feature") + "\n\n" + msg.Text(msg.FlowText) +
				"\n\n```diff\n ## План\n Шаги, проверка и риски.\n+Срок.\n```\n",
		}},
		{"draft with problems", diffProblems(t), []string{
			msg.Text(msg.ReportCreated, "2026-10-09 12:30") + "\n\nОшибки черновика:\n\n- ",
			"\n## " + msg.Text(msg.ReportObjScenario, "bug", "bug") + "\n\nСхема не построена: в сценарии есть ошибки.\n",
			"\n" + hintLineOf(msg.HintFlowShowDraft, "gentry flow show --draft") + "\n",
		}},
	} {
		report := diffView{project: "shop", res: c.res}.report(shopTime)
		for _, part := range c.parts {
			if !strings.Contains(report, part) {
				t.Errorf("%s: the file of changes:\n%s\nwant within:\n%s", c.name, report, part)
			}
		}
	}
}
