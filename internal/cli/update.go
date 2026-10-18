package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/update"
)

// The actions of gentry update.
const (
	updateUpdated = "updated" // the program of the latest release took the place of the program
	updateCurrent = "current" // the latest release was installed already
)

// runUpdate installs the latest release of Gentry in place of the program of
// the data root. Two programs do it. The program the operator runs checks
// what it can without the network — where it runs, its version, the tools
// that serve the plugin of another data root — then finds the latest release,
// downloads it into a temporary folder, checks its sum and replaces itself.
// Then it runs the new program, gentry update with update.FromEnv set, which
// builds the integrations anew, lays out the subagents and tells the result:
// the plugin, the subagents and the texts are those of the new program. A
// failure before the program is replaced leaves the installation as it was.
//
// With the latest release installed the program itself checks the
// integrations, builds anew those that do not match it and lays out the
// subagents if it did.
func runUpdate(args []string, env Env) int {
	f := newFlags("update")
	if code, done := f.parse(args, env); done {
		return code
	}
	if from := os.Getenv(update.FromEnv); update.IsRelease(from) {
		return updated(env, from)
	}

	installed, bad := installedProgram()
	if bad != nil {
		return fail(env, *bad)
	}
	exe, err := executable()
	if err != nil {
		return fail(env, internal(err))
	}
	resolved, err := filepath.EvalSymlinks(installed)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fail(env, programElsewhere(exe, ""))
	case err != nil || !paths.Same(resolved, exe):
		return fail(env, programElsewhere(exe, installed))
	}

	// Two updates at once do not replace the program together: the second
	// waits for the first, and then the program installed may be another.
	ctx := env.context()
	lock, err := update.Lock(ctx, installed)
	if err != nil {
		return fail(env, updateFailure(err))
	}
	own := buildinfo.Version()
	if v, ok := versionOf(installed); ok && v != own {
		lock.Release()
		return runInstalled(env, installed, nil, args)
	}
	defer lock.Release()

	if !update.IsRelease(own) {
		return fail(env, notRelease(own))
	}
	if bad := pluginElsewhereCheck(); bad != nil {
		return fail(env, *bad)
	}
	c := update.New()
	latest, err := c.Latest(ctx)
	if err != nil {
		return fail(env, updateFailure(err))
	}
	switch update.Compare(own, latest) {
	case 1:
		return fail(env, newerThanRelease(own, latest))
	case 0:
		return current(env, installed, own)
	}

	tmp, err := os.MkdirTemp("", "gentry-update-")
	if err != nil {
		return fail(env, internal(err))
	}
	defer os.RemoveAll(tmp)
	downloaded, err := c.Download(ctx, latest, update.Asset(runtime.GOOS, runtime.GOARCH), tmp)
	if err != nil {
		return fail(env, updateFailure(err))
	}
	update.RemoveOld(installed)
	if err := update.Replace(downloaded, installed); err != nil {
		return fail(env, updateFailure(err))
	}
	// The lock is held until the new program has done its part.
	return runInstalled(env, installed, []string{update.FromEnv + "=" + own}, args)
}

// installedProgram returns the path of the program of the data root:
// <data root>/bin/gentry, gentry.exe on Windows.
func installedProgram() (string, *failure) {
	root, err := home.Root()
	if err != nil {
		f := homeUnknown()
		return "", &f
	}
	return filepath.Join(root, "bin", update.Program()), nil
}

// versionTimeout limits the run of the program installed that names its
// version: the update holds the lock meanwhile, and a program that hangs
// must not stop every update.
const versionTimeout = 10 * time.Second

// versionOf returns the version the program names; false if it does not
// run in versionTimeout.
func versionOf(program string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, program, "version", "--json").Output()
	if err != nil {
		return "", false
	}
	var v contract.VersionOutput
	if json.Unmarshal(b, &v) != nil || v.Gentry == "" {
		return "", false
	}
	return v.Gentry, true
}

