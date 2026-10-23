package update_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
	_ "modernc.org/sqlite"
)

// Every named specification case owns a test below. Text and JSON use
// independent stands, including the entire sequence in multi-command cases.
func eachSetupAgents(t *testing.T, jsonOnly bool, f func(*testing.T, string, bool)) {
	t.Helper()
	parallel(t)
	for _, tool := range []string{"claude", "codex"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			modes := []bool{false, true}
			if jsonOnly {
				modes = []bool{true}
			}
			for _, mode := range modes {
				name := "text"
				if mode {
					name = "json"
				}
				t.Run(name, func(t *testing.T) { t.Parallel(); f(t, tool, mode) })
			}
		})
	}
}

func changedSetupPair(t *testing.T, tool string, both bool) (*setupAgentsStand, *setupAgentsStand) {
	t.Helper()
	s, e := setupAgentsPair(t, tool, both, true)
	s.stale()
	e.stale()
	s.stalePlugin()
	return s, e
}

func wantSetupSilent(t *testing.T, r result) {
	t.Helper()
	wantCode(t, r, 0)
	if r.stderr != "" {
		t.Errorf("stderr: %s", r.stderr)
	}
	wantNoLineWith(t, r.stdout, "убагент")
}

func wantSetupNoStderr(t *testing.T, r result) {
	t.Helper()
	if r.stderr != "" {
		t.Errorf("stderr=%q, want empty", r.stderr)
	}
}

// A separate canonical text case owns the words; other cases still require a
// single result block of the prescribed size, with no extra hints or blocks.
func wantSetupResultLines(t *testing.T, r result, count int) {
	t.Helper()
	blocks := blocksOf(r.stdout)
	if len(blocks) != 1 || len(blocks[0]) != count || r.stdout != testkit.Lines(blocks[0]...) {
		t.Errorf("setup result blocks: %#v, want one block of %d lines", blocks, count)
	}
}

func setupAgentsVerify(t *testing.T, s, e *setupAgentsStand, mode bool, r result, action string) setupAgentsResult {
	t.Helper()
	want := e.reference("admin", "shop")
	var got setupAgentsResult
	if mode {
		got = s.parse(r, action)
		s.wantContract(r, got, "admin", "shop")
		s.wantReference(e, got.Agents, want)
	} else {
		wantSetupSilent(t, r)
		s.wantReference(e, nil, nil)
		s.wantReference(e, nil, want)
	}
	return got
}

// Case: update from a project directory.
func TestSetupAgents59UpdateFromProject(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		r := s.runIn(s.program(), s.copy("shop", "A"), "setup", tool, "--json")
		setupAgentsVerify(t, s, e, mode, r, "updated")
		s.wantHidden()
	})
}

// Case: first connection outside all projects.
func TestSetupAgents59FirstConnection(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, false)
		s.stale()
		e.stale()
		r := s.setup(mode)
		if !mode {
			want := fmt.Sprintf("Gentry подключён к %s.\nПлагин начнёт работать со следующей сессии.\n", setupAgentsTitle(tool))
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		}
		setupAgentsVerify(t, s, e, mode, r, "installed")
	})
}

func (s *setupAgentsStand) elsewhere() (string, map[string]string) {
	s.t.Helper()
	other := s.otherData(latest)
	s.gentryOf(other, "setup", s.tool)
	// Register a real project in the previous data root, with real managed
	// files: accidentally processing that pool must be observable.
	old := *s.stand
	old.data, old.root = other, filepath.Join(s.root, "previous-projects")
	if err := os.MkdirAll(old.root, 0o755); err != nil {
		s.t.Fatal(err)
	}
	copy := old.project("legacy")
	files := map[string]string{}
	for rel, text := range setupAgentFiles(s.t, copy) {
		files[filepath.Join(copy, rel)] = text
	}
	return filepath.Join(other, "integrations", s.tool), files
}

// Case: switching from an existing data root.
func TestSetupAgents59Switch(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, false)
		previous, old := s.elsewhere()
		s.stale()
		e.stale()
		r := s.setup(mode, "--switch")
		out := setupAgentsVerify(t, s, e, mode, r, "switched")
		if mode {
			if out.PreviousDir == nil || !samePath(*out.PreviousDir, previous) {
				t.Errorf("previous_dir: %v, want %s", out.PreviousDir, previous)
			}
		} else {
			want := fmt.Sprintf("Плагин Gentry в %s переключён на папку данных этой команды.\nИзменения вступят в силу со следующей сессии.\nПрежняя папка плагина: %s\n", setupAgentsTitle(tool), previous)
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		}
		for p, text := range old {
			wantFileState(t, p, text)
		}
	})
}

// Case: the previous data root is gone.
func TestSetupAgents59PreviousGone(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, false)
		previous, _ := s.elsewhere()
		if err := os.RemoveAll(filepath.Dir(filepath.Dir(previous))); err != nil {
			t.Fatal(err)
		}
		s.stale()
		e.stale()
		out := setupAgentsVerify(t, s, e, mode, s.setup(mode), "switched")
		if out.PreviousDir == nil || !samePath(*out.PreviousDir, previous) {
			t.Errorf("previous_dir: %v, want %s", out.PreviousDir, previous)
		}
	})
}

