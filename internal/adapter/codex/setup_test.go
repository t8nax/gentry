package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// stand is a CODEX_HOME of the test with the fake Codex of the tests of
// Gentry as the codex program, and a data root.
type stand struct {
	home, root, dir, log string
}

// fakeCodex is the fake Codex, built once for the package.
var fakeCodex string

// newStand makes the stand of the test.
func newStand(t *testing.T) *stand {
	t.Helper()
	if fakeCodex == "" {
		dir, err := os.MkdirTemp("", "gentry-fakecodex-")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "codex")
		if runtime.GOOS == "windows" {
			p += ".exe"
		}
		if out, err := exec.Command("go", "build", "-o", p, "../../../tests/testkit/fakecodex").CombinedOutput(); err != nil {
			t.Fatalf("build the fake Codex: %v\n%s", err, out)
		}
		fakeCodex = p
	}
	base := t.TempDir()
	s := &stand{home: filepath.Join(base, "codex"), root: filepath.Join(base, "gentry"), log: filepath.Join(base, "codex.log")}
	s.dir = filepath.Join(s.root, "integrations", "codex")
	os.MkdirAll(s.home, 0o755)
	t.Setenv(HomeEnv, s.home)
	t.Setenv(ProgramEnv, fakeCodex)
	t.Setenv("FAKECODEX_LOG", s.log)
	t.Setenv("FAKECODEX_FAIL", "")
	t.Setenv("FAKECODEX_REQUIREMENTS", "")
	return s
}

func (s *stand) request(worktrees ...string) SetupRequest {
	return SetupRequest{Dir: s.dir, Root: s.root, Exe: testExe, Version: "0.3.0", Worktrees: worktrees}
}

