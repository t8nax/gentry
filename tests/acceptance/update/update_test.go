package update_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/tests/testkit"
)

// 1. Обновление одной командой.

// TestUpdateFrom010 checks that the update of 0.1.0 installs the latest
// release and starts with the versions, the line of the new sessions, an
// empty line and the blocks of the incompatible changes.
func TestUpdateFrom010(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	r := s.update()
	wantCode(t, r, 0)
	s.wantVersion(latest)
	ls := lines(r.stdout)
	if want := []string{updated(v010), newSessions, ""}; len(ls) < 4 || !slices.Equal(ls[:3], want) || !strings.HasPrefix(ls[3], "Несовместимые изменения ") {
		t.Errorf("stdout:\n%s\nwant the lines:\n%s«Несовместимые изменения …»", r.stdout, strings.Join(want, "\n")+"\n")
	}
}

// TestUpdateJSON checks the output of update --json: it matches the schema
// and names the action, the previous and the new version and the program.
func TestUpdateJSON(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	out, res := s.updateJSON()
	if res.Action != "updated" || res.Previous == nil || *res.Previous != v010 || res.Version != latest || !samePath(res.Program, s.program()) {
		t.Errorf("update --json:\n%s\nwant action updated, previous %s, version %s, program %s", out, v010, latest, s.program())
	}
}

// TestUpdateBusyProgram checks that on Windows the update replaces a program
// a process runs: the program is renamed to gentry.exe.old, the process goes
// on, and the output says nothing of the renaming.
func TestUpdateBusyProgram(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a running program holds its file only on Windows")
	}
	parallel(t)
	s := installedStand(t, v010, false)
	server := s.startMCP()
	r := s.update()
	wantCode(t, r, 0)
	s.wantVersion(latest)
	if !testkit.FileExists(s.program() + ".old") {
		t.Errorf("no %s.old next to the program", s.program())
	}
	server.wantRunning()
	for _, out := range []string{r.stdout, r.stderr} {
		wantNoLineWith(t, out, "переименован")
		wantNoLineWith(t, out, ".old")
	}
}

// TestUpdateRemovesFreeOld checks that on Windows the update removes a
// gentry.exe.old no process holds.
func TestUpdateRemovesFreeOld(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("gentry.exe.old is left only on Windows")
	}
	parallel(t)
	s := installedStand(t, v020, false)
	const stale = "a program of an earlier update\n"
	old := s.program() + ".old"
	writeFile(t, old, stale)
	r := s.update()
	wantCode(t, r, 0)
	// The update renames the program it runs from to gentry.exe.old again;
	// the file that was there is gone.
	if b, err := os.ReadFile(old); err == nil && string(b) == stale {
		t.Errorf("%s of the earlier update is still there", old)
	}
}

// TestUpdateConcurrent checks that two updates run at once do not replace
// the program together: one updates it, the other waits for it and then
// finds the latest version installed, so the program of the release is
// downloaded once.
func TestUpdateConcurrent(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	// The download of the program lasts long enough for both updates to run
	// at once.
	s.mock.slowProgram(3 * time.Second)
	first, second := s.start("update"), s.start("update")
	rs := []result{first(), second()}
	current := []string{"Установлена последняя версия Gentry.", "Версия: " + latest}
	var nUpdated, nCurrent int
	for _, r := range rs {
		if r.code != 0 {
			t.Errorf("update: %s\nwant exit code 0", r)
		}
		switch ls := lines(r.stdout); {
		case len(ls) > 0 && ls[0] == updated(v010):
			nUpdated++
		case len(ls) >= 2 && slices.Equal(ls[:2], current):
			nCurrent++
		}
	}
	if nUpdated != 1 || nCurrent != 1 {
		t.Errorf("the updates run at once:\n%s\n%s\nwant one to start with «%s» and the other with:\n%s", rs[0], rs[1], updated(v010), testkit.Lines(current...))
	}
	if n := s.mock.served(latest + "/" + hostAsset); n != 1 {
		t.Errorf("the mock served the program %s of %s %d times, want once; requests %q", hostAsset, latest, n, s.mock.requested())
	}
	s.wantVersion(latest)
}

// TestUpdateClosedInput checks that the update asks nothing: with the
// standard input closed it updates the program.
func TestUpdateClosedInput(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	r := s.updateClosedInput()
	wantCode(t, r, 0)
	s.wantVersion(latest)
}

