package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// shopFlow connects the shop, gives it the flow of the flow testdata and
// returns the flow directory. The test runs in the main worktree of the shop.
func shopFlow(t *testing.T) string {
	t.Helper()
	testdata, err := filepath.Abs("../flow/testdata/process")
	if err != nil {
		t.Fatal(err)
	}
	connectedShop(t)
	process := filepath.Join(os.Getenv(home.EnvVar), "process")
	if err := os.CopyFS(process, os.DirFS(testdata)); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(process, "shop", "flow")
}

func TestFlowShowText(t *testing.T) {
	dir := shopFlow(t)
	code, stdout, stderr := run("flow", "show")
	want := strings.Join([]string{
		"Проект: shop",
		"Папка флоу: " + dir,
		"",
		"СЦЕНАРИЙ  НАЗВАНИЕ",
		"bug       Баг",
		"feature   Фича",
		"",
		"ЭТАП            НАЗВАНИЕ    ИСПОЛНИТЕЛЬ   ВЫХОД",
		"branch          Ветка       orchestrator  создана ветка задачи",
		"implementation  Реализация  orchestrator  изменения сделаны, тесты проходят",
		"merge           Слияние     operator      ветка задачи влита в main",
		"plan-bug        План бага   orchestrator  причина установлена, план исправления согласован",
		"plan-feature    План фичи   orchestrator  план согласован с оператором",
		"review          Ревью       reviewer      замечания ревью записаны и разобраны",
		"Сценарий подробно: gentry flow show <сценарий>",
	}, "\n") + "\n"
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	code, stdout, _ = run("flow", "show", "feature")
	want = strings.Join([]string{
		"Сценарий: feature",
		"Название: Фича",
		"",
		"Путь по умолчанию:",
		"  branch → plan → implementation → review → merge → конец",
		"",
		"Условные переходы:",
		"  review → implementation, не более 3 кругов: ревью выявило существенные замечания",
		"",
		"УЗЕЛ            ЭТАП            ИСПОЛНИТЕЛЬ",
		"branch          branch          orchestrator",
		"plan            plan-feature    orchestrator",
		"implementation  implementation  orchestrator",
		"review          review          reviewer",
		"merge           merge           operator",
	}, "\n") + "\n"
	if code != contract.ExitOK || stdout != want {
		t.Errorf("feature: exit code %d, output:\n%s\nwant:\n%s", code, stdout, want)
	}
}

func TestFlowShowConditional(t *testing.T) {
	dir := shopFlow(t)
	bug := "title: Баг\nstart: triage\nnodes:\n" +
		"  triage:\n    stage: plan-bug\n    next:\n      - to: merge\n        if: срочно\n      - to: finish\n        if: |\n          не воспроизводится\n          на main\n" +
		"  merge: { stage: merge, next: finish }\n"
	if err := os.WriteFile(filepath.Join(dir, "scenarios", "bug.yaml"), []byte(bug), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, stderr := run("flow", "show", "bug")
	// At a fork the default path stops; the end is named in words.
	want := "Путь по умолчанию:\n  triage\n\nУсловные переходы:\n  triage → merge: срочно\n  triage → конец: не воспроизводится на main\n\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stderr %q, output:\n%s\nwant within:\n%s", stderr, stdout, want)
	}
}

func TestFlowShowJSON(t *testing.T) {
	dir := shopFlow(t)
	code, stdout, _ := run("flow", "show", "--json")
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	validate(t, "schemas/flow-show.json", stdout)
	var out contract.FlowShowOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Project != "shop" || out.Dir != dir || len(out.Scenarios) != 2 || len(out.Stages) != 6 || len(out.Parts) != 2 {
		t.Errorf("unexpected output: %s", stdout)
	}

	_, stdout, _ = run("flow", "show", "feature", "--json")
	validate(t, "schemas/flow-show.json", stdout)
	for _, part := range []string{
		`"scenarios":[{"id":"feature","nodes":[{"id":"branch","next":[{"to":"plan"}],"stage":"branch"},`,
		`{"id":"review","next":[{"if":"ревью выявило существенные замечания","max_rounds":3,"to":"implementation"},{"to":"merge"}],"stage":"review"},`,
		`{"id":"merge","next":[{"to":"finish"}],"stage":"merge"}],"start":"branch","title":"Фича"}]`,
		`{"executor":"operator","exit":"ветка задачи влита в main","id":"merge","include":[],"title":"Слияние"}`,
		`"parts":["plan-format","review-checklist"]`,
	} {
		if !strings.Contains(stdout, part) {
			t.Errorf("feature: no %s in\n%s", part, stdout)
		}
	}
	// Only the stages of the scenario.
	if strings.Contains(stdout, `"plan-bug"`) {
		t.Errorf("feature: stage of bug in\n%s", stdout)
	}
}

