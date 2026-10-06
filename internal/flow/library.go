package flow

import (
	"strings"

	"github.com/t8nax/gentry/internal/process"
)

// Event types of the library of subagents.
const (
	EventLibraryApplied        = "library.applied"
	EventLibraryDraftDiscarded = "library.draft_discarded"
)

// NoLibraryDraftError means the library directory has no changes.
type NoLibraryDraftError struct{ Dir string }

func (e *NoLibraryDraftError) Error() string { return e.Dir + ": no library draft" }

// LibraryInvalidError means the draft of the library has problems.
type LibraryInvalidError struct {
	Dir      string
	Problems []Problem
}

func (e *LibraryInvalidError) Error() string { return e.Dir + ": problems in the library draft" }

// ReadLibrary checks the files of the library of subagents by section 10.2:
// each subagent is a pair of its fields and its instruction, the fields are
// valid, and nothing else is there. Paths of problems are inside the library.
func ReadLibrary(files process.Files) []Problem {
	inside := map[string]string{}
	for p, text := range files {
		inside[agentsDir+"/"+p] = text
	}
	l := newLoader(treeOf(inside), mapLibrary(nil))
	l.inLibrary = true
	for _, e := range l.src.entries("") {
		if e.dir && e.name == agentsDir {
			l.scan(agentsDir)
		}
	}
	l.loadAgents()
	l.checkAgents()
	problems := sortProblems(l.problems, nil)
	for i := range problems {
		problems[i].File = strings.TrimPrefix(problems[i].File, agentsDir+"/")
	}
	return problems
}

// LibraryChange is a subagent of the library that differs from the active
// library.
type LibraryChange struct {
	ID     string
	Change string // Added, Modified or Removed
}

// LibraryDiffResult is how the draft of the library differs from the active
// one.
type LibraryDiffResult struct {
	Applied *process.Applied // nil if the library has never been applied
	Changes []LibraryChange
}

// libraryDraft returns the working files of the library and whether they
// differ from the active library.
func libraryDraft(r *process.Repo) (working, active process.Files, ok bool, err error) {
	dir := r.KindDir(process.Library)
	if working, err = r.Working(process.Library); err != nil {
		return nil, nil, false, ioError(OpRead, dir, true, err)
	}
	if active, err = r.Active(process.Library); err != nil {
		return nil, nil, false, err
	}
	return working, active, !working.Equal(active), nil
}

// LibraryDiff compares the draft of the library with the active library.
func LibraryDiff(r *process.Repo) (LibraryDiffResult, error) {
	working, active, ok, err := libraryDraft(r)
	if err != nil {
		return LibraryDiffResult{}, err
	}
	if !ok {
		return LibraryDiffResult{}, &NoLibraryDraftError{Dir: r.KindDir(process.Library)}
	}
	var out LibraryDiffResult
	if a, ok, err := r.Applied(process.Library); err != nil {
		return LibraryDiffResult{}, err
	} else if ok {
		out.Applied = &a
	}
	in := func(f process.Files) Snapshot {
		s := Snapshot{Files: map[string]string{}}
		for p, text := range f {
			s.Files[agentsDir+"/"+p] = text
		}
		return s
	}
	for _, c := range Diff(in(active), in(working)) {
		out.Changes = append(out.Changes, LibraryChange{ID: c.ID, Change: c.Change})
	}
	return out, nil
}

// LibraryApply makes the draft of the library active with a commit. A draft
// with problems is not applied, and nothing changes.
func LibraryApply(r *process.Repo) (process.Applied, error) {
	working, _, ok, err := libraryDraft(r)
	if err != nil {
		return process.Applied{}, err
	}
	dir := r.KindDir(process.Library)
	if !ok {
		return process.Applied{}, &NoLibraryDraftError{Dir: dir}
	}
	if problems := ReadLibrary(working); len(problems) > 0 {
		return process.Applied{}, &LibraryInvalidError{Dir: dir, Problems: problems}
	}
	return r.Apply(process.Library, working, "Apply the library of subagents", "library apply")
}

// LibraryDiscard brings the library directory back to the active library and
// removes the variants of another machine of a conflict.
func LibraryDiscard(r *process.Repo) error {
	_, _, draft, err := libraryDraft(r)
	if err != nil {
		return err
	}
	_, conflict, err := r.Conflict(process.Library)
	if err != nil {
		return ioError(OpRead, r.ConflictDir(process.Library), true, err)
	}
	dir := r.KindDir(process.Library)
	if !draft && !conflict {
		return &NoLibraryDraftError{Dir: dir}
	}
	return ioError(OpWrite, dir, true, r.Discard(process.Library))
}
