// Package update is the scenario of gentry update: it installs the latest
// release of Gentry in place of the program of the data root, builds the
// integrations with the AI tools anew and lays out the subagents of the
// projects.
//
// Two programs do an update. The program the operator runs checks what it
// can without the network — where it runs, its version, the tools that
// serve the plugin of another data root — takes the lock of updates, finds
// the latest release, downloads it, checks its sum and replaces itself; then
// it hands the update over to the new program (a Handoff), gentry update
// with FromEnv set, which builds the integrations anew, lays out the
// subagents and gives the Result: the plugin, the subagents and the texts
// are those of the new program. A failure before the program is replaced
// leaves the installation as it was. With the latest release installed the
// program itself checks the integrations and builds anew those that do not
// match it.
//
// The scenario prints nothing: the entry turns the Result and the errors,
// types with what is needed to tell them, into the output, and runs the
// program of a Handoff. What the update does with an AI tool and with the
// worktrees of a project the entry gives it as a Tool and as Projects: the
// adapters of the tools are above the scenarios.
package update

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/releases"
)

// FromEnv carries the version an update replaced to the program it
// installed, which finishes the update. The variable and the command are
// all the programs of two versions share: they stay as they are from the
// first release on.
const FromEnv = "GENTRY_UPDATE_FROM"

// The actions of an update.
const (
	Updated = "updated" // the program of the latest release took the place of the program
	Current = "current" // the latest release was installed already
)

// unchanged is the action of setup that changed nothing in the tool.
const unchanged = "unchanged"

// A Request is an update asked for.
type Request struct {
	Root    string // the data root
	Running string // the path of the running program, its symbolic links resolved
	Version string // the version of the running program
	// From is the version FromEnv names: the program of that version has
	// installed the running one, which finishes the update; "" for none.
	From string
}

// A Tool is an AI tool Gentry integrates with, as the update needs it: the
// entry gives each tool, by the adapter of the tool.
type Tool struct {
	Name string
	Dir  string // the folder of the integration in the data root; the integration is connected if it is there
	// Elsewhere returns the existing directory of another data root the
	// tool serves the plugin of Gentry from; "" if it serves none, or that
	// of Dir. ErrToolNotFound means the program of the tool is not found.
	Elsewhere func() (string, error)
	// Current returns the integration if the tool serves the plugin this
	// program builds from Dir already; nothing is written.
	Current func() (contract.SetupOutput, bool)
	// Setup builds the integration anew, as gentry setup does.
	Setup func() (contract.SetupOutput, error)
}

// ErrToolNotFound means the program of an AI tool is not found.
var ErrToolNotFound = errors.New("the program of the tool is not found")

// Projects lays out the subagents of the projects for an update.
type Projects interface {
	// List returns the projects in the order of gentry project list; none
	// without a state store, which is not created.
	List() ([]string, error)
	// Deploy lays out the subagents in every worktree of the project, as
	// gentry agents deploy --project does, but a subagent in conflict is
	// skipped and told.
	Deploy(project string) (contract.UpdateAgents, error)
}

// Releases are the releases of Gentry, as releases.Client gives them.
type Releases interface {
	Latest(ctx context.Context) (string, error)
	Download(ctx context.Context, v, asset, dir string) (string, error)
	Changelog(ctx context.Context, v string) (string, error)
}

// An Updater updates Gentry with what the entry gives it.
type Updater struct {
	Releases Releases
	Tools    []Tool
	Projects Projects
	// VersionOf returns the version a program names, as releases.VersionOf
	// does.
	VersionOf func(program string) (string, bool)
}

// A Result is an update done in this program: after the program was
// replaced, or with the latest release installed.
type Result struct {
	Action   string // Updated or Current
	Previous string // the version before the update; "" for Current
	Version  string
	Program  string // the program of the data root
	// Connected reports whether an integration is connected, though it
	// could not be built anew.
	Connected    bool
	Integrations []contract.SetupOutput // the integrations connected, built anew or checked
	// Breaking are the incompatible changes of the versions skipped; only
	// with Updated, and none if the journal could not be read.
	Breaking     []Breaking
	ChangelogErr error // the journal of changes could not be read
	// Agents are the layouts of the projects; nil unless the subagents were
	// laid out and there is a project.
	Agents []contract.UpdateAgents

	// What failed after the program was replaced, or after a plugin was
	// built anew: the update goes on and fails at the end.
	Tools    []ToolFailure
	Projects []ProjectFailure
	ListErr  error // the projects could not be listed
}

