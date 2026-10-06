package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// shopPlaces returns the places of the flow of the shop in the process
// repository of the data root.
func shopPlaces() flow.Places {
	process := filepath.Join(os.Getenv(home.EnvVar), "process")
	return flow.Places{
		Project:  "shop",
		Dir:      filepath.Join(process, "shop", "flow"),
		Library:  filepath.Join(process, "agents"),
		Conflict: filepath.Join(process, "shop", "conflict"),
	}
}

// emptyShopFlow connects the shop and applies the library of the flow
// testdata; the shop has no flow. It returns the places of the flow. The test
// runs in the main worktree of the shop.
func emptyShopFlow(t *testing.T) flow.Places {
	t.Helper()
	connectedShop(t)
	p := shopPlaces()
	if err := os.CopyFS(p.Library, os.DirFS(filepath.Join(flowTestdata, "agents"))); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "library", "apply")
	return p
}

// shopFlow connects the shop and applies the flow of the flow testdata. It
// returns the places of the flow.
func shopFlow(t *testing.T) flow.Places {
	t.Helper()
	p := emptyShopFlow(t)
	copyShopFlow(t, p)
	mustRun(t, "flow", "apply")
	return p
}

// copyShopFlow puts the flow of the flow testdata into the flow directory.
func copyShopFlow(t *testing.T, p flow.Places) {
	t.Helper()
	if err := os.RemoveAll(p.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(p.Dir, os.DirFS(filepath.Join(flowTestdata, "shop", "flow"))); err != nil {
		t.Fatal(err)
	}
}

// mustRun runs a command that must succeed and returns its output.
func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := run(args...)
	if code != contract.ExitOK {
		t.Fatalf("%v: exit code %d, stderr:\n%s", args, code, stderr)
	}
	return stdout
}

// writeDraft writes files of the flow directory by path inside it; an empty
// text removes the file.
func writeDraft(t *testing.T, p flow.Places, files map[string]string) {
	t.Helper()
	writeFiles(t, p.Dir, files)
}

// writeFiles writes files of directory dir by path inside it; an empty text
// removes the file.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if text == "" {
			if err := os.Remove(full); err != nil {
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
}

// timePattern is a time as the output names it, to the minute.
var timePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`)

// masked replaces the times in an output by <время>: they are those of the
// commits.
func masked(s string) string { return timePattern.ReplaceAllString(s, "<время>") }

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
	writeDraft(t, p, map[string]string{
		"scenarios/feature.yaml": text,
		"stages/review.md":       "## Замечания\nЗаписать замечания ревью и проверить, что каждое исправлено\nили отклонено с обоснованием.\n",
		"stages/security.yaml":   "title: Безопасность\nexit: проверка безопасности пройдена\nexecutor: auditor\n",
		"stages/security.md":     "Проверить изменения на уязвимости.\n",
		"agents/auditor.yaml":    "purpose: проверка безопасности изменений\ncapabilities: [read, search]\n",
		"agents/auditor.md":      "Найти уязвимости в изменениях задачи.\n",
	})
}

const shopTables = `СЦЕНАРИЙ  НАЗВАНИЕ
bug       Баг
feature   Фича

ЭТАП            НАЗВАНИЕ    ИСПОЛНИТЕЛЬ   ВЫХОД
branch          Ветка       orchestrator  создана ветка задачи
implementation  Реализация  orchestrator  изменения сделаны, тесты проходят
merge           Слияние     operator      ветка задачи влита в main
plan-bug        План бага   orchestrator  причина установлена, план исправления согласован
plan-feature    План фичи   orchestrator  план согласован с оператором
review          Ревью       reviewer      замечания ревью записаны и разобраны

