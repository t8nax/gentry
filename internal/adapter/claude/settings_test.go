package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllowTools(t *testing.T) {
	write := func(t *testing.T, text string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(t *testing.T, p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	t.Run("no file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "claude", "settings.json")
		if got, err := AllowTools(p); err != nil || got != PermissionAdded {
			t.Fatalf("AllowTools: %q, %v", got, err)
		}
		want := "{\n  \"permissions\": {\n    \"allow\": [\n      \"mcp__plugin_gentry_core\"\n    ]\n  }\n}\n"
		if got := read(t, p); got != want {
			t.Errorf("settings:\n%s", got)
		}
	})
	t.Run("other keys kept in order", func(t *testing.T) {
		p := write(t, "\xef\xbb\xbf"+`{"model":"opus","permissions":{"deny":["Bash(rm:*)"],"allow":["Read"]},"env":{"SHOP":"1"}}`)
		if got, err := AllowTools(p); err != nil || got != PermissionAdded {
			t.Fatalf("AllowTools: %q, %v", got, err)
		}
		want := `{
  "model": "opus",
  "permissions": {
    "deny": [
      "Bash(rm:*)"
    ],
    "allow": [
      "Read",
      "mcp__plugin_gentry_core"
    ]
  },
  "env": {
    "SHOP": "1"
  }
}
`
		if got := read(t, p); got != want {
			t.Errorf("settings:\n%s", got)
		}
		if got, err := AllowTools(p); err != nil || got != PermissionPresent {
			t.Errorf("again: %q, %v", got, err)
		}
		if got := read(t, p); got != want {
			t.Errorf("settings changed by a present rule:\n%s", got)
		}
	})
	for name, text := range map[string]string{
		"not json":      `{"model":`,
		"not an object": `["Read"]`,
		"permissions":   `{"permissions":"all"}`,
		"allow":         `{"permissions":{"allow":"Read"}}`,
		"trailing":      `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, text)
			if got, err := AllowTools(p); err != nil || got != PermissionFailed {
				t.Errorf("AllowTools: %q, %v", got, err)
			}
			if got := read(t, p); got != text {
				t.Errorf("settings changed: %s", got)
			}
		})
	}
}

func TestSettingsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ConfigDirEnv, dir)
	if p, err := SettingsPath(); err != nil || p != filepath.Join(dir, "settings.json") {
		t.Errorf("SettingsPath: %q, %v", p, err)
	}
	t.Setenv(ConfigDirEnv, "")
	home, _ := os.UserHomeDir()
	if p, err := SettingsPath(); err != nil || p != filepath.Join(home, ".claude", "settings.json") {
		t.Errorf("SettingsPath without %s: %q, %v", ConfigDirEnv, p, err)
	}
}
