package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// machines are two machines of the operator in a test: their data roots and
// the remote repository of the process. The shop and its knowledge are
// shared, as if cloned on both.
type machines struct {
	t      *testing.T
	a, b   string
	remote string
}

// twoMachines applies the flow of the shop on machine A and sends its
// process to a remote repository, then connects the shop on machine B and
// takes the process there. The test is on B.
func twoMachines(t *testing.T) machines {
	t.Helper()
	shopFlow(t)
	m := machines{t: t, a: os.Getenv(home.EnvVar)}
	root := filepath.Dir(m.a)
	m.remote = filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", m.remote)
	mustRun(t, "process", "remote", m.remote)
	m.b = filepath.Join(root, "home-b")
	m.onB()
	mustRun(t, "project", "add", "--knowledge", "../shop-knowledge")
	mustRun(t, "process", "remote", m.remote)
	return m
}

func (m machines) onA() { m.t.Setenv(home.EnvVar, m.a) }
func (m machines) onB() { m.t.Setenv(home.EnvVar, m.b) }

func TestProcessRemoteText(t *testing.T) {
	p := shopFlow(t)
	root := filepath.Dir(os.Getenv(home.EnvVar))
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)

	_, stdout, _ := run("process", "status")
	if want := "Удалённый репозиторий не подключён.\n\nПодключить удалённый репозиторий: gentry process remote <адрес>\n"; stdout != want {
		t.Errorf("status without a remote:\n%s\nwant:\n%s", stdout, want)
	}
	code, stdout, stderr := run("process", "remote", remote)
	if want := "Удалённый репозиторий подключён: " + remote + "\nПроцесс: отправлен в удалённый репозиторий\n"; code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("remote: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	_, stdout, _ = run("process", "status")
	if want := "Удалённый репозиторий: " + remote + "\nСинхронизирован: <время>\n"; masked(stdout) != want {
		t.Errorf("status:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("process", "remote", remote); !strings.HasSuffix(stdout, "\nПроцесс: совпадает с удалённым репозиторием\n") {
		t.Errorf("remote again:\n%s", stdout)
	}

	// Another machine gets the process and shows the same flow.
	t.Setenv(home.EnvVar, filepath.Join(root, "home-b"))
	mustRun(t, "project", "add", "--knowledge", "../shop-knowledge")
	if _, stdout, _ := run("process", "remote", remote); !strings.HasSuffix(stdout, "\nПроцесс: получен из удалённого репозитория\n") {
		t.Errorf("remote on b:\n%s", stdout)
	}
	b := shopPlaces()
	_, stdout, _ = run("flow", "show")
	if want := "Проект: shop\nФлоу применён: <время>\nПапка флоу: " + b.Dir + "\n\n" + shopTables; !strings.HasPrefix(masked(stdout), want) || b.Dir == p.Dir {
		t.Errorf("show on b:\n%s", stdout)
	}

	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no address", []string{"process", "remote"}, contract.ExitUsage,
			"Не указан адрес удалённого репозитория.\n\nПосмотреть описание команды: gentry process remote --help"},
		{"unavailable", []string{"process", "remote", filepath.Join(root, "none.git")}, contract.ExitError,
			"Удалённый репозиторий недоступен: " + filepath.Join(root, "none.git") + "\n\nПроверить доступ: git ls-remote " + filepath.Join(root, "none.git")},
		{"code", []string{"process", "remote", filepath.Join(root, "shop")}, contract.ExitError,
			"Репозиторий не является репозиторием процесса: " + filepath.Join(root, "shop")},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}
	if _, stdout, _ := run("process", "status"); !strings.HasPrefix(stdout, "Удалённый репозиторий: "+remote+"\n") {
		t.Errorf("a refused address is kept:\n%s", stdout)
	}
}