СУБАГЕНТ  ИСТОЧНИК    ЭТАПЫ
reviewer  библиотека  review
`

func TestFlowShowText(t *testing.T) {
	p := shopFlow(t)
	code, stdout, stderr := run("flow", "show")
	want := "Проект: shop\nФлоу применён: <время>\nПапка флоу: " + p.Dir + "\n\n" + shopTables + "\nПосмотреть этап подробно: gentry flow show --stage <этап>\n"
	if stdout = masked(stdout); code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--scenario", "feature"}, `Сценарий: feature
Название: Фича

Путь по умолчанию:
  branch → plan → implementation → review → merge → конец

Условные переходы:
  review → implementation, не более 3 возвратов: ревью выявило существенные замечания

УЗЕЛ            ЭТАП            ИСПОЛНИТЕЛЬ
branch          branch          orchestrator
plan            plan-feature    orchestrator
implementation  implementation  orchestrator
review          review          reviewer
merge           merge           operator
`},
		{[]string{"--stage", "review"}, `Этап: review
Название: Ревью
Исполнитель: reviewer
Выход: замечания ревью записаны и разобраны
Фрагменты: review-checklist
Сценарии: bug, feature

Инструкция:
  Проверить изменения задачи по списку.
`},
		{[]string{"--stage=merge"}, `Этап: merge
Название: Слияние
Исполнитель: operator
Выход: ветка задачи влита в main
Фрагменты: —
Сценарии: bug, feature

Инструкция:
  Влить ветку задачи в main.
`},
		{[]string{"--agent", "reviewer"}, `Субагент: reviewer
Источник: библиотека
Назначение: ревью изменений задачи — поведение и текст
Возможности: read, search
Этапы: review

Инструкция:
  Проверить изменения задачи: поведение, тесты и тексты для оператора.
`},
		{[]string{"--part", "review-checklist"}, `Фрагмент: review-checklist
Этапы: review

Текст:
  ## Список ревью
  - поведение
  - тесты
  - тексты
`},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(append([]string{"flow", "show"}, tt.args...)...)
		if code != contract.ExitOK || stdout != tt.want {
			t.Errorf("%v: exit code %d, stderr %q, output:\n%s\nwant:\n%s", tt.args, code, stderr, stdout, tt.want)
		}
	}
}

func TestFlowShowConditional(t *testing.T) {
	p := shopFlow(t)
	writeDraft(t, p, map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: triage\nnodes:\n" +
		"  triage:\n    stage: plan-bug\n    next:\n      - to: merge\n        if: срочно\n      - to: finish\n        if: |\n          не воспроизводится\n          на main\n" +
		"  merge: { stage: merge, next: finish }\n"})
	_, stdout, stderr := run("flow", "show", "--draft", "--scenario", "bug")
	// At a fork the default path stops; the end is named in words.
	want := "Путь по умолчанию:\n  triage\n\nУсловные переходы:\n  triage → merge: срочно\n  triage → конец: не воспроизводится на main\n\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stderr %q, output:\n%s\nwant within:\n%s", stderr, stdout, want)
	}
}

func TestFlowDraftText(t *testing.T) {
	p := emptyShopFlow(t)
	// The flow directory is there from the connection, with the directories
	// of a flow as a prompt.
	for _, d := range []string{"scenarios", "stages", "parts", "agents"} {
		if fi, err := os.Stat(filepath.Join(p.Dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", d, err)
		}
	}
	copyShopFlow(t, p)

	_, stdout, _ := run("flow", "diff")
	if !strings.HasPrefix(stdout, "Проект: shop\nФлоу применён: —\n\nИзменения:\n  Сценарий bug: добавлен\n") ||
		!strings.HasSuffix(stdout, "  Фрагмент review-checklist: добавлен\n\nПрименить черновик: gentry flow apply\n") {
		t.Errorf("diff from nothing:\n%s", stdout)
	}
	_, stdout, _ = run("flow", "show", "--draft")
	want := "Проект: shop\nФлоу применён: —\nПапка флоу: " + p.Dir + "\n\n" + shopTables + "\nПрименить черновик: gentry flow apply\n"
	if stdout != want {
		t.Errorf("show --draft:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("flow", "apply"); masked(stdout) != "Изменения флоу применены.\nПроект: shop\nФлоу применён: <время>\n" {
		t.Errorf("apply:\n%s", stdout)
	}
	process := filepath.Dir(p.Library)
	if log := gittest.Run(t, process, "log", "-1", "--format=%s|%b"); log != "Apply the flow of shop|Gentry: flow apply shop\n\n" {
		t.Errorf("commit: %q", log)
	}

	// Editing the files makes a draft.
	writeDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	_, stdout, _ = run("flow", "show")
	if want := "Проект: shop\nФлоу применён: <время>\nПапка флоу: " + p.Dir + "\nЧерновик существует.\n\n" + shopTables + "\nПосмотреть черновик: gentry flow show --draft\n"; masked(stdout) != want {
		t.Errorf("show with a draft:\n%s\nwant:\n%s", stdout, want)
	}
	// Line ends of another editor are no change.
	writeDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main.\r\n"})
	code, _, stderr := run("flow", "diff")
	if want := "У проекта shop нет черновика флоу.\nПапка флоу: " + p.Dir + "\n"; code != contract.ExitError || stderr != want {
		t.Errorf("diff without changes: exit code %d, stderr:\n%s", code, stderr)
	}

	security(t, p)
	_, stdout, _ = run("flow", "diff")
	want = `Проект: shop