// Case: repeating setup.
func TestSetupAgents59Repeat(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s := newSetupAgentsStand(t, tool, false, true)
		s.stale()
		before := s.snapshot()
		r := s.setup(mode)
		if mode {
			out := s.parse(r, "unchanged")
			s.wantContract(r, out)
		} else {
			wantSetupSilent(t, r)
			if tool == "claude" {
				want := fmt.Sprintf("Gentry уже подключён к %s.\n", setupAgentsTitle(tool))
				if r.stdout != want {
					t.Errorf("stdout=%q, want %q", r.stdout, want)
				}
			} else {
				// TestSetupCodexText/unchanged owns the exact Codex wording.
				wantSetupResultLines(t, r, 1)
			}
		}
		s.wantUnchanged(before)
	})
}

// Case: repairing only Codex settings.
func TestSetupAgents59SettingsOnly(t *testing.T) {
	parallel(t)
	s := newSetupAgentsStand(t, "codex", false, true)
	s.editCodexConfig(func(cfg map[string]any) { toml.Set(cfg, false, "features", "hooks") })
	s.stale()
	before := s.snapshot()
	r := s.setup(true)
	out := s.parse(r, "unchanged")
	s.wantContract(r, out)
	s.wantUnchanged(before)
	if v, _ := toml.At(s.codexConfig(), "features", "hooks"); v != true || out.HooksFeature != "enabled" {
		t.Errorf("hooks not enabled: %+v", out)
	}
}

// Case: the state store is missing.
func TestSetupAgents59NoState(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s := &setupAgentsStand{stand: installedStand(t, latest, false), tool: tool}
		s.useCodex()
		writeFile(t, filepath.Join(s.claudeConfig, "settings.json"), `{"permissions":{"allow":["mcp__plugin_gentry_core"]}}`)
		r := s.setup(mode)
		if mode {
			out := s.parse(r, "installed")
			s.wantContract(r, out)
		} else {
			wantSetupSilent(t, r)
		}
		if testkit.FileExists(s.stateStore()) {
			t.Error("setup created missing state.db")
		}
	})
}

// Case: the project list is empty.
func TestSetupAgents59EmptyProjects(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s := &setupAgentsStand{stand: installedStand(t, latest, false), tool: tool}
		s.useCodex()
		writeFile(t, filepath.Join(s.claudeConfig, "settings.json"), `{"permissions":{"allow":["mcp__plugin_gentry_core"]}}`)
		s.mustIn(s.root, "setup", tool)
		if err := os.MkdirAll(filepath.Dir(s.stateStore()), 0o755); err != nil {
			t.Fatal(err)
		}
		// Read-only list commands correctly leave a missing store missing.
		// This fixture needs an existing, valid store with no project rows.
		store, err := state.Open(s.stateStore())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if !testkit.FileExists(s.stateStore()) {
			t.Fatal("stand did not create an empty state database")
		}
		s.stalePlugin()
		r := s.setup(mode)
		if mode {
			out := s.parse(r, "updated")
			s.wantContract(r, out)
		} else {
			wantSetupSilent(t, r)
		}
	})
}

func (s *setupAgentsStand) taskSnapshot() {
	writeFile(s.t, filepath.Join(s.data, "process", "agents", "reviewer.md"), "Инструкция задачи\n")
	s.mustIn(s.copy("shop", "A"), "flow", "apply")
	s.mustIn(s.copy("shop", "B"), "task", "take", "--scenario", "feature", "--title", "Проверка снимка", "--statement", "Использовать снимок флоу.")
	writeFile(s.t, filepath.Join(s.data, "process", "agents", "reviewer.md"), "Действующая инструкция\n")
	// Applying the shared draft refreshes all free copies of both projects.
	s.mustIn(s.copy("shop", "A"), "flow", "apply")
}

// Case: occupied and free copies.
func TestSetupAgents59TaskAndFree(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, true)
		s.taskSnapshot()
		e.taskSnapshot()
		s.stale()
		e.stale()
		s.stalePlugin()
		out := setupAgentsVerify(t, s, e, mode, s.setup(mode), "updated")
		if len(out.Agents) == 2 && len(out.Agents[1].Worktrees) == 2 && out.Agents[1].Worktrees[1].Task == nil {
			t.Error("occupied worktree has no task")
		}
		for _, c := range []string{"A", "B"} {
			text := readFile(t, setupAgentFile(s.copy("shop", c), tool, "reviewer"))
			want := "Действующая инструкция"
			if c == "B" {
				want = "Инструкция задачи"
			}
			if !strings.Contains(text, want) {
				t.Errorf("shop/%s does not contain %s", c, want)
			}
		}
	})
}