func TestProcessSyncText(t *testing.T) {
	m := twoMachines(t)
	b := shopPlaces()
	writeDraft(t, b, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	if code, _, stderr := run("flow", "apply"); code != contract.ExitOK || stderr != "" {
		t.Fatalf("apply on b: exit code %d, stderr:\n%s", code, stderr)
	}
	m.onA()
	code, stdout, stderr := run("flow", "show", "--stage", "merge")
	if want := "Получены изменения с другой машины:\n  флоу shop\n\n"; code != contract.ExitOK || stderr != want ||
		!strings.HasSuffix(stdout, "  Влить ветку задачи в main после ревью.\n") {
		t.Errorf("show on a: exit code %d, stderr:\n%s\noutput:\n%s", code, stderr, stdout)
	}
	if _, stdout, stderr := run("process", "sync"); stdout != "Процесс синхронизирован.\n" || stderr != "" {
		t.Errorf("sync with nothing new: %q, %q", stdout, stderr)
	}

	// The library goes the same way.
	a := shopPlaces()
	writeFiles(t, a.Library, map[string]string{"reviewer.md": "Проверить изменения задачи и тексты.\n"})
	_, stdout, _ = run("library", "diff")
	want := "Библиотека применена: <время>\n\nИзменения:\n  Субагент reviewer: изменён\n\nПрименить изменения: gentry library apply\n"
	if masked(stdout) != want {
		t.Errorf("library diff:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("library", "apply"); masked(stdout) != "Изменения библиотеки применены.\nБиблиотека применена: <время>\n" {
		t.Errorf("library apply:\n%s", stdout)
	}
	m.onB()
	if _, stdout, _ := run("process", "sync"); stdout != "Процесс синхронизирован.\n\nПолучено:\n  библиотека субагентов\n" {
		t.Errorf("sync on b:\n%s", stdout)
	}
	writeFiles(t, b.Library, map[string]string{"reviewer.md": "Черновик.\n"})
	if _, stdout, _ := run("library", "discard"); stdout != "Изменения библиотеки отменены.\n" {
		t.Errorf("library discard:\n%s", stdout)
	}
	if b, _ := os.ReadFile(filepath.Join(b.Library, "reviewer.md")); string(b) != "Проверить изменения задачи и тексты.\n" {
		t.Errorf("reviewer after discard: %q", b)
	}
}

func TestProcessConflictText(t *testing.T) {
	m := twoMachines(t)
	b := shopPlaces()
	m.onA()
	a := shopPlaces()
	writeDraft(t, a, map[string]string{"stages/review.md": "Проверить изменения по списку машины А.\n"})
	mustRun(t, "flow", "apply")
	m.onB()
	writeDraft(t, b, map[string]string{"stages/review.md": "Проверить изменения по списку машины Б.\n"})
	code, stdout, stderr := run("flow", "apply")
	want := "Флоу проекта shop изменён на другой машине; изменения этой машины сохранены как черновик.\nВерсии файлов другой машины: " + b.Conflict +
		"\n\nИзменены на обеих машинах:\n  Этап review\n\nПосмотреть отличия: gentry flow diff --project shop\n"
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("apply on b: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	if text, err := os.ReadFile(filepath.Join(b.Conflict, "stages", "review.md")); err != nil || string(text) != "Проверить изменения по списку машины А.\n" {
		t.Errorf("variant of a: %q, %v", text, err)
	}
	_, stdout, _ = run("process", "status")
	if want := "Удалённый репозиторий: " + m.remote + "\nСинхронизирован: <время>\n\nЧерновики:\n  флоу shop\n\nКонфликты:\n  флоу shop\n"; masked(stdout) != want {
		t.Errorf("status:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := run("flow", "diff"); !strings.Contains(stdout, "Изменения:\n  Этап review: изменён\n") {
		t.Errorf("diff:\n%s", stdout)
	}
	_, stdout, _ = run("flow", "show", "--json")
	var out contract.FlowShowOutput
	json.Unmarshal([]byte(stdout), &out)
	if out.Draft == nil || out.Draft.ConflictDir == nil || *out.Draft.ConflictDir != b.Conflict || len(out.Draft.Conflicts) != 1 ||
		out.Draft.Conflicts[0].Object != "stage" || *out.Draft.Conflicts[0].Id != "review" {
		t.Errorf("show --json: %s", stdout)
	}
	// Applied again, the variant of this machine wins, and the conflict ends.
	if code, _, stderr := run("flow", "apply"); code != contract.ExitOK || stderr != "" {
		t.Errorf("apply again: exit code %d, stderr:\n%s", code, stderr)
	}
	if _, err := os.Stat(b.Conflict); !os.IsNotExist(err) {
		t.Errorf("conflict/ after apply: %v", err)
	}
	m.onA()
	if _, stdout, _ := run("flow", "show", "--stage", "review"); !strings.Contains(stdout, "машины Б.") {
		t.Errorf("a after the conflict:\n%s", stdout)
	}

	// The --json refusal has its code.
	m.onB()
	writeDraft(t, b, map[string]string{"stages/review.md": "Б снова.\n"})
	m.onA()
	writeDraft(t, a, map[string]string{"stages/review.md": "А снова.\n"})
	mustRun(t, "flow", "apply")
	m.onB()
	_, stdout, _ = run("flow", "apply", "--json")
	validate(t, "schemas/error.json", stdout)
	if !strings.HasPrefix(stdout, `{"error":{"code":"flow_conflict","details":{"conflict_dir":`+jsonString(b.Conflict)+`,"project":"shop"},`) {
		t.Errorf("--json: %s", stdout)
	}
}

func TestProcessOfflineText(t *testing.T) {
	m := twoMachines(t)
	if err := os.Rename(m.remote, m.remote+"-away"); err != nil {
		t.Fatal(err)
	}
	b := shopPlaces()
	writeDraft(t, b, map[string]string{"stages/merge.md": "Влить ветку задачи в main без сети.\n"})
	code, stdout, stderr := run("flow", "apply")
	if code != contract.ExitOK || stderr != "Отправка отложена: удалённый репозиторий недоступен.\n\n" ||
		!strings.HasPrefix(stdout, "Изменения флоу применены.\n") {
		t.Errorf("apply offline: exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	if _, _, stderr := run("flow", "show"); stderr != "Синхронизация пропущена: удалённый репозиторий недоступен.\n\n" {
		t.Errorf("show offline: stderr %q", stderr)
	}
	_, stdout, _ = run("process", "status")
	if want := "\nНе отправлено:\n  флоу shop\n\nСинхронизировать: gentry process sync\n"; !strings.HasSuffix(stdout, want) {
		t.Errorf("status:\n%s", stdout)
	}
	code, stdout, stderr = run("process", "sync")
	if want := "Удалённый репозиторий недоступен: " + m.remote + "\n\nПроверить доступ: git ls-remote " + m.remote + "\n"; code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("sync offline: exit code %d, stderr:\n%s", code, stderr)
	}
	if err := os.Rename(m.remote+"-away", m.remote); err != nil {
		t.Fatal(err)
	}
	if _, stdout, _ := run("process", "sync"); stdout != "Процесс синхронизирован.\n\nОтправлено:\n  флоу shop\n" {
		t.Errorf("sync online:\n%s", stdout)
	}
}

func TestLibraryRefusals(t *testing.T) {
	connectedShop(t)
	p := shopPlaces()
	code, _, stderr := run("library", "diff")
	if want := "У библиотеки субагентов нет черновика.\nПапка библиотеки: " + p.Library + "\n"; code != contract.ExitError || stderr != want {
		t.Errorf("no draft: exit code %d, stderr:\n%s", code, stderr)
	}
	writeFiles(t, p.Library, map[string]string{"tester.yaml": "capabilities: [read]\n", "notes.txt": "x\n"})
	code, _, stderr = run("library", "apply")
	want := "В изменениях библиотеки есть ошибки.\nПапка библиотеки: " + p.Library + "\n\nОшибки:\n" +
		"  Субагент tester: не заполнено поле «purpose».\n  Субагент tester: нет инструкции.\n" +
		"  Файл не относится к библиотеке субагентов: notes.txt\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("invalid: exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ := run("library", "apply", "--json")
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	validate(t, "schemas/library-draft-invalid.json", string(out.Error.Details))
	if out.Error.Code != contract.CodeLibraryDraftInvalid || !strings.Contains(string(out.Error.Details), `"file":"tester.yaml"`) {
		t.Errorf("--json: %s", stdout)
	}
	if _, stdout, _ := run("library", "--help"); !strings.HasPrefix(stdout, msg.Text(msg.CmdLibraryDesc)+"\n") {
		t.Errorf("help:\n%s", stdout)
	}
}

func TestProcessJSON(t *testing.T) {
	shopFlow(t)
	root := filepath.Dir(os.Getenv(home.EnvVar))
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)
	_, stdout, _ := run("process", "status", "--json")
	validate(t, "schemas/process-status.json", stdout)
	if stdout != `{"conflicts":[],"drafts":[],"unsent":[]}`+"\n" {
		t.Errorf("status: %s", stdout)
	}
	_, stdout, _ = run("process", "remote", remote, "--json")
	validate(t, "schemas/process-remote.json", stdout)
	if want := `{"drafts":[],"remote":` + jsonString(remote) + `,"result":"sent"}` + "\n"; stdout != want {
		t.Errorf("remote: %s, want %s", stdout, want)
	}
	p := shopPlaces()
	writeFiles(t, p.Library, map[string]string{"reviewer.md": "Новая инструкция.\n"})
	_, stdout, _ = run("library", "diff", "--json")
	validate(t, "schemas/library-diff.json", stdout)
	if !strings.HasSuffix(stdout, `"changes":[{"change":"modified","id":"reviewer"}],"dir":`+jsonString(p.Library)+"}\n") {
		t.Errorf("library diff: %s", stdout)
	}
	_, stdout, _ = run("library", "apply", "--json")
	validate(t, "schemas/library-apply.json", stdout)
	if !strings.Contains(stdout, `"sent":true,"sync":{"sent":[{"kind":"library"}]}`) {
		t.Errorf("library apply: %s", stdout)
	}
	writeFiles(t, p.Library, map[string]string{"reviewer.md": "Ещё.\n"})
	_, stdout, _ = run("library", "discard", "--json")
	validate(t, "schemas/library-discard.json", stdout)
	_, stdout, _ = run("process", "sync", "--json")
	validate(t, "schemas/process-sync.json", stdout)
	if stdout != `{"conflicts":[],"received":[],"restored":[],"sent":[]}`+"\n" {
		t.Errorf("sync: %s", stdout)
	}
	_, stdout, _ = run("process", "status", "--json")
	validate(t, "schemas/process-status.json", stdout)
	if !strings.Contains(stdout, `"remote":`+jsonString(remote)+`,"synced":"`) {
		t.Errorf("status with a remote: %s", stdout)
	}

	// Events of the process have no project and match their schemas.
	_, stdout, _ = run("events")
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var e contract.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(e.Type, "process.") && !strings.HasPrefix(e.Type, "library.") {
			continue
		}
		if e.Project != nil {
			t.Errorf("%s has a project", e.Type)
		}
		data, _ := json.Marshal(e.Data)
		validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type)
	}
	if got := strings.Join(types, " "); got != "library.applied process.remote_set process.synced library.applied process.synced library.draft_discarded" {
		t.Errorf("events: %s", got)
	}
}

