package claude

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
		if got := AllowTools(p); got != PermissionAdded {
			t.Fatalf("AllowTools: %q", got)
		}
		want := "{\n  \"permissions\": {\n    \"allow\": [\n      \"mcp__plugin_gentry_core\"\n    ]\n  }\n}\n"
		if got := read(t, p); got != want {
			t.Errorf("settings:\n%s", got)
		}
	})
	// The rule goes after the last rule; the rest of the text stays as it
	// is, the escapes of other rules too.
	for name, c := range map[string]struct{ text, want string }{
		"lines": {
			"\xef\xbb\xbf{\n  \"model\": \"opus\",\n  \"permissions\": {\n    \"deny\": [\"Bash(rm:*)\"],\n    \"allow\": [\n      \"Read\",\n      \"Bash(make \\u0026\\u0026 make test)\"\n    ]\n  },\n  \"env\": {\"SHOP\": \"1\"}\n}\n",
			"\xef\xbb\xbf{\n  \"model\": \"opus\",\n  \"permissions\": {\n    \"deny\": [\"Bash(rm:*)\"],\n    \"allow\": [\n      \"Read\",\n      \"Bash(make \\u0026\\u0026 make test)\",\n      \"mcp__plugin_gentry_core\"\n    ]\n  },\n  \"env\": {\"SHOP\": \"1\"}\n}\n",
		},
		"one line": {
			`{"permissions":{"allow":["Read", "Bash(make && make test)"]}}`,
			`{"permissions":{"allow":["Read", "Bash(make && make test)", "mcp__plugin_gentry_core"]}}`,
		},
		"empty allow": {
			"{\n  \"permissions\": { \"allow\": [ ] }\n}\n",
			"{\n  \"permissions\": { \"allow\": [\"mcp__plugin_gentry_core\" ] }\n}\n",
		},
		"empty allow over lines": {
			"{\n  \"permissions\": {\n    \"allow\": [\n    ]\n  }\n}\n",
			"{\n  \"permissions\": {\n    \"allow\": [\n      \"mcp__plugin_gentry_core\"\n    ]\n  }\n}\n",
		},
		"line ends of Windows": {
			"{\r\n  \"permissions\": {\r\n    \"allow\": [\r\n      \"Read\"\r\n    ]\r\n  }\r\n}\r\n",
			"{\r\n  \"permissions\": {\r\n    \"allow\": [\r\n      \"Read\",\r\n      \"mcp__plugin_gentry_core\"\r\n    ]\r\n  }\r\n}\r\n",
		},
		"no allow": {
			`{"model":"opus","permissions":{"deny":["Bash(make && make test)"]}}`,
			`{"model":"opus","permissions":{"deny":["Bash(make && make test)"], "allow": ["mcp__plugin_gentry_core"]}}`,
		},
		"no permissions": {
			"{\n    \"model\": \"opus\",\n    \"env\": {\"A\": \"<b>\"}\n}\n",
			"{\n    \"model\": \"opus\",\n    \"env\": {\"A\": \"<b>\"},\n    \"permissions\": {\"allow\": [\"mcp__plugin_gentry_core\"]}\n}\n",
		},
		"empty": {
			"{}",
			`{"permissions": {"allow": ["mcp__plugin_gentry_core"]}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, c.text)
			if got := AllowTools(p); got != PermissionAdded {
				t.Fatalf("AllowTools: %q", got)
			}
			if got := read(t, p); got != c.want {
				t.Errorf("settings:\n%s\nwant:\n%s", got, c.want)
			}
			if got := AllowTools(p); got != PermissionPresent {
				t.Errorf("again: %q", got)
			}
			if got := read(t, p); got != c.want {
				t.Errorf("settings changed by a present rule:\n%s", got)
			}
		})
	}
	for name, text := range map[string]string{
		"not json":      `{"model":`,
		"not an object": `["Read"]`,
		"permissions":   `{"permissions":"all"}`,
		"allow":         `{"permissions":{"allow":"Read"}}`,
		"trailing":      `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, text)
			if got := AllowTools(p); got != PermissionFailed {
				t.Errorf("AllowTools: %q", got)
			}
			if got := read(t, p); got != text {
				t.Errorf("settings changed: %s", got)
			}
		})
	}
	t.Run("directory", func(t *testing.T) {
		// A directory in place of the file cannot be read.
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := AllowTools(p); got != PermissionFailed {
			t.Errorf("AllowTools: %q", got)
		}
	})
	t.Run("cannot write", func(t *testing.T) {
		text := `{"permissions":{"allow":[]}}`
		p := write(t, text)
		writeSettings = func(string, []byte, fs.FileMode) error { return errors.New("busy") }
		t.Cleanup(func() { writeSettings = writeFile })
		if got := AllowTools(p); got != PermissionUnwritten {
			t.Errorf("AllowTools: %q", got)
		}
		if got := read(t, p); got != text {
			t.Errorf("settings changed: %s", got)
		}
	})
	if runtime.GOOS != "windows" {
		t.Run("mode and link", func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "dotfiles", "settings.json")
			os.MkdirAll(filepath.Dir(real), 0o755)
			if err := os.WriteFile(real, []byte(`{"permissions":{"allow":[]}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "settings.json")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			if got := AllowTools(link); got != PermissionAdded {
				t.Fatalf("AllowTools: %q", got)
			}
			if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the link is replaced: %v", err)
			}
			if fi, err := os.Stat(real); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("mode %v, %v", fi.Mode(), err)
			}
			if got := read(t, real); got != `{"permissions":{"allow":["mcp__plugin_gentry_core"]}}` {
				t.Errorf("settings: %s", got)
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