// Case: both tools are connected.
func TestSetupAgents59BothTools(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, true)
		claude, codex := s.plugin(), s.codexMarketplace()
		cp, _ := s.codexPlugin()
		integration := s.integrationSum()
		otherCodex := fileState(t, s.codexConfigFile())
		setupAgentsVerify(t, s, e, mode, s.setup(mode), "updated")
		if tool == "codex" {
			if s.plugin() != claude || s.integrationSum() != integration {
				t.Error("setup codex changed Claude integration or registration")
			}
		} else {
			after, _ := s.codexPlugin()
			if after != cp || s.codexMarketplace() != codex || fileState(t, s.codexConfigFile()) != otherCodex {
				t.Error("setup claude changed Codex integration or registration")
			}
		}
	})
}

// Case: only one tool is connected.
func TestSetupAgents59OneTool(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, true, true)
		other := "claude"
		if tool == "claude" {
			other = "codex"
		}
		// Disconnect the second integration after real deploys created its
		// managed files and git exclusions. Keep those exact files in place.
		for _, x := range []*setupAgentsStand{s, e} {
			if err := os.RemoveAll(filepath.Join(x.data, "integrations", other)); err != nil {
				t.Fatal(err)
			}
			x.tools = []string{tool}
			x.stale()
		}
		s.stalePlugin()
		before := s.snapshot()
		setupAgentsVerify(t, s, e, mode, s.setup(mode), "updated")
		for p, text := range before {
			if strings.Contains(p, string(filepath.Separator)+"."+other+string(filepath.Separator)) {
				wantFileState(t, p, text)
			}
		}
		s.wantHidden()
	})
}

// Case: the plugin is disabled.
func TestSetupAgents59Disabled(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, true)
		s.disable()
		r := s.setup(mode)
		out := setupAgentsVerify(t, s, e, mode, r, "updated")
		if mode {
			if out.Enabled == nil || *out.Enabled {
				t.Errorf("enabled: %v, want false", out.Enabled)
			}
		} else {
			// DisabledConflict owns the exact warning/hint wording. Here the
			// successful layout must still leave a separate warning block and
			// its single enable command, without a general deployment hint.
			blocks := blocksOf(r.stdout)
			if len(blocks) != 3 || len(blocks[0]) != 2 || len(blocks[1]) != 1 || len(blocks[2]) != 1 {
				t.Errorf("disabled setup blocks: %#v, want result, warning, one enable hint", blocks)
			} else {
				if !strings.Contains(blocks[1][0], "Gentry") || !strings.Contains(blocks[1][0], setupAgentsTitle(tool)) {
					t.Errorf("disabled warning does not identify the plugin/tool: %q", blocks[1][0])
				}
				command := "claude plugin enable gentry@gentry"
				if tool == "codex" {
					command = `plugins."gentry@gentry".enabled = true`
				}
				if !strings.HasSuffix(blocks[2][0], command) {
					t.Errorf("enable hint does not contain its command: %q", blocks[2][0])
				}
			}
		}
		if tool == "codex" {
			p, _ := s.codexPlugin()
			if p.Enabled {
				t.Error("Codex plugin re-enabled")
			}
		} else if s.plugin().Enabled {
			t.Error("Claude plugin re-enabled")
		}
	})
}

// Case: a setup warning does not prevent deployment.
func TestSetupAgents59SettingsWarning(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		settings := filepath.Join(s.claudeConfig, "settings.json")
		if tool == "claude" {
			s.breakSettings()
		} else {
			settings = s.codexHooksFile()
			writeFile(t, settings, "{ это не настройки\n")
		}
		r := s.setup(mode)
		want := e.reference("admin", "shop")
		if mode {
			out := s.parse(r, "updated")
			s.wantContract(r, out, "admin", "shop")
			s.wantReference(e, out.Agents, want)
			if tool == "claude" && out.Permission != "failed" || tool == "codex" && out.HookTrust != "failed" {
				t.Errorf("warning JSON: %+v", out)
			}
		} else {
			wantCode(t, r, 0)
			wantSetupNoStderr(t, r)
			text := setupAgentsUpdated(tool)
			if tool == "claude" {
				text += "Разрешить инструменты Gentry не удалось: настройки Claude Code не прочитаны.\nНастройки: " + settings + "\n\nРазрешить инструменты вручную: правило mcp__plugin_gentry_core в permissions.allow\n"
			} else {
				text += "\nХук запуска сессии не записан: настройки хуков Codex не прочитаны.\nНастройки хуков: " + settings + "\n\nИсправить настройки хуков Codex и повторить: gentry setup codex\n"
			}
			if r.stdout != text {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, text)
			}
			s.wantReference(e, nil, want)
		}
	})
}

