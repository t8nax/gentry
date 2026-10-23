package update_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// GEN-70, 1. Подключение к Codex: gentry setup codex of the installed program
// with the fake Codex of the stand.
//
// Each text of the section «Тексты» is stated word for word in one test
// only: that of the case whose subject is the text or the layout of the
// output by blocks (TestSetupCodexText, TestSetupCodexWarnings), or, for a
// text no such case has, the case it belongs to. The other cases check the
// exit code, the code of the error or the fields of the JSON, or find a line
// by the layout of the blocks and by the data it names.

// codexSkills are the skills of the plugin of Gentry.
var codexSkills = []string{"canceling-task", "editing-flow", "working-on-task"}

// installedCodexPlugin returns the folder of the plugin of Gentry the fake
// Codex installed in its cache.
func (s *stand) installedCodexPlugin() string {
	s.t.Helper()
	cache := filepath.Join(s.codexHome, "plugins", "cache", "gentry", "gentry")
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 {
		s.t.Fatalf("the fake Codex has no plugin of Gentry in %s: %v %v", cache, entries, err)
	}
	return filepath.Join(cache, entries[0].Name())
}

// codexPluginDir returns the folder of the plugin of Gentry in the
// marketplace of the integration of Codex, as its marketplace.json names it.
func (s *stand) codexPluginDir() string {
	s.t.Helper()
	root := s.codexIntegration()
	var m struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	// Codex reads .agents/plugins/marketplace.json, and
	// .claude-plugin/marketplace.json too.
	file := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	if !testkit.FileExists(file) {
		file = filepath.Join(root, ".claude-plugin", "marketplace.json")
	}
	b := readFile(s.t, file)
	if err := json.Unmarshal([]byte(b), &m); err != nil {
		s.t.Fatalf("marketplace.json of %s: %v\n%s", root, err, b)
	}
	for _, p := range m.Plugins {
		if p.Name != "gentry" {
			continue
		}
		var path string
		if json.Unmarshal(p.Source, &path) != nil {
			var obj struct {
				Path string `json:"path"`
			}
			json.Unmarshal(p.Source, &obj)
			path = obj.Path
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		return path
	}
	s.t.Fatalf("marketplace.json of %s has no plugin gentry:\n%s", root, b)
	return ""
}

// pluginServers returns the tool servers of the plugin in dir by name, from
// the file of servers its manifest names (.mcp.json by default) or from the
// manifest itself.
func pluginServers(t *testing.T, dir string) map[string]map[string]any {
	t.Helper()
	var manifest map[string]any
	b := readFile(t, filepath.Join(dir, ".codex-plugin", "plugin.json"))
	if err := json.Unmarshal([]byte(b), &manifest); err != nil {
		t.Fatalf("plugin.json of %s: %v\n%s", dir, err, b)
	}
	var servers map[string]any
	switch v := manifest["mcpServers"].(type) {
	case map[string]any:
		servers = v
	default:
		file, _ := v.(string)
		if file == "" {
			file = ".mcp.json"
		}
		var doc map[string]any
		text := readFile(t, filepath.Join(dir, filepath.FromSlash(file)))
		if err := json.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatalf("the file of servers %s of the plugin: %v\n%s", file, err, text)
		}
		servers = doc
		if inner, ok := doc["mcpServers"].(map[string]any); ok {
			servers = inner
		}
	}
	out := map[string]map[string]any{}
	for name, v := range servers {
		if m, ok := v.(map[string]any); ok {
			out[name] = m
		}
	}
	return out
}

// coreServer returns the server core of the plugin installed in the fake
// Codex.
func (s *stand) coreServer() map[string]any {
	s.t.Helper()
	core, ok := pluginServers(s.t, s.installedCodexPlugin())["core"]
	if !ok {
		s.t.Fatalf("the plugin of Gentry in Codex has no server core")
	}
	return core
}

// wantBlock checks that the output has the lines one after another as a
// block: after an empty line or at its start.
func wantBlock(t *testing.T, out string, want ...string) {
	t.Helper()
	ls := lines(out)
	for i := 0; i+len(want) <= len(ls); i++ {
		if slices.Equal(ls[i:i+len(want)], want) && (i == 0 || ls[i-1] == "") {
			return
		}
	}
	t.Errorf("output has no block:\n%s\noutput:\n%s", testkit.Lines(want...), out)
}

// lineAfter returns the line after the line want of the output; false if
// there is none.
func lineAfter(out, want string) (string, bool) {
	ls := lines(out)
	i := slices.Index(ls, want)
	if i < 0 || i+1 >= len(ls) {
		return "", false
	}
	return ls[i+1], true
}

