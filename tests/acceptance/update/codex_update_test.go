package update_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// GEN-70, 3. Обновление: gentry update with the integration with Codex; and
// the budget of the hook of Codex (4), which only the installed program
// shows.

// updateError runs gentry update --json of the installed program, checks
// that it fails with the exit code 1 and an output of the schema of errors,
// and returns the code and the details of the error.
func (s *stand) updateError() (string, map[string]any) {
	s.t.Helper()
	r := s.update("--json")
	if r.code != 1 {
		s.t.Errorf("update --json: %s\nwant exit code 1", r)
	}
	testkit.Validate(s.t, "schemas/error.json", r.stdout)
	var e struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	json.Unmarshal([]byte(r.stdout), &e)
	return e.Error.Code, e.Error.Details
}

// updateIntegrations runs gentry update --json, which must succeed, and
// returns its output and its integrations by tool, with all their fields.
func (s *stand) updateIntegrations() (string, map[string]map[string]any) {
	s.t.Helper()
	out, _ := s.updateJSON()
	var res struct {
		Integrations []map[string]any `json:"integrations"`
	}
	json.Unmarshal([]byte(out), &res)
	byTool := map[string]map[string]any{}
	for _, i := range res.Integrations {
		tool, _ := i["tool"].(string)
		byTool[tool] = i
	}
	return out, byTool
}

// installCalls are the commands of Codex that install, update or switch a
// plugin.
var installCalls = []string{"plugin add", "plugin marketplace add", "plugin marketplace remove", "plugin remove"}

// TestUpdateCodex checks that the update builds the integration with Codex
// anew by the new program, as setup codex does, along with that of Claude
// Code.
func TestUpdateCodex(t *testing.T) {
	parallel(t)
	t.Run("both_integrations", func(t *testing.T) {
		parallel(t)
		both := func(t *testing.T) *stand {
			s := installedStand(t, v010, true)
			s.connectCodex()
			return s
		}
		s := both(t)
		wantCode(t, s.update(), 0)
		claudeVersion := s.plugin().Version
		codex, _ := s.codexPlugin()
		hs := s.gentryHandlers()
		setup := s.gentry("setup", "claude", "--json")
		var claudeSetup struct {
			PluginVersion string `json:"plugin_version"`
		}
		if setup.code != 0 || json.Unmarshal([]byte(setup.stdout), &claudeSetup) != nil {
			t.Fatalf("setup claude --json after the update: %s", setup)
		}
		_, codexSetup, _ := s.setupCodexJSON()
		if claudeVersion != claudeSetup.PluginVersion {
			t.Errorf("the plugin of Gentry in Claude Code has the version %s, the new program builds %s", claudeVersion, claudeSetup.PluginVersion)
		}
		if codex.Version != codexSetup.PluginVersion {
			t.Errorf("the plugin of Gentry in Codex has the version %s, the new program builds %s", codex.Version, codexSetup.PluginVersion)
		}
		if len(hs) != 1 {
			t.Fatalf("the handlers of Gentry after the update: %+v, want one", hs)
		}
		wantHookCommand(t, hs[0], s.program())
		out, byTool := both(t).updateIntegrations()
		if _, ok := byTool["claude"]; !ok {
			t.Errorf("update --json:\n%s\nwant the integration claude", out)
		}
		if _, ok := byTool["codex"]; !ok {
			t.Errorf("update --json:\n%s\nwant the integration codex", out)
		}
	})
}

// TestUpdateCodexCurrent checks that with the latest version installed the
// update builds the integration with Codex anew only if what setup codex
// wrote is not as the program would write it.
func TestUpdateCodexCurrent(t *testing.T) {
	parallel(t)
	t.Run("stale_plugin", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		s.staleCodexPlugin()
		r := s.update()
		wantCode(t, r, 0)
		wantLine(t, r.stdout, newSessions)
		p, _ := s.codexPlugin()
		if _, res, _ := s.setupCodexJSON(); p.Version != res.PluginVersion {
			t.Errorf("the plugin of Gentry in Codex has the version %s after the update, the program builds %s", p.Version, res.PluginVersion)
		}
	})
	t.Run("hook_untrusted", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		s.editCodexConfig(func(cfg map[string]any) { toml.Delete(cfg, "hooks", "state") })
		if got := s.hookTrust(); got == "trusted" {
			t.Fatalf("stand: the hook is still trusted")
		}
		wantCode(t, s.update(), 0)
		if got := s.hookTrust(); got != "trusted" {
			t.Errorf("the trust of the hook of Gentry in Codex after the update: %q, want trusted", got)
		}
	})
	t.Run("matches", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		n := len(s.codexCalls(0))
		config := fileState(t, s.codexConfigFile())
		out, byTool := s.updateIntegrations()
		for _, c := range s.codexCalls(n) {
			if slices.ContainsFunc(installCalls, func(p string) bool { return strings.HasPrefix(c, p+" ") || c == p }) {
				t.Errorf("the update ran codex %s", c)
			}
		}
		wantFileState(t, s.codexConfigFile(), config)
		if c := byTool["codex"]; c["action"] != "unchanged" || c["hook_trust"] != "present" || c["sandbox"] != "present" {
			t.Errorf("update --json:\n%s\nwant the integration codex with action unchanged, hook_trust present, sandbox present", out)
		}
	})
}