// Case: a missing copy.
func TestSetupAgents59MissingCopy(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		for _, x := range []*setupAgentsStand{s, e} {
			if err := os.RemoveAll(x.copy("shop", "B")); err != nil {
				t.Fatal(err)
			}
		}
		out := setupAgentsVerify(t, s, e, mode, s.setup(mode), "updated")
		if testkit.FileExists(s.copy("shop", "B")) {
			t.Error("missing directory recreated")
		}
		if len(out.Agents) == 2 && len(out.Agents[1].Worktrees) == 2 {
			w := out.Agents[1].Worktrees[1]
			if !w.Missing || len(w.Agents)+len(w.Added)+len(w.Updated)+len(w.Removed) != 0 {
				t.Errorf("missing copy: %+v", w)
			}
		}
	})
}

// Case: installer update followed by setup.
func TestSetupAgents59InstallerMatchesUpdate(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, true)
		for _, x := range []*setupAgentsStand{s, e} {
			x.installed(v020)
			x.mustIn(x.root, "setup", tool)
			x.stale()
		}
		// Both machines download the same local release and scripts.
		e.releaseURL = s.releaseURL
		for _, name := range []string{"install.ps1", "install.sh"} {
			b, err := os.ReadFile(filepath.Join(builds.module, "install", name))
			if err != nil {
				t.Fatal(err)
			}
			s.mock.mu.Lock()
			s.mock.files[latest+"/"+name] = b
			s.mock.mu.Unlock()
		}
		var installed result
		if runtime.GOOS == "windows" {
			p := filepath.Join(builds.module, "install", "install.ps1")
			installed = s.runIn("powershell", s.root, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", p, "-NoModifyPath")
		} else {
			installed = s.runIn("sh", s.root, filepath.Join(builds.module, "install", "install.sh"), "--no-modify-path")
		}
		wantCode(t, installed, 0)
		s.wantVersion(latest)
		r := s.setup(true)
		out := s.parse(r, "updated")
		s.wantContract(r, out, "admin", "shop")
		u := e.update("--json")
		wantCode(t, u, 0)
		var updated struct {
			Agents []setupAgentsProject `json:"agents"`
		}
		if err := json.Unmarshal([]byte(u.stdout), &updated); err != nil {
			t.Fatal(err)
		}
		s.wantReference(e, out.Agents, updated.Agents)
	})
}

// Case: concise update text.
func TestSetupAgents59ShortText(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		r := s.setup(mode)
		if !mode && r.stdout != setupAgentsUpdated(tool) {
			t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, setupAgentsUpdated(tool))
		}
		setupAgentsVerify(t, s, e, mode, r, "updated")
	})
}

// Case: the deployed agents already match the flow.
func TestSetupAgents59AlreadyCurrent(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := setupAgentsPair(t, tool, false, true)
		s.stalePlugin()
		out := setupAgentsVerify(t, s, e, mode, s.setup(mode), "updated")
		if mode {
			for _, p := range out.Agents {
				if p.Changed == nil || *p.Changed {
					t.Errorf("changed: %+v", p)
				}
				for _, w := range p.Worktrees {
					if len(w.Added)+len(w.Updated)+len(w.Removed) != 0 {
						t.Errorf("unexpected changes: %+v", w)
					}
				}
			}
		}
	})
}

func setupTracked(t *testing.T, s *setupAgentsStand, project string) string {
	t.Helper()
	copy := s.copy(project, "A")
	p := setupAgentFile(copy, s.tool, "reviewer")
	gittest.Run(t, copy, "add", "--force", p)
	gittest.Run(t, copy, "commit", "--quiet", "-m", "tracked reviewer")
	return p
}

func setupConflictText(s *setupAgentsStand, conflicts []setupAgentsConflict, hints ...string) string {
	rows := [][]string{}
	for _, c := range conflicts {
		reason := "файл разложен не Gentry"
		if c.Reason == "tracked" {
			reason = "файл отслеживается git проекта"
		}
		rows = append(rows, []string{c.Worktree, c.Agent, c.File, reason})
	}
	return "Часть субагентов не разложена: файлы с их именами уже есть.\n\n" + testkit.Table(append([][]string{{"РАБОЧАЯ КОПИЯ", "СУБАГЕНТ", "ФАЙЛ", "ПРИЧИНА"}}, rows...)...) + "\n" + testkit.Lines(hints...)
}

// Case: a disabled plugin and a conflict.
func TestSetupAgents59DisabledConflict(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		s.disable()
		p := setupTracked(t, s, "shop")
		before := readFile(t, p)
		c := setupAgentsConflict{Worktree: s.copy("shop", "A"), Agent: "reviewer", File: setupConflictFile(tool, "reviewer"), Reason: "tracked"}
		r := s.setup(mode)
		if mode {
			out := s.parse(r, "updated")
			s.wantContract(r, out, "admin", "shop")
			if len(out.Agents) == 2 && !reflect.DeepEqual(out.Agents[1].Conflicts, []setupAgentsConflict{c}) {
				t.Errorf("conflicts: %+v", out.Agents)
			}
		} else {
			wantCode(t, r, 0)
			wantSetupNoStderr(t, r)
			enable := "Включить плагин: claude plugin enable gentry@gentry"
			if tool == "codex" {
				enable = `Включить плагин в настройках Codex: plugins."gentry@gentry".enabled = true`
			}
			want := setupAgentsUpdated(tool) + "\nПлагин Gentry отключён в " + setupAgentsTitle(tool) + ".\n\n" + setupConflictText(s, []setupAgentsConflict{c}, enable, "Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		}
		wantFileState(t, p, before)
		want := e.reference("admin", "shop")
		s.wantReference(e, nil, nil)
		s.wantReference(e, nil, want, p)
	})
}