// blocksOf returns the blocks of the output: its lines apart by empty lines.
func blocksOf(out string) [][]string {
	var bs [][]string
	var cur []string
	for _, l := range lines(out) {
		if l == "" {
			if cur != nil {
				bs = append(bs, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, l)
	}
	if cur != nil {
		bs = append(bs, cur)
	}
	return bs
}

// wantPathsBlock checks that the output has a block of a heading and the
// paths, each indented by two spaces, in any order: the block of the
// worktrees not trusted. Its heading is stated by TestSetupCodexWarnings.
func wantPathsBlock(t *testing.T, out string, paths ...string) {
	t.Helper()
	for _, b := range blocksOf(out) {
		if len(b) != len(paths)+1 || strings.HasPrefix(b[0], " ") {
			continue
		}
		ok := true
		for _, p := range paths {
			ok = ok && slices.Contains(b[1:], "  "+p)
		}
		if ok {
			return
		}
	}
	t.Errorf("output has no block of a heading and the paths %q, each indented by two spaces:\n%s", paths, out)
}

// trustHints returns how many lines of the last block of the output name
// the record of trust key, written as a string of TOML: the hints to trust
// the worktrees of the record.
// The words of the hint are stated by TestSetupCodexWarnings.
func trustHints(out, key string) int {
	n := 0
	for _, l := range lastBlock(out) {
		if strings.Contains(l, `projects."`+testkit.CodexTrustKey(key)+`".trust_level`) {
			n++
		}
	}
	return n
}

// lastBlock returns the lines of the last block of the output, after its
// last empty line.
func lastBlock(out string) []string {
	ls := lines(out)
	i := len(ls) - 1
	for i >= 0 && ls[i] != "" {
		i--
	}
	return ls[i+1:]
}

// TestSetupCodexInstalls checks that setup codex builds the plugin of Gentry
// in the data root and installs it in Codex.
func TestSetupCodexInstalls(t *testing.T) {
	parallel(t)
	t.Run("first_connection", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		// The lines of the first connection are stated by
		// TestSetupCodexText/first_connection; here the action of the JSON.
		if out, res, _ := s.setupCodexJSON(); res.Action != "installed" {
			t.Errorf("setup codex --json: %s\nwant action installed", out)
		}
		p, ok := s.codexPlugin()
		if !ok || !p.Installed || !p.Enabled {
			t.Fatalf("the fake Codex has no plugin %s installed and enabled: %+v", testkit.CodexPluginID, p)
		}
		if root := s.codexMarketplace(); !samePath(root, s.codexIntegration()) {
			t.Errorf("the marketplace gentry of Codex is %q, want %s", root, s.codexIntegration())
		}
		dir := s.installedCodexPlugin()
		var skills []string
		entries, _ := os.ReadDir(filepath.Join(dir, "skills"))
		for _, e := range entries {
			if e.IsDir() && testkit.FileExists(filepath.Join(dir, "skills", e.Name(), "SKILL.md")) {
				skills = append(skills, e.Name())
			}
		}
		if !slices.Equal(skills, codexSkills) {
			t.Errorf("the skills of the plugin: %q, want %q", skills, codexSkills)
		}
		core, ok := pluginServers(t, dir)["core"]
		if !ok {
			t.Fatal("the plugin has no server core")
		}
		cmd, _ := core["command"].(string)
		args, _ := json.Marshal(core["args"])
		if !samePath(cmd, s.program()) || string(args) != `["mcp"]` {
			t.Errorf("the server core runs %q %s, want %s mcp", cmd, args, s.program())
		}
	})
	t.Run("no_settings", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		for _, p := range []string{s.codexConfigFile(), s.codexHooksFile()} {
			if testkit.FileExists(p) {
				t.Fatalf("stand: %s exists", p)
			}
		}
		wantCode(t, s.gentry("setup", "codex"), 0)
		for _, p := range []string{s.codexConfigFile(), s.codexHooksFile()} {
			if !testkit.FileExists(p) {
				t.Errorf("setup codex did not create %s", p)
			}
		}
	})
}

// TestSetupCodexApproval checks that the tools of Gentry are allowed in the
// plugin itself and the setting of the operator is kept.
func TestSetupCodexApproval(t *testing.T) {
	parallel(t)
	t.Run("in_plugin", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		if v := s.coreServer()["default_tools_approval_mode"]; v != "approve" {
			t.Errorf("default_tools_approval_mode of the server core: %v, want approve", v)
		}
		if v, ok := toml.At(s.codexConfig(), "plugins", testkit.CodexPluginID, "mcp_servers"); ok {
			t.Errorf("the settings of Codex have plugins.%q.mcp_servers: %v", testkit.CodexPluginID, v)
		}
	})
	t.Run("operator_setting_kept", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.writeCodexConfig("[plugins.\"gentry@gentry\".mcp_servers.core]\ndefault_tools_approval_mode = \"prompt\"\n")
		wantCode(t, s.gentry("setup", "codex"), 0)
		if v, _ := toml.At(s.codexConfig(), "plugins", testkit.CodexPluginID, "mcp_servers", "core", "default_tools_approval_mode"); v != "prompt" {
			t.Errorf("default_tools_approval_mode of the operator: %v, want prompt\n%s", v, fileState(t, s.codexConfigFile()))
		}
	})
}

// TestSetupCodexToolTimeout checks the time limit of a call of a tool of
// Gentry the plugin sets: a day.
func TestSetupCodexToolTimeout(t *testing.T) {
	parallel(t)
	t.Run("in_plugin", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		if v, ok := s.coreServer()["tool_timeout_sec"].(float64); !ok || v != 86400 {
			t.Errorf("tool_timeout_sec of the server core: %v, want 86400", s.coreServer()["tool_timeout_sec"])
		}
	})
}

// operatorHooks are the settings of the hooks of Codex with a hook of the
// operator.
const operatorHooks = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo operator", "timeout": 30}]}
    ]
  }
}
`

// TestSetupCodexHook checks the handler of the session start hook setup
// codex writes in the settings of the hooks of Codex.
func TestSetupCodexHook(t *testing.T) {
	parallel(t)
	t.Run("beside_operator_hooks", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		writeFile(t, s.codexHooksFile(), operatorHooks)
		before := testkit.ReadCodexHooks(t, s.codexHome)["hooks"].(map[string]any)["PreToolUse"]
		wantCode(t, s.gentry("setup", "codex"), 0)
		hs := s.gentryHandlers()
		if len(hs) != 1 || hs[0].Event != "SessionStart" {
			t.Fatalf("the handlers of Gentry: %+v\n%s\nwant one of SessionStart", hs, fileState(t, s.codexHooksFile()))
		}
		wantHookCommand(t, hs[0], s.program())
		after, _ := testkit.ReadCodexHooks(t, s.codexHome)["hooks"].(map[string]any)
		if !testkit.JSONEqual(after["PreToolUse"], before) {
			t.Errorf("the hook of the operator changed: %v, was %v", after["PreToolUse"], before)
		}
	})
	t.Run("repeat_no_duplicate", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		wantCode(t, s.gentry("setup", "codex"), 0)
		if hs := s.gentryHandlers(); len(hs) != 1 {
			t.Errorf("the handlers of Gentry after a repeat: %+v, want one", hs)
		}
	})
	t.Run("one_after_switch", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		other := s.otherData(latest)
		s.gentryOf(other, "setup", "codex")
		if hs := s.gentryHandlers(); len(hs) != 1 {
			t.Fatalf("stand: the handlers of Gentry of the other data root: %+v", hs)
		}
		if err := os.RemoveAll(filepath.Join(other, "integrations", "codex")); err != nil {
			t.Fatal(err)
		}
		wantCode(t, s.gentry("setup", "codex"), 0)
		hs := s.gentryHandlers()
		if len(hs) != 1 {
			t.Fatalf("the handlers of Gentry: %+v, want one", hs)
		}
		wantHookCommand(t, hs[0], s.program())
	})
	t.Run("path_with_space", func(t *testing.T) {
		parallel(t)
		s := newStand(t)
		s.dataIn("gentry data")
		s.installed(latest)
		s.useCodex()
		wantCode(t, s.gentry("setup", "codex"), 0)
		hs := s.gentryHandlers()
		if len(hs) != 1 {
			t.Fatalf("the handlers of Gentry: %+v, want one", hs)
		}
		cmd := hs[0].Command()
		quoted, _, _ := strings.Cut(strings.TrimPrefix(cmd, `"`), `"`)
		if !strings.HasPrefix(cmd, `"`) || !samePath(quoted, s.program()) || !strings.HasPrefix(strings.TrimPrefix(cmd, `"`+quoted+`"`), " ") {
			t.Errorf("the command of the hook %q does not start with the path of the program %s in double quotes", cmd, s.program())
		}
	})
}