// Failed reports whether a step after the replacement failed.
func (r *Result) Failed() bool {
	return len(r.Tools) > 0 || len(r.Projects) > 0 || r.ListErr != nil
}

// A ToolFailure is an integration that could not be built anew.
type ToolFailure struct {
	Tool string
	Err  error
}

// A ProjectFailure is a project whose subagents could not be laid out.
type ProjectFailure struct {
	Project string
	Err     error
}

// A Handoff is the update going on in another program: gentry update of
// Program, with the variables Env added to the environment. The entry runs
// it, then calls Done.
type Handoff struct {
	Program string
	Env     []string
	unlock  func() // releases the lock of updates the new program works under
}

// Done ends the handoff: the lock of updates is released.
func (h *Handoff) Done() {
	if h.unlock != nil {
		h.unlock()
		h.unlock = nil
	}
}

// ElsewhereError refuses an update of a program other than that of the data
// root.
type ElsewhereError struct {
	Running   string
	Installed string // "" if the data root has no program
}

func (e *ElsewhereError) Error() string { return e.Running + " is not the program of the data root" }

// NotReleaseError refuses an update of a build that is no release.
type NotReleaseError struct{ Version string }

func (e *NotReleaseError) Error() string { return e.Version + " is no release" }

// NewerError refuses an update of a version newer than the latest release.
type NewerError struct{ Version, Latest string }

func (e *NewerError) Error() string {
	return e.Version + " is newer than the latest release " + e.Latest
}

// PluginElsewhereError refuses an update when an AI tool serves the plugin
// of Gentry from the directory of another data root.
type PluginElsewhereError struct{ Tool, Registered, Dir string }

func (e *PluginElsewhereError) Error() string {
	return e.Tool + " serves the plugin from " + e.Registered + ", not " + e.Dir
}

// CheckError refuses an update when an AI tool whose integration is
// connected fails to tell where it serves the plugin from.
type CheckError struct {
	Tool string
	Err  error
}

func (e *CheckError) Error() string { return e.Tool + ": " + e.Err.Error() }
func (e *CheckError) Unwrap() error { return e.Err }

// Run updates Gentry. It returns the Result of an update done here, or the
// Handoff of one that goes on in the program installed, or why it is
// refused.
func (u *Updater) Run(ctx context.Context, req Request) (*Result, *Handoff, error) {
	installed := Installed(req.Root)
	if err := sameProgram(req.Running, installed); err != nil {
		return nil, nil, err
	}
	// The new program finishes the update under the lock its predecessor
	// holds: only the program of the data root, which its predecessor runs.
	if releases.IsRelease(req.From) {
		return u.finish(ctx, req, installed), nil, nil
	}
	// A build that is no release is refused before the lock: a folder of
	// the program closed for writing must not hide the refusal.
	if !releases.IsRelease(req.Version) {
		return nil, nil, &NotReleaseError{Version: req.Version}
	}

	// Two updates at once do not replace the program together: the second
	// waits for the first, and then the program installed is the latest
	// release, which tells the rest.
	lock, err := releases.Lock(ctx, installed)
	if err != nil {
		return nil, nil, err
	}
	if v, ok := u.VersionOf(installed); ok && v != req.Version {
		lock.Release()
		return nil, &Handoff{Program: installed}, nil
	}
	held := true
	defer func() {
		if held {
			lock.Release()
		}
	}()
	// The programs earlier updates renamed aside on Windows go once free,
	// whatever this update does.
	releases.RemoveOld(installed)
	if err := u.checkTools(); err != nil {
		return nil, nil, err
	}

	latest, err := u.Releases.Latest(ctx)
	if err != nil {
		return nil, nil, err
	}
	switch releases.Compare(req.Version, latest) {
	case 1:
		return nil, nil, &NewerError{Version: req.Version, Latest: latest}
	case 0:
		return u.current(ctx, req, installed), nil, nil
	}
	tmp, err := os.MkdirTemp("", "gentry-update-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	downloaded, err := u.Releases.Download(ctx, latest, releases.Asset(runtime.GOOS, runtime.GOARCH), tmp)
	if err != nil {
		return nil, nil, err
	}
	if err := releases.Replace(downloaded, installed); err != nil {
		return nil, nil, err
	}
	// The lock is held until the new program has done its part.
	held = false
	return nil, &Handoff{Program: installed, Env: []string{FromEnv + "=" + req.Version}, unlock: lock.Release}, nil
}

// Installed returns the path of the program of the data root: <data
// root>/bin/gentry, gentry.exe on Windows.
func Installed(root string) string { return filepath.Join(root, "bin", releases.Program()) }

// sameProgram refuses unless running is the program installed, compared
// with its symbolic links resolved.
func sameProgram(running, installed string) error {
	resolved, err := filepath.EvalSymlinks(installed)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &ElsewhereError{Running: running}
	case err != nil || !paths.Same(resolved, running):
		return &ElsewhereError{Running: running, Installed: installed}
	}
	return nil
}