func (s *stand) setup(t *testing.T, worktrees ...string) SetupResult {
	t.Helper()
	r, err := Setup(s.request(worktrees...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (s *stand) config(t *testing.T) *config {
	t.Helper()
	c, err := readConfig(ConfigPath(s.home))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (s *stand) calls() []string {
	b, _ := os.ReadFile(s.log)
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")), "\n")
}

// TestSetup checks the first setup and a repeat: the plugin installed, the
// hook written and trusted, the data root in the sandbox, the worktrees
// trusted; then nothing to do, and the integration current.
func TestSetup(t *testing.T) {
	s := newStand(t)
	a, b, _ := repos(t)
	r := s.setup(t, a, b)
	if r.Action != Installed || !r.Enabled || r.HookTrust != HookTrusted || r.HooksFeature != HooksOn || r.Sandbox != SandboxAdded ||
		!slices.Equal(r.Trust.Trusted, []string{a, b}) || r.Settings != ConfigPath(s.home) || r.HooksFile != HooksPath(s.home) {
		t.Errorf("first setup: %+v", r)
	}
	if !hookCurrent(HooksPath(s.home), testExe) {
		t.Error("no handler of Gentry")
	}
	if !PluginMatches(s.dir, testExe, "0.3.0") {
		t.Error("the plugin does not match after setup")
	}
	r = s.setup(t, a, b)
	if r.Action != Unchanged || r.HookTrust != HookPresent || r.Sandbox != SandboxPresent || len(r.Trust.Trusted) != 0 {
		t.Errorf("repeat: %+v", r)
	}
	n := len(s.calls())
	cur, ok := Current(s.request(a, b))
	if !ok || cur.Action != Unchanged || cur.HookTrust != HookPresent || cur.Sandbox != SandboxPresent {
		t.Errorf("Current = %+v, %v", cur, ok)
	}
	for _, c := range s.calls()[n:] {
		if strings.HasPrefix(c, "plugin add") || strings.HasPrefix(c, "plugin marketplace add") || strings.HasPrefix(c, "plugin marketplace remove") {
			t.Errorf("Current ran codex %s", c)
		}
	}
	if _, ok := Current(SetupRequest{Dir: s.dir, Root: s.root, Exe: testExe, Version: "0.4.0"}); ok {
		t.Error("Current of another version")
	}
}

// TestSetupDisabled checks that a plugin the operator disabled stays
// disabled when it is installed anew.
func TestSetupDisabled(t *testing.T) {
	s := newStand(t)
	s.setup(t)
	c := s.config(t)
	if err := c.set("false", "plugins", pluginID, "enabled"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(ConfigPath(s.home), []byte(c.text), 0o644)
	r, err := Setup(SetupRequest{Dir: s.dir, Root: s.root, Exe: testExe, Version: "0.4.0"})
	if err != nil || r.Action != Updated || r.Enabled {
		t.Errorf("setup of another version: %+v, %v; want updated and disabled", r, err)
	}
	if v, _ := s.config(t).value("plugins", pluginID, "enabled"); v != false {
		t.Errorf("enabled = %v, want false", v)
	}
}

// TestSetupHookUntrusted checks that a hook Codex fails to trust is told
// with its reason, and the rest is set up.
func TestSetupHookUntrusted(t *testing.T) {
	s := newStand(t)
	t.Setenv("FAKECODEX_FAIL", "app-server")
	r := s.setup(t)
	if r.HookTrust != HookUntrusted || !strings.Contains(r.HookError, "simulated failure") || r.Action != Installed {
		t.Errorf("setup: %+v", r)
	}
}

// TestSetupManaged checks that the hooks the administrator turned off are
// not turned on or trusted.
func TestSetupManaged(t *testing.T) {
	s := newStand(t)
	req := filepath.Join(t.TempDir(), "requirements.json")
	os.WriteFile(req, []byte(`{"requirements": {"allowManagedHooksOnly": true}}`), 0o644)
	t.Setenv("FAKECODEX_REQUIREMENTS", req)
	os.WriteFile(ConfigPath(s.home), []byte("[features]\nhooks = false\n"), 0o644)
	r := s.setup(t)
	if r.HookTrust != HookSkipped || r.HooksFeature != HooksManagedOff {
		t.Errorf("setup: %+v", r)
	}
	if v, _ := s.config(t).value("features", "hooks"); v != false {
		t.Errorf("features.hooks = %v, want false", v)
	}
}

// TestSetupRefused checks the refusals: settings that cannot be read before
// the program is looked for, no program, the plugin of another data root.
func TestSetupRefused(t *testing.T) {
	s := newStand(t)
	os.WriteFile(ConfigPath(s.home), []byte("[features\n"), 0o644)
	t.Setenv(ProgramEnv, filepath.Join(t.TempDir(), "no-codex"))
	var se *SettingsError
	if _, err := Setup(s.request()); !errors.As(err, &se) || se.Reason != Unreadable {
		t.Errorf("settings no TOML: %v", err)
	}
	if _, err := os.Stat(s.dir); err == nil {
		t.Error("the folder of the integration is made")
	}
	os.Remove(ConfigPath(s.home))
	if _, err := Setup(s.request()); !errors.Is(err, ErrNotFound) {
		t.Errorf("no codex: %v", err)
	}

	t.Setenv(ProgramEnv, fakeCodex)
	other := *s
	other.dir = filepath.Join(t.TempDir(), "integrations", "codex")
	other.setup(t)
	var ee *ElsewhereError
	if _, err := Setup(s.request()); !errors.As(err, &ee) || !SamePath(ee.Registered, other.dir) {
		t.Errorf("plugin of another data root: %v", err)
	}
	if got, err := Elsewhere(s.dir); err != nil || !SamePath(got, other.dir) {
		t.Errorf("Elsewhere = %q, %v", got, err)
	}
	req := s.request()
	req.Switch = true
	r, err := Setup(req)
	if err != nil || !SamePath(r.Previous, other.dir) {
		t.Errorf("switch: %+v, %v", r, err)
	}
	if got, err := Elsewhere(s.dir); err != nil || got != "" {
		t.Errorf("Elsewhere after the switch = %q, %v", got, err)
	}
}