// runInstalled runs gentry update of the program with args and the
// variables env adds, on the output of env, and returns its exit code.
func runInstalled(env Env, program string, vars, args []string) int {
	cmd := exec.CommandContext(env.context(), program, append([]string{"update"}, args...)...)
	cmd.Env = append(os.Environ(), vars...)
	cmd.Stdout, cmd.Stderr = env.Stdout, env.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		return fail(env, internal(err))
	}
	return contract.ExitOK
}

// pluginElsewhereCheck refuses the update if Claude Code serves the plugin of
// Gentry from an existing directory of another data root, as setup does,
// though this data root has no integration of it: the update would move the
// plugin of the other one. With no claude there is nothing to check; claude
// that fails refuses only if the integration of this data root is connected.
func pluginElsewhereCheck() *failure {
	dir, err := home.Integration(claude.Tool)
	if err != nil {
		f := homeUnknown()
		return &f
	}
	program, err := claude.Program()
	if err != nil {
		return nil
	}
	registered, err := claude.Registered(program)
	if err != nil {
		if connected(dir) {
			f := setupToolError(err)
			return &f
		}
		return nil
	}
	if registered == "" || claude.SamePath(registered, dir) {
		return nil
	}
	if _, err := os.Stat(registered); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	f := pluginElsewhere(claude.Tool, registered, dir)
	return &f
}

