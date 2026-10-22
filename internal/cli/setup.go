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
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
)

// setupSwitched is the action of a setup that moved the plugin from the
// directory of another data root.
const setupSwitched = "switched"

// setupUnchanged is the action of a setup that left the plugin as it was,
// the same for every tool.
const setupUnchanged = claude.Unchanged

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

	out, x, bad := setupTool(f.args[0], *switchFlag)
	if bad != nil {
		return fail(env, *bad)
	}
	// The subagents laid out name the tools of the previous plugin: once it
	// changes, the operator lays them out anew in each project. setup does
	// not do it itself.
	x.projects = connectedProjects()
	return emit(env, out, func(p *page, out contract.SetupOutput) { setupText(p, out, x) })
}

// setupTool builds the integration with the tool in the data root and
// registers it in the tool, as gentry setup does; gentry update builds the
// integrations anew by it. The plugin is moved from an existing directory of
// another data root only if switchDir is set.
func setupTool(tool string, switchDir bool) (contract.SetupOutput, setupExtra, *failure) {
	dir, err := home.Integration(tool)
	if err != nil {
		f := homeUnknown()
		return contract.SetupOutput{}, setupExtra{}, &f
	}
	if tool == codex.Tool {
		return setupCodex(dir, switchDir)
	}
	out, bad := setupClaude(dir, switchDir)
	return out, setupExtra{}, bad
}

