package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/releases"
	"github.com/t8nax/gentry/internal/update"
)

// runUpdate runs the scenario of gentry update, package update, and turns
// its result into the output: the result of an update done here, or the
// program installed it hands the update over to, which this one runs on its
// own output and ends with its code.
func runUpdate(args []string, env Env) int {
	f := newFlags("update")
	if code, done := f.parse(args, env); done {
		return code
	}
	root, err := home.Root()
	if err != nil {
		return fail(env, homeUnknown())
	}
	exe, err := executable()
	if err != nil {
		return fail(env, internal(err))
	}
	own := buildinfo.Version()
	// The variable is for this program alone: the programs it runs, claude
	// and git, and the program of a handoff do not get it.
	from := os.Getenv(update.FromEnv)
	os.Unsetenv(update.FromEnv)
	projects := &updateProjects{}
	defer projects.close()
	u := update.Updater{Releases: releases.New(), Tools: updateTools(own), Projects: projects, VersionOf: releases.VersionOf}
	r, h, err := u.Run(env.context(), update.Request{Root: root, Running: exe, Version: own, From: from})
	switch {
	case err != nil:
		return fail(env, updateRefusal(err))
	case h != nil:
		defer h.Done()
		return runInstalled(env, h.Program, h.Env, args)
	}
	return emitUpdate(env, r)
}

// runInstalled runs gentry update of the program with args and the
// variables vars added, on the output of env, and returns its exit code.
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

// failureError is a failure of a command as the error of a step the
// scenario of update asks the entry for: it turns back into the failure.
type failureError struct{ f failure }

func (e *failureError) Error() string { return e.f.message }

// failureOf returns the failure an error of a step of update is: the one it
// carries, or an internal one.
func failureOf(err error) failure {
	var fe *failureError
	if errors.As(err, &fe) {
		return fe.f
	}
	return internal(err)
}

// An updateAdapter is what the update needs of the adapter of an AI tool
// apart from setupTool.
type updateAdapter struct {
	// elsewhere returns the existing directory of another data root the
	// tool serves the plugin of Gentry from, as update.Tool.Elsewhere.
	elsewhere func(dir string) (string, error)
	// current returns the integration in dir if the tool serves the plugin
	// this program of the version own builds from it already.
	current func(tool, dir, own string) (contract.SetupOutput, bool)
}

// updateAdapters are the adapters of the tools of setup for the update.
var updateAdapters = map[string]updateAdapter{
	claude.Tool: {elsewhere: claudeElsewhere, current: claudeCurrent},
}

// updateTools returns the AI tools of setup for the update, each by its
// adapter.
func updateTools(own string) []update.Tool {
	var out []update.Tool
	for _, tool := range tools {
		a, ok := updateAdapters[tool]
		if !ok {
			continue
		}
		dir, err := home.Integration(tool)
		if err != nil {
			continue
		}
		out = append(out, update.Tool{
			Name:      tool,
			Dir:       dir,
			Elsewhere: func() (string, error) { return a.elsewhere(dir) },
			Current:   func() (contract.SetupOutput, bool) { return a.current(tool, dir, own) },
			Setup: func() (contract.SetupOutput, error) {
				out, bad := setupTool(tool, false)
				if bad != nil {
					return out, &failureError{*bad}
				}
				return out, nil
			},
		})
	}
	return out
}

// claudeElsewhere returns the existing directory of another data root
// Claude Code serves the plugin of Gentry from, as setup refuses it; "" if
// it serves none, or that of dir, or one that is no more.
func claudeElsewhere(dir string) (string, error) {
	program, err := claude.Program()
	if err != nil {
		return "", update.ErrToolNotFound
	}
	registered, err := claude.Registered(program)
	if err != nil {
		return "", &failureError{setupToolError(err)}
	}
	if registered == "" || claude.SamePath(registered, dir) {
		return "", nil
	}
	if _, err := os.Stat(registered); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return registered, nil
}

