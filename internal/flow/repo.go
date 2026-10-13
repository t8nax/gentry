package flow

import (
	"errors"
	"io/fs"
	"slices"

	"github.com/t8nax/gentry/contract"
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

// NoDraftError means neither the flow directory of the project nor the
// directory of the shared subagents has changes: the draft of the flow of a
// project holds both.
type NoDraftError struct{ Project, Dir string }

func (e *NoDraftError) Error() string { return e.Project + ": no flow draft" }

// DraftInvalidError means the draft of the flow of a project has problems,
// in the order of files and lines, or its changes of the shared subagents
// bring problems into the active flows of other projects.
type DraftInvalidError struct {
	Dir      string
	Problems []Problem      // of the draft; empty if only other flows break
	Flows    []FlowProblems // of the active flows of other projects the draft breaks, by project
}

// FlowProblems are the problems of the flow of a project.
type FlowProblems struct {
	Project  string
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
		res, err := Read(files, library)
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

// Active returns the active flow of a project, read and checked; ok is false
// if the project has no flow. The commit that applied it is r.Applied.
func Active(r *process.Repo, project string) (res *Result, ok bool, err error) {
	files, err := r.Active(process.FlowOf(project))
	if err != nil || len(files) == 0 {
		return nil, false, err
	}
	library, err := r.Active(process.Library)
	if err != nil {
		return nil, false, err
	}
	if res, err = Read(files, library); err != nil {
		return nil, false, err
	}
	return res, true, nil
}

// draftFiles are the files of the draft of the flow of a project: of its flow
// directory and of the directory of the shared subagents, as they are and as
// they are active.
type draftFiles struct {
	flow, flowActive     process.Files
	shared, sharedActive process.Files
}

// readDraft reads the files of the draft of the flow of the project at p.
func readDraft(r *process.Repo, p Places) (draftFiles, error) {
	var d draftFiles
	var err error
	k := process.FlowOf(p.Project)
	if d.flow, err = r.Working(k); err != nil {
		return d, ioError(OpRead, p.Dir, false, err)
	}
	if d.flowActive, err = r.Active(k); err != nil {
		return d, err
	}
	if d.shared, err = r.Working(process.Library); err != nil {
		return d, ioError(OpRead, p.Library, true, err)
	}
	d.sharedActive, err = r.Active(process.Library)
	return d, err
}

// flowChanged reports whether the flow directory of the project has changes.
func (d draftFiles) flowChanged() bool { return !d.flow.Equal(d.flowActive) }

// sharedChanged reports whether the directory of the shared subagents has
// changes.
func (d draftFiles) sharedChanged() bool { return !d.shared.Equal(d.sharedActive) }

// hasFlow reports whether the project has a flow, active or in the draft.
func (d draftFiles) hasFlow() bool { return len(d.flow) > 0 || len(d.flowActive) > 0 }

// read reads and checks the draft: the flow of the project as it is with the
// shared subagents as they are. With changes of the shared subagents the
// draft has their problems too, those of a subagent no stage names as well. A
// project without a flow has the problems of the shared subagents alone.
func (d draftFiles) read() (*Result, error) {
	res, err := Read(d.flow, d.shared)
	if err != nil {
		return nil, err
	}
	if !d.hasFlow() {
		res.Problems = nil
	}
	if d.sharedChanged() {
		res.Problems = withShared(res.Problems, ReadLibrary(d.shared))
	}
	res.Flow = res.Read
	if len(res.Problems) > 0 {
		res.Flow = nil
	}
	return res, nil
}

// withShared adds to the problems of a flow the problems of the shared
// subagents that it has not found itself: without a file and a line, as the
// flow has the problems of the shared subagents its stages name, before the
// files that do not belong to the flow.
func withShared(problems, shared []Problem) []Problem {
	var add []Problem
	for _, p := range shared {
		if !slices.ContainsFunc(problems, func(q Problem) bool { return q.Message == p.Message }) {
			add = append(add, Problem{Code: p.Code, Message: p.Message})
		}
	}
	i := slices.IndexFunc(problems, func(p Problem) bool { return p.Code == contract.ProblemExtraFile })
	if i < 0 {
		i = len(problems)
	}
	return slices.Concat(problems[:i], add, problems[i:])
}

// Draft returns the draft of the flow of a project, read and checked: its
// flow directory with the shared subagents, both as they are. ok is false if
// neither has changes.
func Draft(r *process.Repo, p Places) (res *Result, ok bool, err error) {
	d, err := readDraft(r, p)
	if err != nil {
		return nil, false, err
	}
	if !d.flowChanged() && !d.sharedChanged() {
		return nil, false, nil
	}
	res, err = d.read()
	return res, err == nil, err
}

// DiffResult is how the draft of the flow of a project differs from the
// active flow: its flow directory and the shared subagents.
type DiffResult struct {
	Applied *process.Applied // nil if the project has no flow
	Draft   bool             // the flow directory has changes
	Changes []Change         // the objects of the flow directory changed, file by file
	// Old is the active flow and New the draft, as far as they can be read;
	// New is Old without changes of the flow directory. Neither is nil.
	Old, New  *Flow
	Scenarios []ScenarioDiff     // of the changed scenarios, by identifier
	Problems  []Problem          // of the draft: of the flow with the shared subagents and of the shared subagents
	Library   *LibraryDiffResult // nil without changes of the shared subagents
}

// DiffDraft compares the draft of the flow of a project, its flow directory
// and the shared subagents, with the active flow. A draft may have problems:
// what can be read is compared.
func DiffDraft(r *process.Repo, p Places) (DiffResult, error) {
	var out DiffResult
	d, err := readDraft(r, p)
	if err != nil {
		return DiffResult{}, err
	}
	if !d.flowChanged() && !d.sharedChanged() {
		return DiffResult{}, &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	k := process.FlowOf(p.Project)
	old, err := Read(d.flowActive, d.sharedActive)
	if err != nil {
		return DiffResult{}, err
	}
	out.Old, out.New = old.Read, old.Read
	if a, ok, err := r.Applied(k); err != nil {
		return DiffResult{}, err
	} else if ok {
		out.Applied = &a
	}
	res, err := d.read()
	if err != nil {
		return DiffResult{}, err
	}
	out.Problems = res.Problems
	if d.flowChanged() {
		out.Draft, out.New = true, res.Read
		out.Changes = Diff(Snapshot{Files: d.flowActive}, Snapshot{Files: res.Snapshot.Files})
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
			sd := DiffScenario(before, after, stages)
			sd.Unreadable = res.Unreadable[c.ID]
			out.Scenarios = append(out.Scenarios, sd)
		}
	}
	if d.sharedChanged() {
		lib, err := sharedDiff(r, d)
		if err != nil {
			return DiffResult{}, err
		}
		if err := libraryUsers(r, p.Project, out.New, &lib); err != nil {
			return DiffResult{}, err
		}
		out.Library = &lib
	}
	return out, nil
}

// libraryUsers notes the projects whose flows name each changed shared
// subagent: project by flow, its draft as it stands, the others by their
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
		res, err := Read(files, active)
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

// Applied is what Apply made active: the commit, and the other projects
// whose flows name the shared subagents it changed, by identifier.
type Applied struct {
	process.Applied
	Projects []string
}

// Apply makes the draft of the flow of a project active with one commit: its
// flow directory and the shared subagents, which every project may change.
// A draft with problems is not applied, nor are changes of the shared
// subagents that bring problems into the active flow of another project;
// then nothing changes.
func Apply(r *process.Repo, p Places) (Applied, error) {
	d, err := readDraft(r, p)
	if err != nil {
		return Applied{}, err
	}
	if !d.flowChanged() && !d.sharedChanged() {
		return Applied{}, &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	res, err := d.read()
	if err != nil {
		return Applied{}, err
	}
	if len(res.Problems) > 0 {
		return Applied{}, &DraftInvalidError{Dir: p.Dir, Problems: res.Problems}
	}
	out := Applied{Projects: []string{}}
	var kinds []process.KindFiles
	subject := "Apply the flow of " + p.Project
	if d.flowChanged() {
		kinds = append(kinds, process.KindFiles{Kind: process.FlowOf(p.Project), Files: d.flow})
	}
	if d.sharedChanged() {
		broken, err := brokenFlows(r, d.shared, p.Project)
		if err != nil {
			return Applied{}, err
		}
		if len(broken) > 0 {
			return Applied{}, &DraftInvalidError{Dir: p.Dir, Flows: broken}
		}
		if out.Projects, err = sharedUsers(r, d, p.Project, res.Read); err != nil {
			return Applied{}, err
		}
		kinds = append(kinds, process.KindFiles{Kind: process.Library, Files: d.shared})
		subject += " and the shared subagents"
	}
	out.Applied, err = r.ApplyKinds(kinds, subject, "flow apply "+p.Project)
	return out, err
}

// sharedUsers returns the projects other than project whose active flows
// name the shared subagents the draft d changes, by identifier; fl is the
// flow of project in the draft.
func sharedUsers(r *process.Repo, d draftFiles, project string, fl *Flow) ([]string, error) {
	lib, err := sharedDiff(r, d)
	if err != nil {
		return nil, err
	}
	if err := libraryUsers(r, project, fl, &lib); err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range lib.Changes {
		for _, id := range c.Projects {
			if id != project && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// Discard brings the flow directory of a project and the directory of the
// shared subagents back to their active content, and removes the variants of
// another machine of a conflict of either.
func Discard(r *process.Repo, p Places) error {
	var kinds []process.Kind
	for _, k := range []process.Kind{process.FlowOf(p.Project), process.Library} {
		dir, conflictDir := r.KindDir(k), r.ConflictDir(k)
		draft, err := r.HasDraft(k)
		if err != nil {
			return ioError(OpRead, dir, k.IsLibrary(), err)
		}
		_, conflict, err := r.Conflict(k)
		if err != nil {
			return ioError(OpRead, conflictDir, k.IsLibrary(), err)
		}
		if draft || conflict {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return &NoDraftError{Project: p.Project, Dir: p.Dir}
	}
	for _, k := range kinds {
		if err := r.Discard(k); err != nil {
			return ioError(OpWrite, r.KindDir(k), k.IsLibrary(), err)
		}
	}
	return nil
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