// connected reports whether the integration of the folder dir is connected:
// setup made the folder.
func connected(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// checkTools refuses the update if an AI tool serves the plugin of Gentry
// from the directory of another data root, though this data root has no
// integration with it: the update would move the plugin of the other one.
// With no program of the tool there is nothing to check; a tool that fails
// to tell refuses only if its integration is connected.
func (u *Updater) checkTools() error {
	for _, t := range u.Tools {
		registered, err := t.Elsewhere()
		switch {
		case errors.Is(err, ErrToolNotFound):
		case err != nil && !connected(t.Dir):
		case err != nil:
			return &CheckError{Tool: t.Name, Err: err}
		case registered != "":
			return &PluginElsewhereError{Tool: t.Name, Registered: registered, Dir: t.Dir}
		}
	}
	return nil
}

// finish finishes the update in the new program: it builds anew every
// integration connected, lays out the subagents and reads the incompatible
// changes of the versions skipped.
func (u *Updater) finish(ctx context.Context, req Request, installed string) *Result {
	r := &Result{Action: Updated, Previous: req.From, Version: req.Version, Program: installed}
	for _, t := range u.Tools {
		if !connected(t.Dir) {
			continue
		}
		r.Connected = true
		u.setup(r, t)
	}
	u.layout(r)
	journal, err := u.Releases.Changelog(ctx, req.Version)
	if err != nil {
		r.ChangelogErr = err
	} else {
		r.Breaking = BreakingOf(journal, req.From, req.Version)
	}
	return r
}

// current checks the integrations with the latest release installed: one
// that matches the program is left as it is, nothing written; another is
// built anew, and then the subagents are laid out, as they name the tools of
// the plugin.
func (u *Updater) current(ctx context.Context, req Request, installed string) *Result {
	r := &Result{Action: Current, Version: req.Version, Program: installed}
	rebuilt := false
	for _, t := range u.Tools {
		if !connected(t.Dir) {
			continue
		}
		r.Connected = true
		if out, ok := t.Current(); ok {
			r.Integrations = append(r.Integrations, out)
			continue
		}
		if out, ok := u.setup(r, t); ok && out.Action != unchanged {
			rebuilt = true
		}
	}
	if rebuilt {
		u.layout(r)
	}
	return r
}

// setup builds the integration with the tool anew and returns it; false if
// it failed, which does not stop the others.
func (u *Updater) setup(r *Result, t Tool) (contract.SetupOutput, bool) {
	out, err := t.Setup()
	if err != nil {
		r.Tools = append(r.Tools, ToolFailure{Tool: t.Name, Err: err})
		return out, false
	}
	r.Integrations = append(r.Integrations, out)
	return out, true
}

// layout lays out the subagents of each project, in the order of gentry
// project list; a project that fails does not stop the others.
func (u *Updater) layout(r *Result) {
	projects, err := u.Projects.List()
	if err != nil {
		r.ListErr = err
		return
	}
	for _, p := range projects {
		a, err := u.Projects.Deploy(p)
		if err != nil {
			r.Projects = append(r.Projects, ProjectFailure{Project: p, Err: err})
			continue
		}
		r.Agents = append(r.Agents, a)
	}
}