// TestCodexHookContextLimit checks that the introduction of the hook of
// Gentry reaches the agent of Codex whole (GEN-70, 4): its handler has
// additionalContextLimit = 0.
func TestCodexHookContextLimit(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.connectCodex()
	hs := s.gentryHandlers()
	if len(hs) != 1 {
		t.Fatalf("the handlers of Gentry: %+v, want one", hs)
	}
	if v, ok := hs[0].Handler["additionalContextLimit"].(float64); !ok || v != 0 {
		t.Errorf("additionalContextLimit of the handler of Gentry: %v, want 0", hs[0].Handler["additionalContextLimit"])
	}
}

// TestSetupCodexHooksFeature checks that the hooks the operator turned off
// are turned on.
func TestSetupCodexHooksFeature(t *testing.T) {
	parallel(t)
	t.Run("off_by_operator", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.writeCodexConfig("[features]\nhooks = false\n")
		out, res, _ := s.setupCodexJSON()
		if v, _ := toml.At(s.codexConfig(), "features", "hooks"); v != true {
			t.Errorf("features.hooks: %v, want true", v)
		}
		if got := s.hookTrust(); got != "trusted" {
			t.Errorf("the trust of the hook of Gentry in Codex: %q, want trusted", got)
		}
		if res.HooksFeature != "enabled" || res.HookTrust != "trusted" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature enabled, hook_trust trusted", out)
		}
	})
	t.Run("off_by_legacy_key", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.writeCodexConfig("[features]\ncodex_hooks = false\n")
		out, res, _ := s.setupCodexJSON()
		cfg := s.codexConfig()
		if v, _ := toml.At(cfg, "features", "hooks"); v != true {
			t.Errorf("features.hooks: %v, want true", v)
		}
		if v, ok := toml.At(cfg, "features", "codex_hooks"); ok {
			t.Errorf("features.codex_hooks is still there: %v", v)
		}
		if res.HooksFeature != "enabled" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature enabled", out)
		}
	})
	t.Run("on_by_default", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		out, res, _ := s.setupCodexJSON()
		if v, ok := toml.At(s.codexConfig(), "features", "hooks"); ok {
			t.Errorf("features.hooks is written: %v", v)
		}
		if res.HooksFeature != "on" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature on", out)
		}
	})
}

// TestSetupCodexHookTrust checks that Gentry trusts its hook by the
// app-server of Codex.
func TestSetupCodexHookTrust(t *testing.T) {
	parallel(t)
	t.Run("trusted", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		out, res, _ := s.setupCodexJSON()
		if got := s.hookTrust(); got != "trusted" {
			t.Errorf("the trust of the hook of Gentry in Codex: %q, want trusted", got)
		}
		if res.HookTrust != "trusted" {
			t.Errorf("setup codex --json: %s\nwant hook_trust trusted", out)
		}
	})
	t.Run("present", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		wantNoLineWith(t, r.stdout, "Хук")
		if out, res, _ := s.setupCodexJSON(); res.HookTrust != "present" {
			t.Errorf("setup codex --json: %s\nwant hook_trust present", out)
		}
	})
	t.Run("untrusted", func(t *testing.T) {
		parallel(t)
		failing := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.useCodex()
			s.set(testkit.FakeCodexFailEnv, "app-server")
			return s
		}
		// The block of the warning, its reason and its hint are stated by
		// TestSetupCodexWarnings; here the exit code and the JSON.
		wantCode(t, failing(t).gentry("setup", "codex"), 0)
		out, res, _ := failing(t).setupCodexJSON()
		if res.HookTrust != "untrusted" || res.HookError == nil || !strings.Contains(*res.HookError, "simulated failure of codex app-server") || res.HooksFeature != "on" {
			t.Errorf("setup codex --json: %s\nwant hook_trust untrusted, hook_error with the output of Codex, hooks_feature on", out)
		}
	})
}

// TestSetupCodexManagedHooks checks that the hooks the administrator turned
// off are not turned on or trusted.
func TestSetupCodexManagedHooks(t *testing.T) {
	parallel(t)
	t.Run("managed_hooks_only", func(t *testing.T) {
		parallel(t)
		managed := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.useCodex()
			s.requirements(`{"allowManagedHooksOnly": true}`)
			return s
		}
		s := managed(t)
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		if hs := s.gentryHandlers(); len(hs) != 1 {
			t.Errorf("the handlers of Gentry: %+v, want one", hs)
		}
		// The text of the case, which no case of the layout of the output
		// has; the warning of the hook not trusted is absent by hook_trust.
		// Two blocks: the result, then the warning of the administrator; no
		// warning of the hook not trusted and no block of hints.
		if bs := blocksOf(r.stdout); len(bs) != 2 || len(bs[1]) != 1 || bs[1][0] != "Хуки выключены администратором Codex: агент не получит вводных сведений в начале сессии." {
			t.Errorf("stdout:\n%s\nwant two blocks: the result and «Хуки выключены администратором Codex: агент не получит вводных сведений в начале сессии.»", r.stdout)
		}
		if v, ok := toml.At(s.codexConfig(), "hooks", "state"); ok {
			t.Errorf("setup codex trusted the hook: hooks.state %v", v)
		}
		if out, res, _ := managed(t).setupCodexJSON(); res.HooksFeature != "managed_off" || res.HookTrust != "skipped" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature managed_off, hook_trust skipped", out)
		}
	})
	t.Run("feature_requirement", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.requirements(`{"featureRequirements": {"hooks": false}}`)
		s.writeCodexConfig("[features]\nhooks = false\n")
		out, res, _ := s.setupCodexJSON()
		if v, ok := toml.At(s.codexConfig(), "features", "hooks"); !ok || v != false {
			t.Errorf("features.hooks: %v, want false", v)
		}
		if res.HooksFeature != "managed_off" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature managed_off", out)
		}
	})
}