func TestFlowShowInvalid(t *testing.T) {
	dir := shopFlow(t)
	write := func(rel, text string) {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stages/merge.yml", "x")
	write("stages/plan-bug.yaml", "title: План бага\nexecutor: orchestrator\n")
	write("scenarios/bug.yaml", "title: Баг\nstart: brnch\nnodes:\n  branch: { stage: branch, next: finish }\n")

	code, stdout, stderr := run("flow", "show")
	want := strings.Join([]string{
		"Во флоу проекта shop есть ошибки.",
		"Папка флоу: " + dir,
		"",
		"Ошибки:",
		"  Сценарий bug: начальный узел brnch не найден.",
		"  Этап plan-bug: не заполнено поле «exit».",
		"  Файл не относится к флоу: stages/merge.yml",
	}, "\n") + "\n"
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	// A named scenario is not shown either.
	if code, _, stderr := run("flow", "show", "feature"); code != contract.ExitError || stderr != want {
		t.Errorf("feature: exit code %d, stderr:\n%s", code, stderr)
	}

	code, stdout, _ = run("flow", "show", "--json")
	if code != contract.ExitError {
		t.Errorf("--json: exit code %d", code)
	}
	validate(t, "schemas/error.json", stdout)
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error.Code != contract.CodeFlowInvalid || out.Error.Message != msg.Text(msg.ErrFlowInvalid, "shop") {
		t.Errorf("--json: %s", stdout)
	}
	validate(t, "schemas/flow-invalid.json", string(out.Error.Details))
	wantDetails := `{"dir":` + jsonString(dir) + `,"problems":[` +
		`{"code":"unknown_node","file":"scenarios/bug.yaml","line":2,"message":"Сценарий bug: начальный узел brnch не найден."},` +
		`{"code":"missing_field","file":"stages/plan-bug.yaml","message":"Этап plan-bug: не заполнено поле «exit»."},` +
		`{"code":"extra_file","file":"stages/merge.yml","message":"Файл не относится к флоу: stages/merge.yml"}],"project":"shop"}`
	if string(out.Error.Details) != wantDetails {
		t.Errorf("details:\n%s\nwant:\n%s", out.Error.Details, wantDetails)
	}
}

// TestFlowShowByProcessDir checks that the project is found by its process
// directory, so the operator can edit the flow and show it in one place.
func TestFlowShowByProcessDir(t *testing.T) {
	dir := shopFlow(t)
	t.Chdir(filepath.Join(dir, "stages"))
	if code, stdout, stderr := run("flow", "show"); code != contract.ExitOK || !strings.HasPrefix(stdout, "Проект: shop\n") {
		t.Errorf("exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	// Another project of the same data root is named by --project.
	t.Chdir(filepath.Dir(dir))
	if code, stdout, _ := run("worktree", "list", "--json"); code != contract.ExitOK || !strings.Contains(stdout, `"project":"shop"`) {
		t.Errorf("worktree list in the process directory: exit code %d, %s", code, stdout)
	}
}

func TestFlowShowRefusals(t *testing.T) {
	dir := shopFlow(t)
	tests := []struct {
		name    string
		args    []string
		prepare func()
		exit    int
		stderr  string
	}{
		{"unknown scenario", []string{"flow", "show", "bg"}, nil, contract.ExitError,
			msg.Text(msg.ErrScenarioNotFound, "shop", "bg") + "\n" + msg.Text(msg.HintScenarioNotFound)},
		{"unknown project", []string{"flow", "show", "--project", "cart"}, nil, contract.ExitError,
			msg.Text(msg.ErrProjectNotFound, "cart") + "\n" + msg.Text(msg.HintProjectNotFound)},
		{"empty project", []string{"flow", "show", "--project="}, nil, contract.ExitUsage,
			msg.Text(msg.ErrFlagValueMissing, "--project")},
		{"extra argument", []string{"flow", "show", "bug", "feature"}, nil, contract.ExitUsage,
			msg.Text(msg.ErrExtraArgs, "flow show", "feature")},
		{"no action", []string{"flow"}, nil, contract.ExitUsage,
			msg.Text(msg.ErrActionMissing, "flow") + "\n" + "Перечень действий: gentry flow --help"},
		{"unknown action", []string{"flow", "check"}, nil, contract.ExitUsage,
			msg.Text(msg.ErrActionUnknown, "check", "flow") + "\n" + "Перечень действий: gentry flow --help"},
		{"no flow", []string{"flow", "show"}, func() { os.RemoveAll(dir) }, contract.ExitError,
			msg.Text(msg.ErrFlowNotFound, "shop")},
	}
	for _, tt := range tests {
		if tt.prepare != nil {
			tt.prepare()
		}
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}

	code, stdout, _ := run("flow", "show", "--json")
	validate(t, "schemas/error.json", stdout)
	if want := `{"error":{"code":"flow_not_found","details":{"dir":` + jsonString(dir) + `,"project":"shop"},` +
		`"message":"У проекта shop нет флоу."}}` + "\n"; code != contract.ExitError || stdout != want {
		t.Errorf("no flow --json: exit code %d, %s, want %s", code, stdout, want)
	}

	// Outside any project, without a state store.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Chdir(t.TempDir())
	if code, _, stderr := run("flow", "show"); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectUndetermined, "")) {
		t.Errorf("outside a project: exit code %d, stderr %q", code, stderr)
	}
}
