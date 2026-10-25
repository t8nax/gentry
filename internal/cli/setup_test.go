package cli

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/t8nax/gentry/internal/state"
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
	if want := msg.Text(msg.SetupInstalled, "Claude Code") + "\n" + msg.Text(msg.SetupPermissionAdded) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n"; stdout != want {
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

	if _, stdout, _ := run("setup", "claude"); stdout != msg.Text(msg.SetupUnchanged, "Claude Code")+"\n" {
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
	want := msg.Text(msg.SetupUnchanged, "Claude Code") + "\n" + msg.Text(msg.SetupPermissionFailed) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n\n" +
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
	want = msg.Text(msg.SetupUnchanged, "Claude Code") + "\n" + msg.Text(msg.SetupPermissionUnwritten) + "\n" + msg.Text(msg.SetupSettings, settings) + "\n\n" +
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
	want := msg.Text(msg.ErrToolNotFound, "claude") + "\n\n" + HintFor(msg.HintToolNotFound, "tool", "claude", "Claude Code") + "\n"
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
		{[]string{"setup"}, contract.CodeMissingArgument, msg.Text(msg.ErrSetupToolMissing) + "\n\n" + HintText(msg.HintCommandHelp, "setup")},
		{[]string{"setup", "foo"}, contract.CodeInvalidArgument, msg.Text(msg.ErrSetupToolUnknown, "foo") + "\n\n" + HintText(msg.HintCommandHelp, "setup")},
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
	want := msg.Text(msg.ErrPluginElsewhere, "Claude Code", from, to) + "\n\n" + HintFor(msg.HintPluginElsewhere, "tool", "claude", "Claude Code") + "\n"
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

	if code, stdout, _ := setupFrom(t, a); code != contract.ExitOK || stdout != msg.Text(msg.SetupUnchanged, "Claude Code")+"\n" {
		t.Errorf("setup from the data root of the plugin: exit code %d, stdout %q", code, stdout)
	}
}

func TestSetupSwitch(t *testing.T) {
	a, b := twoRoots(t)
	from, to := filepath.Join(a, "integrations", "claude"), filepath.Join(b, "integrations", "claude")

	code, stdout, stderr := setupFrom(t, b, "--switch")
	if code != contract.ExitOK || stdout != msg.Text(msg.SetupSwitched, "Claude Code", from)+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := fakeMarketplace(t); got != to {
		t.Errorf("the plugin is served from %q, want %q", got, to)
	}
	if _, err := os.Stat(from); err != nil {
		t.Errorf("the plugin of the other data root must stay: %v", err)
	}

	if code, stdout, _ := setupFrom(t, b, "--switch"); code != contract.ExitOK || stdout != msg.Text(msg.SetupUnchanged, "Claude Code")+"\n" {
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
	if code != contract.ExitOK || stdout != msg.Text(msg.SetupSwitched, "Claude Code", from)+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got, want := fakeMarketplace(t), filepath.Join(b, "integrations", "claude"); got != want {
		t.Errorf("the plugin is served from %q, want %q", got, want)
	}
}

func TestSetupSwitchFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
	if code, stdout, _ := setupFrom(t, root, "--switch"); code != contract.ExitOK || stdout != msg.Text(msg.SetupInstalled, "Claude Code")+"\n" {
		t.Errorf("--switch on the first setup: exit code %d, stdout %q", code, stdout)
	}
}