// TestSetupCodexSandbox checks that the data root is opened for writing in
// the sandbox of Codex.
func TestSetupCodexSandbox(t *testing.T) {
	parallel(t)
	t.Run("added", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		own := filepath.Join(s.root, "operator-dir")
		s.editCodexConfig(func(cfg map[string]any) {
			toml.Set(cfg, []any{own}, "sandbox_workspace_write", "writable_roots")
		})
		out, res, _ := s.setupCodexJSON()
		roots := s.writableRoots()
		if countPath(roots, own) != 1 || countPath(roots, s.data) != 1 {
			t.Errorf("writable_roots: %q, want %s and %s", roots, own, s.data)
		}
		if res.Sandbox != "added" {
			t.Errorf("setup codex --json: %s\nwant sandbox added", out)
		}
	})
	t.Run("present", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		out, res, _ := s.setupCodexJSON()
		if roots := s.writableRoots(); countPath(roots, s.data) != 1 {
			t.Errorf("writable_roots: %q, want %s once", roots, s.data)
		}
		if res.Sandbox != "present" {
			t.Errorf("setup codex --json: %s\nwant sandbox present", out)
		}
	})
	t.Run("profile", func(t *testing.T) {
		parallel(t)
		profile := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.useCodex()
			s.writeCodexConfig("default_permissions = \"strict\"\n")
			return s
		}
		s := profile(t)
		wantCode(t, s.gentry("setup", "codex"), 0)
		if v, ok := toml.At(s.codexConfig(), "sandbox_workspace_write", "writable_roots"); ok {
			t.Errorf("writable_roots changed: %v", v)
		}
		// The line of the warning is stated by TestSetupCodexWarnings.
		if out, res, _ := profile(t).setupCodexJSON(); res.Sandbox != "profile" {
			t.Errorf("setup codex --json: %s\nwant sandbox profile", out)
		}
	})
}

// TestSetupCodexTrust checks that the worktrees of the projects are trusted
// in Codex by the record of the root of their main repository.
func TestSetupCodexTrust(t *testing.T) {
	parallel(t)
	t.Run("one_repository", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop", "b")
		b := s.worktree("shop", "b")
		s.useCodex()
		out, res, _ := s.setupCodexJSON()
		projects := testkit.CodexProjects(s.codexConfig())
		if _, level, ok := testkit.CodexTrust(s.codexConfig(), a); len(projects) != 1 || !ok || level != "trusted" {
			t.Errorf("the records of trust of Codex: %v, want one: %s trusted", projects, a)
		}
		wantSamePaths(t, "worktrees_trusted of "+out, res.WorktreesTrusted, a, b)
	})
	t.Run("separate_clone", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop")
		c := s.clone("shop", "c")
		s.mustIn(a, "worktree", "add", c)
		s.useCodex()
		out, res, _ := s.setupCodexJSON()
		cfg := s.codexConfig()
		_, la, okA := testkit.CodexTrust(cfg, a)
		_, lc, okC := testkit.CodexTrust(cfg, c)
		if projects := testkit.CodexProjects(cfg); len(projects) != 2 || !okA || !okC || la != "trusted" || lc != "trusted" {
			t.Errorf("the records of trust of Codex: %v, want %s and %s trusted", projects, a, c)
		}
		wantSamePaths(t, "worktrees_trusted of "+out, res.WorktreesTrusted, a, c)
	})
	t.Run("other_case", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("the case of a path matters only on Windows")
		}
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop", "b")
		s.useCodex()
		other := strings.ToUpper(a)
		if other == a {
			other = strings.ToLower(a)
		}
		s.editCodexConfig(func(cfg map[string]any) {
			toml.Set(cfg, map[string]any{"trust_level": "trusted"}, "projects", other)
		})
		out, res, fields := s.setupCodexJSON()
		if projects := testkit.CodexProjects(s.codexConfig()); len(projects) != 1 {
			t.Errorf("the records of trust of Codex: %v, want only %s", projects, other)
		}
		if string(fields["worktrees_trusted"]) != "[]" || len(res.WorktreesTrusted) != 0 {
			t.Errorf("setup codex --json: %s\nwant worktrees_trusted []", out)
		}
	})
	t.Run("missing_folder", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop")
		// The copy gone is a clone of its own: its record of trust would be
		// its own, not that of A.
		gone := s.clone("shop", "gone")
		s.mustIn(a, "worktree", "add", gone)
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
		s.useCodex()
		out, res, _ := s.setupCodexJSON()
		if _, _, ok := testkit.CodexTrust(s.codexConfig(), gone); ok {
			t.Errorf("the settings of Codex have a record of trust of %s", gone)
		}
		if slices.ContainsFunc(res.WorktreesTrusted, func(p string) bool { return samePath(p, gone) }) {
			t.Errorf("setup codex --json: %s\nwant no %s in worktrees_trusted", out, gone)
		}
	})
	t.Run("no_projects", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		out, _, fields := s.setupCodexJSON()
		if string(fields["worktrees_trusted"]) != "[]" || string(fields["worktrees_distrusted"]) != "[]" {
			t.Errorf("setup codex --json: %s\nwant worktrees_trusted [] and worktrees_distrusted []", out)
		}
	})
}

// TestSetupCodexDistrusted checks that the worktrees of a record the operator
// set untrusted are left as they are and named.
func TestSetupCodexDistrusted(t *testing.T) {
	parallel(t)
	distrusted := func(t *testing.T) (*stand, string, string) {
		s := installedStand(t, latest, false)
		a := s.project("shop", "b")
		s.useCodex()
		s.editCodexConfig(func(cfg map[string]any) {
			toml.Set(cfg, map[string]any{"trust_level": "untrusted"}, "projects", a)
		})
		return s, a, s.worktree("shop", "b")
	}
	s, a, b := distrusted(t)
	r := s.gentry("setup", "codex")
	wantCode(t, r, 0)
	cfg := s.codexConfig()
	if projects := testkit.CodexProjects(cfg); len(projects) != 1 || projects[a] != "untrusted" {
		t.Errorf("the records of trust of Codex: %v, want only %s untrusted", projects, a)
	}
	// The words of the block and of the hint are stated by
	// TestSetupCodexWarnings: here the block of A and B, in any order, and one
	// hint, of the record of A.
	wantPathsBlock(t, r.stdout, a, b)
	n := 0
	for _, l := range lastBlock(r.stdout) {
		if strings.Contains(l, `projects."`) {
			n++
		}
	}
	if trustHints(r.stdout, a) != 1 || n != 1 {
		t.Errorf("the last block of the output:\n%s\nwant one hint to trust the worktrees, of the record %s", r.stdout, a)
	}
	s2, a2, b2 := distrusted(t)
	out, res, fields := s2.setupCodexJSON()
	wantSamePaths(t, "worktrees_distrusted of "+out, res.WorktreesDistrusted, a2, b2)
	if string(fields["worktrees_trusted"]) != "[]" {
		t.Errorf("setup codex --json: %s\nwant worktrees_trusted []", out)
	}
}