// Case: Codex warnings and conflicts in two projects.
func TestSetupAgents59CodexWarnings(t *testing.T) {
	parallel(t)
	for _, mode := range []bool{false, true} {
		name := "text"
		if mode {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, e := changedSetupPair(t, "codex", false)
			s.disable()
			s.editCodexConfig(func(cfg map[string]any) {
				toml.Delete(cfg, "hooks", "state")
				toml.Set(cfg, "strict", "default_permissions")
				for _, p := range []string{"admin", "shop"} {
					for _, copy := range s.copies(p) {
						level := "trusted"
						if copy == s.copy("shop", "A") {
							level = "untrusted"
						}
						toml.Set(cfg, map[string]any{"trust_level": level}, "projects", copy)
					}
				}
			})
			s.set("FAKECODEX_HOOK_TRUST_ERROR", "Отказ доверия на стенде.")
			var conflicts []setupAgentsConflict
			for _, p := range []string{"admin", "shop"} {
				setupTracked(t, s, p)
				conflicts = append(conflicts, setupAgentsConflict{Worktree: s.copy(p, "A"), Agent: "reviewer", File: setupConflictFile("codex", "reviewer"), Reason: "tracked"})
			}
			r := s.setup(mode)
			if mode {
				out := s.parse(r, "updated")
				s.wantContract(r, out, "admin", "shop")
				if out.HookTrust != "untrusted" || out.HookError == nil || *out.HookError != "Отказ доверия на стенде." {
					t.Errorf("hook result: %+v", out)
				}
			} else {
				wantCode(t, r, 0)
				wantSetupNoStderr(t, r)
				want := setupAgentsUpdated("codex") + "\nПлагин Gentry отключён в Codex.\n\nХук запуска сессии записан, но не доверен в Codex.\nПричина: Отказ доверия на стенде.\n\nПесочница Codex задана профилем разрешений: папка данных Gentry в ней не открыта.\n\nРабочие копии, которым оператор не доверил в Codex, оставлены как есть:\n  " + s.copy("shop", "A") + "\n\n" + setupConflictText(s, conflicts,
					`Включить плагин в настройках Codex: plugins."gentry@gentry".enabled = true`, "Доверить хук в Codex: /hooks",
					"Доверить рабочие копии в настройках Codex: projects."+strconv.Quote(s.copy("shop", "A"))+`.trust_level = "trusted"`,
					"Разложить субагентов после разбора файлов: gentry agents deploy --project admin", "Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
				if r.stdout != want {
					t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
				}
			}
			want := e.reference("admin", "shop")
			s.wantReference(e, nil, want, setupConflictPhysical(conflicts[0]), setupConflictPhysical(conflicts[1]))
		})
	}
}

// Case: tracked and foreign files.
func TestSetupAgents59TrackedForeign(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		tracked := setupTracked(t, s, "shop")
		foreign := setupAgentFile(s.copy("shop", "B"), tool, "coder")
		writeFile(t, foreign, "Чужой coder, не перезаписывать.\n")
		before := map[string]string{tracked: readFile(t, tracked), foreign: readFile(t, foreign)}
		conflicts := []setupAgentsConflict{{s.copy("shop", "A"), "reviewer", setupConflictFile(tool, "reviewer"), "tracked"}, {s.copy("shop", "B"), "coder", setupConflictFile(tool, "coder"), "foreign"}}
		r := s.setup(mode)
		if mode {
			out := s.parse(r, "updated")
			s.wantContract(r, out, "admin", "shop")
			if len(out.Agents) == 2 && !reflect.DeepEqual(out.Agents[1].Conflicts, conflicts) {
				t.Errorf("conflicts: %+v, want %+v", out.Agents[1].Conflicts, conflicts)
			}
		} else {
			wantCode(t, r, 0)
			wantSetupNoStderr(t, r)
			want := setupAgentsUpdated(tool) + "\n" + setupConflictText(s, conflicts, "Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		}
		for p, text := range before {
			wantFileState(t, p, text)
		}
		want := e.reference("admin", "shop")
		s.wantReference(e, nil, want, tracked, foreign)
	})
}