func TestSetupSwitchKeepsDisabled(t *testing.T) {
	a, b := twoRoots(t)
	if out, err := exec.Command(os.Getenv(claude.ProgramEnv), "plugin", "disable", "gentry@gentry").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	code, stdout, _ := setupFrom(t, b, "--switch")
	want := msg.Text(msg.SetupSwitched, "Claude Code", filepath.Join(a, "integrations", "claude")) + "\n\n" + msg.Text(msg.SetupDisabled, "Claude Code") + "\n\n" + msg.Text(msg.HintPluginEnable) + "\n"
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
	if code, stdout, stderr := setupFrom(t, b); code != contract.ExitOK || stdout != msg.Text(msg.SetupInstalled, "Claude Code")+"\n" {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// Setup stays quiet about successful layouts, including a project with no
// worktrees. The unchanged plugin has no layout in its output.
func TestSetupQuietLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	fake := filepath.Join(root, "fakeclaude-state.json")
	t.Setenv("FAKECLAUDE_STATE", fake)
	t.Setenv(claude.ConfigDirEnv, filepath.Join(root, "claude-config"))
	// An older plugin is served: setup updates it.
	older := func() {
		t.Helper()
		var s map[string]any
		b, err := os.ReadFile(fake)
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(b, &s)
		for _, p := range s["plugins"].([]any) {
			p.(map[string]any)["version"] = "0.0.0+00000000"
		}
		b, _ = json.Marshal(s)
		os.WriteFile(fake, b, 0o644)
	}
	run("setup", "claude")
	older()
	// Without a project there is nothing to lay out.
	if _, stdout, _ := run("setup", "claude"); !strings.HasPrefix(stdout, msg.Text(msg.SetupUpdated, "Claude Code")+"\n") ||
		strings.Contains(stdout, msg.Text(msg.HintAgentsDeployAll)) {
		t.Errorf("updated without a project:\n%s", stdout)
	}
	path, _ := state.Path()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(func(tx *state.Tx) error {
		return tx.AddProject(state.Project{ID: "shop", Prefix: "SHOP", Knowledge: filepath.Join(root, "shop-knowledge")})
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	older()
	if code, stdout, stderr := run("setup", "claude"); code != contract.ExitOK || stderr != "" || stdout != msg.Text(msg.SetupUpdated, "Claude Code")+"\n" {
		t.Errorf("updated with a project:\n%s", stdout)
	}
	older()
	if code, stdout, stderr := run("setup", "claude", "--json"); code != contract.ExitOK || stderr != "" {
		t.Fatalf("updated JSON: code=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	} else {
		validate(t, "schemas/setup.json", stdout)
		var out contract.SetupOutput
		if err := json.Unmarshal([]byte(stdout), &out); err != nil || len(out.Agents) != 1 || out.Agents[0].Project != "shop" || out.Agents[0].Changed || len(out.Agents[0].Worktrees) != 0 {
			t.Errorf("layout of empty project: %s (error %v)", stdout, err)
		}
	}
	if _, stdout, _ := run("setup", "claude"); stdout != msg.Text(msg.SetupUnchanged, "Claude Code")+"\n" {
		t.Errorf("unchanged with a project:\n%s", stdout)
	}
}

func TestSetupClaudeRollbackDirectory(t *testing.T) {
	for _, fail := range []string{"marketplace-add", "install"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", fail, existing), func(t *testing.T) {
				root := t.TempDir()
				t.Setenv(home.EnvVar, root)
				t.Setenv(claude.ConfigDirEnv, filepath.Join(root, "claude-config"))
				t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
				t.Setenv("FAKECLAUDE_FAIL", fail)
				dir := filepath.Join(root, "integrations", "claude")
				if existing {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				_, bad := setupClaude(dir, false)
				if bad == nil {
					t.Fatal("setup succeeded; want the command failure")
				}
				_, err := os.Stat(dir)
				if existing && err != nil || !existing && !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("integration directory after failure: existing=%t, stat error=%v", existing, err)
				}
			})
		}
	}
}

func TestSetupProjectsUnavailable(t *testing.T) {
	for _, kind := range []string{"directory", "invalid database"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			path, err := state.Path()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				err = os.Mkdir(path, 0o755)
			} else {
				err = os.WriteFile(path, []byte("not a database"), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, cause := state.OpenRead(path)
			if cause == nil {
				t.Fatal("invalid state store was readable")
			}
			var unavailable *state.UnavailableError
			if errors.As(cause, &unavailable) {
				cause = unavailable.Err
			}
			projects := &setupProjects{}
			defer projects.close()
			_, err = projects.List()
			if err == nil {
				t.Fatal("invalid state store was listed")
			}
			bad := failureOf(err)
			if bad.code != contract.CodeStateUnavailable || bad.details["path"] != path ||
				bad.message != msg.Text(msg.ErrStateUnavailable, cause) || len(bad.hints) != 1 {
				t.Errorf("state error lost cause, path or hints: %+v", bad)
			}
		})
	}
}

func TestSetupConflictText(t *testing.T) {
	conflicts := []contract.AgentsConflict{
		{Worktree: "/admin/A", Agent: "reviewer", File: ".claude/agents/reviewer.md", Reason: "tracked"},
		{Worktree: "/shop/B", Agent: "coder", File: ".claude/agents/coder.md", Reason: "foreign"},
	}
	for _, tool := range []string{"claude", "codex"} {
		out := contract.SetupOutput{Tool: tool, Action: "updated", Permission: ptr(claude.PermissionPresent)}
		out.Agents = []contract.UpdateAgents{
			{Project: "admin", Conflicts: conflicts[:1]},
			{Project: "shop", Conflicts: conflicts[1:]},
		}
		block := lines(msg.Text(msg.SetupUpdated, toolTitles[tool]), "", msg.Text(msg.SetupDisabled, toolTitles[tool]), "", msg.Text(msg.AgentsConflictWarning), "")
		block += render(cliChannel, func(p *page) { writeConflicts(&p.Builder, conflicts) })
		enable := msg.Text(msg.HintPluginEnable)
		if tool == "codex" {
			enable = msg.Text(msg.HintCodexPluginEnable)
		}
		cli := block + lines("", enable,
			hintLineOf(msg.HintAgentsConflictWarning, "gentry agents deploy --project admin"),
			hintLineOf(msg.HintAgentsConflictWarning, "gentry agents deploy --project shop"))
		agent := block + lines("", enable,
			hintLineOf(msg.HintAgentsConflictWarning, "agents_deploy (project: admin)"),
			hintLineOf(msg.HintAgentsConflictWarning, "agents_deploy (project: shop)"))
		wantText(t, tool, func(p *page) { setupText(p, out, setupExtra{}) }, cli, agent)
		out.Enabled = true
		out.Agents[0].Conflicts, out.Agents[1].Conflicts = nil, nil
		wantText(t, tool+" successful layout", func(p *page) { setupText(p, out, setupExtra{}) }, lines(msg.Text(msg.SetupUpdated, toolTitles[tool])), "")
	}
}
