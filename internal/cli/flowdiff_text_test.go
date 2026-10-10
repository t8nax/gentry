package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

// shopReport is the file of changes of the shop as the texts name it.
const shopReport = "/work/process/shop/changes.md"

// shopDiff is what flow diff finds in the shop: the flow of the flow testdata
// active, its draft with the changes draft and the library draft with the
// changes library, as flow.DiffDraft compares them. applied tells that the
// flow is applied; without changes of the flow it has no draft.
func shopDiff(t *testing.T, applied bool, draft, library map[string]string) flow.DiffResult {
	t.Helper()
	active, lib := testdataFiles(t, "shop/flow", nil), testdataFiles(t, "agents", nil)
	if !applied {
		active = process.Files{}
	}
	old, err := flow.Read(active, lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := flow.DiffResult{Old: old.Read, New: old.Read}
	if applied {
		out.Applied = &process.Applied{Commit: "c0ffee", Time: shopTime}
	}
	if draft != nil {
		working := testdataFiles(t, "shop/flow", draft)
		res, err := flow.Read(working, lib, testdataFiles(t, "agents", library))
		if err != nil {
			t.Fatal(err)
		}
		out.Draft, out.New, out.Problems = true, res.Read, res.Problems
		out.Changes = flow.Diff(flow.Snapshot{Files: active}, flow.Snapshot{Files: res.Snapshot.Files})
		stages := map[string]string{}
		for _, c := range out.Changes {
			if c.Object == flow.ObjectStage {
				stages[c.ID] = c.Change
			}
		}
		for _, c := range out.Changes {
			if c.Object != flow.ObjectScenario {
				continue
			}
			var before, after *flow.Scenario
			if s, ok := out.Old.Scenario(c.ID); ok {
				before = &s
			}
			if s, ok := out.New.Scenario(c.ID); ok {
				after = &s
			}
			if before == nil && after == nil {
				continue
			}
			d := flow.DiffScenario(before, after, stages)
			d.Unreadable = res.Unreadable[c.ID]
			out.Scenarios = append(out.Scenarios, d)
		}
	}
	if library != nil {
		working := testdataFiles(t, "agents", library)
		in := func(f process.Files) flow.Snapshot {
			s := flow.Snapshot{Files: map[string]string{}}
			for p, text := range f {
				s.Files["agents/"+p] = text
			}
			return s
		}
		before, after := flow.LibraryAgents(lib), flow.LibraryAgents(working)
		lr := flow.LibraryDiffResult{Applied: &process.Applied{Commit: "c0ffee", Time: shopTime}}
		for _, c := range flow.Diff(in(lib), in(working)) {
			lc := flow.LibraryChange{ID: c.ID, Change: c.Change, Projects: []string{}}
			if a, ok := before[c.ID]; ok {
				lc.Old = &a
			}
			if a, ok := after[c.ID]; ok {
				lc.New = &a
			}
			if len(out.New.Users(c.ID)) > 0 {
				lc.Projects = append(lc.Projects, "shop")
			}
			lr.Changes = append(lr.Changes, lc)
		}
		out.Library = &lr
	}
	return out
}

// diffOutput is the output of flow diff for the result res, as runFlowDiff
// builds it.
func diffOutput(res flow.DiffResult) contract.FlowDiffOutput {
	out := contract.FlowDiffOutput{Project: "shop", Applied: appliedJSON(res.Applied), Changes: []contract.FlowChange{}, Report: shopReport}
	for _, c := range res.Changes {
		cc := contract.FlowChange{Object: contract.FlowDiffOutputChangesElemObject(c.Object), Change: contract.FlowDiffOutputChangesElemChange(c.Change)}
		if c.ID != "" {
			cc.Id = ptr(c.ID)
		}
		out.Changes = append(out.Changes, cc)
	}
	if lib := res.Library; lib != nil {
		out.Library = &contract.FlowDiffOutputLibrary{Applied: appliedJSON(lib.Applied), Changes: []contract.FlowDiffLibraryChange{}}
		for _, c := range lib.Changes {
			out.Library.Changes = append(out.Library.Changes, contract.FlowDiffLibraryChange{
				Id: c.ID, Change: contract.FlowDiffOutputLibraryChangesElemChange(c.Change), Projects: c.Projects,
			})
		}
	}
	return out
}

// reviewerRuns is the change of the library of the example of the plan: the
// reviewer runs the tests.
var reviewerRuns = map[string]string{
	"reviewer.yaml": "purpose: ревью изменений задачи — поведение и текст\ncapabilities: [read, search, run]\n",
	"reviewer.md":   "Проверить изменения задачи: поведение, тесты и тексты для оператора.\nЗапустить тесты проекта и приложить результат к замечаниям.\n",
}

// example is the change of the example of the plan: security, and a return
// of the bug scenario allowed three times.
func example(t *testing.T) map[string]string {
	changes := security(t)
	bug := testdataFiles(t, "shop/flow", nil)["scenarios/bug.yaml"]
	changes["scenarios/bug.yaml"] = strings.Replace(bug, "max_rounds: 2", "max_rounds: 3", 1)
	return changes
}

func TestDiffText(t *testing.T) {
	inUTC(t)
	incident := map[string]string{
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
	}
	problems := map[string]string{
		"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n",
		"scenarios/bug.yaml": "title: [\n",
	}
	unused := map[string]string{
		"agents/reviewer.yaml": "purpose: ревью магазина\ncapabilities: [read]\n",
		"agents/reviewer.md":   "Проверить изменения магазина.\n",
	}
	head := func(applied, library bool) string {
		h := msg.Text(msg.FlowProject, "shop") + "\n"
		if applied {
			h += msg.Text(msg.FlowAppliedAt, "2026-10-09 12:30") + "\n"
		} else {
			h += msg.Text(msg.FlowAppliedAt, "—") + "\n"
		}
		if library {
			h += "Библиотека применена: 2026-10-09 12:30\n"
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
		{"from nothing", shopDiff(t, false, map[string]string{}, nil), head(false, false) + lines(
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
		{"example of the plan", shopDiff(t, true, example(t), reviewerRuns), head(true, true) + lines(
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
		{"scenarios added and removed", shopDiff(t, true, incident, nil), head(true, false) + lines(
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
		{"draft with problems", shopDiff(t, true, problems, nil), head(true, false) + lines(
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
		{"library alone", shopDiff(t, true, nil, reviewerRuns), head(true, true) + lines(
			"Флоу проекта не изменён.",
			"",
			msg.Text(msg.DiffLibraryAgents),
			"  ~ reviewer: "+msg.Text(msg.DiffUsedBy, "shop"),
			"",
		) + legend,
			[]string{hintLineOf(msg.HintDiffLibraryApply, "gentry library apply")},
			[]string{hintLineOf(msg.HintDiffLibraryApply, "library_apply")}},
		{"library unused", shopDiff(t, true, unused, reviewerRuns), head(true, true) + lines(
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
		wantText(t, tt.name, func(p *page) { diffText(p, diffOutput(tt.res), x) }, tt.body+lines(tt.cli...), tt.body+lines(tt.agent...))
	}
}

// TestDiffReportText checks the file of changes of flow diff.
func TestDiffReportText(t *testing.T) {
	inUTC(t)
	d := diffView{project: "shop", res: shopDiff(t, true, example(t), reviewerRuns)}
	want := "# Изменения флоу проекта shop\n\n" +
		msg.Text(msg.FlowProject, "shop") + "  \n" + msg.Text(msg.FlowAppliedAt, "2026-10-09 12:30") + "  \n" +
		msg.Text(msg.LibraryAppliedAt, "2026-10-09 12:30") + "  \nОшибки черновика: нет  \nФайл сформирован: 2026-10-09 12:30\n\n" +
		hintLineOf(msg.HintDiffLibraryApply, "gentry library apply") + "  \n" + hintLineOf(msg.HintFlowApply, "gentry flow apply") + "\n" + `
## Сводка

| ` + msg.Text(msg.ColObject) + ` | ` + msg.Text(msg.ColChange) + ` |
| --- | --- |
| Сценарий «Баг» (bug) | изменён |
| Сценарий «Фича» (feature) | изменён |
| Этап «Ревью» (review) | изменён |
| Этап «Безопасность» (security) | добавлен |
| ` + msg.Text(msg.FlowObjAgent, "auditor") + ` | добавлен |
| ` + msg.Text(msg.FlowObjLibraryAgent, "reviewer") + ` | изменён; ` + msg.Text(msg.DiffUsedBy, "shop") + ` |

Обозначения на схемах: зелёный — добавлено, жёлтый — изменено, красный пунктир — удалено. Сплошная стрелка — путь по умолчанию, пунктирная — переход с условием.

## Сценарий «Баг» (bug)

` + "```mermaid" + `
flowchart TD
    n1["Ветка"]
    n2["План бага"]
    n3["Реализация"]
    n4["Ревью<br/>исполнитель: reviewer"]
    n5["Слияние<br/>исполнитель: оператор"]
    fin(("конец"))
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
    n6["Слияние<br/>исполнитель: оператор"]
    fin(("конец"))
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

## Этап «Ревью» (review) — изменён

` + msg.Text(msg.FlowScenarios, "bug, feature") + `

` + msg.Text(msg.FlowInstruction) + `

` + "```diff" + `
-Проверить изменения задачи по списку.
+## Замечания
+Записать замечания ревью и проверить, что каждое исправлено
+или отклонено с обоснованием.
` + "```" + `

## Этап «Безопасность» (security) — добавлен

` + lines(msg.Text(msg.FlowExit, "проверка безопасности пройдена")+"  ", msg.Text(msg.FlowExecutor, "auditor")+"  ",
		msg.Text(msg.FlowParts, "—")+"  ", msg.Text(msg.FlowScenarios, "feature")) + `
` + msg.Text(msg.FlowInstruction) + `

> Проверить изменения на уязвимости.

## ` + msg.Text(msg.FlowObjAgent, "auditor") + ` — добавлен

` + lines(msg.Text(msg.FlowPurpose, "проверка безопасности изменений")+"  ",
		msg.Text(msg.FlowCapabilities, "read, search")+"  ", msg.Text(msg.FlowStages, "security")) + `
` + msg.Text(msg.FlowInstruction) + `

> Найти уязвимости в изменениях задачи.

## Библиотека субагентов

### Субагент reviewer — изменён

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
	incident := map[string]string{
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
	}
	problems := map[string]string{
		"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n",
		"scenarios/bug.yaml": "title: [\n",
	}
	for _, c := range []struct {
		name  string
		draft map[string]string
		parts []string
	}{
		{"scenarios added and removed", incident, []string{
			"\n## " + msg.Text(msg.ReportObjScenario, "Баг", "bug") + " — " + msg.Text(msg.FlowChangeRemoved) + "\n\n```mermaid\n",
			"\n## " + msg.Text(msg.ReportObjScenario, "Инцидент", "incident") + " — " + msg.Text(msg.FlowChangeAdded) + "\n\n```mermaid\n",
			"    n2 -.->|\"ошибка воспроизводится\"| n3\n",
			"\n## " + msg.Text(msg.ReportObjStage, "План бага", "plan-bug") + " — " + msg.Text(msg.FlowChangeRemoved) + "\n\n" + msg.Text(msg.FlowExit, ""),
			"\n## " + msg.Text(msg.FlowObjPart, "plan-format") + " — " + msg.Text(msg.FlowChangeModified) + "\n\n" + msg.Text(msg.FlowStages, "plan-feature") + "\n\n" + msg.Text(msg.FlowText) +
				"\n\n```diff\n ## План\n Шаги, проверка и риски.\n+Срок.\n```\n",
		}},
		{"draft with problems", problems, []string{
			msg.Text(msg.ReportCreated, "2026-10-09 12:30") + "\n\nОшибки черновика:\n\n- ",
			"\n## " + msg.Text(msg.ReportObjScenario, "bug", "bug") + "\n\nСхема не построена: в сценарии есть ошибки.\n",
			"\n" + hintLineOf(msg.HintFlowShowDraft, "gentry flow show --draft") + "\n",
		}},
	} {
		report := diffView{project: "shop", res: shopDiff(t, true, c.draft, nil)}.report(shopTime)
		for _, part := range c.parts {
			if !strings.Contains(report, part) {
				t.Errorf("%s: the file of changes:\n%s\nwant within:\n%s", c.name, report, part)
			}
		}
	}
}
