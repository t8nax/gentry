package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

func TestSetupClaude(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
	t.Setenv(claude.ConfigDirEnv, filepath.Join(root, "claude-config"))
	settings := filepath.Join(root, "claude-config", "settings.json")
	dir := filepath.Join(root, "integrations", "claude")
	// Marks of calls left by the hooks of an earlier Gentry are removed.
	marks := filepath.Join(root, "state", "calls", "s1")
	os.MkdirAll(marks, 0o755)

	code, stdout, stderr := run("setup", "claude")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if want := msg.Text(msg.SetupInstalled) + "\n" + msg.Text(msg.SetupPermissionAdded) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n"; stdout != want {
		t.Errorf("first run: stdout %q, want %q", stdout, want)
	}
	for _, f := range []string{".claude-plugin/marketplace.json", "gentry/.claude-plugin/plugin.json", "gentry/hooks/hooks.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	hooks, _ := os.ReadFile(filepath.Join(dir, "gentry", "hooks", "hooks.json"))
	exe, _ := executable()
	if !strings.Contains(string(hooks), filepath.ToSlash(exe)) {
		t.Errorf("hooks.json does not call the running gentry %s:\n%s", exe, hooks)
	}

	if _, stdout, _ := run("setup", "claude"); stdout != msg.Text(msg.SetupUnchanged)+"\n" {
		t.Errorf("second run: stdout %q", stdout)
	}
	_, stdout, _ = run("setup", "claude", "--json")
	validate(t, "schemas/setup.json", stdout)
	var out contract.SetupOutput
	json.Unmarshal([]byte(stdout), &out)
	if out.Tool != "claude" || out.Dir != dir || out.PluginVersion == "" || out.Action != "unchanged" || !out.Enabled ||
		out.Permission == nil || *out.Permission != "present" || out.Settings == nil || *out.Settings != settings {
		t.Errorf("unexpected output %+v", out)
	}
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), claude.PermissionRule) {
		t.Errorf("settings:\n%s", b)
	}
	if _, err := os.Stat(filepath.Dir(marks)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("marks of calls left: %v", err)
	}

	// Settings that cannot be read are left as they are; the plugin is set
	// up all the same.
	os.WriteFile(settings, []byte(`{"permissions":`), 0o644)
	code, stdout, _ = run("setup", "claude")
	want := msg.Text(msg.SetupUnchanged) + "\n" + msg.Text(msg.SetupPermissionFailed) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n\n" +
		msg.Text(msg.HintSetupPermission, claude.PermissionRule) + "\n"
	if code != contract.ExitOK || stdout != want {
		t.Errorf("unreadable settings: exit code %d, stdout:\n%s\nwant:\n%s", code, stdout, want)
	}
	if b, _ := os.ReadFile(settings); string(b) != `{"permissions":` {
		t.Errorf("unreadable settings changed: %s", b)
	}

	// Settings that cannot be written are left as they are too: on Windows a
	// file held open cannot be replaced, elsewhere a directory without the
	// right to write takes no file.
	os.WriteFile(settings, []byte(`{"permissions":{"allow":[]}}`), 0o644)
	if runtime.GOOS == "windows" {
		f, err := os.Open(settings)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	} else {
		os.Chmod(filepath.Dir(settings), 0o555)
		defer os.Chmod(filepath.Dir(settings), 0o755)
	}
	code, stdout, _ = run("setup", "claude")
	want = msg.Text(msg.SetupUnchanged) + "\n" + msg.Text(msg.SetupPermissionUnwritten) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n\n" +
		msg.Text(msg.HintSetupPermission, claude.PermissionRule) + "\n"
	if code != contract.ExitOK || stdout != want {
		t.Errorf("settings that cannot be written: exit code %d, stdout:\n%s\nwant:\n%s", code, stdout, want)
	}
	_, stdout, _ = run("setup", "claude", "--json")
	json.Unmarshal([]byte(stdout), &out)
	if out.Permission == nil || *out.Permission != "unwritten" {
		t.Errorf("settings that cannot be written in JSON: %s", stdout)
	}
}

func TestSetupToolErrors(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(claude.ProgramEnv, filepath.Join(t.TempDir(), "missing"))
	code, stdout, _ := run("setup", "claude", "--json")
	var e contract.ErrorOutput
	json.Unmarshal([]byte(stdout), &e)
	if code != contract.ExitError || e.Error.Code != contract.CodeToolNotFound {
		t.Errorf("missing claude: exit code %d, output %s", code, stdout)
	}
	_, _, stderr := run("setup", "claude")
	want := msg.Text(msg.ErrToolNotFound, "claude") + "\n\n" + msg.Text(msg.HintToolNotFound, "Claude Code", "claude") + "\n"
	if stderr != want {
		t.Errorf("stderr %q, want %q", stderr, want)
	}
}

