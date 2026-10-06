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

// DiffResult is how the draft differs from the active flow.
type DiffResult struct {
	Applied *process.Applied // nil if the project has no flow
	Changes []Change
}

// DiffDraft compares the draft of a project with its active flow. The draft
// may have problems: what can be read is compared.
func DiffDraft(r *process.Repo, p Places) (DiffResult, error) {
	res, ok, err := Draft(r, p)
	if err != nil {
		return DiffResult{}, err
	}
	if !ok {
		return DiffResult{}, &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	k := process.FlowOf(p.Project)
	active, err := r.Active(k)
	if err != nil {
		return DiffResult{}, err
	}
	var out DiffResult
	if a, ok, err := r.Applied(k); err != nil {
		return DiffResult{}, err
	} else if ok {
		out.Applied = &a
	}
	out.Changes = Diff(Snapshot{Files: active}, Snapshot{Files: res.Snapshot.Files})
	return out, nil
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
