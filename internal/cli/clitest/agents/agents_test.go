package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/gittest"
)

// reviewerFile is the file of the subagent reviewer of the shop in worktree w.
func reviewerFile(w string) string { return filepath.Join(w, ".claude", "agents", "reviewer.md") }

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func clean(t *testing.T, w string) {
	t.Helper()
	if files, err := git.Changed(w); err != nil || len(files) > 0 {
		t.Errorf("git status of %s: %v, %v; want clean", w, files, err)
	}
}

const nextSession = "Изменения вступят в силу со следующей сессии агента."

// changeLibrary rewrites the instruction of reviewer in the library and
// applies it.
func changeLibrary(t *testing.T, text string) string {
	t.Helper()
	clitest.WriteFiles(t, clitest.ShopPlaces().Library, map[string]string{"reviewer.md": text})
	_, stdout, _ := clitest.Run("library", "apply")
	return stdout
}

func TestPoolIsLaidOut(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	for _, w := range []string{shop, fix} {
		text := read(t, reviewerFile(w))
		if !strings.HasPrefix(text, "---\n# Gentry: ") || !strings.Contains(text, "Проверить изменения задачи") {
			t.Errorf("reviewer in %s:\n%s", w, text)
		}
		clean(t, w)
	}
}

func TestWorktreeAddText(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-extra"), "extra")
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		"Рабочая копия внесена в пул проекта shop: "+extra,
		"",
		"Субагенты в рабочей копии изменены: добавлен reviewer.",
		nextSession,
	), "", "worktree", "add", extra)
	clean(t, extra)

	other := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-other"), "other")
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", other)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if a := res.Agents; a == nil || len(a.Worktrees) != 1 || a.Worktrees[0].Path != other || strings.Join(a.Worktrees[0].Added, ",") != "reviewer" {
		t.Errorf("JSON: %s", out)
	}
}

func TestWorktreeAddWarning(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	// A branch of the project tracks a file of the name of reviewer.
	gittest.Run(t, shop, "switch", "--quiet", "-c", "own-agents")
	os.WriteFile(reviewerFile(shop), []byte("проектный\n"), 0o644)
	gittest.Run(t, shop, "add", "--force", ".claude/agents/reviewer.md")
	gittest.Run(t, shop, "commit", "--quiet", "-m", "reviewer")
	gittest.Run(t, shop, "switch", "--quiet", "-")
	extra := filepath.Join(filepath.Dir(shop), "shop-own")
	gittest.Run(t, shop, "worktree", "add", "--quiet", extra, "own-agents")

	code, stdout, stderr := clitest.Run("worktree", "add", extra)
	want := clitest.Lines(
		"Рабочая копия внесена в пул проекта shop: "+extra,
		"",
		"Часть субагентов не разложена: файлы с их именами уже есть.",
		"",
	) + clitest.Table(
		[]string{"РАБОЧАЯ КОПИЯ", "СУБАГЕНТ", "ФАЙЛ", "ПРИЧИНА"},
		[]string{extra, "reviewer", ".claude/agents/reviewer.md", "файл отслеживается git проекта"},
	) + clitest.Lines("", "Разложить субагентов после разбора файлов: gentry agents sync")
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Fatalf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	if read(t, reviewerFile(extra)) != "проектный\n" {
		t.Error("the tracked file is changed")
	}

	// A task is not taken there.
	if code, c := clitest.ErrorCode(t, clitest.TakeArgs("--worktree", extra)...); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("take: exit code %d, code %q", code, c)
	}
}

