package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
)

// tools lists the tools `gentry setup` supports.
var tools = []string{claude.Tool}

// setupSwitched is the action of a setup that moved the plugin from the
// directory of another data root.
const setupSwitched = "switched"

var setupMessages = map[string]msg.Key{
	claude.Installed: msg.SetupInstalled,
	claude.Updated:   msg.SetupUpdated,
	claude.Unchanged: msg.SetupUnchanged,
}

func runSetup(args []string, env Env) int {
	f := newFlags("setup")
	switchFlag := f.Bool("switch")
	if code, done := f.parse(args, env); done {
		return code
	}
	switch {
	case len(f.args) == 0:
		return fail(env, missingArgument("setup", "tool", msg.Text(msg.ErrSetupToolMissing), helpHint(msg.HintCommandHelp, "setup")))
	case !slices.Contains(tools, f.args[0]):
		t := f.args[0]
		return fail(env, invalidArgument("setup", "tool", t, msg.Text(msg.ErrSetupToolUnknown, t), helpHint(msg.HintCommandHelp, "setup")))
	}

	dir, err := home.Integration(claude.Tool)
	if err != nil {
		return fail(env, homeUnknown())
	}
	program, err := claude.Program()
	if err != nil {
		return fail(env, toolNotFound(claude.Tool, "claude", "Claude Code"))
	}
	// The plugin serves every session of the tool, whatever the data root of
	// this call: it is moved from an existing directory of another data root
	// only by --switch, and before anything is written.
	registered, err := claude.Registered(program)
	if err != nil {
		return fail(env, setupToolError(err))
	}
	previous := ""
	if registered != "" && !claude.SamePath(registered, dir) {
		if _, err := os.Stat(registered); !errors.Is(err, fs.ErrNotExist) && !*switchFlag {
			return fail(env, pluginElsewhere(claude.Tool, registered, dir))
		}
		previous = registered
	}
	exe, err := executable()
	if err != nil {
		return fail(env, internal(err))
	}
	p, err := claude.Build(integration.Gentry(exe), buildinfo.Version())
	if err != nil {
		return fail(env, internal(err))
	}
	if err := claude.Write(dir, p); err != nil {
		return fail(env, ioError(dir, err))
	}
	// Once written, dir may turn out to be the directory registered, reached
	// by another path: then nothing is switched.
	if previous != "" && claude.SamePath(previous, dir) {
		previous = ""
	}
	r, err := claude.Register(program, dir, p.Version)
	if err != nil {
		return fail(env, setupToolError(err))
	}

	// The tools of the agent are allowed once for every session.
	settings, err := claude.SettingsPath()
	permission := claude.PermissionFailed
	if err == nil {
		permission = claude.AllowTools(settings)
	}
	removeCallMarks()

	out := contract.SetupOutput{Tool: claude.Tool, Dir: dir, PluginVersion: p.Version, Action: r.Action, Enabled: r.Enabled, Permission: &permission}
	if settings != "" {
		out.Settings = &settings
	}
	if previous != "" {
		out.Action, out.PreviousDir = setupSwitched, &previous
	}
	return emit(env, out, setupText)
}

// setupText prints what setup did with the plugin, the rule of permission
// in the settings of the user and whether the plugin is enabled.
func setupText(p *page, out contract.SetupOutput) {
	if out.PreviousDir != nil {
		fmt.Fprintln(p, msg.Text(msg.SetupSwitched, *out.PreviousDir))
	} else {
		fmt.Fprintln(p, msg.Text(setupMessages[out.Action]))
	}
	// A message after a field line, such as the previous directory of a
	// switch, is kept apart from it by a blank line.
	afterField := out.PreviousDir != nil
	permission := *out.Permission
	if permission != claude.PermissionPresent {
		if afterField {
			fmt.Fprintln(p)
		}
		k := msg.SetupPermissionAdded
		switch permission {
		case claude.PermissionFailed:
			k = msg.SetupPermissionFailed
		case claude.PermissionUnwritten:
			k = msg.SetupPermissionUnwritten
		}
		fmt.Fprintln(p, msg.Text(k))
		if out.Settings != nil {
			fmt.Fprintln(p, msg.Text(msg.SetupSettings, *out.Settings))
			afterField = true
		}
	}
	var hints []hint
	if !out.Enabled {
		if afterField {
			fmt.Fprintln(p)
		}
		fmt.Fprintln(p, msg.Text(msg.SetupDisabled))
		hints = append(hints, hintOf(msg.HintPluginEnable))
	}
	if permission == claude.PermissionFailed || permission == claude.PermissionUnwritten {
		hints = append(hints, hintOf(msg.HintSetupPermission, claude.PermissionRule))
	}
	p.hints(hints...)
}

// removeCallMarks removes the marks of calls the hooks of an earlier Gentry
// left in the data root: the source of a record no longer needs them.
func removeCallMarks() {
	if root, err := home.Root(); err == nil {
		os.RemoveAll(filepath.Join(root, "state", "calls"))
	}
}

// setupToolError reports a failure of the tool program as tool_failed.
func setupToolError(err error) failure {
	var ce *claude.CommandError
	if errors.As(err, &ce) {
		return toolFailed(claude.Tool, ce.Command, ce.Output)
	}
	return internal(err)
}

// executable returns the absolute path of the running gentry, with symbolic
// links resolved, for the hook command.
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
