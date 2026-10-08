package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	dir := filepath.Join(root, "integrations", "claude")

	code, stdout, stderr := run("setup", "claude")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if want := msg.Text(msg.SetupInstalled) + "\n"; stdout != want {
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
	if out.Tool != "claude" || out.Dir != dir || out.PluginVersion == "" || out.Action != "unchanged" || !out.Enabled {
		t.Errorf("unexpected output %+v", out)
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
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("FAKECLAUDE_STATE", filepath.Join(root, "fakeclaude-state.json"))
	t.Setenv("FAKECLAUDE_FAIL", "install")
	code, stdout, _ := run("setup", "claude", "--json")
	validate(t, "schemas/error.json", stdout)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(stdout), &e)
	if code != contract.ExitError || e.Error.Code != contract.CodeToolFailed {
		t.Fatalf("exit code %d, output %s", code, stdout)
	}
	if e.Error.Details["command"] != "claude plugin install gentry@gentry --scope user --json" || e.Error.Details["output"] != "simulated failure" {
		t.Errorf("unexpected details %v", e.Error.Details)
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
