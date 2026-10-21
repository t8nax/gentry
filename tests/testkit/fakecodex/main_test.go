package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit/toml"
)

// capture runs the command of the fake with args and returns its exit code
// and standard output.
func capture(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code := run(args)
	w.Close()
	os.Stdout = stdout
	return code, <-done
}

// marketplace makes a marketplace gentry with the plugin gentry of the
// version in dir.
func marketplace(t *testing.T, dir, version string) {
	t.Helper()
	files := map[string]string{
		".agents/plugins/marketplace.json":    `{"name":"gentry","plugins":[{"name":"gentry","source":{"source":"local","path":"./gentry"}}]}`,
		"gentry/.codex-plugin/plugin.json":    `{"name":"gentry","version":"` + version + `","mcpServers":"./.mcp.json"}`,
		"gentry/.mcp.json":                    `{"mcpServers":{"core":{"command":"gentry","args":["mcp"]}}}`,
		"gentry/skills/editing-flow/SKILL.md": "---\nname: editing-flow\n---\n",
	}
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func config(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := toml.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestPlugin checks the commands of the plugins as Codex 0.159.3 runs them.
func TestPlugin(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("FAKECODEX_FAIL", "")
	t.Setenv("FAKECODEX_LOG", "")
	one, two := t.TempDir(), t.TempDir()
	marketplace(t, one, "1.0.0")
	marketplace(t, two, "2.0.0")

	if code, out := capture(t, "plugin", "marketplace", "add", one, "--json"); code != 0 || !strings.Contains(out, `"alreadyAdded": false`) {
		t.Fatalf("marketplace add: %d %s", code, out)
	}
	if code, out := capture(t, "plugin", "add", "gentry@gentry", "--json"); code != 0 || !strings.Contains(out, `"version": "1.0.0"`) {
		t.Fatalf("plugin add: %d %s", code, out)
	}
	if v, _ := toml.At(config(t), "plugins", "gentry@gentry", "enabled"); v != true {
		t.Errorf("enabled after plugin add: %v", v)
	}
	// Another source under the name taken is refused.
	if code, _ := capture(t, "plugin", "marketplace", "add", two, "--json"); code != 1 {
		t.Errorf("marketplace add of another source: exit code %d, want 1", code)
	}
	// A plugin disabled is enabled by plugin add.
	cfg := config(t)
	toml.Set(cfg, false, "plugins", "gentry@gentry", "enabled")
	os.WriteFile(configPath(), []byte(toml.Marshal(cfg)), 0o644)
	_, out := capture(t, "plugin", "list", "--json")
	var list struct {
		Installed []struct {
			PluginID string `json:"pluginId"`
			Version  string `json:"version"`
			Enabled  bool   `json:"enabled"`
		} `json:"installed"`
	}
	json.Unmarshal([]byte(out), &list)
	if len(list.Installed) != 1 || list.Installed[0].Enabled || list.Installed[0].Version != "1.0.0" {
		t.Errorf("plugin list: %s", out)
	}
	capture(t, "plugin", "add", "gentry@gentry", "--json")
	if v, _ := toml.At(config(t), "plugins", "gentry@gentry", "enabled"); v != true {
		t.Errorf("enabled after plugin add of a disabled plugin: %v", v)
	}
	// The removal of the marketplace leaves the plugin out of the list.
	if code, _ := capture(t, "plugin", "marketplace", "remove", "gentry", "--json"); code != 0 {
		t.Fatalf("marketplace remove: exit code %d", code)
	}
	if _, out := capture(t, "plugin", "list", "--json"); !strings.Contains(out, `"installed": []`) {
		t.Errorf("plugin list after marketplace remove: %s", out)
	}
	capture(t, "plugin", "marketplace", "add", two, "--json")
	capture(t, "plugin", "add", "gentry@gentry", "--json")
	if _, out := capture(t, "plugin", "marketplace", "list", "--json"); !strings.Contains(out, strings.ReplaceAll(two, `\`, `\\`)) {
		t.Errorf("marketplace list after the switch: %s", out)
	}
	t.Setenv("FAKECODEX_FAIL", "plugin add")
	if code, _ := capture(t, "plugin", "add", "gentry@gentry", "--json"); code != 1 {
		t.Errorf("plugin add with FAKECODEX_FAIL: exit code %d, want 1", code)
	}
}

// TestAppServer checks the trust of a hook by hooks/list and
// config/batchWrite, and the hooks off.
func TestAppServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("FAKECODEX_REQUIREMENTS", "")
	os.WriteFile(hooksPath(), []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"gentry hook session-start --tool codex","additionalContextLimit":0}]}]}}`), 0o644)
	list := func() map[string]any {
		t.Helper()
		v, rerr := answer(request{Method: "hooks/list", Params: json.RawMessage(`{"cwds":["` + strings.ReplaceAll(home, `\`, `\\`) + `"]}`)})
		if rerr != nil {
			t.Fatal(rerr.Message)
		}
		hooks := v.(map[string]any)["data"].([]map[string]any)[0]["hooks"].([]map[string]any)
		if len(hooks) == 0 {
			return nil
		}
		return hooks[0]
	}
	h := list()
	if h == nil || h["trustStatus"] != "untrusted" || !strings.HasSuffix(h["key"].(string), ":session_start:0:0") {
		t.Fatalf("hooks/list: %v", h)
	}
	edit, _ := json.Marshal(map[string]any{"edits": []any{map[string]any{"keyPath": "hooks.state", "mergeStrategy": "upsert",
		"value": map[string]any{h["key"].(string): map[string]any{"trusted_hash": h["currentHash"]}}}}})
	if _, rerr := answer(request{Method: "config/batchWrite", Params: edit}); rerr != nil {
		t.Fatal(rerr.Message)
	}
	if h := list(); h["trustStatus"] != "trusted" {
		t.Errorf("hooks/list after config/batchWrite: %v", h)
	}
	os.WriteFile(configPath(), []byte(toml.Marshal(map[string]any{"features": map[string]any{"hooks": false}})), 0o644)
	if h := list(); h != nil {
		t.Errorf("hooks/list with the hooks off: %v", h)
	}
	req := filepath.Join(t.TempDir(), "requirements.json")
	os.WriteFile(req, []byte(`{"requirements":{"allowManagedHooksOnly":true}}`), 0o644)
	t.Setenv("FAKECODEX_REQUIREMENTS", req)
	os.Remove(configPath())
	if h := list(); h != nil {
		t.Errorf("hooks/list with the hooks of the administrator only: %v", h)
	}
	if v, _ := answer(request{Method: "configRequirements/read"}); !strings.Contains(toJSON(v), `"allowManagedHooksOnly":true`) {
		t.Errorf("configRequirements/read: %v", v)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
