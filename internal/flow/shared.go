package flow

import (
	"strings"

	"github.com/t8nax/gentry/internal/process"
)

// ReadShared checks the files of the shared subagents by section 10.2:
// each subagent is a pair of its fields and its instruction, the fields are
// valid, and nothing else is there. Paths of problems are inside the
// directory of the shared subagents.
func ReadShared(files process.Files) []Problem {
	inside := map[string]string{}
	for p, text := range files {
		inside[agentsDir+"/"+p] = text
	}
	l := newLoader(treeOf(inside), mapShared(nil))
	l.inShared = true
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

// SharedChange is a shared subagent that differs from the active one.
type SharedChange struct {
	ID       string
	Change   string   // Added, Modified or Removed
	Old, New *Agent   // as far as they can be read; nil if absent
	Projects []string // whose flows name the subagent, by DiffDraft only
}

// SharedDiffResult is how the shared subagents of the draft differ from
// the active ones.
type SharedDiffResult struct {
	Applied *process.Applied // nil if the shared subagents have never been applied
	Changes []SharedChange
}

// sharedDiff compares the shared subagents of the draft d with the active
// ones.
func sharedDiff(r *process.Repo, d draftFiles) (SharedDiffResult, error) {
	var out SharedDiffResult
	if a, ok, err := r.Applied(process.Shared); err != nil {
		return SharedDiffResult{}, err
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
	before, after := SharedAgents(d.sharedActive), SharedAgents(d.shared)
	for _, c := range Diff(in(d.sharedActive), in(d.shared)) {
		lc := SharedChange{ID: c.ID, Change: c.Change}
		if a, ok := before[c.ID]; ok {
			lc.Old = &a
		}
		if a, ok := after[c.ID]; ok {
			lc.New = &a
		}
		out.Changes = append(out.Changes, lc)
	}
	return out, nil
}

// brokenFlows returns the problems of the active flows of the projects other
// than project that are valid with the active shared subagents and not with
// shared: a flow with problems before is not the fault of the change.
func brokenFlows(r *process.Repo, shared process.Files, project string) ([]FlowProblems, error) {
	active, err := r.Active(process.Shared)
	if err != nil {
		return nil, err
	}
	kinds, err := r.Kinds()
	if err != nil {
		return nil, err
	}
	var out []FlowProblems
	for _, k := range kinds {
		if k.IsShared() || k.Project == project {
			continue
		}
		files, err := r.Active(k)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		before, err := Read(files, active)
		if err != nil {
			return nil, err
		}
		if len(before.Problems) > 0 {
			continue
		}
		after, err := Read(files, shared)
		if err != nil {
			return nil, err
		}
		if len(after.Problems) > 0 {
			out = append(out, FlowProblems{Project: k.Project, Problems: after.Problems})
		}
	}
	return out, nil
}