// TestUpdatePlugin checks that the update builds the plugin by the new
// program, as its setup claude does, and does not say so in the text; the
// JSON has the integration of Claude Code updated.
func TestUpdatePlugin(t *testing.T) {
	parallel(t)
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v010, true)
		r := s.update()
		wantCode(t, r, 0)
		got := s.plugin().Version
		setup := s.gentry("setup", "claude", "--json")
		var out struct {
			PluginVersion string `json:"plugin_version"`
		}
		if setup.code != 0 || json.Unmarshal([]byte(setup.stdout), &out) != nil || out.PluginVersion == "" {
			t.Fatalf("setup claude --json after the update: %s", setup)
		}
		if got != out.PluginVersion {
			t.Errorf("the plugin of Gentry in Claude Code has the version %s, the new program builds %s", got, out.PluginVersion)
		}
		for _, o := range []string{r.stdout, r.stderr} {
			wantNoLineWith(t, o, "Плагин Gentry в Claude Code обновлён.")
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v010, true)
		out, res := s.updateJSON()
		if len(res.Integrations) != 1 || res.Integrations[0].Tool != "claude" || res.Integrations[0].Action != "updated" {
			t.Errorf("update --json:\n%s\nwant one integration: tool claude, action updated", out)
		}
	})
}

// TestUpdateDisabledPlugin checks that the update tells a plugin the
// operator disabled, with the hint to enable it, and leaves it disabled.
func TestUpdateDisabledPlugin(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	s.editPlugin(func(p *claudePlugin) { p.Enabled = false })
	r := s.update()
	wantCode(t, r, 0)
	wantLine(t, r.stdout, "Плагин Gentry отключён в Claude Code.")
	wantLine(t, r.stdout, "Включить плагин: claude plugin enable gentry@gentry")
	if s.plugin().Enabled {
		t.Error("the plugin the operator disabled is enabled")
	}
}

// TestUpdatePluginPaths checks that the hook and the tool server of the
// plugin after the update name the installed program.
func TestUpdatePluginPaths(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	wantCode(t, s.update(), 0)
	program := filepath.ToSlash(s.program())

	var manifest struct {
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	b := readFile(t, filepath.Join(s.integration(), "gentry", ".claude-plugin", "plugin.json"))
	if err := json.Unmarshal([]byte(b), &manifest); err != nil || len(manifest.MCPServers) == 0 {
		t.Fatalf("plugin.json: %v\n%s", err, b)
	}
	for name, srv := range manifest.MCPServers {
		if !samePath(srv.Command, program) {
			t.Errorf("the tool server %s of the plugin runs %s, want %s", name, srv.Command, program)
		}
	}

	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	b = readFile(t, filepath.Join(s.integration(), "gentry", "hooks", "hooks.json"))
	if err := json.Unmarshal([]byte(b), &hooks); err != nil || len(hooks.Hooks["SessionStart"]) == 0 {
		t.Fatalf("hooks.json: %v\n%s", err, b)
	}
	for _, entry := range hooks.Hooks["SessionStart"] {
		for _, h := range entry.Hooks {
			quoted, _, _ := strings.Cut(strings.TrimPrefix(h.Command, `"`), `"`)
			if !strings.HasPrefix(h.Command, `"`) || !samePath(quoted, program) {
				t.Errorf("the hook of the plugin runs %s, want the program %s", h.Command, program)
			}
		}
	}
}

// TestUpdateNewSessionsLine checks that the second line is that of the new
// sessions and the output has none of the lines the update does not print.
func TestUpdateNewSessionsLine(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	r := s.update()
	wantCode(t, r, 0)
	if ls := lines(r.stdout); len(ls) < 2 || ls[1] != newSessions {
		t.Errorf("stdout:\n%s\nwant the second line «%s»", r.stdout, newSessions)
	}
	for _, out := range []string{r.stdout, r.stderr} {
		wantNoLineWith(t, out, "Открытые сессии")
		wantNoLineWith(t, out, "Плагин Gentry в Claude Code обновлён.")
		wantNoLineWith(t, out, "Субагенты разложены")
		for _, l := range lines(out) {
			if strings.HasPrefix(l, "Программа:") {
				t.Errorf("output has the line %q:\n%s", l, out)
			}
		}
	}
}

// 2. Что изменилось в выпуске.

// TestUpdateBreaking checks the blocks of the incompatible changes of the
// versions after 0.1.0, in the order of the versions, and the field
// breaking of the JSON.
func TestUpdateBreaking(t *testing.T) {
	parallel(t)
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v010, false)
		r := s.update()
		wantCode(t, r, 0)
		wantInOrder(t, r.stdout,
			"Несовместимые изменения 0.2.0:", "  "+breaking020,
			"Несовместимые изменения 0.3.0:", "  "+breaking030)
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v010, false)
		out, res := s.updateJSON()
		want := []updateBreaking{{v020, []string{breaking020}}, {v030, []string{breaking030}}}
		if !slices.EqualFunc(res.Breaking, want, func(a, b updateBreaking) bool { return a.Version == b.Version && slices.Equal(a.Lines, b.Lines) }) {
			t.Errorf("update --json:\n%s\nwant breaking %+v", out, want)
		}
	})
}