Флоу применён: <время>

Изменения:
  Сценарий feature: изменён
  Этап review: изменён
  Этап security: добавлен
  Субагент auditor: добавлен

Применить черновик: gentry flow apply
`
	if masked(stdout) != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}
	mustRun(t, "flow", "apply")
	_, stdout, _ = run("flow", "show")
	if !strings.Contains(stdout, "\nСУБАГЕНТ  ИСТОЧНИК    ЭТАПЫ\nauditor   проект      security\nreviewer  библиотека  review\n") {
		t.Errorf("show after apply:\n%s", stdout)
	}

	writeDraft(t, p, map[string]string{"stages/security.md": "", "parts/extra.md": "Лишнее.\n"})
	if _, stdout, _ := run("flow", "discard"); stdout != "Изменения флоу отменены.\nПроект: shop\n" {
		t.Errorf("discard:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "parts", "extra.md")); !os.IsNotExist(err) {
		t.Errorf("an added file after discard: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "stages", "security.md")); err != nil || string(b) != "Проверить изменения на уязвимости.\n" {
		t.Errorf("a removed file after discard: %q, %v", b, err)
	}
	if code, _, _ := run("flow", "show", "--draft"); code != contract.ExitError {
		t.Errorf("a draft after discard")
	}
}

func TestFlowJSON(t *testing.T) {
	p := emptyShopFlow(t)
	copyShopFlow(t, p)
	_, stdout, _ := run("flow", "apply", "--json")
	validate(t, "schemas/flow-apply.json", stdout)
	var applied contract.FlowApplyOutput
	if err := json.Unmarshal([]byte(stdout), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Project != "shop" || applied.Sent || len(applied.Applied.Commit) != 40 || applied.Sync != nil ||
		!regexp.MustCompile(`"time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.000Z"`).MatchString(stdout) {
		t.Errorf("apply: %s", stdout)
	}

	security(t, p)
	_, stdout, _ = run("flow", "diff", "--json")
	validate(t, "schemas/flow-diff.json", stdout)
	want := `{"applied":{"commit":"` + applied.Applied.Commit + `",`
	tail := `"changes":[{"change":"modified","id":"feature","object":"scenario"},` +
		`{"change":"modified","id":"review","object":"stage"},{"change":"added","id":"security","object":"stage"},` +
		`{"change":"added","id":"auditor","object":"agent"}],"project":"shop"}` + "\n"
	if !strings.HasPrefix(stdout, want) || !strings.HasSuffix(stdout, tail) {
		t.Errorf("diff: %s", stdout)
	}

	code, stdout, _ := run("flow", "show", "--json")
	if code != contract.ExitOK {
		t.Fatalf("show: exit code %d: %s", code, stdout)
	}
	validate(t, "schemas/flow-show.json", stdout)
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
	_, stdout, _ = run("flow", "show", "--draft", "--stage", "security", "--json")
	validate(t, "schemas/flow-show.json", stdout)
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Scenarios) != 0 || len(out.Stages) != 1 || len(out.Parts) != 0 || len(out.Agents) != 1 || out.Agents[0].Source != "project" {
		t.Errorf("show --draft --stage security: %s", stdout)
	}
	_, stdout, _ = run("flow", "show", "--scenario", "feature", "--json")
	if strings.Contains(stdout, `"plan-bug"`) || !strings.Contains(stdout, `"id":"reviewer"`) {
		t.Errorf("show --scenario feature: %s", stdout)
	}

	_, stdout, _ = run("flow", "discard", "--json")
	validate(t, "schemas/flow-discard.json", stdout)
	if want := `{"dir":` + jsonString(p.Dir) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("discard: %s, want %s", stdout, want)
	}

	// Every event of the flow and the library matches its schema.
	_, stdout, _ = run("events")
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
		validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type)
	}
	if got := strings.Join(types, " "); got != "library.applied flow.applied flow.draft_discarded" {
		t.Errorf("events: %s", got)
	}
}

func TestFlowDraftInvalid(t *testing.T) {
	p := shopFlow(t)
	writeDraft(t, p, map[string]string{
		"stages/security.yaml": "title: Безопасность\nexecutor: auditor\n",
		"stages/security.md":   "Проверить безопасность.\n",
		"agents/auditor.yaml":  "purpose: проверка безопасности изменений\ncapabilities: [read]\n",
	})
	want := "В черновике флоу проекта shop есть ошибки.\nПапка флоу: " + p.Dir + "\n\nОшибки:\n" +
		"  Этап security: не заполнено поле «exit».\n  Субагент auditor: нет инструкции.\n"
	for _, args := range [][]string{{"flow", "show", "--draft"}, {"flow", "apply"}, {"flow", "show", "--draft", "--stage", "security"}} {
		code, stdout, stderr := run(args...)
		if code != contract.ExitError || stdout != "" || stderr != want {
			t.Errorf("%v: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", args, code, stdout, stderr, want)
		}
	}
	// Nothing changed: the draft is there.
	if _, stdout, _ := run("flow", "show"); !strings.Contains(stdout, "\nЧерновик существует.\n") {
		t.Errorf("show after a refused apply:\n%s", stdout)
	}

	code, stdout, _ := run("flow", "apply", "--json")
	validate(t, "schemas/error.json", stdout)
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	validate(t, "schemas/flow-draft-invalid.json", string(out.Error.Details))
	wantDetails := `{"dir":` + jsonString(p.Dir) + `,"problems":[` +
		`{"code":"missing_field","file":"stages/security.yaml","message":"Этап security: не заполнено поле «exit»."},` +
		`{"code":"missing_instruction","file":"agents/auditor.yaml","message":"Субагент auditor: нет инструкции."}],"project":"shop"}`
	if code != contract.ExitError || out.Error.Code != contract.CodeFlowDraftInvalid || string(out.Error.Details) != wantDetails {
		t.Errorf("--json: exit code %d, details:\n%s\nwant:\n%s", code, out.Error.Details, wantDetails)
	}
}

// TestFlowInvalid checks an active flow that this Gentry finds problems in,
// as one applied by a later Gentry that knows more fields.
func TestFlowInvalid(t *testing.T) {
	p := shopFlow(t)
	writeDraft(t, p, map[string]string{"flow.yaml": "on_take: x\n"})
	gittest.Run(t, filepath.Dir(p.Library), "add", "--all")
	gittest.Run(t, filepath.Dir(p.Library), "commit", "--quiet", "-am", "Apply the flow of shop\n\nGentry: flow apply shop")
	code, stdout, stderr := run("flow", "show")
	want := "Во флоу проекта shop есть ошибки.\nПапка флоу: " + p.Dir + "\n\nОшибки:\n" +
		"  Общие правила флоу: поле «on_take» не поддерживается этой версией Gentry.\n"
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ = run("flow", "show", "--json")
	var out struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	validate(t, "schemas/flow-invalid.json", string(out.Error.Details))
	if !strings.Contains(string(out.Error.Details), `"project":"shop"`) {
		t.Errorf("details: %s", out.Error.Details)
	}
}

// TestFlowBypass checks a change of the flow committed around Gentry.
func TestFlowBypass(t *testing.T) {
	p := shopFlow(t)
	process := filepath.Dir(p.Library)
	// Without problems it is taken as it is.
	writeDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main вручную.\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand")
	if code, _, stderr := run("flow", "show", "--stage", "merge"); code != contract.ExitOK || stderr != "" {
		t.Errorf("a valid change: exit code %d, stderr:\n%s", code, stderr)
	}
	// With problems it becomes a draft, and the checked flow is active.
	writeDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n"})
	gittest.Run(t, process, "commit", "--quiet", "-am", "by hand again")
	code, stdout, stderr := run("flow", "show", "--stage", "review")
	want := "Изменение флоу проекта shop, внесённое без Gentry, содержит ошибки и сохранено как черновик.\n\nОшибки:\n" +
		"  Этап review: не заполнено поле «exit».\n\nПосмотреть отличия: gentry flow diff --project shop\n\n"
	if code != contract.ExitOK || stderr != want || !strings.Contains(stdout, "Выход: замечания ревью записаны и разобраны\n") {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s\noutput:\n%s", code, stderr, want, stdout)
	}
	if log := gittest.Run(t, process, "log", "-1", "--format=%s"); log != "Restore the flow of shop\n" {
		t.Errorf("commit: %q", log)
	}
	if _, stdout, _ := run("flow", "diff"); !strings.Contains(stdout, "\n  Этап review: изменён\n") {
		t.Errorf("diff:\n%s", stdout)
	}
}

// TestFlowExecutorInDraftLibrary checks a stage carried out by a subagent of
// the library that is not applied yet.
func TestFlowExecutorInDraftLibrary(t *testing.T) {
	p := shopFlow(t)
	writeFiles(t, p.Library, map[string]string{
		"tester.yaml": "purpose: тесты\ncapabilities: [read, run]\n",
		"tester.md":   "Прогнать тесты.\n",
	})
	writeDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: tester\n"})
	code, _, stderr := run("flow", "apply")
	if want := "  Этап review: субагент tester есть только в черновике библиотеки.\n"; code != contract.ExitError || !strings.HasSuffix(stderr, want) {
		t.Errorf("exit code %d, stderr:\n%s", code, stderr)
	}
	mustRun(t, "library", "apply")
	mustRun(t, "flow", "apply")
}

// TestFlowShowByFlowDir checks that the project is found by its flow
// directory, so the operator can edit the flow and show it in one place.
func TestFlowShowByFlowDir(t *testing.T) {
	p := shopFlow(t)
	t.Chdir(filepath.Join(p.Dir, "stages"))
	if code, stdout, stderr := run("flow", "show"); code != contract.ExitOK || !strings.HasPrefix(stdout, "Проект: shop\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
}

func TestFlowRefusals(t *testing.T) {
	p := emptyShopFlow(t)
	dir := "\nПапка флоу: " + p.Dir
	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no flow", []string{"flow", "show"}, contract.ExitError, "У проекта shop нет флоу." + dir},
		{"no draft to show", []string{"flow", "show", "--draft"}, contract.ExitError, "У проекта shop нет черновика флоу." + dir},
		{"no draft to compare", []string{"flow", "diff"}, contract.ExitError, "У проекта shop нет черновика флоу." + dir},
		{"no draft to apply", []string{"flow", "apply"}, contract.ExitError, "У проекта shop нет черновика флоу." + dir},
		{"no draft to discard", []string{"flow", "discard"}, contract.ExitError, "У проекта shop нет черновика флоу." + dir},
		{"edit", []string{"flow", "edit"}, contract.ExitUsage,
			msg.Text(msg.ErrActionUnknown, "edit", "flow") + "\n\nПосмотреть перечень действий: gentry flow --help"},
		{"two objects", []string{"flow", "show", "--stage", "review", "--agent", "reviewer"}, contract.ExitUsage,
			"Флаги --stage и --agent нельзя указывать вместе.\n\nПосмотреть описание команды: gentry flow show --help"},
		{"empty object", []string{"flow", "show", "--stage="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--stage")},
		{"argument", []string{"flow", "show", "feature"}, contract.ExitUsage, msg.Text(msg.ErrUnexpectedArgs, "flow show")},
		{"unknown project", []string{"flow", "diff", "--project", "cart"}, contract.ExitError,
			"Проект «cart» не подключён.\n\nПосмотреть перечень проектов: gentry project list"},
		{"empty project", []string{"flow", "diff", "--project="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--project")},
		{"no action", []string{"flow"}, contract.ExitUsage, msg.Text(msg.ErrActionMissing, "flow") + "\n\nПосмотреть перечень действий: gentry flow --help"},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	code, stdout, _ := run("flow", "show", "--json")
	validate(t, "schemas/error.json", stdout)
	if want := `{"error":{"code":"flow_not_found","details":{"dir":` + jsonString(p.Dir) + `,"project":"shop"},` +
		`"message":"У проекта shop нет флоу."}}` + "\n"; code != contract.ExitError || stdout != want {
		t.Errorf("no flow --json: exit code %d, %s, want %s", code, stdout, want)
	}

	// Objects the flow or the draft does not have.
	copyShopFlow(t, p)
	mustRun(t, "flow", "apply")
	writeDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку.\n"})
	tests = []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"unknown stage", []string{"flow", "show", "--stage", "revew"}, contract.ExitError,
			"Во флоу проекта shop нет этапа «revew».\n\nПосмотреть перечень объектов: gentry flow show"},
		{"unknown agent in the draft", []string{"flow", "show", "--draft", "--agent", "auditor"}, contract.ExitError,
			"В черновике флоу проекта shop нет субагента «auditor».\n\nПосмотреть перечень объектов: gentry flow show --draft"},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	// Outside any project, without a state store.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"flow", "show"}, {"flow", "diff"}} {
		if code, _, stderr := run(args...); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectUndetermined, "")) {
			t.Errorf("%v outside a project: exit code %d, stderr %q", args, code, stderr)
		}
	}
	if entries, _ := os.ReadDir(os.Getenv(home.EnvVar)); len(entries) != 0 {
		t.Errorf("reading must not create the store: %v", entries)
	}
}

func TestFlowHelp(t *testing.T) {
	_, stdout, _ := run("flow", "--help")
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
	_, stdout, _ = run("flow", "show", "--help")
	if usage := "  gentry flow show [--scenario <сценарий> | --stage <этап> | --agent <субагент> | --part <фрагмент>] [--draft] [--project <идентификатор>] [--json]\n"; !strings.Contains(stdout, usage) {
		t.Errorf("flow show --help:\n%s\nwant within:\n%s", stdout, usage)
	}
}

// flowTestdata is the process directory of the flow testdata, found before
// any test changes the current directory.
var flowTestdata = func() string {
	dir, err := filepath.Abs("../flow/testdata/process")
	if err != nil {
		panic(err)
	}
	return dir
}()
