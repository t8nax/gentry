package update_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// Each top-level subtest is one case of GEN-74-spec-1. Cases which retain
// existing behavior are kept beside the changed behavior as regression
// checks; every case starts with its own machine and session environment.
func TestCodexPowerShellHints(t *testing.T) {
	t.Run("stale_plugin_space", func(t *testing.T) {
		windowsHints(t)
		s, _ := hintStand(t, "Shop tools")
		s.connectCodex()
		s.staleCodexPlugin()
		r := hintHook(s, s.root, "codex")
		want := testkit.Lines(
			"Плагин Gentry не соответствует программе gentry: скиллы могут описывать прежние команды.",
			"Сообщить оператору и по его слову обновить плагин: "+psHintPath(s.program())+" setup codex",
		)
		if r.code != 0 || r.stderr != "" || r.stdout != want {
			t.Errorf("SessionStart outside the pool: %s\nwant exactly:\n%s", r, want)
		}
		wantHintSetup(t, s, hookHintCommand(t, r))
	})

	t.Run("hook_ignores_shell_session", func(t *testing.T) {
		windowsHints(t)
		s, _ := hintStand(t, "Shop tools")
		s.connectCodex()
		s.staleCodexPlugin()
		before := hintData(t, s)
		s.set(testkit.CodexThreadEnv, "")
		outside := hintHook(s, s.root, "codex")
		s.set(testkit.ClaudeCodeEnv, "1")
		claude := hintHook(s, s.root, "codex")
		for _, r := range []result{outside, claude} {
			wantHintCommand(t, hookHintCommand(t, r), psHintPath(s.program())+" setup codex")
		}
		if outside != claude {
			t.Errorf("explicit Codex hook depends on shell session:\noutside: %s\nClaude: %s", outside, claude)
		}
		wantHintData(t, s, before)
	})

	t.Run("update_text", func(t *testing.T) {
		windowsHints(t)
		s, other := hintStand(t, "Shop tools")
		r := s.runIn(other, s.root, "update")
		want := testkit.Lines(
			"Обновляется только программа из папки данных.",
			"Программа: "+other,
			"Программа в папке данных: "+s.program(),
			"",
			"Обновить программу из папки данных: "+psHintPath(s.program())+" update",
		)
		if r.code != 1 || r.stdout != "" || r.stderr != want {
			t.Errorf("update of another program: %s\nwant exactly stderr:\n%s", r, want)
		}
		wantHintUpdate(t, s, updateHintCommand(t, r))
	})

	t.Run("update_json", func(t *testing.T) {
		windowsHints(t)
		s, other := hintStand(t, "Shop tools")
		r := s.runIn(other, s.root, "update", "--json")
		e := readHintError(t, r, other, s.program())
		text := s.runIn(other, s.root, "update")
		message, textHint, ok := strings.Cut(text.stderr, "\n\n")
		if !ok || e.Error.Message != message {
			t.Errorf("JSON changed the refusal message: %q; text: %q", e.Error.Message, text.stderr)
		}
		if e.Error.Hint != strings.TrimSuffix(textHint, "\n") {
			t.Errorf("JSON changed the complete hint: %q; text block: %q", e.Error.Hint, textHint)
		}
		wantHintCommand(t, hintCommand(t, e.Error.Hint), psHintPath(s.program())+" update")
		fromJSON := wantHintUpdate(t, s, hintCommand(t, e.Error.Hint))
		fromText := wantHintUpdate(t, s, updateHintCommand(t, text))
		if fromJSON != fromText {
			t.Errorf("decoded JSON hint and text hint differ in execution:\nJSON: %s\ntext: %s", fromJSON, fromText)
		}
	})

	t.Run("no_spaces", func(t *testing.T) {
		windowsHints(t)
		s, other := hintStand(t, "plain")
		if strings.ContainsAny(s.program(), " \t") {
			t.Fatalf("the no-space case needs a temporary root without spaces: %s", s.program())
		}
		s.connectCodex()
		s.staleCodexPlugin()
		wantHintCommand(t, hookHintCommand(t, hintHook(s, s.root, "codex")), psHintPath(s.program())+" setup codex")
		wantHintCommand(t, updateHintCommand(t, s.runIn(other, s.root, "update")), psHintPath(s.program())+" update")
		e := readHintError(t, s.runIn(other, s.root, "update", "--json"), other, s.program())
		wantHintCommand(t, hintCommand(t, e.Error.Hint), psHintPath(s.program())+" update")
	})

	for _, pathCase := range []struct{ name, folder string }{
		{"special_characters", "Касса & (tools); $HOME `tick 'quote'"},
		{"typographic_quotes", "Касса ‘left’ “double”"},
	} {
		t.Run(pathCase.name, func(t *testing.T) {
			windowsHints(t)
			// Each channel is a fresh stand: executing setup must not turn the
			// subsequent stale-plugin cases into matching-plugin cases.
			for _, channel := range []string{"hook", "text", "json"} {
				t.Run(channel, func(t *testing.T) {
					s, other := hintStand(t, pathCase.folder)
					switch channel {
					case "hook":
						s.connectCodex()
						s.staleCodexPlugin()
						command := hookHintCommand(t, hintHook(s, s.root, "codex"))
						wantHintCommand(t, command, psHintPath(s.program())+" setup codex")
						wantHintSetup(t, s, command)
					case "text":
						command := updateHintCommand(t, s.runIn(other, s.root, "update"))
						wantHintCommand(t, command, psHintPath(s.program())+" update")
						wantHintUpdate(t, s, command)
					case "json":
						e := readHintError(t, s.runIn(other, s.root, "update", "--json"), other, s.program())
						command := hintCommand(t, e.Error.Hint)
						wantHintCommand(t, command, psHintPath(s.program())+" update")
						wantHintUpdate(t, s, command)
					}
				})
			}
		})
	}

	t.Run("connected_codex_outside_session", func(t *testing.T) {
		windowsHints(t)
		for _, folder := range []string{"plain", "Shop tools"} {
			t.Run(folder, func(t *testing.T) {
				s, other := hintStand(t, folder)
				s.connectCodex()
				s.set(testkit.CodexThreadEnv, "")
				before := hintData(t, s)
				wantHintCommand(t, updateHintCommand(t, s.runIn(other, s.root, "update")), oldHintPath(s.program())+" update")
				e := readHintError(t, s.runIn(other, s.root, "update", "--json"), other, s.program())
				wantHintCommand(t, hintCommand(t, e.Error.Hint), oldHintPath(s.program())+" update")
				wantHintData(t, s, before)
			})
		}
	})

	t.Run("claude_windows", func(t *testing.T) {
		windowsHints(t)
		legacyHints(t, false)
	})

	t.Run("explicit_claude_in_codex", func(t *testing.T) {
		windowsHints(t)
		for _, folder := range []string{"plain", "Shop tools"} {
			t.Run(folder, func(t *testing.T) {
				s, _ := hintStand(t, folder)
				s.stalePlugin()
				wantHintCommand(t, hookHintCommand(t, hintHook(s, s.root, "claude")), oldHintPath(s.program())+" setup claude")
			})
		}
	})

	t.Run("unix_legacy", func(t *testing.T) {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skip("GEN-74: unchanged hints on Linux and macOS")
		}
		legacyHints(t, true)
	})

	t.Run("trusted_pool_intro", func(t *testing.T) {
		windowsHints(t)
		for _, active := range []bool{true, false} {
			t.Run(map[bool]string{true: "active", false: "free"}[active], func(t *testing.T) {
				s, dir := hintPool(t, active, "trusted")
				matching := hintHook(s, dir, "codex")
				intro := poolHintIntro(dir, active)
				if matching.code != 0 || matching.stderr != "" || matching.stdout != intro {
					t.Fatalf("matching-plugin introduction: %s\nwant:\n%s", matching, intro)
				}
				s.staleCodexPlugin()
				before := hintData(t, s)
				outside := hintHook(s, s.root, "codex")
				wantHintCommand(t, hookHintCommand(t, outside), psHintPath(s.program())+" setup codex")
				got := hintHook(s, dir, "codex")
				if got.code != 0 || got.stderr != "" || got.stdout != outside.stdout+"\n"+intro {
					t.Errorf("trusted pool introduction: %s\nwant:\n%s", got, outside.stdout+"\n"+intro)
				}
				wantHintData(t, s, before)
			})
		}
	})

	t.Run("untrusted_pool_order", func(t *testing.T) {
		windowsHints(t)
		for _, active := range []bool{false, true} {
			t.Run(map[bool]string{true: "active", false: "free"}[active], func(t *testing.T) {
				s, dir := hintPool(t, active, "untrusted")
				intro := poolHintIntro(dir, active)
				matching := hintHook(s, dir, "codex")
				// The introduction is independent of the control output, so an
				// active worktree incorrectly shown as free cannot be its own
				// expectation. The unchanged warning's wording is checked by
				// TestIntroCodexTrust/untrusted; here only its boundaries matter.
				trustWarning, ok := strings.CutSuffix(matching.stdout, "\n"+intro)
				warningLines := strings.Split(trustWarning, "\n")
				if matching.code != 0 || matching.stderr != "" || !ok || len(warningLines) != 3 || warningLines[0] == "" || warningLines[1] == "" || warningLines[2] != "" {
					t.Fatalf("matching plugin lost the two-line trust warning or the expected introduction: %s\nwant introduction:\n%s", matching, intro)
				}
				s.staleCodexPlugin()
				before := hintData(t, s)
				outside := hintHook(s, s.root, "codex")
				wantHintCommand(t, hookHintCommand(t, outside), psHintPath(s.program())+" setup codex")
				got := hintHook(s, dir, "codex")
				want := outside.stdout + trustWarning + "\n" + intro
				if got.code != 0 || got.stderr != "" || got.stdout != want {
					t.Errorf("untrusted pool warning order: %s\nwant exactly:\n%s", got, want)
				}
				wantHintData(t, s, before)
			})
		}
	})

	t.Run("matching_plugin_outside", func(t *testing.T) {
		windowsHints(t)
		s, _ := hintStand(t, "Shop tools")
		s.connectCodex()
		before := hintData(t, s)
		r := hintHook(s, s.root, "codex")
		if r.code != 0 || r.stdout != "" || r.stderr != "" {
			t.Errorf("matching plugin outside the pool: %s; want empty output", r)
		}
		wantHintData(t, s, before)
	})

	t.Run("not_installed", func(t *testing.T) {
		windowsHints(t)
		s, other := hintStand(t, "Shop tools")
		if err := os.Remove(s.program()); err != nil {
			t.Fatal(err)
		}
		before := hintData(t, s)
		text := s.runIn(other, s.root, "update")
		command := updateHintCommand(t, text)
		const install = "irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex"
		wantHintCommand(t, command, install)
		textMessage, textHint, ok := strings.Cut(text.stderr, "\n\n")
		if !ok || textHint != "Установить Gentry: "+install+"\n" {
			t.Errorf("missing-program hint: %q", textHint)
		}
		e := readHintError(t, s.runIn(other, s.root, "update", "--json"), other, "")
		if textMessage != e.Error.Message {
			t.Errorf("text and JSON differ in the missing-program message: text %q; JSON %q", textMessage, e.Error.Message)
		}
		if e.Error.Hint+"\n" != textHint || !strings.HasSuffix(e.Error.Message, "\nПрограмма в папке данных: не установлена") {
			t.Errorf("missing-program JSON: %+v", e.Error)
		}
		if testkit.FileExists(s.program()) {
			t.Error("refusal installed a program")
		}
		wantHintData(t, s, before)
		s.wantNoRequests()
	})

	t.Run("path_fields_and_direct_launch", func(t *testing.T) {
		windowsHints(t)
		s, other := hintStand(t, "Shop tools")
		readHintError(t, s.runIn(other, s.root, "update", "--json"), other, s.program())
		integrationProgram := filepath.Join(s.root, "Integration tools", "gentry.exe")
		copyFile(t, builds.dev, integrationProgram)
		r := s.runIn(integrationProgram, s.root, "setup", "codex")
		if r.code != 0 || r.stderr != "" {
			t.Fatalf("stand: setup codex by integration program: %s", r)
		}
		p := filepath.Join(s.codexIntegration(), "gentry", ".mcp.json")
		var config struct {
			Servers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(readFile(t, p)), &config); err != nil {
			t.Fatal(err)
		}
		core, ok := config.Servers["core"]
		if !ok || core.Command != integrationProgram || !reflect.DeepEqual(core.Args, []string{"mcp"}) {
			t.Errorf("direct MCP launch contains shell syntax or changed arguments: %+v; want %q [mcp]", core, integrationProgram)
		}
	})
}

