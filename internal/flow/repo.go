package flow

import (
	"errors"
	"io/fs"
	"slices"

	"github.com/t8nax/gentry/internal/process"
)

// Event types of the flow.
const (
	EventApplied        = "flow.applied"
	EventDraftDiscarded = "flow.draft_discarded"
)

// Places is where the flow of a project lives in the process repository.
type Places struct {
	Project  string
	Dir      string // the flow directory: the active flow and the draft over it
	Library  string // the library of subagents
	Conflict string // the variants of another machine while the flow is in conflict
}

// PlacesOf returns the places of the flow of project in repository r.
func PlacesOf(r *process.Repo, project string) Places {
	k := process.FlowOf(project)
	return Places{Project: project, Dir: r.KindDir(k), Library: r.KindDir(process.Library), Conflict: r.ConflictDir(k)}
}

// NoFlowError means the project has no active flow.
type NoFlowError struct{ Project, Dir string }

func (e *NoFlowError) Error() string { return e.Project + ": no flow" }

// NoDraftError means the flow directory of the project has no changes.
type NoDraftError struct{ Project, Dir string }

func (e *NoDraftError) Error() string { return e.Project + ": no flow draft" }

// DraftInvalidError means the draft has problems, in the order of files and
// lines.
type DraftInvalidError struct {
	Dir      string
	Problems []Problem
}

func (e *DraftInvalidError) Error() string { return e.Dir + ": problems in the flow draft" }

// Draft operations on files.
const (
	OpRead   = "read"
	OpWrite  = "write"
	OpDelete = "delete"
)

// DraftIOError means the files of the flow, or of the library, cannot be
// read or written.
type DraftIOError struct {
	Op      string // OpRead, OpWrite or OpDelete
	Path    string
	Library bool // the library of subagents, not the flow
	Err     error
}

func (e *DraftIOError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }
func (e *DraftIOError) Unwrap() error { return e.Err }

// ioError wraps an error of the files of the flow or of the library in dir.
func ioError(op, dir string, library bool, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &DraftIOError{Op: op, Path: pe.Path, Library: library, Err: err}
	}
	return err
}

// Check checks the content of a kind of the process for package process: a
// flow with the library of the same commit, or the library itself.
func Check(k process.Kind, files, library process.Files) ([]string, error) {
	var problems []Problem
	if k.IsLibrary() {
		problems = ReadLibrary(files)
	} else {
		res, err := Read(files, library, nil)
		if err != nil {
			return nil, err
		}
		problems = res.Problems
	}
	messages := make([]string, len(problems))
	for i, p := range problems {
		messages[i] = p.Message
	}
	return messages, nil
}

// Active returns the active flow of a project, read and checked, and the
// commit that applied it; ok is false if the project has no flow.
func Active(r *process.Repo, project string) (res *Result, applied process.Applied, ok bool, err error) {
	k := process.FlowOf(project)
	files, err := r.Active(k)
	if err != nil || len(files) == 0 {
		return nil, process.Applied{}, false, err
	}
	library, err := r.Active(process.Library)
	if err != nil {
		return nil, process.Applied{}, false, err
	}
	if res, err = Read(files, library, nil); err != nil {
		return nil, process.Applied{}, false, err
	}
	applied, ok, err = r.Applied(k)
	return res, applied, ok, err
}

// Draft returns the draft of the flow of a project, read and checked with
// the active library; ok is false if the flow directory has no changes.
func Draft(r *process.Repo, p Places) (res *Result, ok bool, err error) {
	k := process.FlowOf(p.Project)
	working, err := r.Working(k)
	if err != nil {
		return nil, false, ioError(OpRead, p.Dir, false, err)
	}
	active, err := r.Active(k)
	if err != nil {
		return nil, false, err
	}
	if working.Equal(active) {
		return nil, false, nil
	}
	library, err := r.Active(process.Library)
	if err != nil {
		return nil, false, err
	}
	draftLibrary, err := r.Working(process.Library)
	if err != nil {
		return nil, false, ioError(OpRead, p.Library, true, err)
	}
	res, err = Read(working, library, draftLibrary)
	return res, err == nil, err
}

// NoDraftsError means neither the flow of the project nor the library of
// subagents has a draft: gentry flow diff shows both.
type NoDraftsError struct{ Project, Dir string }

func (e *NoDraftsError) Error() string { return e.Project + ": no flow or library draft" }

// DiffResult is how the drafts of the flow of a project and of the library
// differ from the active ones.
type DiffResult struct {
	Applied *process.Applied // nil if the project has no flow
	Draft   bool             // the flow has a draft
	Changes []Change         // the objects of the flow draft, file by file
	// Old is the active flow and New the draft, as far as they can be read;
	// New is Old without a draft. Neither is nil.
	Old, New  *Flow
	Scenarios []ScenarioDiff     // of the changed scenarios, by identifier
	Problems  []Problem          // of the flow draft
	Library   *LibraryDiffResult // nil without a draft of the library
}

