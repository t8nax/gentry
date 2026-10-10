package task_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
)

// introIn runs the session start hook for a session in dir, as the plugin of
// Claude Code runs it, and returns the introduction.
func introIn(t *testing.T, dir string) string {
	t.Helper()
	return introBy(t, dir, "--tool", claude.Tool)
}

// introBy runs the session start hook for a session in dir with the flags
// and returns the introduction.
func introBy(t *testing.T, dir string, flags ...string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	code, stdout, stderr := clitest.RunWith(string(in), append([]string{"hook", "session-start"}, flags...)...)
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("hook in %s: exit code %d, stderr %q", dir, code, stderr)
	}
	return stdout
}

// The introduction is built from the state store: no test of package cli
// reaches the line of no task, this test states its words.
func TestIntroWithoutTask(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	want := clitest.Lines(
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskWorktree, fix),
		"Задача в этой копии не взята.",
		"",
		msg.Text(msg.HintIntroTake, "gentry:working-on-task"),
	)
	if got := introIn(t, fix); got != want {
		t.Errorf("introduction:\n%s\nwant:\n%s", got, want)
	}
	// A subdirectory of the worktree is the worktree; without cwd in the
	// input the hook takes the current directory.
	sub := filepath.Join(fix, "backend")
	os.MkdirAll(sub, 0o755)
	if got := introIn(t, sub); got != want {
		t.Errorf("introduction in a subdirectory:\n%s", got)
	}
	t.Chdir(fix)
	if _, got, _ := clitest.RunWith("{}", "hook", "session-start", "--tool", "claude"); got != want {
		t.Errorf("introduction without cwd:\n%s", got)
	}
	// Without a tool the skill is named by its name alone.
	want = strings.Replace(want, "gentry:working-on-task", "working-on-task", 1)
	if got := introBy(t, fix); got != want {
		t.Errorf("introduction without a tool:\n%s\nwant:\n%s", got, want)
	}
}

func TestIntroWithTask(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	t.Chdir(fix)
	clitest.MustRun(t, clitest.TakeArgs()...)
	head := clitest.Lines(
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskWorktree, fix),
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
	)
	tail := clitest.Lines("", msg.Text(msg.HintIntroContinue, "gentry:working-on-task"))
	branch := msg.Text(msg.TaskStage, round("Ветка", "branch", 1))
	progress := func(passed int) string { return msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, passed, 5)) }

	want := head + clitest.Lines(branch, msg.Text(msg.IntroNoSteps), progress(0)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("stage without steps:\n%s\nwant:\n%s", got, want)
	}

	// Dropped steps do not count.
	clitest.MustRun(t, "step", "add", "Создать ветку", "Проверить имя", "Удалить старую ветку")
	clitest.MustRun(t, "step", "done", "1")
	clitest.MustRun(t, "step", "drop", "3", "--reason", "Старой ветки нет")
	want = head + clitest.Lines(branch, msg.Text(msg.IntroSteps, 1, 2), progress(0)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("stage with steps:\n%s\nwant:\n%s", got, want)
	}

	// A passed scenario has no stage and no steps.
	clitest.MustRun(t, "step", "done", "2")
	passScenario(t)
	want = head + clitest.Lines(msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)), progress(5)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("passed scenario:\n%s\nwant:\n%s", got, want)
	}

	// A closed task frees the worktree.
	clitest.MustRun(t, "task", "close")
	if got := introIn(t, fix); got != clitest.Lines(
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskWorktree, fix),
		msg.Text(msg.IntroNoTask),
		"",
		msg.Text(msg.HintIntroTake, "gentry:working-on-task"),
	) {
		t.Errorf("closed task:\n%s", got)
	}
}

func TestIntroOutsidePool(t *testing.T) {
	shop, _ := clitest.TaskShop(t)
	for name, dir := range map[string]string{
		"knowledge": filepath.Join(filepath.Dir(shop), "shop-knowledge"),
		"process":   filepath.Join(os.Getenv(home.EnvVar), "process", "shop"),
		"outside":   t.TempDir(),
		"missing":   filepath.Join(t.TempDir(), "gone"),
	} {
		if got := introIn(t, dir); got != "" {
			t.Errorf("%s: want no introduction, got:\n%s", name, got)
		}
	}
}

func TestIntroWithoutStore(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if got := introIn(t, t.TempDir()); got != "" {
		t.Errorf("want no introduction, got:\n%s", got)
	}
	if clitest.FileExists(filepath.Join(os.Getenv(home.EnvVar), "state", "state.db")) {
		t.Error("the hook created the state store")
	}
}

func TestIntroStalePlugin(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	root := t.TempDir()
	manifest := filepath.Join(root, ".claude-plugin", "plugin.json")
	writeVersion := func(v string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(manifest), 0o755)
		if err := os.WriteFile(manifest, []byte(`{"name":"gentry","version":"`+v+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(claude.PluginRootEnv, root)

	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)

	writeVersion("0.0.0+00000000")
	// The warning is built in the introduction from the plugin: this test
	// states its words.
	want := clitest.Lines(
		"Плагин Gentry не соответствует программе gentry: скиллы могут описывать прежние команды.",
		msg.Text(msg.HintIntroPluginStale)+": setup (tool: claude)",
		"",
		msg.Text(msg.TaskProject, "shop"),
	)
	if got := introIn(t, fix); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("stale plugin:\n%s", got)
	}

	p, err := claude.Build(integration.Gentry(exe), buildinfo.Version())
	if err != nil {
		t.Fatal(err)
	}
	writeVersion(p.Version)
	project := msg.Text(msg.TaskProject, "")
	if got := introIn(t, fix); !strings.HasPrefix(got, project) {
		t.Errorf("plugin of this gentry:\n%s", got)
	}

	// Another tool has no plugin of Claude Code to compare.
	writeVersion("0.0.0+00000000")
	if got := introBy(t, fix); !strings.HasPrefix(got, project) {
		t.Errorf("no tool:\n%s", got)
	}

	// Without the plugin of the hook there is nothing to compare.
	os.Remove(manifest)
	if got := introIn(t, fix); !strings.HasPrefix(got, project) {
		t.Errorf("no manifest:\n%s", got)
	}
}
