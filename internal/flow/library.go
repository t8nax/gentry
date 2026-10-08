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

// LibraryInvalidError means the draft of the library has problems, or
// breaks the active flow of a project.
type LibraryInvalidError struct {
	Dir      string
	Problems []Problem      // of the library itself
	Flows    []FlowProblems // of the flows the draft breaks, by project
}

// FlowProblems are the problems of the flow of a project.
type FlowProblems struct {
	Project  string
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
	ID       string
	Change   string   // Added, Modified or Removed
	Old, New *Agent   // as far as they can be read; nil if absent
	Projects []string // whose flows name the subagent, by DiffDraft only
}

// LibraryDiffResult is how the draft of the library differs from the active
// one.
type LibraryDiffResult struct {
	Applied  *process.Applied // nil if the library has never been applied
	Changes  []LibraryChange
	Problems []Problem // of the draft, with paths inside the library
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
	before, after := LibraryAgents(active), LibraryAgents(working)
	for _, c := range Diff(in(active), in(working)) {
		lc := LibraryChange{ID: c.ID, Change: c.Change}
		if a, ok := before[c.ID]; ok {
			lc.Old = &a
		}
		if a, ok := after[c.ID]; ok {
			lc.New = &a
		}
		out.Changes = append(out.Changes, lc)
	}
	out.Problems = ReadLibrary(working)
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
	broken, err := brokenFlows(r, working)
	if err != nil {
		return process.Applied{}, err
	}
	if len(broken) > 0 {
		return process.Applied{}, &LibraryInvalidError{Dir: dir, Flows: broken}
	}
	return r.Apply(process.Library, working, "Apply the library of subagents", "library apply")
}

// brokenFlows returns the problems of the active flows that are valid with
// the active library and not with library: a flow with problems before is
// not the fault of the library.
func brokenFlows(r *process.Repo, library process.Files) ([]FlowProblems, error) {
	active, err := r.Active(process.Library)
	if err != nil {
		return nil, err
	}
	kinds, err := r.Kinds()
	if err != nil {
		return nil, err
	}
	var out []FlowProblems
	for _, k := range kinds {
		if k.IsLibrary() {
			continue
		}
		files, err := r.Active(k)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		before, err := Read(files, active, nil)
		if err != nil {
			return nil, err
		}
		if len(before.Problems) > 0 {
			continue
		}
		after, err := Read(files, library, nil)
		if err != nil {
			return nil, err
		}
		if len(after.Problems) > 0 {
			out = append(out, FlowProblems{Project: k.Project, Problems: after.Problems})
		}
	}
	return out, nil
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
