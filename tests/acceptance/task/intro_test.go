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
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
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
	code, stdout, stderr := testkit.RunWith(string(in), append([]string{"hook", "session-start"}, flags...)...)
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("hook in %s: exit code %d, stderr %q", dir, code, stderr)
	}
	return stdout
}

// The introduction is built from the state store: no test of package cli
// reaches the line of no task, this test states its words.
func TestIntroWithoutTask(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	want := testkit.Lines(
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
	if _, got, _ := testkit.RunWith("{}", "hook", "session-start", "--tool", "claude"); got != want {
		t.Errorf("introduction without cwd:\n%s", got)
	}
	// Without a tool the skill is named by its name alone.
	want = strings.Replace(want, "gentry:working-on-task", "working-on-task", 1)
	if got := introBy(t, fix); got != want {
		t.Errorf("introduction without a tool:\n%s\nwant:\n%s", got, want)
	}
}

func TestIntroWithTask(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	t.Chdir(fix)
	testkit.MustRun(t, testkit.TakeArgs()...)
	head := testkit.Lines(
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskWorktree, fix),
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
	)
	tail := testkit.Lines("", msg.Text(msg.HintIntroContinue, "gentry:working-on-task"))
	branch := msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1))
	progress := func(passed int) string { return msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, passed, 5)) }

	want := head + testkit.Lines(branch, msg.Text(msg.IntroNoSteps), progress(0)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("stage without steps:\n%s\nwant:\n%s", got, want)
	}

	// Dropped steps do not count.
	testkit.MustRun(t, "task", "step", "add", "Создать ветку", "Проверить имя", "Удалить старую ветку")
	testkit.MustRun(t, "task", "step", "done", "1")
	testkit.MustRun(t, "task", "step", "drop", "3", "--reason", "Старой ветки нет")
	want = head + testkit.Lines(branch, msg.Text(msg.IntroSteps, 1, 2), progress(0)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("stage with steps:\n%s\nwant:\n%s", got, want)
	}

	// A passed scenario has no stage and no steps.
	testkit.MustRun(t, "task", "step", "done", "2")
	passScenario(t)
	want = head + testkit.Lines(msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)), progress(5)) + tail
	if got := introIn(t, fix); got != want {
		t.Errorf("passed scenario:\n%s\nwant:\n%s", got, want)
	}

	// A closed task frees the worktree.
	testkit.MustRun(t, "task", "close")
	if got := introIn(t, fix); got != testkit.Lines(
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
	shop, _ := testkit.TaskShop(t)
	for name, dir := range map[string]string{
		"knowledge": filepath.Join(filepath.Dir(shop), "shop-knowledge"),
		"process":   filepath.Join(os.Getenv(testkit.HomeEnv), "process", "shop"),
		"outside":   t.TempDir(),
		"missing":   filepath.Join(t.TempDir(), "gone"),
	} {
		if got := introIn(t, dir); got != "" {
			t.Errorf("%s: want no introduction, got:\n%s", name, got)
		}
	}
}

func TestIntroWithoutStore(t *testing.T) {
	t.Setenv(testkit.HomeEnv, t.TempDir())
	if got := introIn(t, t.TempDir()); got != "" {
		t.Errorf("want no introduction, got:\n%s", got)
	}
	if testkit.FileExists(filepath.Join(os.Getenv(testkit.HomeEnv), "state", "state.db")) {
		t.Error("the hook created the state store")
	}
}

func TestIntroStalePlugin(t *testing.T) {
	_, fix := testkit.TaskShop(t)
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

	// The plugin of this gentry is that of the program the commands run on;
	// testkit checks that its version is that of the test.
	exe := testkit.Executable(t)

	writeVersion("0.0.0+00000000")
	// The warning is built in the introduction from the plugin: this test
	// states its words. The hint names setup with the full path of the
	// program, not a tool.
	want := testkit.Lines(
		"Плагин Gentry не соответствует программе gentry: скиллы могут описывать прежние команды.",
		"Сообщить оператору и по его слову обновить плагин: "+exe+" setup claude",
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

// stalePluginWarning is the warning of the hook about a plugin that does not
// match the program, with its hint: setup with the full path of the program
// the commands run on.
func stalePluginWarning(t *testing.T) string {
	t.Helper()
	return testkit.Lines(
		"Плагин Gentry не соответствует программе gentry: скиллы могут описывать прежние команды.",
		"Сообщить оператору и по его слову обновить плагин: "+testkit.Executable(t)+" setup claude",
	)
}

// pluginOf points CLAUDE_PLUGIN_ROOT at a plugin of the version v.
func pluginOf(t *testing.T, v string) {
	t.Helper()
	root := t.TempDir()
	testkit.WriteFiles(t, root, map[string]string{".claude-plugin/plugin.json": `{"name":"gentry","version":"` + v + `"}`})
	t.Setenv(claude.PluginRootEnv, root)
}

// TestIntroStalePluginOutsidePool checks that outside the worktrees of the
// pools the hook warns of a stale plugin with the warning and its hint only.
func TestIntroStalePluginOutsidePool(t *testing.T) {
	testkit.TaskShop(t)
	pluginOf(t, "0.0.0+00000000")
	if got, want := introIn(t, t.TempDir()), stalePluginWarning(t); got != want {
		t.Errorf("hook outside a pool:\n%s\nwant:\n%s", got, want)
	}
}

// TestIntroStalePluginWithTask checks that in a worktree of a pool the
// warning comes before the introduction to the task, apart by an empty line.
func TestIntroStalePluginWithTask(t *testing.T) {
	_, fix := testkit.TaskShop(t)
	t.Chdir(fix)
	testkit.MustRun(t, testkit.TakeArgs()...)
	intro := introIn(t, fix)
	pluginOf(t, "0.0.0+00000000")
	if got, want := introIn(t, fix), stalePluginWarning(t)+"\n"+intro; got != want {
		t.Errorf("hook in the worktree of the task:\n%s\nwant:\n%s", got, want)
	}
}

// TestIntroPluginOutsidePool checks that outside the pools the hook of the
// plugin this program builds prints nothing.
func TestIntroPluginOutsidePool(t *testing.T) {
	testkit.TaskShop(t)
	p, err := claude.Build(integration.Gentry(testkit.Executable(t)), buildinfo.Version())
	if err != nil {
		t.Fatal(err)
	}
	pluginOf(t, p.Version)
	if got := introIn(t, t.TempDir()); got != "" {
		t.Errorf("hook outside a pool with the plugin of this program:\n%s\nwant nothing", got)
	}
}

// TestIntroStalePluginWithoutStore checks that with no state store the hook
// warns of a stale plugin and creates no state store.
func TestIntroStalePluginWithoutStore(t *testing.T) {
	home := testkit.EmptyHome(t)
	pluginOf(t, "0.0.0+00000000")
	if got, want := introIn(t, t.TempDir()), stalePluginWarning(t); got != want {
		t.Errorf("hook with no state store:\n%s\nwant:\n%s", got, want)
	}
	if testkit.FileExists(filepath.Join(home, "state", "state.db")) {
		t.Error("the hook created the state store")
	}
}
