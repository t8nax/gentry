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
	"time"
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
	if got, err := Elsewhere(s.request()); err != nil || !SamePath(got, other.dir) {
		t.Errorf("Elsewhere = %q, %v", got, err)
	}
	req := s.request()
	req.Switch = true
	r, err := Setup(req)
	if err != nil || !SamePath(r.Previous, other.dir) {
		t.Errorf("switch: %+v, %v", r, err)
	}
	if got, err := Elsewhere(s.request()); err != nil || got != "" {
		t.Errorf("Elsewhere after the switch = %q, %v", got, err)
	}
}

// TestSetupEditRefused checks that settings with a value in a form Gentry
// does not edit refuse the setup before Codex runs and anything is written,
// and refuse the update too.
func TestSetupEditRefused(t *testing.T) {
	s := newStand(t)
	const text = "features = { hooks = false, codex_hooks = false }\n"
	os.WriteFile(ConfigPath(s.home), []byte(text), 0o644)
	var se *SettingsError
	if _, err := Setup(s.request()); !errors.As(err, &se) || se.Reason != Unwritable {
		t.Errorf("Setup = %v, want unwritable", err)
	}
	if _, err := Elsewhere(s.request()); !errors.As(err, &se) || se.Reason != Unwritable {
		t.Errorf("Elsewhere = %v, want unwritable", err)
	}
	if _, err := os.Stat(s.log); err == nil {
		t.Errorf("Codex ran: %q", s.calls())
	}
	if _, err := os.Stat(s.dir); err == nil {
		t.Error("the folder of the integration is made")
	}
	if b, _ := os.ReadFile(ConfigPath(s.home)); string(b) != text {
		t.Errorf("the settings changed: %q", b)
	}
}

// TestSetupFirstFails checks that a first setup that fails once the folder
// of the integration is written removes it, and a later one keeps it.
func TestSetupFirstFails(t *testing.T) {
	s := newStand(t)
	t.Setenv("FAKECODEX_FAIL", "plugin add")
	var ce *CommandError
	if _, err := Setup(s.request()); !errors.As(err, &ce) {
		t.Fatalf("Setup = %v, want a command error", err)
	}
	if _, err := os.Stat(s.dir); err == nil {
		t.Error("the folder of the integration stays after the first setup failed")
	}
	if got, err := Registered(fakeCodex); err != nil || got != "" {
		t.Errorf("the marketplace of Gentry stays after the first setup failed: %q, %v", got, err)
	}
	t.Setenv("FAKECODEX_FAIL", "")
	if r := s.setup(t); r.Action != Installed {
		t.Errorf("the setup after the failed one: %s, want installed", r.Action)
	}
	t.Setenv("FAKECODEX_FAIL", "plugin add")
	if _, err := Setup(SetupRequest{Dir: s.dir, Root: s.root, Exe: testExe, Version: "0.4.0"}); !errors.As(err, &ce) {
		t.Fatalf("Setup of another version = %v, want a command error", err)
	}
	if _, err := os.Stat(s.dir); err != nil {
		t.Errorf("the folder of the integration is gone after a later setup failed: %v", err)
	}
}

// TestNotListed checks the reason of a hook Codex does not list.
func TestNotListed(t *testing.T) {
	if got := notListed(testExe); got != `codex app-server hooks/list: no handler with the command "/opt/Gentry Tools/gentry" hook session-start --tool codex` {
		t.Errorf("notListed = %q", got)
	}
}

// TestPluginMatches checks the plugin the hook compares with the program,
// from the files of Codex: missing from the settings, of another version in
// the cache, and with settings that cannot be read.
func TestPluginMatches(t *testing.T) {
	s := newStand(t)
	s.setup(t)
	cfg := ConfigPath(s.home)
	good, _ := os.ReadFile(cfg)
	if !PluginMatches(s.dir, testExe, "0.3.0") {
		t.Fatal("the plugin of the program does not match")
	}
	if PluginMatches(s.dir, testExe, "0.4.0") {
		t.Error("the plugin of another version matches")
	}
	// The settings without the marketplace, then without the plugin.
	for _, text := range []string{
		"[plugins.\"gentry@gentry\"]\nenabled = true\n",
		"[marketplaces.gentry]\nsource_type = \"local\"\n",
	} {
		os.WriteFile(cfg, []byte(text), 0o644)
		if PluginMatches(s.dir, testExe, "0.3.0") {
			t.Errorf("the plugin matches with the settings:\n%s", text)
		}
	}
	// Settings that cannot be read leave the cache to tell.
	os.WriteFile(cfg, []byte("[features\n"), 0o644)
	if !PluginMatches(s.dir, testExe, "0.3.0") {
		t.Error("the plugin in the cache does not match with the settings not read")
	}
	os.WriteFile(cfg, good, 0o644)
	cache := filepath.Join(s.home, "plugins", "cache", "gentry", "gentry")
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		os.Rename(filepath.Join(cache, e.Name()), filepath.Join(cache, "0.0.0"))
	}
	if PluginMatches(s.dir, testExe, "0.3.0") {
		t.Error("the plugin of another version in the cache matches")
	}
}