// TestUpdateBreakingAfterInstalled checks that the update shows the
// incompatible changes of the versions after the installed one only.
func TestUpdateBreakingAfterInstalled(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	r := s.update()
	wantCode(t, r, 0)
	wantLine(t, r.stdout, "Несовместимые изменения 0.3.0:")
	wantNoLineWith(t, r.stdout, "Несовместимые изменения 0.2.0:")
}

// TestUpdateBreakingMultiline checks that a line of the journal on three
// lines of the file is one line of the output, its parts apart by a space.
func TestUpdateBreakingMultiline(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	s.mock.setJournal(v030, journal(section030Multiline, section020, section010))
	r := s.update()
	wantCode(t, r, 0)
	wantInOrder(t, r.stdout, "Несовместимые изменения 0.3.0:", "  "+breaking030)
}

// TestUpdateNoBreaking checks that a version with no «Несовместимые
// изменения» gives no block.
func TestUpdateNoBreaking(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	s.mock.setJournal(v030, journal(section030NoBreaking, section020, section010))
	r := s.update()
	wantCode(t, r, 0)
	for _, l := range lines(r.stdout) {
		if strings.HasPrefix(l, "Несовместимые изменения") {
			t.Errorf("stdout has the line %q:\n%s", l, r.stdout)
		}
	}
}

// TestUpdatePage checks the link to the page of the new version and no link
// to the list of the releases.
func TestUpdatePage(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	r := s.update()
	wantCode(t, r, 0)
	wantLine(t, r.stdout, pageLine)
	for _, out := range []string{r.stdout, r.stderr} {
		for rest := out; ; {
			i := strings.Index(rest, pageURL)
			if i < 0 {
				break
			}
			rest = rest[i+len(pageURL):]
			if !strings.HasPrefix(rest, latest+"/") {
				t.Errorf("the output links to %s without the version:\n%s", pageURL, out)
				break
			}
		}
	}
}

// TestUpdateNoChangelog checks that the update succeeds when the journal of
// changes cannot be read: it tells so with the reason, then the link to the
// page; the JSON has changelog_error.
func TestUpdateNoChangelog(t *testing.T) {
	parallel(t)
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v020, false)
		s.mock.setJournal(v030, "")
		r := s.update()
		wantCode(t, r, 0)
		s.wantVersion(latest)
		ls := lines(r.stdout)
		i := slices.Index(ls, "Не удалось прочитать журнал изменений.")
		if i < 0 || i+1 >= len(ls) {
			t.Fatalf("stdout has no line «Не удалось прочитать журнал изменений.» and a line after it:\n%s", r.stdout)
		}
		wantReason(t, ls[i+1], "Ошибка: ")
		if j := slices.Index(ls, pageLine); j <= i+1 {
			t.Errorf("stdout has no line «%s» after the reason:\n%s", pageLine, r.stdout)
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, v020, false)
		s.mock.setJournal(v030, "")
		out, res := s.updateJSON()
		if res.ChangelogError == nil || strings.TrimSpace(*res.ChangelogError) == "" {
			t.Errorf("update --json:\n%s\nwant the field changelog_error", out)
		}
	})
}

// 3. Последняя версия уже установлена.

// TestUpdateCurrent checks that the latest version installed is not
// downloaded or replaced: the output is the two lines, the mock gives no
// program, the program is the same, and the JSON has the action current.
func TestUpdateCurrent(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, true)
	before := s.programSum()
	r := s.update()
	wantCode(t, r, 0)
	if want := "Установлена последняя версия Gentry.\nВерсия: " + latest + "\n"; r.stdout != want || r.stderr != "" {
		t.Errorf("%s\nwant stdout:\n%s", r, want)
	}
	out, res := s.updateJSON()
	if res.Action != "current" {
		t.Errorf("update --json:\n%s\nwant action current", out)
	}
	if p := s.mock.programRequests(); len(p) > 0 {
		t.Errorf("the mock gave the programs %q", p)
	}
	s.wantProgram(before)
}

