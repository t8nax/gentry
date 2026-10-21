package task_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-70, 4. Хук в сессии Codex; 8. Задача продолжается в другом
// ИИ-инструменте. Codex is connected by gentry setup codex with the fake
// Codex before the shop is connected.

// The warning of the hook of Codex about a worktree not trusted, as the
// specification gives it.
const (
	codexUntrusted1 = "Рабочая копия не доверена в Codex: субагенты флоу в этой сессии недоступны."
	codexUntrusted2 = "Сообщить оператору."
)

// codexStaleWarning is the warning of the hook of Codex about the plugin,
// with its hint: setup codex with the full path of the program the commands
// run on. The words are those of the hook of Claude Code, which
// TestIntroStalePlugin states.
func codexStaleWarning(t *testing.T) string {
	t.Helper()
	return testkit.Lines(msg.Text(msg.IntroPluginStale), msg.Text(msg.HintIntroPluginStale)+": "+testkit.Executable(t)+" setup codex")
}

// codexUntrustedWarning is the warning of the hook of Codex about a worktree
// not trusted.
var codexUntrustedWarning = testkit.Lines(codexUntrusted1, codexUntrusted2)

// codexIntro runs the session start hook for a session of Codex in dir, with
// the input Codex gives it, and returns the introduction.
func codexIntro(t *testing.T, dir string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"cwd": dir, "session_id": "s1", "source": "startup", "hook_event_name": "SessionStart"})
	code, stdout, stderr := testkit.RunWith(string(in), "hook", "session-start", "--tool", "codex")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("hook --tool codex in %s: exit code %d, stderr %q", dir, code, stderr)
	}
	return stdout
}

// trustA writes the record of trust of the worktree a, replacing any.
func trustA(t *testing.T, a, level string) {
	t.Helper()
	testkit.SetCodexTrust(t, testkit.CodexHome(), a, level)
}

// TestIntroCodex checks the introduction of the hook of Codex in a worktree
// of the pool: that of Claude Code, with the skill of Codex.
func TestIntroCodex(t *testing.T) {
	t.Run("without_task", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "trusted")
		want := testkit.Lines(
			msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.TaskWorktree, a),
			msg.Text(msg.IntroNoTask),
			"",
			msg.Text(msg.HintIntroTake, "gentry:working-on-task"),
		)
		if got := codexIntro(t, a); got != want {
			t.Errorf("introduction:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("with_task", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "trusted")
		testkit.MustRun(t, testkit.TakeArgs()...)
		want := testkit.Lines(
			msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.TaskWorktree, a),
			msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
			msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
			msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1)),
			msg.Text(msg.IntroNoSteps),
			msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)),
			"",
			msg.Text(msg.HintIntroContinue, "gentry:working-on-task"),
		)
		if got := codexIntro(t, a); got != want {
			t.Errorf("introduction:\n%s\nwant:\n%s", got, want)
		}
	})
}

// removeCodexPlugin removes the plugin of Gentry from the fake Codex by
// codex plugin marketplace remove gentry.
func removeCodexPlugin(t *testing.T) {
	t.Helper()
	cmd := exec.Command(testkit.FakeCodex(t), "plugin", "marketplace", "remove", "gentry")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("codex plugin marketplace remove gentry: %v\n%s", err, out)
	}
}