// TestUpdateCodexElsewhere checks that the update refuses before it
// downloads anything when the plugin of Codex is served from another data
// root, with the integration with Codex connected or not.
func TestUpdateCodexElsewhere(t *testing.T) {
	parallel(t)
	t.Run("connected", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		other := s.otherData(v010)
		s.gentryOf(other, "setup", "codex", "--switch")
		before := s.programSum()
		wantCode(t, s.update(), 1)
		if code, details := s.updateError(); code != contract.CodePluginElsewhere || details["tool"] != "codex" {
			t.Errorf("update --json: the error %s with %v, want plugin_elsewhere with tool codex", code, details)
		}
		s.wantNoRequests()
		s.wantProgram(before)
	})
	t.Run("not_connected", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.useCodex()
		s.gentryOf(s.otherData(v010), "setup", "codex")
		wantCode(t, s.update(), 1)
		if code, details := s.updateError(); code != contract.CodePluginElsewhere || details["tool"] != "codex" {
			t.Errorf("update --json: the error %s with %v, want plugin_elsewhere with tool codex", code, details)
		}
	})
}

// TestUpdateCodexRefuses checks that the update refuses before it downloads
// anything when Codex, connected, cannot be used.
func TestUpdateCodexRefuses(t *testing.T) {
	parallel(t)
	t.Run("not_found", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.noCodex()
		before := s.programSum()
		wantCode(t, s.update(), 1)
		if code, details := s.updateError(); code != contract.CodeToolNotFound || details["tool"] != "codex" {
			t.Errorf("update --json: the error %s with %v, want tool_not_found with tool codex", code, details)
		}
		s.wantProgram(before)
	})
	t.Run("tool_failed", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.set(testkit.FakeCodexFailEnv, "plugin marketplace list")
		before := s.programSum()
		wantCode(t, s.update(), 1)
		if code, details := s.updateError(); code != contract.CodeToolFailed || details["tool"] != "codex" {
			t.Errorf("update --json: the error %s with %v, want tool_failed with tool codex", code, details)
		}
		s.wantProgram(before)
	})
	t.Run("settings_unreadable", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.writeCodexConfig(brokenCodexConfig)
		before := s.programSum()
		r := s.update()
		wantCode(t, r, 1)
		wantLastBlock(t, r.stderr, "Исправить настройки Codex и повторить: gentry update")
		if code, details := s.updateError(); code != "settings_invalid" || details["tool"] != "codex" {
			t.Errorf("update --json: the error %s with %v, want settings_invalid with tool codex", code, details)
		}
		s.wantProgram(before)
	})
	t.Run("settings_before_program", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.writeCodexConfig(brokenCodexConfig)
		s.noCodex()
		wantCode(t, s.update(), 1)
		if code, _ := s.updateError(); code != "settings_invalid" {
			t.Errorf("update --json: the error %s, want settings_invalid", code)
		}
	})
}

// TestUpdateWithoutCodex checks that Codex, neither connected nor installed,
// does not stop the update.
func TestUpdateWithoutCodex(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	s.noCodex()
	wantCode(t, s.update(), 0)
	s.wantVersion(latest)
}

// TestUpdateCodexWarnings checks the warnings of Codex the update prints
// along with those of Claude Code, without the lines of fields and of the
// settings changed.
func TestUpdateCodexWarnings(t *testing.T) {
	parallel(t)
	t.Run("hook_untrusted", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.set(testkit.FakeCodexFailEnv, "app-server")
		r := s.update()
		wantCode(t, r, 0)
		wantLine(t, r.stdout, hookUntrusted)
		for _, l := range lines(r.stdout) {
			if strings.HasPrefix(l, "Причина:") {
				t.Errorf("stdout has the line %q:\n%s", l, r.stdout)
			}
		}
	})
	t.Run("both_disabled", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, true)
		s.connectCodex()
		s.editPlugin(func(p *claudePlugin) { p.Enabled = false })
		s.disableCodexPlugin()
		r := s.update()
		wantCode(t, r, 0)
		wantOrdered(t, r.stdout, "Плагин Gentry отключён в Claude Code.", codexDisabled)
	})
	t.Run("no_settings_lines", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, v010, false)
		s.connectCodex()
		s.editCodexConfig(func(cfg map[string]any) { toml.Set(cfg, false, "features", "hooks") })
		r := s.update()
		wantCode(t, r, 0)
		if v, _ := toml.At(s.codexConfig(), "features", "hooks"); v != true {
			t.Errorf("features.hooks after the update: %v, want true", v)
		}
		for _, out := range []string{r.stdout, r.stderr} {
			for _, l := range lines(out) {
				if strings.Contains(strings.ToLower(l), "хук") || strings.HasPrefix(l, "Настройки:") {
					t.Errorf("output has the line %q:\n%s", l, out)
				}
			}
		}
	})
}