// legacyHints checks the previous absolute-command form in separate tool
// sessions. On Windows only Claude Code is selected; Unix checks both.
func legacyHints(t *testing.T, withCodex bool) {
	t.Helper()
	tools := []string{"claude"}
	if withCodex {
		tools = append(tools, "codex")
	}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			for _, folder := range []string{"plain", "Shop tools"} {
				t.Run(folder, func(t *testing.T) {
					s, other := hintStand(t, folder)
					if tool == "claude" {
						s.set(testkit.CodexThreadEnv, "")
						s.set(testkit.ClaudeCodeEnv, "1")
						s.stalePlugin()
					} else {
						s.connectCodex()
						s.staleCodexPlugin()
					}
					wantHintCommand(t, hookHintCommand(t, hintHook(s, s.root, tool)), oldHintPath(s.program())+" setup "+tool)
					wantHintCommand(t, updateHintCommand(t, s.runIn(other, s.root, "update")), oldHintPath(s.program())+" update")
					e := readHintError(t, s.runIn(other, s.root, "update", "--json"), other, s.program())
					wantHintCommand(t, hintCommand(t, e.Error.Hint), oldHintPath(s.program())+" update")
				})
			}
		})
	}
}

func hintPool(t *testing.T, active bool, trust string) (*stand, string) {
	t.Helper()
	s, _ := hintStand(t, "Shop tools")
	s.connectCodex()
	dir := s.project("shop")
	if active {
		s.mustIn(dir, testkit.TakeArgs()...)
	}
	testkit.SetCodexTrust(t, s.codexHome, dir, trust)
	return s, dir
}

// These are the introductions specified in «Тексты», with the established
// shop fixture's identifiers, scenario, stage and progress substituted.
func poolHintIntro(dir string, active bool) string {
	head := testkit.Lines("Проект: shop", "Рабочая копия: "+dir)
	if !active {
		return head + testkit.Lines("Задача в этой копии не взята.", "", "Взять задачу, когда оператор её поставит: скилл gentry:working-on-task")
	}
	return head + testkit.Lines(
		"Задача SHOP-1: Частичный возврат по карте",
		"Сценарий: Фича (feature)",
		"Этап: Ветка (branch), круг 1",
		"Шаги этапа: не заданы",
		"Прогресс: 0 из 5",
		"",
		"Продолжить задачу по слову оператора: скилл gentry:working-on-task",
	)
}
