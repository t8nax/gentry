package codex

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/paths"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	if fakeCodex != "" {
		os.RemoveAll(filepath.Dir(fakeCodex))
	}
	os.Exit(code)
}

// repos makes a repository a with a worktree b of it and a clone c of it,
// and returns them.
func repos(t *testing.T) (a, b, c string) {
	t.Helper()
	root, _ := paths.Canonical(t.TempDir())
	a = gittest.Repo(t, filepath.Join(root, "shop"))
	b = gittest.Worktree(t, a, filepath.Join(root, "shop-b"), "b")
	c = filepath.Join(root, "shop-c")
	gittest.Run(t, root, "clone", "--quiet", a, c)
	return a, b, c
}

// configOf reads the settings of the text.
func configOf(t *testing.T, text string) *config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := readConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// record is a record of trust of the settings of Codex.
func record(dir, level string) string {
	return "[projects." + tomlKey(dir) + "]\ntrust_level = \"" + level + "\"\n"
}

// TestTrustRecord checks the record of trust that applies to a folder by
// the rules of Codex: of the folder, of the root of its project, of the root
// of its main repository.
func TestTrustRecord(t *testing.T) {
	a, b, c := repos(t)
	sub := filepath.Join(b, "src")
	os.MkdirAll(sub, 0o755)
	cfg := configOf(t, record(a, "trusted")+record(c, "untrusted"))
	for _, tt := range []struct {
		dir, key, level string
	}{
		{a, a, "trusted"},
		{b, a, "trusted"},
		{sub, a, "trusted"},
		{c, c, "untrusted"},
	} {
		key, level, ok := cfg.trustRecord(tt.dir)
		if !ok || key != tt.key || level != tt.level {
			t.Errorf("%s: %q %q %v, want %q %q", tt.dir, key, level, ok, tt.key, tt.level)
		}
	}
	// The record of the folder comes first.
	cfg = configOf(t, record(a, "trusted")+record(b, "untrusted"))
	if key, level, _ := cfg.trustRecord(b); key != b || level != "untrusted" {
		t.Errorf("the record of the worktree: %q %q, want that of %s", key, level, b)
	}
	if _, _, ok := configOf(t, "").trustRecord(a); ok {
		t.Error("a record of trust in empty settings")
	}
	if runtime.GOOS == "windows" {
		cfg = configOf(t, record(strings.ToUpper(a), "trusted"))
		if _, level, ok := cfg.trustRecord(b); !ok || level != "trusted" {
			t.Errorf("a record of another case: %q %v, want trusted", level, ok)
		}
	}
	if !SamePath(`\\?\`+a, a) {
		t.Errorf("the prefix \\\\?\\ is not taken off")
	}
}

// TestTrust checks that the worktrees of one repository are trusted by one
// record of the main one, a clone by its own, and the others are left.
func TestTrust(t *testing.T) {
	a, b, c := repos(t)
	cfg := configOf(t, "# mine\n")
	res, err := cfg.trust([]string{a, b, c, filepath.Join(filepath.Dir(a), "gone")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Trusted, []string{a, b, c}) || len(res.Distrusted) != 0 {
		t.Errorf("trust: %+v", res)
	}
	projects, _ := cfg.value("projects")
	if m, _ := projects.(map[string]any); len(m) != 2 || m[a] == nil || m[c] == nil {
		t.Errorf("records: %v, want those of %s and %s", projects, a, c)
	}
	if !strings.HasPrefix(cfg.text, "# mine\n") {
		t.Errorf("the text of the operator is lost:\n%s", cfg.text)
	}

	cfg = configOf(t, record(a, "untrusted"))
	res, err = cfg.trust([]string{a, b})
	want := []Distrusted{{Path: a, Key: a}, {Path: b, Key: a}}
	if err != nil || len(res.Trusted) != 0 || !slices.Equal(res.Distrusted, want) {
		t.Errorf("trust with the record untrusted: %+v, %v; want %+v", res, err, want)
	}
}

// TestTrustWorktrees checks the settings it cannot read or write: they are
// left as they are.
func TestTrustWorktrees(t *testing.T) {
	a, _, _ := repos(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[features\n"), 0o644)
	var se *SettingsError
	if _, err := TrustWorktrees(p, []string{a}); !errors.As(err, &se) || se.Reason != Unreadable {
		t.Errorf("settings no TOML: %v, want unreadable", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "[features\n" {
		t.Errorf("the settings changed: %q", b)
	}

	os.WriteFile(p, []byte("model = \"m\"\n"), 0o644)
	if res, err := TrustWorktrees(p, []string{a}); err != nil || len(res.Trusted) != 1 {
		t.Fatalf("TrustWorktrees = %+v, %v", res, err)
	}
	if yes, ok := Trusted(p, a); !yes || !ok {
		t.Errorf("Trusted after TrustWorktrees: %v %v", yes, ok)
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return
	}
	os.WriteFile(p, []byte("model = \"m\"\n"), 0o644)
	os.Chmod(p, 0o444)
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	if _, err := checkSettings(p, SetupRequest{Root: t.TempDir()}); !errors.As(err, &se) || se.Reason != Unwritable {
		t.Errorf("settings read-only: %v, want unwritable", err)
	}
	if _, err := TrustWorktrees(p, []string{a}); !errors.As(err, &se) || se.Reason != Unwritable {
		t.Errorf("TrustWorktrees with settings read-only: %v, want unwritable", err)
	}
}

// TestHooksFeature checks that the hooks the operator turned off are turned
// on and those the administrator turned off are left.
func TestHooksFeature(t *testing.T) {
	for _, tt := range []struct {
		text    string
		managed bool
		want    string
		after   string
	}{
		{"", false, HooksOn, ""},
		{"[features]\nhooks = false\n", false, HooksEnabled, "[features]\nhooks = true\n"},
		{"[features]\ncodex_hooks = false\n", false, HooksEnabled, "[features]\nhooks = true\n"},
		{"[features]\nhooks = false\n", true, HooksManagedOff, "[features]\nhooks = false\n"},
	} {
		c := configOf(t, tt.text)
		got, err := c.hooksFeature(tt.managed)
		if err != nil || got != tt.want || c.text != tt.after {
			t.Errorf("%q managed %v: %s %v, text %q; want %s, %q", tt.text, tt.managed, got, err, c.text, tt.want, tt.after)
		}
	}
}

// TestSandbox checks the data root opened for writing in the sandbox.
func TestSandbox(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gentry")
	c := configOf(t, "[sandbox_workspace_write]\nwritable_roots = ['/mine']\n")
	if got, err := c.sandbox(root); err != nil || got != SandboxAdded {
		t.Fatalf("sandbox = %s, %v", got, err)
	}
	if got, _ := c.sandbox(root); got != SandboxPresent {
		t.Errorf("sandbox again = %s, want present", got)
	}
	roots, _ := c.value("sandbox_workspace_write", "writable_roots")
	if !slicesEqual(roots, []any{"/mine", root}) {
		t.Errorf("writable_roots = %v", roots)
	}
	c = configOf(t, "default_permissions = \"strict\"\n")
	if got, _ := c.sandbox(root); got != SandboxProfile || c.text != "default_permissions = \"strict\"\n" {
		t.Errorf("sandbox with a profile = %s, text %q", got, c.text)
	}
}

func slicesEqual(v any, want []any) bool {
	got, _ := v.([]any)
	return slices.Equal(got, want)
}

// TestSamePlace checks a place named with the prefix Codex writes on
// Windows, and through a symbolic link where the system makes one.
func TestSamePlace(t *testing.T) {
	dir, _ := paths.Canonical(t.TempDir())
	if got := withoutVerbatim(verbatim + dir); got != dir {
		t.Errorf("withoutVerbatim = %q, want %q", got, dir)
	}
	if !samePlace(verbatim+dir, dir) {
		t.Errorf("%s%s is not %s", verbatim, dir, dir)
	}
	link := filepath.Join(t.TempDir(), "link")
	if os.Symlink(dir, link) != nil {
		t.Log("no symbolic link: the system does not make one")
		return
	}
	if !samePlace(verbatim+link, dir) || !samePlace(link, verbatim+dir) {
		t.Errorf("%s through the link %s is not the same place", dir, link)
	}
	if samePlace(verbatim+link, t.TempDir()) {
		t.Error("another folder is the same place")
	}
}

// TestTrustRecordChoice checks the record taken of two of one place: the
// key as the place is written, else the first by key.
func TestTrustRecordChoice(t *testing.T) {
	a, _, _ := repos(t)
	other := a + string(filepath.Separator)
	for i := 0; i < 10; i++ {
		cfg := configOf(t, record(other, "untrusted")+record(a, "trusted"))
		if key, level, _ := cfg.trustRecord(a); key != a || level != "trusted" {
			t.Fatalf("the record of the key as written: %q %q, want %q trusted", key, level, a)
		}
		cfg = configOf(t, record(other+".", "untrusted")+record(other, "trusted"))
		if key, _, _ := cfg.trustRecord(a); key != other {
			t.Fatalf("the first record by key: %q, want %q", key, other)
		}
	}
}

// TestTrustWorktreesLock checks that the settings locked by another command
// of Gentry are left as they are after the wait: unwritable.
func TestTrustWorktreesLock(t *testing.T) {
	a, _, _ := repos(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	held, err := lockSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	wait := LockWait
	LockWait = 100 * time.Millisecond
	t.Cleanup(func() { LockWait = wait })
	var se *SettingsError
	if _, err := TrustWorktrees(p, []string{a}); !errors.As(err, &se) || se.Reason != Unwritable {
		t.Errorf("TrustWorktrees with the lock held: %v, want unwritable", err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("the settings are written")
	}
}