// TestUpdateCurrentWithoutIntegrations checks that with the latest version
// installed and no integration connected the update says so after the
// lines of the version and hints at setup, and downloads no program.
func TestUpdateCurrentWithoutIntegrations(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	r := s.update()
	wantCode(t, r, 0)
	want := []string{"Установлена последняя версия Gentry.", "Версия: " + latest, "Интеграции с ИИ-инструментами не подключены."}
	if ls := lines(r.stdout); len(ls) < len(want) || !slices.Equal(ls[:len(want)], want) {
		t.Errorf("stdout:\n%s\nwant the first lines:\n%s", r.stdout, testkit.Lines(want...))
	}
	wantLastBlock(t, r.stdout, "Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>")
	if p := s.mock.programRequests(); len(p) > 0 {
		t.Errorf("the mock gave the programs %q", p)
	}
}

// TestUpdateCurrentPluginMatches checks that the plugin that matches the
// program is not updated or installed anew.
func TestUpdateCurrentPluginMatches(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, true)
	n := len(s.claude().Log)
	r := s.update()
	wantCode(t, r, 0)
	if want := "Установлена последняя версия Gentry.\nВерсия: " + latest + "\n"; r.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
	for _, c := range s.claudeCalls(n) {
		if strings.HasPrefix(c, "plugin update ") || strings.HasPrefix(c, "plugin install ") {
			t.Errorf("the update ran claude %s", c)
		}
	}
}

// TestUpdateCurrentPluginStale checks that a plugin that does not match the
// program is updated when the latest version is installed.
func TestUpdateCurrentPluginStale(t *testing.T) {
	parallel(t)
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, latest, true)
		s.editPlugin(func(p *claudePlugin) { p.Version = "0.0.0+00000000" })
		r := s.update()
		wantCode(t, r, 0)
		wantLine(t, r.stdout, newSessions)
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := installedStand(t, latest, true)
		s.editPlugin(func(p *claudePlugin) { p.Version = "0.0.0+00000000" })
		out, res := s.updateJSON()
		i := slices.IndexFunc(res.Integrations, func(x updateIntegration) bool { return x.Tool == "claude" })
		if i < 0 || res.Integrations[i].Action != "updated" {
			t.Errorf("update --json:\n%s\nwant the integration claude with action updated", out)
		}
	})
}

// TestUpdateCurrentPluginStaleAgents checks that with the latest version
// installed the update of a stale plugin lays out the subagents anew, which
// agents deploy then finds in place, says nothing of it in the text and
// gives the project in the field agents of the JSON. A file of a subagent
// changed by hand stands for a file the previous plugin laid out otherwise.
func TestUpdateCurrentPluginStaleAgents(t *testing.T) {
	parallel(t)
	setup := func(t *testing.T) *stand {
		s := installedStand(t, latest, true)
		editByHand(t, reviewer(s.project("shop")))
		s.editPlugin(func(p *claudePlugin) { p.Version = "0.0.0+00000000" })
		return s
	}
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		r := s.update()
		wantCode(t, r, 0)
		for _, o := range []string{r.stdout, r.stderr} {
			wantNoLineWith(t, o, "убагент")
		}
		s.wantAgentsInPlace("shop")
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		out, res := s.updateJSON()
		if !slices.ContainsFunc(res.Agents, func(a updateAgents) bool { return a.Project == "shop" }) {
			t.Errorf("update --json:\n%s\nwant an element of agents for shop", out)
		}
	})
}

// 4. Субагенты и что сделать после обновления.