// setupClaude builds the integration with Claude Code in dir and registers
// it in Claude Code.
func setupClaude(dir string, switchDir bool) (contract.SetupOutput, *failure) {
	refuse := func(f failure) (contract.SetupOutput, *failure) { return contract.SetupOutput{}, &f }
	program, err := claude.Program()
	if err != nil {
		return refuse(toolNotFound(claude.Tool, "claude", toolTitles[claude.Tool]))
	}
	// The plugin serves every session of the tool, whatever the data root of
	// this call: it is moved from an existing directory of another data root
	// only by --switch, and before anything is written.
	registered, err := claude.Registered(program)
	if err != nil {
		return refuse(setupToolError(err))
	}
	previous := ""
	if registered != "" && !claude.SamePath(registered, dir) {
		if _, err := os.Stat(registered); !errors.Is(err, fs.ErrNotExist) && !switchDir {
			return refuse(pluginElsewhere(claude.Tool, registered, dir))
		}
		previous = registered
	}
	exe, err := executable()
	if err != nil {
		return refuse(internal(err))
	}
	p, err := claude.Build(integration.Gentry(exe), buildinfo.Version())
	if err != nil {
		return refuse(internal(err))
	}
	if err := claude.Write(dir, p); err != nil {
		return refuse(ioError(dir, err))
	}
	// Once written, dir may turn out to be the directory registered, reached
	// by another path: then nothing is switched.
	if previous != "" && claude.SamePath(previous, dir) {
		previous = ""
	}
	r, err := claude.Register(program, dir, p.Version)
	if err != nil {
		return refuse(setupToolError(err))
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
	return out, nil
}

// setupCodex builds the integration with Codex in dir, installs it in Codex
// and sets Codex up for Gentry, the worktrees of the pools trusted.
func setupCodex(dir string, switchDir bool) (contract.SetupOutput, setupExtra, *failure) {
	refuse := func(f failure) (contract.SetupOutput, setupExtra, *failure) {
		return contract.SetupOutput{}, setupExtra{}, &f
	}
	root, err := home.Root()
	if err != nil {
		return refuse(homeUnknown())
	}
	exe, err := executable()
	if err != nil {
		return refuse(internal(err))
	}
	res, err := codex.Setup(codex.SetupRequest{Dir: dir, Root: root, Exe: exe, Version: buildinfo.Version(), Switch: switchDir, Worktrees: poolPaths()})
	if err != nil {
		return refuse(codexFailure(err, dir))
	}
	return codexOutput(dir, res), codexExtra(res), nil
}

// poolPaths returns the worktrees of the pools, trusted in Codex. The state
// store is only read, and not created; a store that cannot be read is told
// by the commands of the projects, not by setup: then no worktree is
// trusted.
func poolPaths() []string {
	_, worktrees, bad := readPool()
	if bad != nil {
		return nil
	}
	var paths []string
	for _, w := range worktrees {
		paths = append(paths, w.Path)
	}
	return paths
}

// codexFailure turns a refusal of the setup of Codex into a failure.
func codexFailure(err error, dir string) failure {
	var se *codex.SettingsError
	var ee *codex.ElsewhereError
	var ce *codex.CommandError
	var pe *fs.PathError
	switch {
	case errors.As(err, &se):
		return settingsInvalid(codex.Tool, se.Path, se.Reason)
	case errors.Is(err, codex.ErrNotFound):
		return toolNotFound(codex.Tool, "codex", toolTitles[codex.Tool])
	case errors.As(err, &ee):
		return pluginElsewhere(codex.Tool, ee.Registered, dir)
	case errors.As(err, &ce):
		return toolFailed(codex.Tool, ce.Command, ce.Output)
	case errors.As(err, &pe):
		return ioError(pe.Path, pe.Err)
	}
	return internal(err)
}

// codexOutput is the integration with Codex as the contract has it.
func codexOutput(dir string, r codex.SetupResult) contract.SetupOutput {
	out := contract.SetupOutput{
		Tool: codex.Tool, Dir: dir, PluginVersion: r.PluginVersion, Action: r.Action, Enabled: r.Enabled,
		Settings: &r.Settings, HooksFile: &r.HooksFile, HookTrust: &r.HookTrust, HooksFeature: &r.HooksFeature, Sandbox: &r.Sandbox,
		WorktreesTrusted: append([]string{}, r.Trust.Trusted...), WorktreesDistrusted: []string{},
	}
	for _, d := range r.Trust.Distrusted {
		out.WorktreesDistrusted = append(out.WorktreesDistrusted, d.Path)
	}
	if r.HookError != "" {
		out.HookError = &r.HookError
	}
	if r.Previous != "" {
		out.Action, out.PreviousDir = setupSwitched, &r.Previous
	}
	return out
}

// codexExtra is what the text of setup codex has apart from the contract:
// the records of trust of the worktrees the operator did not trust.
func codexExtra(r codex.SetupResult) setupExtra {
	return setupExtra{distrustKeys: distrustKeys(r.Trust.Distrusted)}
}

// distrustKeys returns the keys of the records of trust of the worktrees, each
// once, in the order of the worktrees.
func distrustKeys(list []codex.Distrusted) []string {
	var keys []string
	for _, d := range list {
		if !slices.Contains(keys, d.Key) {
			keys = append(keys, d.Key)
		}
	}
	return keys
}

// setupExtra is what the text of setup has apart from the contract: whether
// a project is connected, whose worktrees have subagents laid out, and the
// records of trust in Codex the operator did not trust, which the hints
// name.
type setupExtra struct {
	projects     bool
	distrustKeys []string
}

// connectedProjects reports whether a project is connected on the machine.
// The state store is only read, and not created; a store that cannot be read
// is told by the commands of the projects, not by setup.
func connectedProjects() bool {
	st, bad := openTasks()
	if bad != nil || st == nil {
		return false
	}
	defer st.Close()
	projects, err := st.Projects()
	return err == nil && len(projects) > 0
}

// pluginChanged reports whether setup installed, updated or switched the
// plugin: then the subagents laid out are laid out anew.
func pluginChanged(out contract.SetupOutput) bool {
	return out.Action != setupUnchanged || out.PreviousDir != nil
}

// setupText prints what setup did with the plugin, in the words of the tool.
// Once the plugin is installed, updated or switched, the last hint is to lay
// out the subagents in each project, if there is one.
func setupText(p *page, out contract.SetupOutput, x setupExtra) {
	title := toolTitles[out.Tool]
	if out.PreviousDir != nil {
		fmt.Fprintln(p, msg.Text(msg.SetupSwitched, title, *out.PreviousDir))
	} else {
		fmt.Fprintln(p, msg.Text(setupMessages[out.Action], title))
	}
	if out.Tool == codex.Tool {
		codexSetupText(p, out, x)
		return
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
		fmt.Fprintln(p, msg.Text(msg.SetupDisabled, title))
		hints = append(hints, hintOf(msg.HintPluginEnable))
	}
	if permission == claude.PermissionFailed || permission == claude.PermissionUnwritten {
		hints = append(hints, hintOf(msg.HintSetupPermission, claude.PermissionRule))
	}
	if x.projects && pluginChanged(out) {
		hints = append(hints, hintOf(msg.HintAgentsDeployAll))
	}
	p.hints(hints...)
}

// codexSetupText prints, after the result, what the operator does by hand
// in Codex, each warning a block of its own, then the hints. What setup set
// in Codex is told by --json only.
func codexSetupText(p *page, out contract.SetupOutput, x setupExtra) {
	block := func(lines ...string) {
		p.WriteString("\n")
		for _, l := range lines {
			fmt.Fprintln(p, l)
		}
	}
	var hints []hint
	if !out.Enabled {
		block(msg.Text(msg.SetupDisabled, toolTitles[codex.Tool]))
		hints = append(hints, hintOf(msg.HintCodexPluginEnable))
	}
	switch str(out.HookTrust) {
	case codex.HookUntrusted:
		block(msg.Text(msg.SetupCodexHookUntrusted), msg.Text(msg.SetupCodexHookReason, str(out.HookError)))
		hints = append(hints, hintOf(msg.HintCodexHookTrust))
	case codex.HookFailed, codex.HookUnwritten:
		k := msg.SetupCodexHooksUnread
		if str(out.HookTrust) == codex.HookUnwritten {
			k = msg.SetupCodexHooksUnwritten
		}
		block(msg.Text(k), msg.Text(msg.SetupCodexHooksFile, str(out.HooksFile)))
		hints = append(hints, hintOf(msg.HintCodexHooksFix).set("tool", codex.Tool))
	}
	if str(out.HooksFeature) == codex.HooksManagedOff {
		block(msg.Text(msg.SetupCodexManagedHooks))
	}
	if str(out.Sandbox) == codex.SandboxProfile {
		block(msg.Text(msg.SetupCodexProfile))
	}
	if len(out.WorktreesDistrusted) > 0 {
		p.WriteString("\n")
		writeList(&p.Builder, msg.Text(msg.CodexDistrusted), out.WorktreesDistrusted)
		for _, k := range x.distrustKeys {
			hints = append(hints, hintOf(msg.HintCodexTrust, codex.KeyText(k)))
		}
	}
	if x.projects && pluginChanged(out) {
		hints = append(hints, hintOf(msg.HintAgentsDeployAll))
	}
	p.hints(hints...)
}

// str is the value of an optional string of the contract; "" if absent.
func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
