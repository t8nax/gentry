package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
)

// tools lists the tools `gentry setup` supports.
var tools = []string{claude.Tool}

var setupMessages = map[string]msg.Key{
	claude.Installed: msg.SetupInstalled,
	claude.Updated:   msg.SetupUpdated,
	claude.Unchanged: msg.SetupUnchanged,
}

func runSetup(args []string, env Env) int {
	f := newFlags("setup")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	list := strings.Join(tools, ", ")
	switch {
	case len(f.args) == 0:
		return fail(env, missingArgument("setup", "tool", msg.Text(msg.ErrSetupToolMissing), msg.Text(msg.HintSetupTools, list)))
	case !slices.Contains(tools, f.args[0]):
		t := f.args[0]
		return fail(env, invalidArgument("setup", "tool", t, msg.Text(msg.ErrSetupToolUnknown, t), msg.Text(msg.HintSetupTools, list)))
	case len(f.args) > 1:
		return fail(env, extraArgs("setup", f.args[1:]))
	}

	dir, err := home.Integration(claude.Tool)
	if err != nil {
		return fail(env, homeUnknown())
	}
	exe, err := executable()
	if err != nil {
		return fail(env, internal(err))
	}
	p, err := claude.Build(integration.Gentry(exe, msg.Text(msg.HelpIntro)), buildinfo.Version())
	if err != nil {
		return fail(env, internal(err))
	}
	if err := claude.Write(dir, p); err != nil {
		return fail(env, ioError(dir, err))
	}
	program, err := claude.Program()
	if err != nil {
		return fail(env, toolNotFound(claude.Tool, "claude", "Claude Code"))
	}
	r, err := claude.Register(program, dir, p.Version)
	var ce *claude.CommandError
	if errors.As(err, &ce) {
		return fail(env, toolFailed(claude.Tool, "Claude Code", ce.Command, ce.Output))
	}
	if err != nil {
		return fail(env, internal(err))
	}

	out := contract.SetupOutput{Tool: claude.Tool, Dir: dir, PluginVersion: p.Version, Action: r.Action, Enabled: r.Enabled}
	if *asJSON {
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(setupMessages[r.Action]))
	if !r.Enabled {
		fmt.Fprintln(env.Stdout, msg.Text(msg.SetupDisabled))
	}
	return contract.ExitOK
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