// Case: a conflict in one of two formats.
func TestSetupAgents59OneFormatConflict(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, true)
		p := setupAgentFile(s.copy("shop", "A"), "codex", "reviewer")
		writeFile(t, p, "Чужой TOML.\n")
		r := s.setup(mode)
		out := s.parse(r, "updated")
		s.wantContract(r, out, "admin", "shop")
		wantFileState(t, p, "Чужой TOML.\n")
		if len(out.Agents) == 2 {
			a := out.Agents[1]
			want := []setupAgentsConflict{{s.copy("shop", "A"), "reviewer", setupConflictFile("codex", "reviewer"), "foreign"}}
			if !reflect.DeepEqual(a.Conflicts, want) {
				t.Errorf("conflicts: %+v, want %+v", a.Conflicts, want)
			}
			if len(a.Worktrees) > 0 && (!slices.Contains(a.Worktrees[0].Updated, "reviewer") || !slices.Contains(a.Worktrees[0].Agents, "reviewer")) {
				t.Errorf("merged format changes: %+v", a.Worktrees[0])
			}
		}
		want := e.reference("admin", "shop")
		s.wantReference(e, nil, want, p)
	})
}

func setupBreakRepo(t *testing.T, s *setupAgentsStand, project string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(s.copy(project, "A"), ".git")); err != nil {
		t.Fatal(err)
	}
}

// Case: the first project fails to deploy.
func TestSetupAgents59FirstProjectFails(t *testing.T) { setupAgentsProjectFailureCase(t, "first") }

// Case: a write fails after another copy was changed.
func TestSetupAgents59LateWriteFails(t *testing.T) { setupAgentsProjectFailureCase(t, "late_write") }

// Case: all projects fail to deploy.
func TestSetupAgents59AllProjectsFail(t *testing.T) { setupAgentsProjectFailureCase(t, "all") }

// Case: a failure and a conflict in different projects.
func TestSetupAgents59FailureAndConflict(t *testing.T) { setupAgentsProjectFailureCase(t, "conflict") }

func setupAgentsProjectFailureCase(t *testing.T, kind string) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		want := e.reference("admin", "shop")
		var wantSetup map[string]any
		if mode {
			// The reference is already fully deployed. A future automatic
			// layout by this successful control cannot change the captured
			// added/updated/removed results or the desired file oracle.
			wantSetup = e.updatedSetupControl(s.setupManifestVersion())
		}
		blocked, old := "", ""
		var conflict *setupAgentsConflict
		if kind == "late_write" {
			skipAsRoot(t)
			blocked = setupAgentFile(s.copy("admin", "B"), tool, "reviewer")
			old = readFile(t, blocked)
			readOnly(t, blocked)
		} else {
			setupBreakRepo(t, s, "admin")
		}
		if kind == "all" {
			setupBreakRepo(t, s, "shop")
		}
		if kind == "conflict" {
			setupTracked(t, s, "shop")
			conflict = &setupAgentsConflict{s.copy("shop", "A"), "reviewer", setupConflictFile(tool, "reviewer"), "tracked"}
		}
		failed := []string{"admin"}
		if kind == "all" {
			failed = append(failed, "shop")
		}
		// Obtain the original failure from the independent command under the
		// same fault. This checks both the stable code and the complete reason,
		// without assuming how the OS or git words its error on this platform.
		var original []setupAgentsFailure
		for _, project := range failed {
			if kind == "late_write" {
				p := setupAgentFile(e.copy("admin", "B"), tool, "reviewer")
				writeFile(t, p, strings.ReplaceAll(old, s.root, e.root))
				readOnly(t, p)
			} else {
				setupBreakRepo(t, e, project)
			}
			control := e.gentry("agents", "deploy", "--project", project, "--json")
			var errOut setupAgentsError
			if err := json.Unmarshal([]byte(control.stdout), &errOut); err != nil {
				t.Fatal(err)
			}
			if control.code != 1 || errOut.Error.Code == "" || errOut.Error.Message == "" {
				t.Fatalf("fault did not refuse reference deployment: %s", control)
			}
			reason := strings.Split(errOut.Error.Message, "\n")[0]
			original = append(original, setupAgentsFailure{Project: project, Reason: strings.ReplaceAll(reason, e.root, s.root), Code: errOut.Error.Code})
		}
		r := s.setup(mode)
		if mode {
			errOut, out, failures := s.failure(r, "agents_failed", wantSetup)
			s.wantFailureProjects(failures, failed...)
			partial := result{stdout: string(errOut.Error.Details["setup"])}
			if kind == "all" {
				s.wantContract(partial, out)
				if out.Agents != nil {
					t.Errorf("all failed, agents must be absent: %+v", out.Agents)
				}
			} else {
				s.wantContract(partial, out, "shop")
				if len(out.Agents) != 1 || out.Agents[0].Project != "shop" {
					t.Errorf("partial successful projects: %+v", out.Agents)
				} else if conflict != nil {
					// A tracked reviewer prevents that name's sole format in A.
					// All other paths and exact changes come from the independent
					// conflict-free deploy, including the full result of copy B.
					expected := want[1]
					expected.Worktrees = slices.Clone(expected.Worktrees)
					expected.Worktrees[0].Agents = slices.DeleteFunc(slices.Clone(expected.Worktrees[0].Agents), func(name string) bool { return name == "reviewer" })
					expected.Worktrees[0].Updated = slices.DeleteFunc(slices.Clone(expected.Worktrees[0].Updated), func(name string) bool { return name == "reviewer" })
					c := *conflict
					c.Worktree = e.copy("shop", "A")
					expected.Conflicts = []setupAgentsConflict{c}
					s.wantReference(e, out.Agents, []setupAgentsProject{expected}, setupConflictPhysical(*conflict))
				} else {
					s.wantReference(e, out.Agents, want[1:])
				}
			}
			for _, f := range failures {
				if kind == "late_write" && !strings.Contains(f.Reason, blocked) {
					t.Errorf("write failure does not name reviewer: %+v", f)
				}
			}
			if !reflect.DeepEqual(failures, original) {
				t.Errorf("original failure lost: got %+v, want %+v", failures, original)
			}
		} else {
			wantCode(t, r, 1)
			if conflict == nil {
				if r.stdout != setupAgentsUpdated(tool) {
					t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, setupAgentsUpdated(tool))
				}
			} else {
				text := setupAgentsUpdated(tool) + "\n" + setupConflictText(s, []setupAgentsConflict{*conflict}, "Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
				if r.stdout != text {
					t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, text)
				}
			}
			// Dynamic OS/git reasons must occupy exactly their specified fields;
			// all fixed text, separators and hints are compared byte for byte.
			{
				text := "Субагентов не удалось разложить.\n"
				var hints []string
				for i, p := range failed {
					if i > 0 {
						text += "\n"
					}
					text += "Проект: " + p + "\nПричина: " + original[i].Reason + "\n"
					hints = append(hints, "Повторить раскладку: gentry agents deploy --project "+p)
				}
				text += "\n" + testkit.Lines(hints...)
				if r.stderr != text {
					t.Errorf("stderr:\n%s\nwant:\n%s", r.stderr, text)
				}
			}
		}
		s.wantPluginCurrent()
		if kind != "all" {
			if conflict == nil {
				s.wantReference(e, nil, want[1:])
			} else {
				s.wantReference(e, nil, want[1:], setupConflictPhysical(*conflict))
			}
		}
		if kind == "late_write" {
			wantFileState(t, blocked, old)
			got := setupAgentFiles(t, s.copy("admin", "A"))
			expected := setupAgentFiles(t, e.copy("admin", "A"))
			if !maps.Equal(got, expected) {
				t.Errorf("changes to admin/A rolled back or not performed: got %#v want %#v", got, expected)
			}
		}
		if kind == "first" {
			retry := s.setup(true)
			out := s.parse(retry, "unchanged")
			s.wantContract(retry, out)
		}
	})
}

