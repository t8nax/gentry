package claude

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/t8nax/gentry/internal/integration"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

const testExe = `C:\Program Files\Gentry\gentry.exe`

func testPlugin(t *testing.T, exe, version string) Plugin {
	t.Helper()
	p, err := Build(integration.Gentry(exe, "Gentry ведёт задачи агента по флоу проекта."), version)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGolden compares the built plugin with testdata/golden; run
// `go test ./internal/adapter/claude -update` to rewrite it after a deliberate change.
func TestGolden(t *testing.T) {
	p := testPlugin(t, testExe, "0.1.0")
	golden := filepath.Join("testdata", "golden")
	if *update {
		os.RemoveAll(golden)
		if err := writeFiles(golden, p.Files); err != nil {
			t.Fatal(err)
		}
	}
	var onDisk []string
	filepath.WalkDir(golden, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(golden, path)
			onDisk = append(onDisk, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(onDisk) != len(p.Files) {
		t.Errorf("golden has %v, plugin has %d files", onDisk, len(p.Files))
	}
	for path, got := range p.Files {
		want, err := os.ReadFile(filepath.Join(golden, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from golden:\n%s\nwant:\n%s", path, got, want)
		}
	}
}

func TestHookCommand(t *testing.T) {
	p := testPlugin(t, testExe, "0.1.0")
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(p.Files["gentry/hooks/hooks.json"], &hooks); err != nil {
		t.Fatal(err)
	}
	got := hooks.Hooks["SessionStart"][0].Hooks[0].Command
	want := `"C:/Program Files/Gentry/gentry.exe" hook session-start`
	if got != want {
		t.Errorf("command %q, want %q", got, want)
	}
}

func TestVersion(t *testing.T) {
	a := testPlugin(t, testExe, "0.1.1-0.20261004175847-74a0120d2d39+dirty")
	if !regexp.MustCompile(`^0\.1\.1-0\.20261004175847-74a0120d2d39\+[0-9a-f]{8}$`).MatchString(a.Version) {
		t.Errorf("version %q: want the gentry version without build metadata plus a fingerprint", a.Version)
	}
	if b := testPlugin(t, testExe, "0.1.1-0.20261004175847-74a0120d2d39+dirty"); b.Version != a.Version {
		t.Errorf("same content, different versions: %q and %q", a.Version, b.Version)
	}
	if c := testPlugin(t, `D:\gentry.exe`, "0.1.1-0.20261004175847-74a0120d2d39+dirty"); c.Version == a.Version {
		t.Errorf("content changed but the version did not: %q", c.Version)
	}
	for _, path := range []string{".claude-plugin/marketplace.json", "gentry/.claude-plugin/plugin.json"} {
		if !bytes.Contains(a.Files[path], []byte(`"version": "`+a.Version+`"`)) {
			t.Errorf("%s does not carry the version %q", path, a.Version)
		}
	}
}

func TestWriteReplacesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "integrations", "claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "stale.txt")
	os.WriteFile(stale, []byte("old"), 0o644)

	p := testPlugin(t, testExe, "0.1.0")
	if err := Write(dir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a file from the previous plugin was left behind")
	}
	for path, want := range p.Files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s not written: %v", path, err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 {
		t.Errorf("temporary directories left behind: %v", entries)
	}
}