// TestUpdateCodexHints checks the order of the hints of both AI tools in the
// last block of the update.
func TestUpdateCodexHints(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	s.connectCodex()
	s.editPlugin(func(p *claudePlugin) { p.Enabled = false })
	s.disableCodexPlugin()
	s.set(testkit.FakeCodexFailEnv, "app-server")
	r := s.update()
	wantCode(t, r, 0)
	wantLastBlock(t, r.stdout, "Включить плагин: claude plugin enable gentry@gentry", codexEnableHint, hookTrustHint)
}

// specReviewer writes the subagent reviewer of the specification into the
// shared subagents of the data root and applies the flow of the project in
// dir: the shorthand «субагент reviewer».
func (s *stand) specReviewer(dir string) {
	s.t.Helper()
	agents := filepath.Join(s.data, "process", "agents")
	writeFile(s.t, filepath.Join(agents, "reviewer.yaml"), "purpose: ревью изменений заказа — корректность и тесты\ncapabilities: [read, search, task]\nmodel: strong\n")
	writeFile(s.t, filepath.Join(agents, "reviewer.md"), "Проверить изменения задачи и вернуть замечания с местом в коде.\n")
	s.mustIn(dir, "flow", "apply")
}

// TestUpdateCodexAgents checks that the update lays out the subagents in the
// format of each AI tool connected.
func TestUpdateCodexAgents(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	s.connectCodex()
	a := s.project("shop")
	s.specReviewer(a)
	for _, p := range []string{reviewer(a), codexReviewer(a)} {
		os.Remove(p)
	}
	wantCode(t, s.update(), 0)
	for _, p := range []string{reviewer(a), codexReviewer(a)} {
		if !testkit.FileExists(p) {
			t.Errorf("no file %s after the update", p)
		}
	}
}

// codexHookIn runs the session start hook of the installed program for a
// session of Codex in dir, with the input Codex gives it.
func (s *stand) codexHookIn(dir string) result {
	s.t.Helper()
	in, _ := json.Marshal(map[string]string{"cwd": dir, "session_id": "s1", "source": "startup", "hook_event_name": "SessionStart"})
	return s.runEnv(s.program(), dir, bytes.NewReader(in), nil, "hook", "session-start", "--tool", "codex")
}

// TestHookCodexBudget checks the session start hook of Codex against the
// time and the output of the hook of Claude Code, with both warnings: 0.1 s
// for the median of 30 runs and 10 thousand characters. The case does not
// run in parallel with the others: they would slow the runs down.
func TestHookCodexBudget(t *testing.T) {
	testkit.SkipOnBinary(t)
	s := installedStand(t, latest, false)
	s.connectCodex()
	a := s.project("shop")
	s.mustIn(a, "task", "take", "--scenario", "feature", "--title", "Частичный возврат по карте", "--statement", "Деньги возвращаются на карту.")
	testkit.SetCodexTrust(t, s.codexHome, a, "untrusted")
	s.staleCodexPlugin()
	r := s.codexHookIn(a)
	wantCode(t, r, 0)
	if ls := lines(r.stdout); len(ls) < 5 || !strings.HasPrefix(ls[1], pluginStaleHint) {
		t.Fatalf("the hook of Codex:\n%s\nwant the warning of the plugin, that of the worktree and the introduction", r.stdout)
	}
	if n := utf8.RuneCountInString(r.stdout); n > 10000 {
		t.Errorf("the output of the hook of Codex is %d characters, the budget 10000", n)
	}
	const warmup, runs = 3, 30
	var times []time.Duration
	for i := 0; i < warmup+runs; i++ {
		start := time.Now()
		s.codexHookIn(a)
		if i >= warmup {
			times = append(times, time.Since(start))
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	median := times[len(times)/2]
	t.Logf("the hook of Codex in a worktree with a task, both warnings, %d runs: median %s", runs, median)
	if median > 100*time.Millisecond {
		t.Errorf("the median of the hook of Codex is %s, the budget 100ms", median)
	}
}
