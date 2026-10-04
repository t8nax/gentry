package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

func TestSetupClaude(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	dir := filepath.Join(root, "integrations", "claude")

	code, stdout, stderr := run("setup", "claude")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if want := msg.Text(msg.SetupClaudeReady, dir) + "\n"; stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
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

	_, stdout, _ = run("setup", "claude", "--json")
	validate(t, "schemas/setup.json", stdout)
	var first contract.SetupOutput
	json.Unmarshal([]byte(stdout), &first)
	if first.Tool != "claude" || first.Dir != dir || first.PluginVersion == "" {
		t.Errorf("unexpected output %+v", first)
	}
	_, stdout, _ = run("setup", "claude", "--json")
	var second contract.SetupOutput
	json.Unmarshal([]byte(stdout), &second)
	if second.PluginVersion != first.PluginVersion {
		t.Errorf("plugin version changed without a content change: %q, then %q", first.PluginVersion, second.PluginVersion)
	}
}

func TestSetupErrors(t *testing.T) {
	tests := []struct {
		args   []string
		code   string
		stderr string
	}{
		{[]string{"setup"}, contract.CodeMissingArgument, msg.Text(msg.ErrSetupToolMissing) + " " + msg.Text(msg.HintSetupTools, "claude")},
		{[]string{"setup", "foo"}, contract.CodeInvalidArgument, msg.Text(msg.ErrSetupToolUnknown, "foo") + " " + msg.Text(msg.HintSetupTools, "claude")},
		{[]string{"setup", "claude", "extra"}, contract.CodeUnexpectedArgs, msg.Text(msg.ErrExtraArgs, "setup", "extra")},
	}
	for _, tt := range tests {
		exit, _, stderr := run(tt.args...)
		if exit != contract.ExitUsage || stderr != "gentry: "+tt.stderr+"\n" {
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