// TestProjectAddMakesFlowDir checks that connecting a project prepares the
// place of its flow in the process repository.
func TestProjectAddMakesFlowDir(t *testing.T) {
	connectedShop(t)
	p := shopPlaces()
	process := filepath.Dir(p.Library)
	if log := gittest.Run(t, process, "log", "--format=%s"); log != "Create the process repository\n" {
		t.Errorf("log: %q", log)
	}
	if fi, err := os.Stat(filepath.Join(p.Dir, "stages")); err != nil || !fi.IsDir() {
		t.Errorf("flow directory: %v", err)
	}
}

// TestLibraryBreaksFlow checks that a library that breaks an active flow is
// not applied.
func TestLibraryBreaksFlow(t *testing.T) {
	p := shopFlow(t)
	writeFiles(t, p.Library, map[string]string{"reviewer.yaml": "", "reviewer.md": ""})
	code, stdout, stderr := run("library", "apply")
	want := "Изменения библиотеки вносят ошибки во флоу проектов.\nПапка библиотеки: " + p.Library + "\n\n" +
		"Ошибки флоу проекта shop:\n  Этап review: субагент reviewer не найден.\n"
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ = run("library", "apply", "--json")
	var out struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	validate(t, "schemas/library-draft-invalid.json", string(out.Error.Details))
	if want := `{"dir":` + jsonString(p.Library) + `,"flows":[{"problems":[{"code":"unknown_executor","file":"stages/review.yaml","line":3,` +
		`"message":"Этап review: субагент reviewer не найден."}],"project":"shop"}],"problems":[]}`; string(out.Error.Details) != want {
		t.Errorf("details:\n%s\nwant:\n%s", out.Error.Details, want)
	}

	// Once the flow does without the subagent, the library is applied.
	writeDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n"})
	mustRun(t, "flow", "apply")
	mustRun(t, "library", "apply")
}