// connected reports whether the integration of the directory dir is
// connected: setup made it.
func connected(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// An updateRun is what an update did after the program, with what failed.
type updateRun struct {
	out          contract.UpdateOutput
	connected    bool // an integration is connected, built anew or not
	integrations []integrationFailure
	agents       []agentsFailure
	bad          *failure // the state store could not be read for the layout
}

// integrationFailure is an integration that could not be built anew.
type integrationFailure struct {
	tool   string
	reason string
}

// agentsFailure is a project whose subagents could not be laid out.
type agentsFailure struct {
	project string
	reason  string
}

// updated finishes the update in the new program, which the program of the
// version from has installed: it builds anew every integration connected,
// lays out the subagents and tells the update with the incompatible changes
// of the versions it skipped.
func updated(env Env, from string) int {
	installed, bad := installedProgram()
	if bad != nil {
		return fail(env, *bad)
	}
	own := buildinfo.Version()
	page := update.Page(own)
	run := updateRun{out: contract.UpdateOutput{
		Action: updateUpdated, Previous: &from, Version: own, Program: installed, Page: &page,
		Integrations: []contract.SetupOutput{},
	}}
	for _, tool := range tools {
		dir, err := home.Integration(tool)
		if err != nil || !connected(dir) {
			continue
		}
		run.connected = true
		run.setup(tool)
	}
	run.layout()
	journal, err := update.New().Changelog(env.context(), own)
	if err != nil {
		e := err.Error()
		run.out.ChangelogError = &e
	} else {
		for _, b := range update.BreakingOf(journal, from, own) {
			run.out.Breaking = append(run.out.Breaking, contract.UpdateBreaking{Version: b.Version, Lines: b.Lines})
		}
	}
	return run.emit(env)
}

// current checks the integrations when the latest release is installed:
// one that matches the program is left as it is, nothing written; another is
// built anew, and then the subagents are laid out.
func current(env Env, installed, own string) int {
	run := updateRun{out: contract.UpdateOutput{Action: updateCurrent, Version: own, Program: installed, Integrations: []contract.SetupOutput{}}}
	rebuilt := false
	for _, tool := range tools {
		dir, err := home.Integration(tool)
		if err != nil || !connected(dir) {
			continue
		}
		run.connected = true
		if out, ok := currentIntegration(tool, dir, own); ok {
			run.out.Integrations = append(run.out.Integrations, out)
			continue
		}
		if out, ok := run.setup(tool); ok && out.Action != claude.Unchanged {
			rebuilt = true
		}
	}
	if rebuilt {
		run.layout()
	}
	return run.emit(env)
}

// currentIntegration returns the integration with the tool in dir if the
// tool serves the plugin this program builds from it already: then nothing
// is written.
func currentIntegration(tool, dir, own string) (contract.SetupOutput, bool) {
	program, err := claude.Program()
	if err != nil {
		return contract.SetupOutput{}, false
	}
	exe, err := executable()
	if err != nil {
		return contract.SetupOutput{}, false
	}
	p, err := claude.Build(integration.Gentry(exe), own)
	if err != nil {
		return contract.SetupOutput{}, false
	}
	r, ok, err := claude.Current(program, dir, p.Version)
	if err != nil || !ok {
		return contract.SetupOutput{}, false
	}
	return contract.SetupOutput{Tool: tool, Dir: dir, PluginVersion: p.Version, Action: r.Action, Enabled: r.Enabled}, true
}

// setup builds the integration with the tool anew, as gentry setup does,
// and returns it; false if it failed, which does not stop the others.
func (u *updateRun) setup(tool string) (contract.SetupOutput, bool) {
	out, bad := setupTool(tool, false)
	if bad != nil {
		u.integrations = append(u.integrations, integrationFailure{tool: tool, reason: bad.message})
		return out, false
	}
	u.out.Integrations = append(u.out.Integrations, out)
	return out, true
}

// layout lays out the subagents in every worktree of each project, in the
// order of gentry project list, as gentry agents deploy --project does, but
// a conflict skips the subagent and is told; a project that fails does not
// stop the others. The state store is only read, and not created.
func (u *updateRun) layout() {
	p, bad := readLayoutPool()
	if bad != nil {
		u.bad = bad
		return
	}
	defer p.close()
	if p.st == nil {
		return
	}
	projects, err := p.st.Projects()
	if err != nil {
		f := stateFailure(err)
		u.bad = &f
		return
	}
	if len(projects) == 0 {
		return
	}
	r, bad := openProcess()
	if bad != nil {
		for _, pr := range projects {
			u.agents = append(u.agents, agentsFailure{project: pr.ID, reason: bad.message})
		}
		return
	}
	defer r.Close()
	for _, pr := range projects {
		d, bad := deployOf(r, p, pr.ID)
		if bad != nil {
			u.agents = append(u.agents, agentsFailure{project: pr.ID, reason: bad.message})
			continue
		}
		res, err := agents.Sync(agentsLayout, d.targets, p.paths())
		if err != nil {
			u.agents = append(u.agents, agentsFailure{project: pr.ID, reason: layoutFailure(err).message})
			continue
		}
		out := d.output(res, p)
		a := contract.UpdateAgents{Project: out.Project, Changed: out.Changed, Worktrees: out.Worktrees}
		if len(res.Conflicts) > 0 {
			a.Conflicts = conflictsJSON(res.Conflicts)
		}
		u.out.Agents = append(u.out.Agents, a)
	}
}

// emit prints the result of the update, then what failed after the program
// was replaced. In JSON a failure is the output, with the result of the
// update in its details.
func (u *updateRun) emit(env Env) int {
	bad := u.failure()
	if bad == nil {
		return emit(env, u.out, func(p *page, out contract.UpdateOutput) { updateText(p, out, u.extra()) })
	}
	if !env.json {
		p := &page{ch: channelOf(env)}
		updateText(p, u.out, u.extra())
		fmt.Fprint(env.Stdout, p.String())
	}
	bad.details["update"] = u.out
	return fail(env, *bad)
}

// failure is what failed after the program was replaced: the integrations,
// then the subagents; nil if nothing did.
func (u *updateRun) failure() *failure {
	var parts []failure
	if len(u.integrations) > 0 {
		parts = append(parts, integrationsFailed(u.integrations))
	}
	if len(u.agents) > 0 {
		parts = append(parts, agentsFailed(u.agents))
	}
	if u.bad != nil {
		parts = append(parts, *u.bad)
	}
	if len(parts) == 0 {
		return nil
	}
	f := parts[0]
	if f.details == nil {
		f.details = map[string]any{}
	}
	// The failures after the first are told under it, each a block.
	for _, more := range parts[1:] {
		f.more += "\n" + more.message + "\n" + more.more
		f.hints = append(f.hints, more.hints...)
		for k, v := range more.details {
			if _, ok := f.details[k]; !ok {
				f.details[k] = v
			}
		}
	}
	return &f
}

// updateExtra is what the text of update has apart from the contract:
// whether an integration is connected, though it could not be built anew.
type updateExtra struct {
	connected bool
}

func (u *updateRun) extra() updateExtra { return updateExtra{connected: u.connected} }

// updateText prints the result of the update: the versions and whether the
// changes take effect in new sessions, what needs the attention of the
// operator, the incompatible changes of the versions skipped and the page of
// the release. What was built anew and laid out is told by --json only.
func updateText(p *page, out contract.UpdateOutput, x updateExtra) {
	if out.Action == updateUpdated && out.Previous != nil {
		fmt.Fprintln(p, msg.Text(msg.UpdateUpdated, *out.Previous, out.Version))
	} else {
		fmt.Fprintln(p, msg.Text(msg.UpdateCurrent, out.Version))
	}
	changed := out.Action == updateUpdated || slices.ContainsFunc(out.Integrations, func(s contract.SetupOutput) bool { return s.Action != claude.Unchanged })
	if changed {
		fmt.Fprintln(p, msg.Text(msg.UpdateNewSessions))
	}
	var hints []hint
	if !x.connected {
		fmt.Fprintln(p, msg.Text(msg.UpdateNoIntegrations))
	}
	if slices.ContainsFunc(out.Integrations, func(s contract.SetupOutput) bool { return !s.Enabled }) {
		fmt.Fprintln(p, msg.Text(msg.SetupDisabled))
		hints = append(hints, hintOf(msg.HintPluginEnable))
	}

	var conflicts []contract.AgentsConflict
	for _, a := range out.Agents {
		if len(a.Conflicts) > 0 {
			conflicts = append(conflicts, a.Conflicts...)
			hints = append(hints, hintOf(msg.HintAgentsConflictWarning).forProject(a.Project))
		}
	}
	if len(conflicts) > 0 {
		fmt.Fprintf(p, "\n%s\n\n", msg.Text(msg.AgentsConflictWarning))
		writeConflicts(&p.Builder, conflicts)
	}

	if len(out.Breaking) > 0 {
		p.WriteString("\n")
		for _, b := range out.Breaking {
			fmt.Fprintln(p, msg.Text(msg.UpdateBreaking, b.Version))
			for _, l := range b.Lines {
				fmt.Fprintln(p, "  "+l)
			}
		}
	}
	if out.ChangelogError != nil {
		fmt.Fprintf(p, "\n%s\n", msg.Text(msg.UpdateChangelogFailed, *out.ChangelogError))
	}
	if out.Page != nil {
		fmt.Fprintf(p, "\n%s\n", msg.Text(msg.ReleasePageLink, *out.Page))
	}
	if !x.connected {
		hints = append(hints, hintOf(msg.HintUpdateSetup))
	}
	p.hints(hints...)
}

// programElsewhere refuses an update of a program other than that of the
// data root, installed; "" if there is none.
func programElsewhere(running, installed string) failure {
	details := map[string]any{"program": running}
	shown := msg.Text(msg.UpdateNotInstalled)
	h := hintOf(msg.HintInstallGentry, update.InstallCommand())
	if installed != "" {
		details["installed"] = installed
		shown = installed
		h = hintOf(msg.HintUpdateProgram).by(installed)
	}
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeProgramElsewhere,
		message: msg.Text(msg.ErrProgramElsewhere, running, shown),
		hints:   []hint{h},
		details: details,
	}
}

