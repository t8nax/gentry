package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// emptyShopFlow connects the shop and puts the library of the flow testdata
// into the process directory; the shop has no flow. It returns the places of
// the flow. The test runs in the main worktree of the shop.
func emptyShopFlow(t *testing.T) flow.Places {
	t.Helper()
	connectedShop(t)
	process := filepath.Join(os.Getenv(home.EnvVar), "process")
	if err := os.CopyFS(filepath.Join(process, "agents"), os.DirFS(filepath.Join(flowTestdata, "agents"))); err != nil {
		t.Fatal(err)
	}
	return flow.PlacesOf(process, "shop")
}

// shopFlow connects the shop and applies the flow of the flow testdata as
// version 1 through a draft. It returns the places of the flow.
func shopFlow(t *testing.T) flow.Places {
	t.Helper()
	p := emptyShopFlow(t)
	mustRun(t, "flow", "edit")
	if err := os.CopyFS(p.Draft+"-src", os.DirFS(filepath.Join(flowTestdata, "shop", "flow-draft"))); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.Draft); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.Draft+"-src", p.Draft); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "flow", "apply")
	return p
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

// writeDraft writes files of the draft by path inside it.
func writeDraft(t *testing.T, p flow.Places, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		full := filepath.Join(p.Draft, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// security is the change of the example of the plan: a stage security after
// the review, carried out by the project subagent auditor, and a new
// instruction of the review.
func security(t *testing.T, p flow.Places) {
	t.Helper()
	feature, err := os.ReadFile(filepath.Join(p.Draft, "scenarios", "feature.yaml"))
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
	shopFlow(t)
	code, stdout, stderr := run("flow", "show")
	want := "Проект: shop\nВерсия флоу: 1\n\n" + shopTables + "\nПосмотреть этап подробно: gentry flow show --stage <этап>\n"
	if code != contract.ExitOK || stderr != "" || stdout != want {
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
  review → implementation, не более 3 кругов: ревью выявило существенные замечания

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
	mustRun(t, "flow", "edit")
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
	// Without a flow, edit opens an empty draft.
	_, stdout, _ := run("flow", "edit")
	tail := "Проект: shop\nПапка черновика: " + p.Draft + "\n\nПосмотреть черновик: gentry flow show --draft\n"
	if want := "Создан пустой черновик флоу.\n" + tail; stdout != want {
		t.Errorf("edit without a flow:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("flow", "edit"); stdout != "Черновик уже открыт.\n"+tail {
		t.Errorf("edit again:\n%s", stdout)
	}
	if err := os.CopyFS(p.Draft+"-src", os.DirFS(filepath.Join(flowTestdata, "shop", "flow-draft"))); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(p.Draft)
	if err := os.Rename(p.Draft+"-src", p.Draft); err != nil {
		t.Fatal(err)
	}

	_, stdout, _ = run("flow", "diff")
	if !strings.HasPrefix(stdout, "Проект: shop\nВерсия флоу: —\n\nИзменения:\n  Сценарий bug: добавлен\n") ||
		!strings.HasSuffix(stdout, "  Субагент reviewer из библиотеки: добавлен\n\nПрименить черновик: gentry flow apply\n") {
		t.Errorf("diff from nothing:\n%s", stdout)
	}
	_, stdout, _ = run("flow", "show", "--draft")
	want := "Проект: shop\nПапка черновика: " + p.Draft + "\nВерсия флоу: —\n\n" + shopTables + "\nПрименить черновик: gentry flow apply\n"
	if stdout != want {
		t.Errorf("show --draft:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("flow", "apply"); stdout != "Черновик применён.\nПроект: shop\nВерсия флоу: 1\n" {
		t.Errorf("apply:\n%s", stdout)
	}

	// The next draft is made from the version.
	_, stdout, _ = run("flow", "edit")
	if want := "Черновик создан из версии 1.\n" + tail; stdout != want {
		t.Errorf("edit version 1:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = run("flow", "show")
	if want := "Проект: shop\nВерсия флоу: 1\nОткрыт черновик.\n\n" + shopTables + "\nПосмотреть черновик: gentry flow show --draft\n"; stdout != want {
		t.Errorf("show with a draft:\n%s\nwant:\n%s", stdout, want)
	}
	_, stdout, _ = run("flow", "diff")
	if want := "Проект: shop\nВерсия флоу: 1\n\nЧерновик совпадает с действующим флоу.\n\nУдалить черновик: gentry flow discard\n"; stdout != want {
		t.Errorf("diff unchanged:\n%s\nwant:\n%s", stdout, want)
	}
	code, stdout, stderr := run("flow", "apply")
	if want := "Черновик совпадает с флоу версии 1.\n\nУдалить черновик: gentry flow discard\n"; code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("apply unchanged: exit code %d, stderr:\n%s", code, stderr)
	}

	security(t, p)
	// A library subagent changed since the version is a change of the draft.
	if err := os.WriteFile(filepath.Join(p.Library, "reviewer.md"), []byte("Новая инструкция.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ = run("flow", "diff")
	want = `Проект: shop
Версия флоу: 1

Изменения:
  Сценарий feature: изменён
  Этап review: изменён
  Этап security: добавлен
  Субагент auditor: добавлен
  Субагент reviewer из библиотеки: изменён

Применить черновик: gentry flow apply
`
	if stdout != want {
		t.Errorf("diff:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("flow", "apply"); stdout != "Черновик применён.\nПроект: shop\nВерсия флоу: 2\n" {
		t.Errorf("apply version 2:\n%s", stdout)
	}
	_, stdout, _ = run("flow", "show")
	if !strings.Contains(stdout, "\nСУБАГЕНТ  ИСТОЧНИК    ЭТАПЫ\nauditor   проект      security\nreviewer  библиотека  review\n") {
		t.Errorf("show version 2:\n%s", stdout)
	}
	// The version keeps the copy of the library subagent.
	_, stdout, _ = run("flow", "show", "--agent", "reviewer")
	if !strings.HasSuffix(stdout, "Инструкция:\n  Новая инструкция.\n") {
		t.Errorf("reviewer of version 2:\n%s", stdout)
	}

	mustRun(t, "flow", "edit")
	if _, stdout, _ := run("flow", "discard"); stdout != "Черновик удалён.\nПроект: shop\n" {
		t.Errorf("discard:\n%s", stdout)
	}
	if _, err := os.Stat(p.Draft); !os.IsNotExist(err) {
		t.Errorf("draft after discard: %v", err)
	}
	if _, stdout, _ := run("flow", "show"); !strings.HasPrefix(stdout, "Проект: shop\nВерсия флоу: 2\n\n") {
		t.Errorf("show after discard:\n%s", stdout)
	}
}

func TestFlowJSON(t *testing.T) {
	p := emptyShopFlow(t)
	_, stdout, _ := run("flow", "edit", "--json")
	validate(t, "schemas/flow-edit.json", stdout)
	if want := `{"created":true,"dir":` + jsonString(p.Draft) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("edit: %s, want %s", stdout, want)
	}
	if err := os.CopyFS(p.Draft+"-src", os.DirFS(filepath.Join(flowTestdata, "shop", "flow-draft"))); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(p.Draft)
	if err := os.Rename(p.Draft+"-src", p.Draft); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ = run("flow", "apply", "--json")
	validate(t, "schemas/flow-apply.json", stdout)
	if stdout != `{"project":"shop","version":1}`+"\n" {
		t.Errorf("apply: %s", stdout)
	}

	_, stdout, _ = run("flow", "edit", "--json")
	if want := `{"base_version":1,"created":true,"dir":` + jsonString(p.Draft) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("edit version 1: %s, want %s", stdout, want)
	}
	if _, stdout, _ := run("flow", "edit", "--json"); !strings.HasPrefix(stdout, `{"base_version":1,"created":false,`) {
		t.Errorf("edit again: %s", stdout)
	}
	security(t, p)
	_, stdout, _ = run("flow", "diff", "--json")
	validate(t, "schemas/flow-diff.json", stdout)
	want := `{"changes":[{"change":"modified","id":"feature","object":"scenario"},` +
		`{"change":"modified","id":"review","object":"stage"},{"change":"added","id":"security","object":"stage"},` +
		`{"change":"added","id":"auditor","object":"agent","source":"project"}],"project":"shop","version":1}` + "\n"
	if stdout != want {
		t.Errorf("diff: %s, want %s", stdout, want)
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
	if out.Project != "shop" || out.Version == nil || *out.Version != 1 || out.Draft == nil || out.Draft.Dir != p.Draft ||
		out.Draft.BaseVersion == nil || len(out.Scenarios) != 2 || len(out.Stages) != 6 || len(out.Parts) != 2 || len(out.Agents) != 1 {
		t.Errorf("show: %s", stdout)
	}
	for _, part := range []string{
		`{"id":"review","next":[{"if":"ревью выявило существенные замечания","max_rounds":3,"to":"implementation"},{"to":"merge"}],"stage":"review"},`,
		`{"executor":"operator","exit":"ветка задачи влита в main","id":"merge","include":[],"instruction":"Влить ветку задачи в main.\n","title":"Слияние"}`,
		`"parts":[{"id":"plan-format","text":`,
		`"agents":[{"capabilities":["read","search"],"id":"reviewer","instruction":"Проверить изменения задачи: поведение, тесты и тексты для оператора.\n","purpose":"ревью изменений задачи — поведение и текст","source":"library"}]`,
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
	_, stdout, _ = run("flow", "show", "--part", "plan-format", "--json")
	if !strings.Contains(stdout, `"agents":[],`) || !strings.Contains(stdout, `"parts":[{"id":"plan-format"`) || !strings.Contains(stdout, `"stages":[]`) {
		t.Errorf("show --part: %s", stdout)
	}

	_, stdout, _ = run("flow", "discard", "--json")
	validate(t, "schemas/flow-discard.json", stdout)
	if want := `{"dir":` + jsonString(p.Draft) + `,"project":"shop"}` + "\n"; stdout != want {
		t.Errorf("discard: %s, want %s", stdout, want)
	}

	// Every event of the draft matches its schema.
	_, stdout, _ = run("events")
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
		validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type+" "+string(data))
	}
	want = "flow.draft_created {}|flow.applied {\"version\":1}|flow.draft_created {\"base_version\":1}|flow.draft_discarded {}"
	if got := strings.Join(types, "|"); got != want {
		t.Errorf("events: %s, want %s", got, want)
	}
}

func TestFlowDraftInvalid(t *testing.T) {
	p := shopFlow(t)
	mustRun(t, "flow", "edit")
	writeDraft(t, p, map[string]string{
		"stages/security.yaml": "title: Безопасность\nexecutor: auditor\n",
		"stages/security.md":   "Проверить безопасность.\n",
		"agents/auditor.yaml":  "purpose: проверка безопасности изменений\ncapabilities: [read]\n",
	})
	want := "В черновике флоу проекта shop есть ошибки.\nПапка черновика: " + p.Draft + "\n\nОшибки:\n" +
		"  Этап security: не заполнено поле «exit».\n  Субагент auditor: нет инструкции.\n"
	for _, args := range [][]string{{"flow", "show", "--draft"}, {"flow", "apply"}, {"flow", "show", "--draft", "--stage", "security"}} {
		code, stdout, stderr := run(args...)
		if code != contract.ExitError || stdout != "" || stderr != want {
			t.Errorf("%v: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", args, code, stdout, stderr, want)
		}
	}
	// Nothing changed: the version is 1, the draft is open.
	if _, stdout, _ := run("flow", "show"); !strings.HasPrefix(stdout, "Проект: shop\nВерсия флоу: 1\nОткрыт черновик.\n") {
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
	wantDetails := `{"dir":` + jsonString(p.Draft) + `,"problems":[` +
		`{"code":"missing_field","file":"stages/security.yaml","message":"Этап security: не заполнено поле «exit»."},` +
		`{"code":"missing_instruction","file":"agents/auditor.yaml","message":"Субагент auditor: нет инструкции."}],"project":"shop"}`
	if code != contract.ExitError || out.Error.Code != contract.CodeFlowDraftInvalid || string(out.Error.Details) != wantDetails {
		t.Errorf("--json: exit code %d, details:\n%s\nwant:\n%s", code, out.Error.Details, wantDetails)
	}
}

// TestFlowVersionInvalid checks an active version that a later Gentry finds
// problems in.
func TestFlowVersionInvalid(t *testing.T) {
	shopFlow(t)
	path, err := state.Path()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(func(tx *state.Tx) error {
		return tx.AddFlowVersion("shop", 2, []byte(`{"files":{"flow.yaml":"on_take: x\n"},"library":{}}`))
	})
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run("flow", "show")
	want := "Во флоу проекта shop есть ошибки.\nВерсия флоу: 2\n\nОшибки:\n  Во флоу нет ни одного сценария.\n" +
		"  Общие правила флоу: поле «on_take» не поддерживается этой версией Gentry.\n\nИсправить флоу: gentry flow edit\n"
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
	if !strings.Contains(string(out.Error.Details), `"project":"shop","version":2`) {
		t.Errorf("details: %s", out.Error.Details)
	}
}

// TestFlowShowByDraftDir checks that the project is found by its draft
// directory, so the operator can edit the flow and show it in one place.
func TestFlowShowByDraftDir(t *testing.T) {
	p := shopFlow(t)
	mustRun(t, "flow", "edit")
	t.Chdir(filepath.Join(p.Draft, "stages"))
	if code, stdout, stderr := run("flow", "show", "--draft"); code != contract.ExitOK || !strings.HasPrefix(stdout, "Проект: shop\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
}

func TestFlowRefusals(t *testing.T) {
	p := emptyShopFlow(t)
	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no flow", []string{"flow", "show"}, contract.ExitError, "У проекта shop нет флоу.\n\nСоздать флоу: gentry flow edit"},
		{"no draft to show", []string{"flow", "show", "--draft"}, contract.ExitError, "У проекта shop нет черновика флоу.\n\nНачать правку: gentry flow edit"},
		{"no draft to compare", []string{"flow", "diff"}, contract.ExitError, "У проекта shop нет черновика флоу.\n\nНачать правку: gentry flow edit"},
		{"no draft to apply", []string{"flow", "apply"}, contract.ExitError, "У проекта shop нет черновика флоу.\n\nНачать правку: gentry flow edit"},
		{"no draft to discard", []string{"flow", "discard"}, contract.ExitError, "У проекта shop нет черновика флоу.\n\nНачать правку: gentry flow edit"},
		{"two objects", []string{"flow", "show", "--stage", "review", "--agent", "reviewer"}, contract.ExitUsage,
			"Флаги --stage и --agent нельзя указывать вместе.\n\nПосмотреть описание команды: gentry flow show --help"},
		{"empty object", []string{"flow", "show", "--stage="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--stage")},
		{"argument", []string{"flow", "show", "feature"}, contract.ExitUsage, msg.Text(msg.ErrUnexpectedArgs, "flow show")},
		{"unknown project", []string{"flow", "edit", "--project", "cart"}, contract.ExitError,
			"Проект «cart» не подключён.\n\nПосмотреть перечень проектов: gentry project list"},
		{"empty project", []string{"flow", "diff", "--project="}, contract.ExitUsage, msg.Text(msg.ErrFlagValueMissing, "--project")},
		{"no action", []string{"flow"}, contract.ExitUsage, msg.Text(msg.ErrActionMissing, "flow") + "\n\nПосмотреть перечень действий: gentry flow --help"},
		{"unknown action", []string{"flow", "check"}, contract.ExitUsage,
			msg.Text(msg.ErrActionUnknown, "check", "flow") + "\n\nПосмотреть перечень действий: gentry flow --help"},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	code, stdout, _ := run("flow", "show", "--json")
	validate(t, "schemas/error.json", stdout)
	if want := `{"error":{"code":"flow_not_found","details":{"project":"shop"},"hint":"Создать флоу: gentry flow edit",` +
		`"message":"У проекта shop нет флоу."}}` + "\n"; code != contract.ExitError || stdout != want {
		t.Errorf("no flow --json: exit code %d, %s, want %s", code, stdout, want)
	}
	_, stdout, _ = run("flow", "show", "--part", "x", "--scenario", "y", "--json")
	if want := `{"error":{"code":"conflicting_flags","details":{"command":"flow show","flags":["--scenario","--part"]},` +
		`"hint":"Посмотреть описание команды: gentry flow show --help","message":"Флаги --scenario и --part нельзя указывать вместе."}}` + "\n"; stdout != want {
		t.Errorf("two objects --json: %s, want %s", stdout, want)
	}

	// Objects the flow or the draft does not have.
	shopFlowOver(t, p)
	mustRun(t, "flow", "edit")
	tests = []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"unknown stage", []string{"flow", "show", "--stage", "revew"}, contract.ExitError,
			"Во флоу проекта shop нет этапа «revew».\n\nПосмотреть перечень объектов: gentry flow show"},
		{"unknown scenario", []string{"flow", "show", "--scenario", "bg"}, contract.ExitError,
			"Во флоу проекта shop нет сценария «bg».\n\nПосмотреть перечень объектов: gentry flow show"},
		{"unknown agent in the draft", []string{"flow", "show", "--draft", "--agent", "auditor"}, contract.ExitError,
			"В черновике флоу проекта shop нет субагента «auditor».\n\nПосмотреть перечень объектов: gentry flow show --draft"},
		{"unknown part", []string{"flow", "show", "--part", "x"}, contract.ExitError,
			"Во флоу проекта shop нет фрагмента «x».\n\nПосмотреть перечень объектов: gentry flow show"},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}
	_, stdout, _ = run("flow", "show", "--draft", "--agent", "auditor", "--json")
	if want := `{"error":{"code":"flow_object_not_found","details":{"draft":true,"id":"auditor","kind":"agent","project":"shop"},`; !strings.HasPrefix(stdout, want) {
		t.Errorf("unknown agent --json: %s", stdout)
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

// shopFlowOver applies the flow of the flow testdata as the next version of
// the shop whose places are p.
func shopFlowOver(t *testing.T, p flow.Places) {
	t.Helper()
	mustRun(t, "flow", "edit")
	if err := os.CopyFS(p.Draft+"-src", os.DirFS(filepath.Join(flowTestdata, "shop", "flow-draft"))); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(p.Draft)
	if err := os.Rename(p.Draft+"-src", p.Draft); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "flow", "apply")
}

func TestFlowHelp(t *testing.T) {
	_, stdout, _ := run("flow", "--help")
	want := `Показать или изменить флоу проекта.
Флоу меняется только через черновик.

Использование:
  gentry flow <действие> [аргументы] [флаги]

Действия:
  show      Показать флоу проекта
  edit      Начать правку флоу
  diff      Показать изменения черновика
  apply     Применить черновик флоу
  discard   Удалить черновик флоу

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