func TestSetupToolFailed(t *testing.T) {
	tests := []struct{ fail, command string }{
		{"install", "claude plugin install gentry@gentry --scope user --json"},
		// The check of the plugin served before anything is written.
		{"marketplace-list", "claude plugin marketplace list --json"},
	}
	for _, tt := range tests {
		root := t.TempDir()
		t.Setenv(home.EnvVar, root)
		t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
		t.Setenv("FAKECLAUDE_FAIL", tt.fail)
		code, stdout, _ := run("setup", "claude", "--json")
		validate(t, "schemas/error.json", stdout)
		var e contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &e)
		if code != contract.ExitError || e.Error.Code != contract.CodeToolFailed {
			t.Fatalf("%s: exit code %d, output %s", tt.fail, code, stdout)
		}
		if e.Error.Details["command"] != tt.command || e.Error.Details["output"] != "simulated failure" {
			t.Errorf("%s: unexpected details %v", tt.fail, e.Error.Details)
		}
	}
}

func TestSetupErrors(t *testing.T) {
	tests := []struct {
		args   []string
		code   string
		stderr string
	}{
		{[]string{"setup"}, contract.CodeMissingArgument, msg.Text(msg.ErrSetupToolMissing) + "\n\n" + "Посмотреть описание команды: gentry setup --help"},
		{[]string{"setup", "foo"}, contract.CodeInvalidArgument, msg.Text(msg.ErrSetupToolUnknown, "foo") + "\n\n" + "Посмотреть описание команды: gentry setup --help"},
		{[]string{"setup", "claude", "extra"}, contract.CodeUnexpectedArgs, msg.Text(msg.ErrExtraArgs, "setup", "extra")},
	}
	for _, tt := range tests {
		exit, _, stderr := run(tt.args...)
		if exit != contract.ExitUsage || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, exit, stderr, tt.stderr)
		}
		_, stdout, _ := run(append(tt.args, "--json")...)
		validate(t, "schemas/error.json", stdout)
		var e contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &e)
		if e.Error.Code != tt.code {
			t.Errorf("%v --json: code %q, want %q", tt.args, e.Error.Code, tt.code)
		}
	}
}

func TestSetupHomeUnknown(t *testing.T) {
	t.Setenv(home.EnvVar, "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "") // Plan 9
	code, stdout, _ := run("setup", "claude", "--json")
	var e contract.ErrorOutput
	json.Unmarshal([]byte(stdout), &e)
	if code != contract.ExitError || e.Error.Code != contract.CodeHomeUnknown {
		t.Errorf("exit code %d, output %s; want %d and %s", code, stdout, contract.ExitError, contract.CodeHomeUnknown)
	}
}

// fakeState is what the fake claude records in its state file.
type fakeState struct {
	Marketplaces []struct{ Name, Path string }
	Plugins      []struct {
		ID      string
		Enabled bool
	}
	Log []string // commands run, one line each
}

func readFake(t *testing.T) fakeState {
	t.Helper()
	var s fakeState
	b, err := os.ReadFile(os.Getenv("FAKECLAUDE_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &s)
	return s
}

// fakeMarketplace returns the directory the fake claude serves the plugin of
// Gentry from.
func fakeMarketplace(t *testing.T) string {
	t.Helper()
	for _, m := range readFake(t).Marketplaces {
		if m.Name == "gentry" {
			return m.Path
		}
	}
	return ""
}

// setupFrom runs `gentry setup claude` with args and root as the data root.
func setupFrom(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv(home.EnvVar, root)
	return run(append([]string{"setup", "claude"}, args...)...)
}

// twoRoots gives two data roots and a fake claude serving the plugin from
// the first.
func twoRoots(t *testing.T) (a, b string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("FAKECLAUDE_STATE", filepath.Join(base, "fakeclaude-state.json"))
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	if code, _, stderr := setupFrom(t, a); code != contract.ExitOK {
		t.Fatalf("setup from a: exit code %d, stderr %q", code, stderr)
	}
	return a, b
}

func TestSetupPluginElsewhere(t *testing.T) {
	a, b := twoRoots(t)
	from, to := filepath.Join(a, "integrations", "claude"), filepath.Join(b, "integrations", "claude")

	before := readFake(t).Log
	code, stdout, stderr := setupFrom(t, b)
	want := msg.Text(msg.ErrPluginElsewhere, from, to) + "\n\n" + msg.Text(msg.HintPluginElsewhere, "claude") + "\n"
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stdout %q, stderr %q; want %q", code, stdout, stderr, want)
	}
	if got := readFake(t).Log[len(before):]; !slices.Equal(got, []string{"plugin marketplace list --json"}) {
		t.Errorf("the refused setup ran %q; want only the marketplace list", got)
	}
	if got := fakeMarketplace(t); got != from {
		t.Errorf("the plugin is served from %q, want it kept at %q", got, from)
	}
	if _, err := os.Stat(b); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused setup wrote into its data root: %v", err)
	}

	code, stdout, _ = setupFrom(t, b, "--json")
	validate(t, "schemas/error.json", stdout)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(stdout), &e)
	if code != contract.ExitError || e.Error.Code != contract.CodePluginElsewhere ||
		e.Error.Details["tool"] != "claude" || e.Error.Details["registered"] != from || e.Error.Details["dir"] != to {
		t.Errorf("--json: exit code %d, output %s", code, stdout)
	}

	if code, stdout, _ := setupFrom(t, a); code != contract.ExitOK || stdout != msg.Text(msg.SetupUnchanged)+"\n" {
		t.Errorf("setup from the data root of the plugin: exit code %d, stdout %q", code, stdout)
	}
}

