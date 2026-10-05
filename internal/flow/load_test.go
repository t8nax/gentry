package flow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// shop copies the process of testdata — the flow of the shop and the shared
// subagents — into a temporary directory, applies changes and returns the
// dirs of the shop. A change is the new text of a file by its path inside the
// flow directory; an empty text removes the file or directory.
func shop(t *testing.T, changes map[string]string) Dirs {
	t.Helper()
	process := t.TempDir()
	if err := os.CopyFS(process, os.DirFS("testdata/process")); err != nil {
		t.Fatal(err)
	}
	d := ProjectDirs(process, "shop")
	for rel, text := range changes {
		p := filepath.Join(d.Flow, filepath.FromSlash(rel))
		if text == "" {
			if err := os.RemoveAll(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// problems loads the flow of d and returns its problems.
func problems(t *testing.T, d Dirs) []Problem {
	t.Helper()
	_, err := Load(d)
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("Load: got %v, want InvalidError", err)
	}
	if ie.Dir != d.Flow {
		t.Errorf("dir %s, want %s", ie.Dir, d.Flow)
	}
	return ie.Problems
}

func TestLoadShop(t *testing.T) {
	f, err := Load(shop(t, nil))
	if err != nil {
		t.Fatal(err)
	}
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
	if want := (Stage{ID: "review", Title: "Ревью", Exit: "замечания ревью записаны и разобраны", Executor: "reviewer", Include: []string{"review-checklist"}}); !reflect.DeepEqual(review, want) {
		t.Errorf("review: %+v, want %+v", review, want)
	}
	if merge, _ := f.Stage("merge"); merge.Executor != Operator || merge.Include == nil {
		t.Errorf("merge: %+v; want the operator and an empty include", merge)
	}
	if want := []string{"plan-format", "review-checklist"}; !reflect.DeepEqual(f.Parts, want) {
		t.Errorf("parts %v, want %v", f.Parts, want)
	}
	if got, want := feature.DefaultPath(), []string{"branch", "plan", "implementation", "review", "merge", Finish}; !reflect.DeepEqual(got, want) {
		t.Errorf("default path %v, want %v", got, want)
	}

	only := f.Only(feature)
	ids = nil
	for _, s := range only.Stages {
		ids = append(ids, s.ID)
	}
	if len(only.Scenarios) != 1 || !reflect.DeepEqual(ids, []string{"branch", "implementation", "merge", "plan-feature", "review"}) ||
		!reflect.DeepEqual(only.Parts, []string{"plan-format", "review-checklist"}) {
		t.Errorf("only feature: stages %v, parts %v", ids, only.Parts)
	}
}

func TestLoadNotFound(t *testing.T) {
	d := shop(t, nil)
	d.Flow = filepath.Join(filepath.Dir(d.Flow), "none")
	var nf *NotFoundError
	if _, err := Load(d); !errors.As(err, &nf) || nf.Dir != d.Flow {
		t.Errorf("got %v, want NotFoundError for %s", err, d.Flow)
	}
}

func TestLoadIgnoresHidden(t *testing.T) {
	d := shop(t, map[string]string{
		".git/config":         "x",
		"stages/.draft.yaml":  "not: [yaml",
		"scenarios/.old.yaml": "x",
	})
	if _, err := Load(d); err != nil {
		t.Error(err)
	}
}

func TestLoadAcceptsYAMLForms(t *testing.T) {
	// Anchors, aliases, block text, a byte order mark and line ends of Windows
	// are plain YAML.
	d := shop(t, map[string]string{
		"stages/branch.yaml":         "\xEF\xBB\xBFtitle: &t Ветка\nexit: *t\nexecutor: orchestrator\ninclude: []\n",
		"stages/implementation.yaml": "title: Реализация\nexit: |\n  изменения сделаны,\n  тесты проходят\nexecutor: orchestrator\n",
		"stages/merge.yaml":          "title: Слияние\r\nexit: ветка задачи влита в main\r\nexecutor: operator\r\n",
		"flow.yaml":                  "# общие правила\n",
	})
	f, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := f.Stage("branch"); b.Title != "Ветка" || b.Exit != "Ветка" {
		t.Errorf("branch: %+v", b)
	}
	if i, _ := f.Stage("implementation"); i.Exit != "изменения сделаны,\nтесты проходят\n" {
		t.Errorf("implementation: %+v", i)
	}
	// Line ends of Windows are not part of the values.
	if m, _ := f.Stage("merge"); m.Title != "Слияние" || m.Executor != Operator {
		t.Errorf("merge: %+v", m)
	}
}

func TestProjectSubagent(t *testing.T) {
	// A subagent of the project, next to the shared ones.
	d := shop(t, map[string]string{
		"../agents/linter.yaml": "purpose: проверка стиля\n",
		"stages/review.yaml":    "title: Ревью\nexit: замечания разобраны\nexecutor: linter\n",
	})
	if _, err := Load(d); err != nil {
		t.Error(err)
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
		"Сценарий bug: начальный узел brnch не найден.",
		"Сценарий feature: у цикла implementation → review → implementation нет предела кругов.",
		"Этап plan-bug: не заполнено поле «exit».",
		"Этап review: субагент reviewr не найден.",
		"Файл не относится к флоу: stages/merge.yml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// d0 returns the dirs of an unchanged copy of the shop.
func d0(t *testing.T) Dirs { return shop(t, nil) }

// read returns the text of file rel of the flow of d.
func read(t *testing.T, d Dirs, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(d.Flow, filepath.FromSlash(rel)))
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
			{contract.ProblemSyntax, "stages/review.md", 0, "Этап review: текст не в кодировке UTF-8."},
		}},
		{"part not utf-8", map[string]string{"parts/plan-format.md": "\xff"}, []Problem{
			{contract.ProblemSyntax, "parts/plan-format.md", 0, "Фрагмент plan-format: текст не в кодировке UTF-8."},
		}},
		{"two documents", map[string]string{"stages/plan-bug.yaml": stage("---\ntitle: x\n")}, []Problem{
			{contract.ProblemSyntax, "stages/plan-bug.yaml", 5, "Этап plan-bug: ошибка синтаксиса YAML."},
		}},
		{"unknown field", map[string]string{"stages/plan-bug.yaml": "title: План бага\nexits: x\nexit: y\nexecutor: orchestrator\n"}, []Problem{
			{contract.ProblemUnknownField, "stages/plan-bug.yaml", 2, "Этап plan-bug: неизвестное поле «exits»."},
		}},
		{"later fields", map[string]string{
			"flow.yaml":          "on_take:\n  tracker:\n    - set_state: In Progress\non_enter: x\n",
			"stages/branch.yaml": "title: Ветка\nexit: создана ветка\nexecutor: orchestrator\nprocedures: []\non_enter: x\n",
		}, []Problem{
			{contract.ProblemUnsupportedField, "flow.yaml", 1, "Общие правила флоу: поле «on_take» не поддерживается этой версией Gentry."},
			{contract.ProblemUnknownField, "flow.yaml", 4, "Общие правила флоу: неизвестное поле «on_enter»."},
			{contract.ProblemUnsupportedField, "stages/branch.yaml", 4, "Этап branch: поле «procedures» не поддерживается этой версией Gentry."},
			{contract.ProblemUnsupportedField, "stages/branch.yaml", 5, "Этап branch: поле «on_enter» не поддерживается этой версией Gentry."},
		}},
		{"scenario event", map[string]string{"scenarios/bug.yaml": "on_close:\n  tracker: []\n" + read(t, d0(t), "scenarios/bug.yaml")}, []Problem{
			{contract.ProblemUnsupportedField, "scenarios/bug.yaml", 1, "Сценарий bug: поле «on_close» не поддерживается этой версией Gentry."},
		}},
		{"missing fields", map[string]string{
			"stages/plan-bug.yaml": "title: План бага\nexit: \"\"\n",
			"stages/merge.yaml":    "# пусто\n",
		}, []Problem{
			{contract.ProblemMissingField, "stages/merge.yaml", 0, "Этап merge: не заполнено поле «title»."},
			{contract.ProblemMissingField, "stages/merge.yaml", 0, "Этап merge: не заполнено поле «exit»."},
			{contract.ProblemMissingField, "stages/merge.yaml", 0, "Этап merge: не заполнено поле «executor»."},
			{contract.ProblemMissingField, "stages/plan-bug.yaml", 0, "Этап plan-bug: не заполнено поле «executor»."},
			{contract.ProblemMissingField, "stages/plan-bug.yaml", 2, "Этап plan-bug: не заполнено поле «exit»."},
		}},
		{"invalid values", map[string]string{
			"stages/plan-bug.yaml": stage("include: plan-format\n"),
			"stages/review.yaml":   "title: 5\nexit: [a]\nexecutor: reviewer\ninclude: [Review-Checklist]\n",
			"stages/merge.yaml":    "- title\n",
		}, []Problem{
			{contract.ProblemInvalidValue, "stages/merge.yaml", 1, "Этап merge: описание должно состоять из полей."},
			{contract.ProblemInvalidValue, "stages/plan-bug.yaml", 4, "Этап plan-bug: значение поля «include» должно быть списком идентификаторов."},
			{contract.ProblemInvalidValue, "stages/review.yaml", 1, "Этап review: значение поля «title» должно быть строкой."},
			{contract.ProblemInvalidValue, "stages/review.yaml", 2, "Этап review: значение поля «exit» должно быть строкой."},
			{contract.ProblemInvalidValue, "stages/review.yaml", 4, "Этап review: значение поля «include» должно быть списком идентификаторов."},
		}},
		{"invalid ids", map[string]string{
			"stages/Plan_Bug.yaml":   stage(""),
			"stages/Plan_Bug.md":     "x",
			"parts/Big.md":           "x",
			"scenarios/Bug_Fix.yaml": "x",
		}, []Problem{
			{contract.ProblemInvalidID, "scenarios/Bug_Fix.yaml", 0, "Сценарий Bug_Fix: недопустимый идентификатор; допустимы до 64 строчных латинских букв, цифр и дефисов, первая — буква."},
			{contract.ProblemInvalidID, "stages/Plan_Bug.md", 0, "Этап Plan_Bug: недопустимый идентификатор; допустимы до 64 строчных латинских букв, цифр и дефисов, первая — буква."},
			{contract.ProblemInvalidID, "parts/Big.md", 0, "Фрагмент Big: недопустимый идентификатор; допустимы до 64 строчных латинских букв, цифр и дефисов, первая — буква."},
		}},
		{"extra files", map[string]string{
			"stages/merge.yml":   "x",
			"templates/close.md": "x",
			"README.md":          "x",
			"scenarios/old/x":    "x",
			"parts/note.txt":     "x",
		}, []Problem{
			{contract.ProblemExtraFile, "README.md", 0, "Файл не относится к флоу: README.md"},
			{contract.ProblemExtraFile, "parts/note.txt", 0, "Файл не относится к флоу: parts/note.txt"},
			{contract.ProblemExtraFile, "scenarios/old", 0, "Папка не относится к флоу: scenarios/old"},
			{contract.ProblemExtraFile, "stages/merge.yml", 0, "Файл не относится к флоу: stages/merge.yml"},
			{contract.ProblemExtraFile, "templates", 0, "Папка не относится к флоу: templates"},
		}},
		{"instructions", map[string]string{"stages/merge.md": "", "stages/old.md": "x"}, []Problem{
			{contract.ProblemMissingInstruction, "stages/merge.yaml", 0, "Этап merge: нет инструкции."},
			{contract.ProblemOrphanInstruction, "stages/old.md", 0, "Этап old: есть инструкция, но нет полей этапа."},
		}},
		{"no scenarios", map[string]string{"scenarios": ""}, []Problem{
			{contract.ProblemNoScenarios, "", 0, "Во флоу нет ни одного сценария."},
		}},
		{"unknown references", map[string]string{
			"stages/plan-bug.yaml": stage("include: [plan-formt]\n"),
			"stages/review.yaml":   "title: Ревью\nexit: замечания разобраны\nexecutor: reviewr\n",
			"scenarios/bug.yaml":   strings.Replace(read(t, d0(t), "scenarios/bug.yaml"), "stage: plan-bug", "stage: plan-bugg", 1),
		}, []Problem{
			{contract.ProblemUnknownStage, "scenarios/bug.yaml", 5, "Сценарий bug, узел plan: этап plan-bugg не найден."},
			{contract.ProblemUnknownPart, "stages/plan-bug.yaml", 4, "Этап plan-bug: фрагмент plan-formt не найден."},
			{contract.ProblemUnknownExecutor, "stages/review.yaml", 3, "Этап review: субагент reviewr не найден."},
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

func TestSyntaxProblem(t *testing.T) {
	got := problems(t, shop(t, map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: [branch\n"}))
	if len(got) != 1 || got[0].Code != contract.ProblemSyntax || got[0].File != "scenarios/bug.yaml" || got[0].Line < 1 ||
		got[0].Message != msg.Text(msg.ProblemSyntax, "Сценарий bug") {
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