func TestTakeConflict(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)

	code, stdout, stderr := clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	want := clitest.Lines("Субагентов нельзя разложить: файлы с их именами уже есть.", "") + clitest.Table(
		[]string{"РАБОЧАЯ КОПИЯ", "СУБАГЕНТ", "ФАЙЛ", "ПРИЧИНА"},
		[]string{fix, "reviewer", ".claude/agents/reviewer.md", "файл разложен не Gentry"},
	) + clitest.Lines("", "Необходимо переименовать субагента во флоу или удалить файл из рабочей копии, затем повторить команду.")
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Fatalf("exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	if _, out, _ := clitest.Run("task", "list"); strings.Contains(out, "SHOP-1") {
		t.Errorf("the task is taken:\n%s", out)
	}
	if read(t, reviewerFile(fix)) != "личный\n" {
		t.Error("the file of the operator is changed")
	}

	out := clitest.WantJSON(t, contract.ExitError, "schemas/error.json", clitest.TakeArgs("--worktree", fix)...)
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Conflicts []contract.AgentsConflict `json:"conflicts"`
			} `json:"details"`
		} `json:"error"`
	}
	json.Unmarshal([]byte(out), &e)
	if c := e.Error.Details.Conflicts; e.Error.Code != contract.CodeAgentsConflict || len(c) != 1 || c[0].Reason != "foreign" || c[0].Worktree != fix {
		t.Errorf("JSON: %s", out)
	}

	// Once the file is gone, the task is taken and the subagent laid out.
	os.Remove(reviewerFile(fix))
	code, stdout, _ = clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\nСубагенты в рабочей копии изменены: добавлен reviewer.\n"+nextSession+"\n\n") {
		t.Errorf("take after the file is removed: exit code %d, output:\n%s", code, stdout)
	}
	clean(t, fix)
}

func TestApplyLaysOutFreeWorktreesOnly(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	stdout := changeLibrary(t, "Проверить изменения и тексты.\n")
	if !strings.HasSuffix(clitest.Masked(stdout), "\n\nСубагенты разложены в свободные рабочие копии.\n"+nextSession+"\n") {
		t.Errorf("library apply:\n%s", stdout)
	}
	if !strings.Contains(read(t, reviewerFile(shop)), "Проверить изменения и тексты.") {
		t.Error("the free worktree keeps the old reviewer")
	}
	if read(t, reviewerFile(fix)) != before {
		t.Error("the worktree of the task is changed")
	}

	// The worktree the task releases gets the active flow.
	code, stdout, _ := clitest.Run("task", "cancel", "SHOP-1")
	if code != contract.ExitOK || !strings.Contains(stdout, "\n\nСубагенты в рабочей копии изменены: обновлён reviewer.\n"+nextSession+"\n") {
		t.Errorf("cancel: exit code %d, output:\n%s", code, stdout)
	}
	if !strings.Contains(read(t, reviewerFile(fix)), "Проверить изменения и тексты.") {
		t.Error("the released worktree keeps the reviewer of the snapshot")
	}
	clean(t, fix)
}

func TestFlowApplyRemoves(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	_, stdout, _ := clitest.Run("flow", "apply")
	if !strings.HasSuffix(clitest.Masked(stdout), "\n\nСубагенты разложены в свободные рабочие копии проекта shop.\n"+nextSession+"\n") {
		t.Errorf("flow apply:\n%s", stdout)
	}
	for _, w := range []string{shop, fix} {
		if clitest.FileExists(reviewerFile(w)) {
			t.Errorf("reviewer is still in %s", w)
		}
		clean(t, w)
	}
}