// brokenCodexConfig is a text that is no TOML.
const brokenCodexConfig = "[features\nhooks = true\n"

// TestSetupCodexSettingsInvalid checks that setup codex refuses settings of
// Codex it cannot read or write before it looks for the program of Codex,
// and changes nothing.
func TestSetupCodexSettingsInvalid(t *testing.T) {
	parallel(t)
	t.Run("unreadable", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.writeCodexConfig(brokenCodexConfig)
		config, hooks := fileState(t, s.codexConfigFile()), fileState(t, s.codexHooksFile())
		r := s.gentry("setup", "codex")
		wantCode(t, r, 1)
		// The words of the hint, the same for worktree add, are stated by
		// TestWorktreeAddCodexText/settings_warning of the area pool.
		if ls := lines(r.stderr); len(ls) != 4 || ls[0] != "Настройки Codex не прочитаны." || ls[1] != "Настройки: "+s.codexConfigFile() || ls[2] != "" || !strings.HasSuffix(ls[3], ": gentry setup codex") {
			t.Errorf("stderr:\n%s\nwant «Настройки Codex не прочитаны.», «Настройки: %s», an empty line and the hint to repeat gentry setup codex", r.stderr, s.codexConfigFile())
		}
		code, details := s.setupError("codex")
		if code != "settings_invalid" || details["tool"] != "codex" || details["reason"] != "unreadable" || !samePath(str(details["path"]), s.codexConfigFile()) {
			t.Errorf("setup codex --json: the error %s with %v, want settings_invalid with tool codex, reason unreadable, path %s", code, details, s.codexConfigFile())
		}
		if calls := s.codexCalls(0); len(calls) > 0 {
			t.Errorf("setup codex ran the fake Codex: %q", calls)
		}
		if testkit.FileExists(s.codexIntegration()) {
			t.Errorf("setup codex made %s", s.codexIntegration())
		}
		wantFileState(t, s.codexConfigFile(), config)
		wantFileState(t, s.codexHooksFile(), hooks)
	})
	t.Run("unwritable", func(t *testing.T) {
		skipAsRoot(t)
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		s.writeCodexConfig("model = \"gpt-6-astra\"\n")
		readOnly(t, s.codexConfigFile())
		config, hooks := fileState(t, s.codexConfigFile()), fileState(t, s.codexHooksFile())
		r := s.gentry("setup", "codex")
		wantCode(t, r, 1)
		if ls := lines(r.stderr); len(ls) == 0 || ls[0] != "Настройки Codex не записаны." {
			t.Errorf("stderr:\n%s\nwant the first line «Настройки Codex не записаны.»", r.stderr)
		}
		if code, details := s.setupError("codex"); code != "settings_invalid" || details["reason"] != "unwritable" {
			t.Errorf("setup codex --json: the error %s with %v, want settings_invalid with reason unwritable", code, details)
		}
		if calls := s.codexCalls(0); len(calls) > 0 {
			t.Errorf("setup codex ran the fake Codex: %q", calls)
		}
		if testkit.FileExists(s.codexIntegration()) {
			t.Errorf("setup codex made %s", s.codexIntegration())
		}
		wantFileState(t, s.codexConfigFile(), config)
		wantFileState(t, s.codexHooksFile(), hooks)
	})
	t.Run("before_program", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.noCodex()
		writeFile(t, s.codexConfigFile(), brokenCodexConfig)
		wantCode(t, s.gentry("setup", "codex"), 1)
		if code, _ := s.setupError("codex"); code != "settings_invalid" {
			t.Errorf("setup codex --json: the error %s, want settings_invalid", code)
		}
	})
}

// readOnly makes the file read-only until the test ends.
func readOnly(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
}

// str returns v as a string; "" if it is none.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// brokenHooks is a text that is no JSON.
const brokenHooks = "{ это не JSON\n"

// TestSetupCodexHooksFile checks that settings of the hooks of Codex setup
// codex cannot read or write are left as they are and do not stop it.
func TestSetupCodexHooksFile(t *testing.T) {
	parallel(t)
	// The texts of the settings of the hooks have no case of the layout of
	// the output: the cases of this test state them.
	t.Run("unreadable", func(t *testing.T) {
		parallel(t)
		unread := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.useCodex()
			writeFile(t, s.codexHooksFile(), brokenHooks)
			return s
		}
		s := unread(t)
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		wantFileState(t, s.codexHooksFile(), brokenHooks)
		if p, ok := s.codexPlugin(); !ok || !p.Installed {
			t.Errorf("the plugin of Gentry is not installed in Codex: %+v", p)
		}
		wantBlock(t, r.stdout,
			"Хук запуска сессии не записан: настройки хуков Codex не прочитаны.",
			"Настройки хуков: "+s.codexHooksFile())
		wantInLastBlock(t, r.stdout, "Исправить настройки хуков Codex и повторить: gentry setup codex")
		if out, res, _ := unread(t).setupCodexJSON(); res.HookTrust != "failed" {
			t.Errorf("setup codex --json: %s\nwant hook_trust failed", out)
		}
	})
	t.Run("unwritable", func(t *testing.T) {
		skipAsRoot(t)
		parallel(t)
		const hooks = "{\"hooks\": {}}\n"
		unwritable := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.useCodex()
			writeFile(t, s.codexHooksFile(), hooks)
			readOnly(t, s.codexHooksFile())
			return s
		}
		s := unwritable(t)
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		wantFileState(t, s.codexHooksFile(), hooks)
		wantLine(t, r.stdout, "Хук запуска сессии не записан: настройки хуков Codex не записаны.")
		if out, res, _ := unwritable(t).setupCodexJSON(); res.HookTrust != "unwritten" {
			t.Errorf("setup codex --json: %s\nwant hook_trust unwritten", out)
		}
	})
	t.Run("managed_unreadable", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		writeFile(t, s.codexHooksFile(), brokenHooks)
		s.writeCodexConfig("[features]\nhooks = false\n")
		s.requirements(`{"allowManagedHooksOnly": true}`)
		out, res, _ := s.setupCodexJSON()
		if res.HookTrust != "failed" || res.HooksFeature != "managed_off" {
			t.Errorf("setup codex --json: %s\nwant hook_trust failed, hooks_feature managed_off", out)
		}
		if v, ok := toml.At(s.codexConfig(), "features", "hooks"); !ok || v != false {
			t.Errorf("features.hooks: %v, want false", v)
		}
	})
	t.Run("operator_off_unreadable", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.useCodex()
		writeFile(t, s.codexHooksFile(), brokenHooks)
		s.writeCodexConfig("[features]\nhooks = false\n")
		out, res, _ := s.setupCodexJSON()
		if v, _ := toml.At(s.codexConfig(), "features", "hooks"); v != true {
			t.Errorf("features.hooks: %v, want true", v)
		}
		if res.HookTrust != "failed" || res.HooksFeature != "enabled" {
			t.Errorf("setup codex --json: %s\nwant hook_trust failed, hooks_feature enabled", out)
		}
	})
}