// TestSetupFirstFailsUndo checks that a first setup that fails after the
// plugin is installed removes the marketplace of Gentry from Codex, and
// that a removal that fails leaves the failure of the setup told.
func TestSetupFirstFailsUndo(t *testing.T) {
	s := newStand(t)
	// The lock of the settings held by another command fails the edits after
	// the install.
	held, err := lockSettings(ConfigPath(s.home))
	if err != nil {
		t.Fatal(err)
	}
	wait := LockWait
	LockWait = 100 * time.Millisecond
	t.Cleanup(func() { LockWait = wait })
	var se *SettingsError
	if _, err := Setup(s.request()); !errors.As(err, &se) {
		t.Fatalf("Setup = %v, want the settings unwritable", err)
	}
	if got, _ := Registered(fakeCodex); got != "" {
		t.Errorf("the marketplace of Gentry stays: %q", got)
	}
	t.Setenv("FAKECODEX_FAIL", "plugin marketplace remove")
	if _, err := Setup(s.request()); !errors.As(err, &se) {
		t.Errorf("Setup with the removal failing = %v, want the settings unwritable", err)
	}
	held.Release()
	if _, err := os.Stat(s.dir); err == nil {
		t.Error("the folder of the integration stays")
	}
}

// TestCurrentWindowsCommand checks that the integration is not current for
// update when the handler of Gentry has no command of Windows or another.
func TestCurrentWindowsCommand(t *testing.T) {
	s := newStand(t)
	s.setup(t)
	if _, ok := Current(s.request()); !ok {
		t.Fatal("the integration is not current after setup")
	}
	p := HooksPath(s.home)
	good, _ := os.ReadFile(p)
	// Without the command of Windows, and with it in double quotes, as
	// Gentry wrote it before.
	stale := strings.Replace(string(good), string(raw(HookCommandWindows(testExe))), string(raw("& "+HookCommand(testExe))), 1)
	if stale == string(good) {
		t.Fatal("stand: no command of Windows to make stale")
	}
	for _, text := range []string{
		strings.Replace(string(good), `"commandWindows"`, `"commandOther"`, 1),
		stale,
	} {
		os.WriteFile(p, []byte(text), 0o644)
		if _, ok := Current(s.request()); ok {
			t.Errorf("the integration is current with the settings of the hooks:\n%s", text)
		}
	}
}

// TestSetupFailsKeepsMarketplace checks that a setup that fails leaves in
// Codex the marketplace and the plugin it did not add: the folder of the
// integration is gone, the marketplace of Gentry names it, and plugin list
// fails.
func TestSetupFailsKeepsMarketplace(t *testing.T) {
	s := newStand(t)
	s.setup(t)
	if err := os.RemoveAll(s.dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKECODEX_FAIL", "plugin list")
	var ce *CommandError
	if _, err := Setup(s.request()); !errors.As(err, &ce) {
		t.Fatalf("Setup = %v, want a command error", err)
	}
	t.Setenv("FAKECODEX_FAIL", "")
	if got, err := Registered(fakeCodex); err != nil || !SamePath(got, s.dir) {
		t.Errorf("the marketplace of Gentry: %q, %v; want that of %s", got, err, s.dir)
	}
	if p, found, err := (cli{program: fakeCodex}).plugin(); err != nil || !found {
		t.Errorf("the plugin of Gentry: %+v %v, %v; want it installed", p, found, err)
	}
	for _, c := range s.calls() {
		if strings.HasPrefix(c, "plugin marketplace remove") {
			t.Errorf("the setup ran codex %s", c)
		}
	}
}