// TestUpdateAgents checks that the update lays out the subagents in the
// worktrees of both projects, which agents deploy then finds in place, says
// nothing of it in the text and gives an element per project in the order
// of project list in the JSON. A file of a subagent changed by hand stands
// for a file the previous program laid out otherwise.
func TestUpdateAgents(t *testing.T) {
	parallel(t)
	setup := func(t *testing.T) *stand {
		s := installedStand(t, v010, true)
		for _, name := range []string{"shop", "admin"} {
			editByHand(t, reviewer(s.project(name)))
		}
		return s
	}
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		r := s.update()
		wantCode(t, r, 0)
		for _, o := range []string{r.stdout, r.stderr} {
			wantNoLineWith(t, o, "убагент")
		}
		for _, name := range []string{"shop", "admin"} {
			s.wantAgentsInPlace(name)
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		out, res := s.updateJSON()
		order := s.projectOrder()
		var got []string
		for _, a := range res.Agents {
			got = append(got, a.Project)
			// The file of reviewer edited by hand is rewritten in each worktree.
			if a.Changed == nil || !*a.Changed || len(a.Worktrees) == 0 {
				t.Errorf("the element %s of agents is no layout that changed the worktrees: %s", a.Project, out)
			}
			for _, w := range a.Worktrees {
				if !slices.Contains(w.Updated, "reviewer") {
					t.Errorf("the worktree %s of %s has no reviewer in updated: %s", w.Path, a.Project, out)
				}
			}
		}
		if !slices.Equal(got, order) {
			t.Errorf("update --json:\n%s\nwant agents of the projects %q, in the order of project list", out, order)
		}
	})
}

// TestUpdateAgentsTask checks that the worktree of a task gets the
// subagents of the flow of the task and the free one those of the active
// flow, as agents deploy lays them out.
func TestUpdateAgentsTask(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	shop := s.project("shop", "fix")
	fix := s.worktree("shop", "fix")
	s.mustIn(fix, "task", "take", "--scenario", "feature", "--title", "Частичный возврат по карте", "--statement", "Деньги возвращаются на карту.")
	taskText := readFile(t, reviewer(fix))
	const active = "Проверить изменения и тексты магазина."
	writeFile(t, filepath.Join(s.data, "process", "agents", "reviewer.md"), active+"\n")
	s.mustIn(shop, "flow", "apply")
	if !strings.Contains(readFile(t, reviewer(shop)), active) || strings.Contains(readFile(t, reviewer(fix)), active) {
		t.Fatal("stand: flow apply did not lay out the active flow in the free worktree only")
	}
	editByHand(t, reviewer(shop))
	editByHand(t, reviewer(fix))

	wantCode(t, s.update(), 0)
	if got := readFile(t, reviewer(fix)); got != taskText {
		t.Errorf("reviewer in the worktree of the task:\n%s\nwant that of the flow of the task:\n%s", got, taskText)
	}
	if got := readFile(t, reviewer(shop)); !strings.Contains(got, active) || strings.Contains(got, handEdit) {
		t.Errorf("reviewer in the free worktree:\n%s\nwant that of the active flow", got)
	}
}

// TestUpdateAgentsWithoutIntegrations checks that the update lays out the
// subagents with no integration connected.
func TestUpdateAgentsWithoutIntegrations(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	s.project("shop")
	out, res := s.updateJSON()
	if !slices.ContainsFunc(res.Agents, func(a updateAgents) bool { return a.Project == "shop" }) {
		t.Errorf("update --json:\n%s\nwant an element of agents for shop", out)
	}
}

// TestUpdateAgentsUnchanged checks that with the same version and the plugin
// unchanged the update does not touch the subagents.
func TestUpdateAgentsUnchanged(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, true)
	shop := s.project("shop")
	s.project("admin")
	editByHand(t, reviewer(shop))
	edited := readFile(t, reviewer(shop))
	wantCode(t, s.update(), 0)
	out, _ := s.updateJSON()
	if got := readFile(t, reviewer(shop)); got != edited {
		t.Errorf("the file of the subagent changed:\n%s\nwant:\n%s", got, edited)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(out), &fields)
	if _, ok := fields["agents"]; ok {
		t.Errorf("update --json:\n%s\nwant no field agents", out)
	}
}

// TestUpdateNoProjects checks that the update with no project and no state
// store succeeds, says nothing of subagents and creates no state store.
func TestUpdateNoProjects(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	if testkit.FileExists(s.stateStore()) {
		t.Fatalf("stand: %s exists", s.stateStore())
	}
	r := s.update()
	wantCode(t, r, 0)
	for _, o := range []string{r.stdout, r.stderr} {
		wantNoLineWith(t, o, "убагент")
	}
	if testkit.FileExists(s.stateStore()) {
		t.Errorf("the update created the state store %s", s.stateStore())
	}
}