// TestSetupCodexText checks that the text of setup codex starts with the
// result and does not list what it set in Codex.
func TestSetupCodexText(t *testing.T) {
	parallel(t)
	t.Run("first_connection", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop", "b")
		s.useCodex()
		s.writeCodexConfig("[features]\nhooks = false\n")
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		want := testkit.Lines(
			"Gentry подключён к Codex.",
			"Плагин начнёт работать со следующей сессии.")
		if r.stdout != want {
			t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
		}
		cfg := s.codexConfig()
		if v, _ := toml.At(cfg, "features", "hooks"); v != true {
			t.Errorf("features.hooks: %v, want true", v)
		}
		if got := s.hookTrust(); got != "trusted" {
			t.Errorf("the trust of the hook of Gentry in Codex: %q, want trusted", got)
		}
		if roots := s.writableRoots(); countPath(roots, s.data) != 1 {
			t.Errorf("writable_roots: %q, want %s", roots, s.data)
		}
		if _, level, ok := testkit.CodexTrust(cfg, a); !ok || level != "trusted" {
			t.Errorf("the record of trust of %s: %q, want trusted", a, level)
		}
	})
	t.Run("updated", func(t *testing.T) {
		parallel(t)
		stale := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.connectCodex()
			s.staleCodexPlugin()
			return s
		}
		s := stale(t)
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		if want := testkit.Lines("Плагин Gentry в Codex обновлён.", "Изменения вступят в силу со следующей сессии."); r.stdout != want {
			t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
		}
		if out, res, _ := stale(t).setupCodexJSON(); res.Action != "updated" {
			t.Errorf("setup codex --json: %s\nwant action updated", out)
		}
	})
	t.Run("unchanged", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		if want := testkit.Lines("Gentry уже подключён к Codex."); r.stdout != want {
			t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
		}
		if out, res, _ := s.setupCodexJSON(); res.Action != "unchanged" {
			t.Errorf("setup codex --json: %s\nwant action unchanged", out)
		}
	})
	t.Run("settings_changed", func(t *testing.T) {
		parallel(t)
		hooksOff := func(t *testing.T) *stand {
			s := installedStand(t, latest, false)
			s.connectCodex()
			s.editCodexConfig(func(cfg map[string]any) { toml.Set(cfg, false, "features", "hooks") })
			return s
		}
		s := hooksOff(t)
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		// The words of the line of the plugin unchanged are stated by the
		// case unchanged: here the output is that one line, as the JSON
		// tells the action unchanged.
		if ls := lines(r.stdout); len(ls) != 1 || ls[0] == "" {
			t.Errorf("stdout:\n%s\nwant one line, that of the plugin unchanged", r.stdout)
		}
		if v, _ := toml.At(s.codexConfig(), "features", "hooks"); v != true {
			t.Errorf("features.hooks: %v, want true", v)
		}
		if out, res, _ := hooksOff(t).setupCodexJSON(); res.HooksFeature != "enabled" || res.Action != "unchanged" {
			t.Errorf("setup codex --json: %s\nwant hooks_feature enabled, action unchanged", out)
		}
	})
}

// TestSetupCodexWarnings checks the blocks of the warnings of setup codex and
// the order of the warnings and of the hints.
func TestSetupCodexWarnings(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.connectCodex()
	a := s.project("shop", "b")
	b := s.worktree("shop", "b")
	key := a
	s.editCodexConfig(func(cfg map[string]any) {
		for k := range testkit.CodexProjects(cfg) {
			toml.Delete(cfg, "projects", k)
		}
		toml.Set(cfg, map[string]any{"trust_level": "untrusted"}, "projects", key)
		toml.Set(cfg, false, "plugins", testkit.CodexPluginID, "enabled")
		toml.Set(cfg, "strict", "default_permissions")
		toml.Delete(cfg, "hooks", "state")
	})
	s.set(testkit.FakeCodexFailEnv, "app-server")
	r := s.gentry("setup", "codex")
	wantCode(t, r, 0)
	// The line of the result, the plugin unchanged, is stated by
	// TestSetupCodexText/unchanged; here it is the first block, of one line.
	// The paths of the block of the worktrees not trusted come in any order.
	const reason = "Причина: "
	want := [][]string{
		nil,
		{"Плагин Gentry отключён в Codex."},
		{"Хук запуска сессии записан, но не доверен в Codex.", reason},
		{"Песочница Codex задана профилем разрешений: папка данных Gentry в ней не открыта."},
		{"Рабочие копии, которым оператор не доверил в Codex, оставлены как есть:", "  " + a, "  " + b},
		{
			`Включить плагин в настройках Codex: plugins."gentry@gentry".enabled = true`,
			"Доверить хук в Codex: /hooks",
			`Доверить рабочие копии в настройках Codex: projects."` + testkit.CodexTrustKey(key) + `".trust_level = "trusted"`,
		},
	}
	got := blocksOf(r.stdout)
	same := func(i int) bool {
		g, w := got[i], want[i]
		switch {
		case i == 0:
			return len(g) == 1
		case len(g) != len(w):
			return false
		case i == 4:
			return g[0] == w[0] && (slices.Equal(g[1:], w[1:]) || slices.Equal(g[1:], []string{w[2], w[1]}))
		}
		for j := range w {
			if w[j] == reason {
				// The reason is the output of Codex: that of the fake.
				if !strings.HasPrefix(g[j], reason) || !strings.Contains(g[j], "simulated failure of codex app-server") {
					return false
				}
			} else if g[j] != w[j] {
				return false
			}
		}
		return true
	}
	ok := len(got) == len(want)
	for i := 0; ok && i < len(want); i++ {
		ok = same(i)
	}
	if !ok || strings.Contains(r.stdout, "\n\n\n") {
		t.Errorf("stdout:\n%s\nwant the blocks, apart by an empty line:\n%q", r.stdout, want)
	}
}