// Case: the state store is unavailable.
func TestSetupAgents59StateUnavailable(t *testing.T) { setupAgentsStateFailureCase(t, false) }

// Case: the state schema is newer.
func TestSetupAgents59StateNewer(t *testing.T) { setupAgentsStateFailureCase(t, true) }

func setupAgentsStateFailureCase(t *testing.T, newer bool) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		var wantSetup map[string]any
		if mode {
			wantSetup = e.updatedSetupControl(s.setupManifestVersion())
		}
		before := s.snapshot()
		code := "state_unavailable"
		if newer {
			code = "state_newer"
			db, err := sql.Open("sqlite", s.stateStore())
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`UPDATE meta SET value = '999999' WHERE key = 'schema_version'`)
			closeErr := db.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		} else {
			if err := os.Remove(s.stateStore()); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(s.stateStore(), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// Preserve the original state failure's details and hint exactly.
		original := s.gentry("project", "list", "--json")
		var baseline setupAgentsError
		if err := json.Unmarshal([]byte(original.stdout), &baseline); err != nil {
			t.Fatal(err)
		}
		if original.code != 1 || newer && baseline.Error.Code != code {
			t.Fatalf("bad state fixture: %s", original)
		}
		if !newer {
			const prefix = "Хранилище состояния недоступно: "
			var reason string
			var ok bool
			switch baseline.Error.Code {
			case "state_unavailable":
				reason, ok = strings.CutPrefix(baseline.Error.Message, prefix)
			case "internal":
				// The old read-only command can leave the SQLite failure under
				// this known wrapper, observed on Windows. Preserve its entire
				// reason; only the setup error's specified envelope differs.
				if len(baseline.Error.Details) != 0 || baseline.Error.Hint != "" || len(baseline.Error.Hints) != 0 {
					t.Fatalf("unexpected internal state control: %s", original)
				}
				reason, ok = strings.CutPrefix(baseline.Error.Message, "Внутренняя ошибка Gentry: ")
				path, err := json.Marshal(s.stateStore())
				if err != nil {
					t.Fatal(err)
				}
				baseline.Error.Code = code
				baseline.Error.Message = prefix + reason
				baseline.Error.Details = map[string]json.RawMessage{"path": path}
				baseline.Error.Hint = "Повторите команду; если ошибка повторится, проверьте доступ к файлу: " + s.stateStore()
			default:
				t.Fatalf("unexpected state control: %s", original)
			}
			if !ok || strings.TrimSpace(reason) == "" || strings.Contains(reason, "\n") {
				t.Fatalf("state control has no complete single-line reason: %s", original)
			}
		}
		textBaseline := s.gentry("project", "list")
		r := s.setup(mode)
		if mode {
			out, setup, failures := s.failure(r, code, wantSetup)
			if setup.Action != "updated" || setup.Agents != nil || len(failures) != 0 {
				t.Errorf("partial state error: setup=%+v, failures=%+v", setup, failures)
			}
			delete(out.Error.Details, "setup")
			if !reflect.DeepEqual(out.Error.Details, baseline.Error.Details) || out.Error.Message != baseline.Error.Message || out.Error.Hint != baseline.Error.Hint || !slices.Equal(out.Error.Hints, baseline.Error.Hints) {
				t.Errorf("original state failure lost: got %+v want %+v", out.Error, baseline.Error)
			}
			if !newer {
				var path string
				json.Unmarshal(out.Error.Details["path"], &path)
				if !samePath(path, s.stateStore()) {
					t.Errorf("state error path=%q, want %q", path, s.stateStore())
				}
				wantHint := "Повторите команду; если ошибка повторится, проверьте доступ к файлу: " + s.stateStore()
				if out.Error.Hint != wantHint {
					t.Errorf("state error hint=%q, want %q", out.Error.Hint, wantHint)
				}
			}
		} else {
			wantCode(t, r, 1)
			if r.stdout != setupAgentsUpdated(tool) || newer && r.stderr != textBaseline.stderr {
				t.Errorf("state failure output:\n%s\nwant stdout:\n%s\nstderr:\n%s", r, setupAgentsUpdated(tool), textBaseline.stderr)
			}
			if newer {
				want := fmt.Sprintf("Хранилище состояния создано более новой версией Gentry: схема 999999, поддерживается до ")
				if !strings.HasPrefix(r.stderr, want) || !strings.HasSuffix(r.stderr, ".\n\nОбновите Gentry.\n") {
					t.Errorf("state_newer text:\n%s", r.stderr)
				}
			} else {
				want := baseline.Error.Message + "\n\n" + baseline.Error.Hint + "\n"
				if r.stderr != want {
					t.Errorf("state_unavailable text:\n%s\nwant:\n%s", r.stderr, want)
				}
			}
		}
		s.wantPluginCurrent()
		s.wantUnchanged(before)
	})
}