// TestIntroCodexPlugin checks the warning of the hook of Codex about a
// plugin of Gentry that is missing or does not match the program.
func TestIntroCodexPlugin(t *testing.T) {
	t.Run("stale_in_pool", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "trusted")
		intro := codexIntro(t, a)
		testkit.StaleCodexPlugin(t, testkit.CodexHome())
		want := codexStaleWarning(t) + "\n" + intro
		if got := codexIntro(t, a); got != want {
			t.Errorf("hook in the worktree:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("stale_outside", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		testkit.StaleCodexPlugin(t, testkit.CodexHome())
		if got, want := codexIntro(t, t.TempDir()), codexStaleWarning(t); got != want {
			t.Errorf("hook outside the pools:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("missing", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		removeCodexPlugin(t)
		if got, want := codexIntro(t, t.TempDir()), codexStaleWarning(t); got != want {
			t.Errorf("hook outside the pools without the plugin:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("matches", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		if got := codexIntro(t, t.TempDir()); got != "" {
			t.Errorf("hook outside the pools with the plugin of the program:\n%s\nwant nothing", got)
		}
	})
}

// TestIntroCodexTrust checks the warning of the hook of Codex about a
// worktree not trusted in Codex.
func TestIntroCodexTrust(t *testing.T) {
	t.Run("untrusted", func(t *testing.T) {
		shop, _ := testkit.ToolsTaskShop(t, false, true)
		trustA(t, shop, "untrusted")
		want := testkit.Lines(codexUntrusted1, codexUntrusted2) + "\n" + introIn(t, shop)
		if got := codexIntro(t, shop); got != want {
			t.Errorf("hook in the worktree:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("no_record", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "")
		if got := codexIntro(t, a); !strings.HasPrefix(got, codexUntrustedWarning+"\n") {
			t.Errorf("hook in the worktree:\n%s\nwant it to start with:\n%s", got, codexUntrustedWarning)
		}
	})
	t.Run("inherits", func(t *testing.T) {
		shop, fix := testkit.ToolsTaskShop(t, false, true)
		trustA(t, shop, "trusted")
		testkit.SetCodexTrust(t, testkit.CodexHome(), fix, "")
		if got, want := codexIntro(t, fix), introIn(t, fix); got != want {
			t.Errorf("hook in the worktree of the main one trusted:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("both_warnings", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "untrusted")
		testkit.StaleCodexPlugin(t, testkit.CodexHome())
		want := codexStaleWarning(t) + codexUntrustedWarning + "\n" + introIn(t, a)
		if got := codexIntro(t, a); got != want {
			t.Errorf("hook in the worktree:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("settings_unreadable", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		testkit.WriteFiles(t, testkit.CodexHome(), map[string]string{"config.toml": "[features\nhooks = true\n"})
		if got, want := codexIntro(t, a), introIn(t, a); got != want {
			t.Errorf("hook with the settings of Codex not read:\n%s\nwant the introduction:\n%s", got, want)
		}
	})
	t.Run("claude_tool", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		a := testkit.MustWd(t)
		trustA(t, a, "untrusted")
		if got := introIn(t, a); strings.Contains(got, codexUntrusted1) {
			t.Errorf("hook of Claude Code:\n%s\nwant no warning about the trust in Codex", got)
		}
	})
}

// TestTaskAcrossTools checks that a task taken in Claude Code goes on in
// Codex: the hook of Codex shows it, and the task is shown as one of a
// single tool.
func TestTaskAcrossTools(t *testing.T) {
	testkit.ToolsShopFlow(t, true, true)
	a := testkit.MustWd(t)
	c := filepath.Join(filepath.Dir(a), "shop-c")
	gittest.Run(t, filepath.Dir(a), "clone", "--quiet", a, c)
	testkit.MustRun(t, "worktree", "add", c)
	trustA(t, a, "trusted")
	const note = "Возврат проверен на тестовой карте."

	t.Setenv(testkit.ClaudeCodeEnv, "1")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", a)...)
	t.Setenv(testkit.ClaudeCodeEnv, "")
	t.Setenv(testkit.CodexThreadEnv, "t1")
	t.Chdir(a)
	if code, _, stderr := testkit.Run("task", "note", "add", note); code != contract.ExitOK {
		t.Fatalf("task note add in Codex: exit code %d, stderr:\n%s", code, stderr)
	}
	t.Setenv(testkit.CodexThreadEnv, "")
	t.Setenv(testkit.ClaudeCodeEnv, "1")
	testkit.MustRun(t, testkit.TakeArgs("--worktree", c)...)
	t.Chdir(c)
	testkit.MustRun(t, "task", "note", "add", note)
	t.Setenv(testkit.ClaudeCodeEnv, "")
	t.Chdir(a)

	if _, notes, _ := testkit.Run("task", "note", "list"); !strings.Contains(notes, note) {
		t.Errorf("the note of Codex is not in the task of %s:\n%s", a, notes)
	}
	intro := codexIntro(t, a)
	if !strings.Contains(intro, msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте")+"\n") ||
		!strings.HasSuffix(intro, "\n"+msg.Text(msg.HintIntroContinue, "gentry:working-on-task")+"\n") {
		t.Errorf("the hook of Codex in %s:\n%s\nwant the task SHOP-1 and the hint of the skill gentry:working-on-task", a, intro)
	}
	shown := func(task, dir string) string {
		t.Helper()
		out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", task)
		out = strings.ReplaceAll(out, task, "<задача>")
		out = strings.ReplaceAll(out, strings.Trim(testkit.JSONString(dir), `"`), "<копия>")
		return regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`).ReplaceAllString(out, "<время>")
	}
	if inA, inC := shown("SHOP-1", a), shown("SHOP-2", c); inA != inC {
		t.Errorf("task show --json of the task of %s:\n%s\nof the same task of one tool in %s:\n%s", a, inA, c, inC)
	}
}