// TestSetupCodexJSON checks the JSON of the first connection to Codex.
func TestSetupCodexJSON(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.useCodex()
	out, res, fields := s.setupCodexJSON()
	enabled := res.Enabled != nil && *res.Enabled
	if res.Tool != "codex" || res.Action != "installed" || !enabled || !samePath(res.Dir, s.codexIntegration()) ||
		res.HookTrust != "trusted" || res.HooksFeature != "on" || res.Sandbox != "added" ||
		string(fields["worktrees_trusted"]) != "[]" || string(fields["worktrees_distrusted"]) != "[]" ||
		!samePath(res.Settings, s.codexConfigFile()) || !samePath(res.HooksFile, s.codexHooksFile()) {
		t.Errorf("setup codex --json:\n%s\nwant tool codex, action installed, enabled true, dir %s, hook_trust trusted, hooks_feature on, sandbox added, worktrees_trusted [], worktrees_distrusted [], settings %s, hooks_file %s",
			out, s.codexIntegration(), s.codexConfigFile(), s.codexHooksFile())
	}
	for _, f := range []string{"permission", "hook_error"} {
		if _, ok := fields[f]; ok {
			t.Errorf("setup codex --json has the field %s: %s", f, out)
		}
	}
}

// TestSetupCodexTrustLists checks that the lists of trust of the JSON name
// the worktrees of this command only.
func TestSetupCodexTrustLists(t *testing.T) {
	parallel(t)
	t.Run("already_trusted", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		a := s.project("shop", "b")
		s.useCodex()
		s.editCodexConfig(func(cfg map[string]any) {
			toml.Set(cfg, map[string]any{"trust_level": "trusted"}, "projects", a)
		})
		out, _, fields := s.setupCodexJSON()
		if string(fields["worktrees_trusted"]) != "[]" || string(fields["worktrees_distrusted"]) != "[]" {
			t.Errorf("setup codex --json: %s\nwant worktrees_trusted [] and worktrees_distrusted []", out)
		}
	})
}

// TestSetupCodexUpdatesPlugin checks that a plugin of Gentry of another
// version is installed anew in the version of the program.
func TestSetupCodexUpdatesPlugin(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.connectCodex()
	s.staleCodexPlugin()
	out, res, _ := s.setupCodexJSON()
	if p, ok := s.codexPlugin(); !ok || p.Version != s.codexManifestVersion() {
		t.Errorf("the plugin of Gentry in Codex: %+v, want the version %s of the plugin of the program", p, s.codexManifestVersion())
	}
	if res.Action != "updated" {
		t.Errorf("setup codex --json: %s\nwant action updated", out)
	}
}

// codexManifestVersion returns the version of the plugin of Gentry the
// program built in the marketplace of the data root,
// integrations/codex: that of its .codex-plugin/plugin.json.
func (s *stand) codexManifestVersion() string {
	s.t.Helper()
	return manifestVersion(s.t, filepath.Join(s.codexPluginDir(), ".codex-plugin", "plugin.json"))
}

// manifestVersion returns the field version of the manifest of a plugin.
func manifestVersion(t *testing.T, p string) string {
	t.Helper()
	var m struct {
		Version string `json:"version"`
	}
	b := readFile(t, p)
	if err := json.Unmarshal([]byte(b), &m); err != nil || m.Version == "" {
		t.Fatalf("%s has no version: %v\n%s", p, err, b)
	}
	return m.Version
}

// TestSetupClaudeJSON checks that setup claude --json has none of the fields
// of Codex.
func TestSetupClaudeJSON(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	r := s.gentry("setup", "claude", "--json")
	wantCode(t, r, 0)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.stdout), &fields); err != nil {
		t.Fatalf("setup claude --json: %v\n%s", err, r.stdout)
	}
	for _, f := range []string{"hook_trust", "hooks_feature", "sandbox", "worktrees_trusted", "worktrees_distrusted", "hooks_file", "hook_error"} {
		if _, ok := fields[f]; ok {
			t.Errorf("setup claude --json has the field %s: %s", f, r.stdout)
		}
	}
}

// TestSetupCodexDisabled checks that a plugin the operator disabled stays
// disabled, with the hint to enable it.
func TestSetupCodexDisabled(t *testing.T) {
	parallel(t)
	disabled := func(t *testing.T) *stand {
		s := installedStand(t, latest, false)
		s.connectCodex()
		s.staleCodexPlugin()
		s.disableCodexPlugin()
		return s
	}
	s := disabled(t)
	r := s.gentry("setup", "codex")
	wantCode(t, r, 0)
	if enabled, ok := s.codexPluginEnabled(); !ok || enabled {
		t.Errorf("plugins.%q.enabled after setup codex: %v (%v), want false", testkit.CodexPluginID, enabled, ok)
	}
	// The block of the plugin disabled and its hint are stated by
	// TestSetupCodexWarnings; here the JSON of the same state, on a stand of
	// its own: the plugin of another version installed anew stays disabled.
	if out, res, _ := disabled(t).setupCodexJSON(); res.Enabled == nil || *res.Enabled || res.Action != "updated" {
		t.Errorf("setup codex --json: %s\nwant enabled false, action updated", out)
	}
}

