package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
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

func TestFlowDiffReport(t *testing.T) {
	p := clitest.ShopFlow(t)
	security(t, p)
	clitest.WriteDraft(t, p, map[string]string{"scenarios/bug.yaml": strings.Replace(readFlowFile(t, p, "scenarios/bug.yaml"), "max_rounds: 2", "max_rounds: 3", 1)})
	reviewerRuns(t, p)
	report := filepath.Join(filepath.Dir(p.Dir), "changes.md")

	_, stdout, _ := clitest.Run("flow", "diff")
	want := `Проект: shop
Флоу применён: <время>
Библиотека применена: <время>
Файл изменений: ` + report + `

Сценарии:
  Баг: Ветка → План бага → Реализация → ~ Ревью → Слияние
    ~ возврат «Ревью → Реализация»: до 3 раз вместо 2
  Фича: Ветка → План фичи → Реализация → ~ Ревью → + Безопасность → Слияние

Этапы:
  ~ Ревью (review)
  + Безопасность (security)

Субагенты проекта:
  + auditor

Субагенты библиотеки:
  ~ reviewer: касается проектов shop

Обозначения: + добавлено, ~ изменено, − удалено.

Применить изменения библиотеки: gentry library apply
Применить черновик: gentry flow apply
`
	if clitest.Masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}

	want = "# Изменения флоу проекта shop\n\n" +
		"Проект: shop  \nФлоу применён: <время>  \nБиблиотека применена: <время>  \nОшибки черновика: нет  \nФайл сформирован: <время>\n\n" +
		"Применить изменения библиотеки: gentry library apply  \nПрименить черновик: gentry flow apply\n" + `
## Сводка

| Объект | Изменение |
| --- | --- |
| Сценарий «Баг» (bug) | изменён |
| Сценарий «Фича» (feature) | изменён |
| Этап «Ревью» (review) | изменён |
| Этап «Безопасность» (security) | добавлен |
| Субагент auditor | добавлен |
| Субагент reviewer из библиотеки | изменён; касается проектов shop |

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
- Изменён возврат «Ревью → Реализация»: до 3 раз вместо 2.

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
- Добавлен переход «Ревью → Безопасность».
- Добавлен переход «Безопасность → Слияние».
- Удалён переход «Ревью → Слияние».

## Этап «Ревью» (review) — изменён

Сценарии: bug, feature

Инструкция:

` + "```diff" + `
-Проверить изменения задачи по списку.
+## Замечания
+Записать замечания ревью и проверить, что каждое исправлено
+или отклонено с обоснованием.
` + "```" + `

## Этап «Безопасность» (security) — добавлен

` + clitest.Lines("Выход: проверка безопасности пройдена  ", "Исполнитель: auditor  ", "Фрагменты: —  ", "Сценарии: feature") + `
Инструкция:

> Проверить изменения на уязвимости.

## Субагент auditor — добавлен

` + clitest.Lines("Назначение: проверка безопасности изменений  ", "Возможности: read, search  ", "Этапы: security") + `
Инструкция:

> Найти уязвимости в изменениях задачи.

## Библиотека субагентов

### Субагент reviewer — изменён

Касается проектов: shop

| Поле | Было | Стало |
| --- | --- | --- |
| Возможности | read, search | read, search, run |

Инструкция:

` + "```diff" + `
 Проверить изменения задачи: поведение, тесты и тексты для оператора.
+Запустить тесты проекта и приложить результат к замечаниям.
` + "```" + `
`
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
	want := `Сценарии:
  − Баг
  + Инцидент: Ветка → Реализация
    + переход «Реализация → План фичи»: если ошибка воспроизводится
    + переход «Реализация → Слияние»: если ошибка не воспроизводится

Этапы:
  − План бага (plan-bug)

Фрагменты:
  ~ plan-format
`
	if !strings.Contains(stdout, "\n\n"+want+"\n") {
		t.Errorf("diff:\n%s\nwant within:\n%s", stdout, want)
	}
	report := reportOf(t, p)
	for _, want := range []string{
		"\n## Сценарий «Баг» (bug) — удалён\n\n```mermaid\n",
		"\n## Сценарий «Инцидент» (incident) — добавлен\n\n```mermaid\n",
		"    n2 -.->|\"ошибка воспроизводится\"| n3\n",
		"\n## Этап «План бага» (plan-bug) — удалён\n\nВыход: ",
		"\n## Фрагмент plan-format — изменён\n\nЭтапы: plan-feature\n\nТекст:\n\n```diff\n ## План\n Шаги, проверка и риски.\n+Срок.\n```\n",
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
	want := `Сценарии:
  Баг (bug): в сценарии есть ошибки

Этапы:
  ~ Ревью (review)

Ошибки:
`
	if code != contract.ExitOK || stderr != "" || !strings.Contains(stdout, "\n\n"+strings.Replace(want, "Баг (bug)", "bug", 1)) ||
		!strings.HasSuffix(stdout, "\n\nОбозначения: + добавлено, ~ изменено, − удалено.\n\nПосмотреть черновик: gentry flow show --draft\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	report := reportOf(t, p)
	for _, want := range []string{
		"Файл сформирован: <время>\n\nОшибки черновика:\n\n- ",
		"\n## Сценарий «bug» (bug)\n\nСхема не построена: в сценарии есть ошибки.\n",
		"\nПосмотреть черновик: gentry flow show --draft\n",
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
	want := `Проект: shop
Флоу применён: <время>
Библиотека применена: <время>
Файл изменений: ` + report + `

Флоу проекта не изменён.

Субагенты библиотеки:
  ~ reviewer: касается проектов shop

Обозначения: + добавлено, ~ изменено, − удалено.

Применить изменения библиотеки: gentry library apply
`
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
	if _, stdout, _ := clitest.Run("flow", "diff"); !strings.Contains(stdout, "\n  ~ reviewer: не используется ни одним проектом\n") {
		t.Errorf("diff:\n%s", stdout)
	}
	// Discarding the library removes the file.
	clitest.MustRun(t, "library", "discard")
	if clitest.FileExists(report) {
		t.Error("the file of changes after library discard")
	}
}

func TestFlowGuide(t *testing.T) {
	clitest.WantRun(t, contract.ExitOK, agenttext.FlowGuide(), "", "flow", "guide")
	clitest.WantRun(t, contract.ExitUsage, "", "Команда flow guide не поддерживает флаг --json.\n", "flow", "guide", "--json")
	if code, _, _ := clitest.Run("flow", "guide", "extra"); code != contract.ExitUsage {
		t.Errorf("an argument: exit code %d", code)
	}
}