// TestUpdateAgentsConflict checks that a file of the name of a subagent git
// tracks in a worktree does not stop the update: it warns with the file and
// the hint to lay out the project, and leaves the file.
func TestUpdateAgentsConflict(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, false)
	s.project("shop", "fix")
	fix := s.worktree("shop", "fix")
	const own = "Ревьюер проекта.\n"
	writeFile(t, reviewer(fix), own)
	gittest.Run(t, fix, "add", "--force", ".claude/agents/reviewer.md")
	gittest.Run(t, fix, "commit", "--quiet", "-m", "reviewer")

	r := s.update()
	wantCode(t, r, 0)
	s.wantVersion(latest)
	wantLine(t, r.stdout, "Часть субагентов не разложена: файлы с их именами уже есть.")
	if !strings.Contains(r.stdout, ".claude/agents/reviewer.md") || !strings.Contains(r.stdout, fix) {
		t.Errorf("stdout does not name the file .claude/agents/reviewer.md of %s:\n%s", fix, r.stdout)
	}
	wantLine(t, r.stdout, "Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
	if got := readFile(t, reviewer(fix)); got != own {
		t.Errorf("the tracked file changed:\n%s", got)
	}
}

// TestUpdateOutputOrder checks the order of the blocks of the output with a
// warning of each kind: the versions, the new sessions, the disabled plugin,
// the conflict of the layout, the incompatible changes, the page, and the
// hints of both warnings in the last block.
func TestUpdateOutputOrder(t *testing.T) {
	parallel(t)
	s := installedStand(t, v010, true)
	s.editPlugin(func(p *claudePlugin) { p.Enabled = false })
	s.project("shop", "fix")
	fix := s.worktree("shop", "fix")
	writeFile(t, reviewer(fix), "Ревьюер проекта.\n")
	gittest.Run(t, fix, "add", "--force", ".claude/agents/reviewer.md")
	gittest.Run(t, fix, "commit", "--quiet", "-m", "reviewer")

	r := s.update()
	wantCode(t, r, 0)
	wantOrdered(t, r.stdout,
		updated(v010),
		newSessions,
		"Плагин Gentry отключён в Claude Code.",
		"Часть субагентов не разложена: файлы с их именами уже есть.",
		"Несовместимые изменения 0.2.0:",
		"Несовместимые изменения 0.3.0:",
		pageLine)
	wantInLastBlock(t, r.stdout,
		"Включить плагин: claude plugin enable gentry@gentry",
		"Разложить субагентов после разбора файлов: gentry agents deploy --project shop")
}

// TestUpdateAgentsFailed checks that a project whose layout fails does not
// stop the layout of the others: the new program stays, and the update
// fails with the project, the reason and the hint to lay it out again.
func TestUpdateAgentsFailed(t *testing.T) {
	parallel(t)
	setup := func(t *testing.T) *stand {
		s := installedStand(t, v010, false)
		s.project("shop", "fix")
		editByHand(t, reviewer(s.project("admin")))
		// git no longer knows the worktree.
		writeFile(t, filepath.Join(s.worktree("shop", "fix"), ".git"), "gitdir: "+filepath.Join(s.root, "nowhere")+"\n")
		return s
	}
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		r := s.update()
		wantCode(t, r, 1)
		s.wantVersion(latest)
		s.wantAgentsInPlace("admin")
		ls := lines(r.stderr)
		if len(ls) < 3 || ls[0] != "Субагентов не удалось разложить." || ls[1] != "Проект: shop" {
			t.Fatalf("stderr:\n%s\nwant «Субагентов не удалось разложить.», «Проект: shop» and «Причина: <причина>»", r.stderr)
		}
		wantReason(t, ls[2], "Причина: ")
		wantLastBlock(t, r.stderr, "Повторить раскладку: gentry agents deploy --project shop")
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		setup(t).wantError("agents_failed")
	})
}

// TestUpdateWithoutIntegrations checks that with no integration connected
// the update installs the program, tells so and hints at setup; it changes
// nothing in Claude Code.
func TestUpdateWithoutIntegrations(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	r := s.update()
	wantCode(t, r, 0)
	s.wantVersion(latest)
	wantLine(t, r.stdout, "Интеграции с ИИ-инструментами не подключены.")
	wantLastBlock(t, r.stdout, "Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>")
	// The update may read the lists of Claude Code, to find a plugin of
	// another data root; it runs no other command of plugin.
	for _, c := range s.claudeCalls(0) {
		if strings.HasPrefix(c, "plugin ") && !strings.HasPrefix(c, "plugin marketplace list") && !strings.HasPrefix(c, "plugin list") {
			t.Errorf("the update ran claude %s", c)
		}
	}
}