// Case: repeating setup after a conflict.
func TestSetupAgents59RetryAfterConflict(t *testing.T) {
	eachSetupAgents(t, true, func(t *testing.T, tool string, mode bool) {
		s, e := changedSetupPair(t, tool, false)
		p := setupAgentFile(s.copy("shop", "A"), tool, "reviewer")
		writeFile(t, p, "Чужой файл.\n")
		first := s.setup(true)
		firstOut := s.parse(first, "updated")
		if len(firstOut.Agents) != 2 || len(firstOut.Agents[1].Conflicts) != 1 {
			t.Errorf("first setup did not report conflict: %s", first.stdout)
		}
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		r := s.setup(true)
		out := s.parse(r, "unchanged")
		s.wantContract(r, out)
		if testkit.FileExists(p) {
			t.Error("unchanged setup repeated layout")
		}
		s.mustIn(s.root, "agents", "deploy", "--project", "shop")
		want := e.reference("shop")
		s.wantReference(e, nil, want)
	})
}

// Case: refusing a switch without its flag.
func TestSetupAgents59RefusedSwitch(t *testing.T) {
	eachSetupAgents(t, false, func(t *testing.T, tool string, mode bool) {
		s, _ := setupAgentsPair(t, tool, false, false)
		previous, _ := s.elsewhere()
		s.stale()
		before := s.snapshot()
		r := s.setup(mode)
		wantCode(t, r, 1)
		if mode {
			if r.stderr != "" {
				t.Errorf("JSON error stderr: %s", r.stderr)
			}
			var out setupAgentsError
			if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
				t.Fatal(err)
			}
			if out.Error.Code != "plugin_elsewhere" {
				t.Errorf("code: %s", out.Error.Code)
			}
			if _, ok := out.Error.Details["setup"]; ok {
				t.Error("refused setup has partial result")
			}
		} else {
			want := fmt.Sprintf("Плагин Gentry в %s подключён из другой папки данных.\nПапка плагина: %s\nПапка плагина этой команды: %s\n\nПереключить плагин всех сессий %s на папку данных этой команды: gentry setup %s --switch\n", setupAgentsTitle(tool), previous, filepath.Join(s.data, "integrations", tool), setupAgentsTitle(tool), tool)
			if r.stdout != "" || r.stderr != want {
				t.Errorf("refused output:\n%s\nwant stderr:\n%s", r, want)
			}
		}
		s.wantUnchanged(before)
	})
}