func TestAgentsSyncText(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	gone := filepath.Join(filepath.Dir(shop), "shop-gone")
	gittest.Worktree(t, shop, gone, "gone")
	clitest.MustRun(t, "worktree", "add", gone)
	os.RemoveAll(gone)

	header := []string{"РАБОЧАЯ КОПИЯ", "ЗАДАЧА", "СУБАГЕНТЫ", "ИЗМЕНЕНИЯ"}
	clitest.WantRun(t, contract.ExitOK, clitest.Lines("Субагенты в рабочих копиях проекта shop соответствуют флоу.", "")+clitest.Table(header,
		[]string{shop, "—", "reviewer", "—"},
		[]string{fix, "SHOP-1", "reviewer", "—"},
		[]string{gone, "—", "—", "папки нет, копия пропущена"},
	), "", "agents", "sync")

	os.Remove(reviewerFile(shop))
	clitest.WantRun(t, contract.ExitOK, clitest.Lines("Субагенты разложены в рабочие копии проекта shop.", nextSession, "")+clitest.Table(header,
		[]string{shop, "—", "reviewer", "добавлен reviewer"},
		[]string{fix, "SHOP-1", "reviewer", "—"},
		[]string{gone, "—", "—", "папки нет, копия пропущена"},
	), "", "agents", "sync")

	out := clitest.WantJSON(t, contract.ExitOK, "schemas/agents-sync.json", "agents", "sync")
	var res contract.AgentsSyncOutput
	json.Unmarshal([]byte(out), &res)
	if res.Project != "shop" || res.Changed || len(res.Worktrees) != 3 || res.Worktrees[1].Task == nil || *res.Worktrees[1].Task != "SHOP-1" {
		t.Errorf("JSON: %s", out)
	}

	// A conflict in any worktree refuses the command and changes nothing.
	os.Remove(reviewerFile(shop))
	os.WriteFile(reviewerFile(fix), []byte("личный\n"), 0o644)
	if code, c := clitest.ErrorCode(t, "agents", "sync"); code != contract.ExitError || c != contract.CodeAgentsConflict {
		t.Errorf("conflict: exit code %d, code %q", code, c)
	}
	if clitest.FileExists(reviewerFile(shop)) {
		t.Error("a worktree is laid out despite the conflict")
	}
}

// passScenario takes the task of the current worktree through the feature
// scenario by skips.
func passScenario(t *testing.T) {
	t.Helper()
	for range 3 {
		clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен")
	}
	clitest.MustRun(t, "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	clitest.MustRun(t, "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
}

func TestCloseAndFlowApplyWithTask(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
	before := read(t, reviewerFile(fix))

	// flow apply leaves the worktree of the task to its snapshot.
	clitest.WriteDraft(t, clitest.ShopPlaces(), map[string]string{
		"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
	})
	clitest.MustRun(t, "flow", "apply")
	if read(t, reviewerFile(fix)) != before {
		t.Error("flow apply changed the worktree of the task")
	}

	t.Chdir(fix)
	passScenario(t)
	code, stdout, _ := clitest.Run("task", "close")
	if code != contract.ExitOK || !strings.HasSuffix(stdout, "\n\nСубагенты в рабочей копии изменены: удалён reviewer.\n"+nextSession+"\n") {
		t.Errorf("close: exit code %d, output:\n%s", code, stdout)
	}
	if clitest.FileExists(reviewerFile(fix)) {
		t.Error("the released worktree keeps reviewer")
	}
	clean(t, fix)
}

func TestLayoutFailureText(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	extra := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-broken"), "broken")
	// A file where the directory of subagents must be.
	os.WriteFile(filepath.Join(extra, ".claude"), []byte("файл\n"), 0o644)
	gittest.Run(t, extra, "add", ".claude")
	gittest.Run(t, extra, "commit", "--quiet", "-m", "file")

	code, stdout, _ := clitest.Run("worktree", "add", extra)
	head := clitest.Lines("Рабочая копия внесена в пул проекта shop: "+extra, "", "Субагентов не удалось разложить.")
	tail := clitest.Lines("", "Повторить раскладку: gentry agents sync")
	if code != contract.ExitOK || !strings.HasPrefix(stdout, head+"Причина: ") || !strings.HasSuffix(stdout, tail) {
		t.Errorf("exit code %d, output:\n%s", code, stdout)
	}
	out := clitest.WantJSON(t, contract.ExitOK, "schemas/worktree-add.json", "worktree", "add", extra)
	var res contract.WorktreeAddOutput
	json.Unmarshal([]byte(out), &res)
	if res.Agents == nil || res.Agents.Error == nil || *res.Agents.Error == "" {
		t.Errorf("JSON: %s", out)
	}
}