// claudeCurrent returns the integration with Claude Code in dir if Claude
// Code serves the plugin this program builds from it already: then nothing
// is written.
func claudeCurrent(tool, dir, own string) (contract.SetupOutput, bool) {
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

// updateProjects lays out the subagents of the projects for the update, as
// agents deploy --project does, but a subagent in conflict is skipped and
// told. The state store is only read, and not created; the process is
// opened for the first project.
type updateProjects struct {
	pool *layoutPool
	repo *process.Repo
}

func (p *updateProjects) List() ([]string, error) {
	pool, bad := readLayoutPool()
	if bad != nil {
		return nil, &failureError{*bad}
	}
	p.pool = pool
	if pool.st == nil {
		return nil, nil
	}
	projects, err := pool.st.Projects()
	if err != nil {
		return nil, &failureError{stateFailure(err)}
	}
	var ids []string
	for _, pr := range projects {
		ids = append(ids, pr.ID)
	}
	return ids, nil
}

func (p *updateProjects) Deploy(id string) (contract.UpdateAgents, error) {
	if p.repo == nil {
		r, bad := openProcess()
		if bad != nil {
			return contract.UpdateAgents{}, &failureError{*bad}
		}
		p.repo = r
	}
	d, bad := deployOf(p.repo, p.pool, id)
	if bad != nil {
		return contract.UpdateAgents{}, &failureError{*bad}
	}
	res, err := agents.Sync(agentsLayout, d.targets, p.pool.paths())
	if err != nil {
		return contract.UpdateAgents{}, &failureError{layoutFailure(err)}
	}
	out := d.output(res, p.pool)
	a := contract.UpdateAgents{Project: out.Project, Changed: out.Changed, Worktrees: out.Worktrees}
	if len(res.Conflicts) > 0 {
		a.Conflicts = conflictsJSON(res.Conflicts)
	}
	return a, nil
}

func (p *updateProjects) close() {
	if p.repo != nil {
		p.repo.Close()
	}
	if p.pool != nil {
		p.pool.close()
	}
}

// updateRefusal turns why the update is refused into the failure.
func updateRefusal(err error) failure {
	var elsewhere *update.ElsewhereError
	var notRel *update.NotReleaseError
	var newer *update.NewerError
	var plugin *update.PluginElsewhereError
	var check *update.CheckError
	switch {
	case errors.As(err, &elsewhere):
		return programElsewhere(elsewhere.Running, elsewhere.Installed)
	case errors.As(err, &notRel):
		return notRelease(notRel.Version)
	case errors.As(err, &newer):
		return newerThanRelease(newer.Version, newer.Latest)
	case errors.As(err, &plugin):
		return pluginElsewhere(plugin.Tool, plugin.Registered, plugin.Dir)
	case errors.As(err, &check):
		return failureOf(check.Err)
	}
	return updateFailure(err)
}

// emitUpdate prints the result of the update, then what failed after the
// program was replaced. In JSON a failure is the output, with the result of
// the update in its details.
func emitUpdate(env Env, r *update.Result) int {
	out := updateOutput(r)
	x := updateExtra{connected: r.Connected}
	text := func(p *page, out contract.UpdateOutput) { updateText(p, out, x) }
	if bad := updateFailed(r); bad != nil {
		return emitFailed(env, out, text, *bad, "update")
	}
	return emit(env, out, text)
}

// updateOutput is the result of the update as the contract has it.
func updateOutput(r *update.Result) contract.UpdateOutput {
	out := contract.UpdateOutput{Action: r.Action, Version: r.Version, Program: r.Program, Integrations: append([]contract.SetupOutput{}, r.Integrations...), Agents: r.Agents}
	if r.Action == update.Updated {
		page := releases.Page(r.Version)
		out.Previous, out.Page = &r.Previous, &page
	}
	for _, b := range r.Breaking {
		out.Breaking = append(out.Breaking, contract.UpdateBreaking{Version: b.Version, Lines: b.Lines})
	}
	if r.ChangelogErr != nil {
		e := r.ChangelogErr.Error()
		out.ChangelogError = &e
	}
	return out
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

// updateFailed is what failed after the program was replaced, as one
// failure: of the integrations, with the subagents under it; nil if nothing
// did.
func updateFailed(r *update.Result) *failure {
	var parts []failure
	if len(r.Tools) > 0 {
		var fs []integrationFailure
		for _, t := range r.Tools {
			fs = append(fs, integrationFailure{tool: t.Tool, reason: failureOf(t.Err).message})
		}
		parts = append(parts, integrationsFailed(fs))
	}
	if len(r.Projects) > 0 {
		var fs []agentsFailure
		for _, p := range r.Projects {
			fs = append(fs, agentsFailure{project: p.Project, reason: failureOf(p.Err).message})
		}
		parts = append(parts, agentsFailed(fs))
	}
	if r.ListErr != nil {
		parts = append(parts, failureOf(r.ListErr))
	}
	return joinFailures(parts)
}

// joinFailures returns the failures as one: the first, with the others told
// under it, each a block, and the hints of all; nil for none.
func joinFailures(parts []failure) *failure {
	if len(parts) == 0 {
		return nil
	}
	f := parts[0]
	if f.details == nil {
		f.details = map[string]any{}
	}
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

// updateText prints the result of the update: the versions and whether the
// changes take effect in new sessions, what needs the attention of the
// operator, the incompatible changes of the versions skipped and the page of
// the release. What was built anew and laid out is told by --json only.
func updateText(p *page, out contract.UpdateOutput, x updateExtra) {
	if out.Action == update.Updated && out.Previous != nil {
		fmt.Fprintln(p, msg.Text(msg.UpdateUpdated, *out.Previous, out.Version))
	} else {
		fmt.Fprintln(p, msg.Text(msg.UpdateCurrent, out.Version))
	}
	changed := out.Action == update.Updated || slices.ContainsFunc(out.Integrations, func(s contract.SetupOutput) bool { return s.Action != claude.Unchanged })
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
	h := hintOf(msg.HintInstallGentry, releases.InstallCommand())
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
		hints:   []hint{hintOf(msg.HintInstallRelease, releases.InstallCommand())},
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
	var nf *releases.NotFoundError
	var de *releases.DownloadError
	var ce *releases.ChecksumError
	var we *releases.WriteError
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
			hints:   []hint{hintOf(msg.HintReleases, releases.GitHub)},
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