func TestSetupSwitch(t *testing.T) {
	a, b := twoRoots(t)
	from, to := filepath.Join(a, "integrations", "claude"), filepath.Join(b, "integrations", "claude")

	code, stdout, stderr := setupFrom(t, b, "--switch")
	if code != contract.ExitOK || stdout != msg.Text(msg.SetupSwitched, from)+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := fakeMarketplace(t); got != to {
		t.Errorf("the plugin is served from %q, want %q", got, to)
	}
	if _, err := os.Stat(from); err != nil {
		t.Errorf("the plugin of the other data root must stay: %v", err)
	}

	if code, stdout, _ := setupFrom(t, b, "--switch"); code != contract.ExitOK || stdout != msg.Text(msg.SetupUnchanged)+"\n" {
		t.Errorf("--switch with nothing to switch: exit code %d, stdout %q", code, stdout)
	}
	code, stdout, _ = setupFrom(t, a, "--switch", "--json")
	validate(t, "schemas/setup.json", stdout)
	var out contract.SetupOutput
	json.Unmarshal([]byte(stdout), &out)
	if code != contract.ExitOK || out.Action != "switched" || out.PreviousDir == nil || *out.PreviousDir != to || out.Dir != from || !out.Enabled {
		t.Errorf("switch back --json: exit code %d, output %s", code, stdout)
	}
}

func TestSetupSwitchFromMissing(t *testing.T) {
	a, b := twoRoots(t)
	from := filepath.Join(a, "integrations", "claude")
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := setupFrom(t, b)
	if code != contract.ExitOK || stdout != msg.Text(msg.SetupSwitched, from)+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got, want := fakeMarketplace(t), filepath.Join(b, "integrations", "claude"); got != want {
		t.Errorf("the plugin is served from %q, want %q", got, want)
	}
}

func TestSetupSwitchFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
	if code, stdout, _ := setupFrom(t, root, "--switch"); code != contract.ExitOK || stdout != msg.Text(msg.SetupInstalled)+"\n" {
		t.Errorf("--switch on the first setup: exit code %d, stdout %q", code, stdout)
	}
}

func TestSetupSwitchKeepsDisabled(t *testing.T) {
	a, b := twoRoots(t)
	if out, err := exec.Command(os.Getenv(claude.ProgramEnv), "plugin", "disable", "gentry@gentry").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	code, stdout, _ := setupFrom(t, b, "--switch")
	want := msg.Text(msg.SetupSwitched, filepath.Join(a, "integrations", "claude")) + "\n\n" + msg.Text(msg.SetupDisabled) + "\n"
	if code != contract.ExitOK || stdout != want {
		t.Errorf("exit code %d, stdout %q, want %q", code, stdout, want)
	}
	if p := readFake(t).Plugins; len(p) != 1 || p[0].Enabled {
		t.Errorf("the plugin the operator disabled must stay disabled after the switch: %+v", p)
	}
}

// A plugin registered through a link to the data root of this command, not
// yet written, is the same directory once written: nothing is switched.
func TestSetupSameThroughLink(t *testing.T) {
	base := t.TempDir()
	b := filepath.Join(base, "b")
	link := filepath.Join(base, "link")
	if err := os.Symlink(b, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	state := filepath.Join(base, "fakeclaude-state.json")
	t.Setenv("FAKECLAUDE_STATE", state)
	seed, _ := json.Marshal(map[string]any{"marketplaces": []any{map[string]string{
		"name": "gentry", "source": "directory", "path": filepath.Join(link, "integrations", "claude"),
	}}})
	if err := os.WriteFile(state, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := setupFrom(t, b); code != contract.ExitOK || stdout != msg.Text(msg.SetupInstalled)+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