// TestSetupCodexNotFound checks that without the program of Codex setup
// codex refuses and writes nothing.
func TestSetupCodexNotFound(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.noCodex()
	s.writeCodexConfig("model = \"gpt-6-astra\"\n")
	config := fileState(t, s.codexConfigFile())
	r := s.gentry("setup", "codex")
	wantCode(t, r, 1)
	wantStderr(t, r, "Программа codex не найдена.", "", "Установите Codex и повторите: gentry setup codex")
	code, details := s.setupError("codex")
	if code != contract.CodeToolNotFound || details["tool"] != "codex" || details["program"] != "codex" {
		t.Errorf("setup codex --json: the error %s with %v, want tool_not_found with tool codex, program codex", code, details)
	}
	if testkit.FileExists(s.codexIntegration()) {
		t.Errorf("setup codex made %s", s.codexIntegration())
	}
	wantFileState(t, s.codexConfigFile(), config)
}

// TestSetupCodexToolFailed checks that a command of Codex that fails refuses
// setup codex with the command and its output.
func TestSetupCodexToolFailed(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.useCodex()
	s.set(testkit.FakeCodexFailEnv, "plugin add")
	r := s.gentry("setup", "codex")
	wantCode(t, r, 1)
	failed := regexp.MustCompile(`^Не удалось подключить плагин, команда «(.+)» завершилась ошибкой: (.+)$`)
	m := failed.FindStringSubmatch(strings.SplitN(r.stderr, "\n", 2)[0])
	if m == nil || !strings.Contains(m[1], "plugin add") || !strings.Contains(m[2], "simulated failure") {
		t.Errorf("stderr:\n%s\nwant «Не удалось подключить плагин, команда «<команда plugin add>» завершилась ошибкой: <вывод Codex>»", r.stderr)
	}
	code, details := s.setupError("codex")
	if code != contract.CodeToolFailed || details["tool"] != "codex" || str(details["command"]) == "" || str(details["output"]) == "" {
		t.Errorf("setup codex --json: the error %s with %v, want tool_failed with tool codex, command and output", code, details)
	}
}

// codexElsewhere makes the marketplace gentry of the fake Codex point at
// the folder of another data root, by setup codex of its program, and
// returns that folder.
func (s *stand) codexElsewhere() string {
	s.t.Helper()
	s.useCodex()
	other := s.otherData(latest)
	s.gentryOf(other, "setup", "codex")
	dir := filepath.Join(other, "integrations", "codex")
	if root := s.codexMarketplace(); !samePath(root, dir) {
		s.t.Fatalf("stand: the marketplace gentry is %q, want %s", root, dir)
	}
	return dir
}

// TestSetupCodexElsewhere checks that the plugin of another data root is
// switched by --switch only, unless its folder is gone.
func TestSetupCodexElsewhere(t *testing.T) {
	parallel(t)
	t.Run("refused", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		previous := s.codexElsewhere()
		config, hooks := fileState(t, s.codexConfigFile()), fileState(t, s.codexHooksFile())
		plugin, _ := s.codexPlugin()
		r := s.gentry("setup", "codex")
		wantCode(t, r, 1)
		wantStderr(t, r,
			"Плагин Gentry в Codex подключён из другой папки данных.",
			"Папка плагина: "+previous,
			"Папка плагина этой команды: "+s.codexIntegration(),
			"",
			"Переключить плагин всех сессий Codex на папку данных этой команды: gentry setup codex --switch")
		if code, details := s.setupError("codex"); code != contract.CodePluginElsewhere || details["tool"] != "codex" {
			t.Errorf("setup codex --json: the error %s with %v, want plugin_elsewhere with tool codex", code, details)
		}
		if p, _ := s.codexPlugin(); p != plugin {
			t.Errorf("the plugin of Gentry in Codex changed: %+v, was %+v", p, plugin)
		}
		wantFileState(t, s.codexConfigFile(), config)
		wantFileState(t, s.codexHooksFile(), hooks)
	})
	t.Run("switch", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		previous := s.codexElsewhere()
		r := s.gentry("setup", "codex", "--switch")
		wantCode(t, r, 0)
		if root := s.codexMarketplace(); !samePath(root, s.codexIntegration()) {
			t.Errorf("the marketplace gentry is %q, want %s", root, s.codexIntegration())
		}
		// The text of the switch has no case of the layout of the output:
		// this case states it.
		want := []string{"Плагин Gentry в Codex переключён на папку данных этой команды.", "Изменения вступят в силу со следующей сессии.", "Прежняя папка плагина: " + previous}
		if ls := lines(r.stdout); len(ls) < 3 || !slices.Equal(ls[:3], want) {
			t.Errorf("stdout:\n%s\nwant the first lines:\n%s", r.stdout, testkit.Lines(want...))
		}
		s2 := installedStand(t, latest, false)
		previous2 := s2.codexElsewhere()
		out, res, _ := s2.setupCodexJSON("--switch")
		if res.Action != "switched" || res.PreviousDir == nil || !samePath(*res.PreviousDir, previous2) {
			t.Errorf("setup codex --switch --json: %s\nwant action switched, previous_dir %s", out, previous2)
		}
	})
	t.Run("previous_gone", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		previous := s.codexElsewhere()
		if err := os.RemoveAll(previous); err != nil {
			t.Fatal(err)
		}
		if out, res, _ := s.setupCodexJSON(); res.Action != "switched" {
			t.Errorf("setup codex --json: %s\nwant action switched", out)
		}
	})
}

// TestSetupAgentsHint checks GEN-59's removal of the general manual-layout
// hint after either tool is connected. Detailed layout cases are covered
// by TestSetupAgents59FirstConnection.
func TestSetupAgentsHint(t *testing.T) {
	parallel(t)
	noHint := func(t *testing.T, r result) {
		t.Helper()
		wantCode(t, r, 0)
		if strings.Contains(r.stdout, "gentry agents deploy --project") {
			t.Errorf("setup still prints the removed general layout hint:\n%s", r.stdout)
		}
	}
	t.Run("codex_first", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.project("shop")
		s.useCodex()
		noHint(t, s.gentry("setup", "codex"))
	})
	t.Run("claude_first", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.project("shop")
		noHint(t, s.gentry("setup", "claude"))
		s.useCodex()
		noHint(t, s.gentry("setup", "codex"))
	})
	t.Run("codex_repeat", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.project("shop")
		s.useCodex()
		noHint(t, s.gentry("setup", "codex"))
		r := s.gentry("setup", "codex")
		wantCode(t, r, 0)
		noHint(t, r)
	})
}