// notRelease refuses an update of a build that is no release.
func notRelease(version string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeNotRelease,
		message: msg.Text(msg.ErrNotRelease, version),
		hints:   []hint{hintOf(msg.HintInstallRelease, update.InstallCommand())},
		details: map[string]any{"version": version},
	}
}

// newerThanRelease refuses an update of a version newer than the latest
// release.
func newerThanRelease(version, latest string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeNewerThanRelease,
		message: msg.Text(msg.ErrNewerThanRelease, version, latest),
		details: map[string]any{"version": version, "latest": latest},
	}
}

// updateFailure is a failure to find, download, check or write the latest
// release; the installation is as it was.
func updateFailure(err error) failure {
	var nf *update.NotFoundError
	var de *update.DownloadError
	var ce *update.ChecksumError
	var we *update.WriteError
	switch {
	case errors.As(err, &nf):
		version, details := msg.Text(msg.UpdateLatest), map[string]any{}
		if nf.Version != "" {
			version, details["version"] = nf.Version, nf.Version
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeReleaseNotFound,
			message: msg.Text(msg.ErrReleaseNotFound, version),
			hints:   []hint{hintOf(msg.HintReleases, update.GitHub)},
			details: details,
		}
	case errors.As(err, &de):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeDownloadFailed,
			message: msg.Text(msg.ErrDownloadFailed, de.URL, de.Err),
			details: map[string]any{"url": de.URL, "error": de.Err.Error()},
		}
	case errors.As(err, &ce):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeChecksumMismatch,
			message: msg.Text(msg.ErrChecksumMismatch, ce.File, ce.Want, ce.Got),
			details: map[string]any{"file": ce.File, "expected": ce.Want, "actual": ce.Got},
		}
	case errors.As(err, &we):
		// A program that could not get its name back is told on the line of
		// the reason: the operator finds it at .old.
		reason, details := we.Err.Error(), map[string]any{"path": we.Program}
		if we.Restore != nil {
			reason += "; " + we.Restore.Error()
			details["restore"] = we.Restore.Error()
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeIOError,
			message: msg.Text(msg.ErrProgramWrite, we.Program, reason),
			details: details,
		}
	}
	return internal(err)
}