// DiffDraft compares the drafts of the flow of a project and of the library
// with the active ones. A draft may have problems: what can be read is
// compared.
func DiffDraft(r *process.Repo, p Places) (DiffResult, error) {
	var out DiffResult
	k := process.FlowOf(p.Project)
	active, err := r.Active(k)
	if err != nil {
		return DiffResult{}, err
	}
	library, err := r.Active(process.Library)
	if err != nil {
		return DiffResult{}, err
	}
	old, err := Read(active, library, nil)
	if err != nil {
		return DiffResult{}, err
	}
	out.Old, out.New = old.Read, old.Read
	if a, ok, err := r.Applied(k); err != nil {
		return DiffResult{}, err
	} else if ok {
		out.Applied = &a
	}
	res, draft, err := Draft(r, p)
	if err != nil {
		return DiffResult{}, err
	}
	if draft {
		out.Draft, out.New, out.Problems = true, res.Read, res.Problems
		out.Changes = Diff(Snapshot{Files: active}, Snapshot{Files: res.Snapshot.Files})
		stages := map[string]string{}
		for _, c := range out.Changes {
			if c.Object == ObjectStage {
				stages[c.ID] = c.Change
			}
		}
		for _, c := range out.Changes {
			if c.Object != ObjectScenario {
				continue
			}
			var before, after *Scenario
			if s, ok := out.Old.Scenario(c.ID); ok {
				before = &s
			}
			if s, ok := out.New.Scenario(c.ID); ok {
				after = &s
			}
			if before == nil && after == nil {
				continue
			}
			d := DiffScenario(before, after, stages)
			d.Unreadable = res.Unreadable[c.ID]
			out.Scenarios = append(out.Scenarios, d)
		}
	}
	lib, err := LibraryDiff(r)
	var none *NoLibraryDraftError
	switch {
	case errors.As(err, &none):
	case err != nil:
		return DiffResult{}, err
	default:
		if err := libraryUsers(r, p.Project, out.New, &lib); err != nil {
			return DiffResult{}, err
		}
		out.Library = &lib
	}
	if !draft && out.Library == nil {
		return DiffResult{}, &NoDraftsError{Project: p.Project, Dir: p.Dir}
	}
	return out, nil
}

// libraryUsers notes the projects whose flows name each changed subagent of
// the library: project by flow, its draft as it stands, the others by their
// active flows.
func libraryUsers(r *process.Repo, project string, flow *Flow, lib *LibraryDiffResult) error {
	kinds, err := r.Kinds()
	if err != nil {
		return err
	}
	active, err := r.Active(process.Library)
	if err != nil {
		return err
	}
	flows := map[string]*Flow{}
	for _, k := range kinds {
		if k.IsLibrary() {
			continue
		}
		if k.Project == project {
			flows[k.Project] = flow
			continue
		}
		files, err := r.Active(k)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			continue
		}
		res, err := Read(files, active, nil)
		if err != nil {
			return err
		}
		flows[k.Project] = res.Read
	}
	if _, ok := flows[project]; !ok {
		flows[project] = flow
	}
	for i, c := range lib.Changes {
		lib.Changes[i].Projects = []string{}
		for id, f := range flows {
			if len(f.Users(c.ID)) > 0 {
				lib.Changes[i].Projects = append(lib.Changes[i].Projects, id)
			}
		}
		slices.Sort(lib.Changes[i].Projects)
	}
	return nil
}

// Apply makes the draft of a project its active flow with a commit. A draft
// with problems is not applied, and nothing changes.
func Apply(r *process.Repo, p Places) (process.Applied, error) {
	res, ok, err := Draft(r, p)
	if err != nil {
		return process.Applied{}, err
	}
	if !ok {
		return process.Applied{}, &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	if len(res.Problems) > 0 {
		return process.Applied{}, &DraftInvalidError{Dir: p.Dir, Problems: res.Problems}
	}
	return r.Apply(process.FlowOf(p.Project), res.Snapshot.Files, "Apply the flow of "+p.Project, "flow apply "+p.Project)
}

// Discard brings the flow directory of a project back to the active flow and
// removes the variants of another machine of a conflict.
func Discard(r *process.Repo, p Places) error {
	k := process.FlowOf(p.Project)
	draft, err := r.HasDraft(k)
	if err != nil {
		return ioError(OpRead, p.Dir, false, err)
	}
	_, conflict, err := r.Conflict(k)
	if err != nil {
		return ioError(OpRead, p.Conflict, false, err)
	}
	if !draft && !conflict {
		return &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	return ioError(OpWrite, p.Dir, false, r.Discard(k))
}

// ConflictObjects returns the objects of the flow, or of the library, whose
// variants of another machine are in the conflict directory of kind k, as
// changes without a kind of change; ok is false if k is not in conflict.
func ConflictObjects(r *process.Repo, k process.Kind) (objects []Change, ok bool, err error) {
	files, ok, err := r.Conflict(k)
	if err != nil || !ok {
		return nil, false, err
	}
	return ObjectsOf(k, mapKeysSorted(files)), true, nil
}

// ObjectsOf returns the objects of kind k that files at paths belong to, in
// the order of a diff; a file of no object is left out.
func ObjectsOf(k process.Kind, paths []string) []Change {
	files := map[string]string{}
	for _, p := range paths {
		if k.IsLibrary() {
			p = agentsDir + "/" + p
		}
		files[p] = ""
	}
	return Diff(Snapshot{}, Snapshot{Files: files})
}

func mapKeysSorted(m process.Files) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