// integrationsFailed is the failure of the integrations that could not be
// built anew after the program was replaced: the new program stays.
func integrationsFailed(failures []integrationFailure) failure {
	f := failure{exit: contract.ExitError, code: contract.CodeIntegrationFailed}
	var items []map[string]any
	for i, x := range failures {
		text := msg.Text(msg.ErrIntegrationFailed, toolTitles[x.tool], x.reason)
		if i == 0 {
			f.message = text
		} else {
			f.more += "\n" + text + "\n"
		}
		f.hints = append(f.hints, hintOf(msg.HintIntegrationSetup).set("tool", x.tool))
		items = append(items, map[string]any{"tool": x.tool, "error": x.reason})
	}
	f.details = map[string]any{"integrations": items}
	return f
}

// agentsFailed is the failure of the projects whose subagents could not be
// laid out after the update: the other projects are laid out.
func agentsFailed(failures []agentsFailure) failure {
	f := failure{exit: contract.ExitError, code: contract.CodeAgentsFailed, message: msg.Text(msg.AgentsSyncFailed)}
	var items []map[string]any
	for i, x := range failures {
		if i > 0 {
			f.more += "\n"
		}
		f.more += msg.Text(msg.TaskProject, x.project) + "\n" + msg.Text(msg.AgentsSyncFailedReason, x.reason) + "\n"
		f.hints = append(f.hints, hintOf(msg.HintAgentsDeployFailed).forProject(x.project))
		items = append(items, map[string]any{"project": x.project, "reason": x.reason})
	}
	f.details = map[string]any{"agents": items}
	return f
}
