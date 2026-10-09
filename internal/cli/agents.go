package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// agentsLayout is how subagents are laid out for the tool the agent works
// in. Claude Code is the one tool so far.
var agentsLayout agents.Layout = claude.Agents{}

// layoutPool is the worktrees of the pools and the tasks that hold them, as read
// for a layout. The store is closed by close.
type layoutPool struct {
	st        *state.Store // nil without a store
	owned     bool         // close closes st
	worktrees []state.Worktree
	held      map[string]state.Task // by worktree path
}

// readLayoutPool opens the state store for reading and reads the pools and
// the tasks in work; an empty pool without a store.
func readLayoutPool() (*layoutPool, *failure) {
	st, bad := openTasks()
	if bad != nil {
		return nil, bad
	}
	p, bad := poolOf(st)
	if bad != nil {
		if st != nil {
			st.Close()
		}
		return nil, bad
	}
	p.owned = true
	return p, nil
}

// poolOf reads the pools and the tasks in work of the open store st.
func poolOf(st *state.Store) (*layoutPool, *failure) {
	p := &layoutPool{st: st, held: map[string]state.Task{}}
	tasks, worktrees, bad := readTasks(st)
	if bad != nil {
		return nil, bad
	}
	p.worktrees = worktrees
	for _, t := range tasks {
		if !t.IsEnded() && t.Worktree != "" {
			p.held[t.Worktree] = t
		}
	}
	return p, nil
}

func (p *layoutPool) close() {
	if p.owned && p.st != nil {
		p.st.Close()
	}
}

// paths returns the paths of every worktree of the pools.
func (p *layoutPool) paths() []string {
	var out []string
	for _, w := range p.worktrees {
		out = append(out, w.Path)
	}
	return out
}

// of returns the worktrees of the pool of project.
func (p *layoutPool) of(project string) []state.Worktree {
	var out []state.Worktree
	for _, w := range p.worktrees {
		if w.Project == project {
			out = append(out, w)
		}
	}
	return out
}

// taskAgents returns the subagents of the snapshot of the flow of t; ok is
// false if the snapshot cannot be read.
func (p *layoutPool) taskAgents(t state.Task) ([]flow.Agent, bool, error) {
	views, err := task.Views(p.st, []state.Task{t})
	if err != nil {
		return nil, false, err
	}
	if views[0].Flow == nil {
		return nil, false, nil
	}
	return views[0].Flow.Agents, true, nil
}

// activeAgents returns the subagents of the active flow of project; none if
// the project has no flow. ok is false if the active flow has problems with
// this Gentry: its worktrees are left as they are.
func activeAgents(r *process.Repo, project string) ([]flow.Agent, bool, error) {
	res, _, ok, err := flow.Active(r, project)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, true, nil
	}
	if len(res.Problems) > 0 {
		return nil, false, nil
	}
	return res.Flow.Agents, true, nil
}

// layoutOutcome is a layout inside another command: it never fails the
// command, and what it did is told after the result of the command.
type layoutOutcome struct {
	changes   []agents.Change // the worktrees changed
	conflicts []agents.Conflict
	err       error    // the layout failed
	bad       *failure // the store or the process could not be read for it
}

func (o layoutOutcome) failed() bool { return o.err != nil || o.bad != nil }

func (o layoutOutcome) empty() bool {
	return len(o.changes) == 0 && len(o.conflicts) == 0 && !o.failed()
}

// reason names why the layout failed, as other failures of Gentry do.
func (o layoutOutcome) reason() string {
	if o.bad != nil {
		return o.bad.message
	}
	return layoutFailure(o.err).message
}

// json returns the outcome as the contract has it; nil if there is nothing
// to tell.
func (o layoutOutcome) json(p *layoutPool) *contract.AgentsLayout {
	if o.empty() {
		return nil
	}
	out := &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{}, Conflicts: conflictsJSON(o.conflicts)}
	for _, c := range o.changes {
		out.Worktrees = append(out.Worktrees, changeJSON(c, p))
	}
	if o.failed() {
		e := o.reason()
		out.Error = &e
	}
	return out
}

// layoutWorktree lays out the subagents of list in worktree path.
func layoutWorktree(p *layoutPool, path string, list []flow.Agent) layoutOutcome {
	res, err := agents.Sync(agentsLayout, []agents.Target{{Path: path, Agents: list}}, p.paths())
	return layoutOutcome{changes: res.Changed(), conflicts: res.Conflicts, err: err}
}

// layoutFree lays out the free worktrees of projects in the active flows of
// r. It reads the pool itself: a command that does not use the state store
// opens it only when there is something to lay out.
func layoutFree(r *process.Repo, projects map[string]bool) (layoutOutcome, *layoutPool) {
	if len(projects) == 0 {
		return layoutOutcome{}, nil
	}
	p, bad := readLayoutPool()
	if bad != nil {
		return layoutOutcome{bad: bad}, nil
	}
	defer p.close()
	var targets []agents.Target
	for _, id := range slices.Sorted(maps.Keys(projects)) {
		list, ok, err := activeAgents(r, id)
		if err != nil {
			return layoutOutcome{err: err}, p
		}
		if !ok {
			continue
		}
		for _, w := range p.of(id) {
			if _, held := p.held[w.Path]; !held {
				targets = append(targets, agents.Target{Path: w.Path, Agents: list})
			}
		}
	}
	if len(targets) == 0 {
		return layoutOutcome{}, p
	}
	res, err := agents.Sync(agentsLayout, targets, p.paths())
	return layoutOutcome{changes: res.Changed(), conflicts: res.Conflicts, err: err}, p
}

// layoutReleased lays out a worktree a task released, in the active flow of
// its project; st is open.
func layoutReleased(st *state.Store, project, path string) (layoutOutcome, *layoutPool) {
	r, bad := openProcess()
	if bad != nil {
		return layoutOutcome{bad: bad}, nil
	}
	defer r.Close()
	list, ok, err := activeAgents(r, project)
	if err != nil || !ok {
		return layoutOutcome{err: err}, nil
	}
	p, bad := poolOf(st)
	if bad != nil {
		return layoutOutcome{bad: bad}, nil
	}
	return layoutWorktree(p, path, list), p
}

// layoutAdded lays out the worktrees added to the pool of project, each by
// the task it holds or by the active flow; st and r are open.
func layoutAdded(st *state.Store, r *process.Repo, project string, paths []string) (layoutOutcome, *layoutPool) {
	if len(paths) == 0 {
		return layoutOutcome{}, nil
	}
	p, bad := poolOf(st)
	if bad != nil {
		return layoutOutcome{bad: bad}, nil
	}
	active, activeOK, err := activeAgents(r, project)
	if err != nil {
		return layoutOutcome{err: err}, p
	}
	var targets []agents.Target
	for _, path := range paths {
		list, ok := active, activeOK
		if t, held := p.held[path]; held {
			if list, ok, err = p.taskAgents(t); err != nil {
				return layoutOutcome{err: err}, p
			}
		}
		if ok {
			targets = append(targets, agents.Target{Path: path, Agents: list})
		}
	}
	res, err := agents.Sync(agentsLayout, targets, p.paths())
	return layoutOutcome{changes: res.Changed(), conflicts: res.Conflicts, err: err}, p
}

// syncProjects returns the projects whose free worktrees a synchronization
// changed the active flow or library of: those of the flows received, in
// conflict or restored, and those whose flow uses the library if the library
// is.
func syncProjects(r *process.Repo, s *process.Sync) map[string]bool {
	if s == nil {
		return nil
	}
	kinds := append([]process.Kind{}, s.Received...)
	kinds = append(kinds, conflictKinds(s.Conflicts)...)
	kinds = append(kinds, restoredKinds(s.Restored)...)
	out := map[string]bool{}
	for _, k := range kinds {
		if k.IsLibrary() {
			maps.Copy(out, libraryProjects(r))
			continue
		}
		out[k.Project] = true
	}
	return out
}

// libraryProjects returns the projects whose active flow has subagents of the
// library.
func libraryProjects(r *process.Repo) map[string]bool {
	out := map[string]bool{}
	kinds, err := r.Kinds()
	if err != nil {
		return out
	}
	for _, k := range kinds {
		if k.IsLibrary() {
			continue
		}
		list, ok, err := activeAgents(r, k.Project)
		if err != nil || !ok {
			continue
		}
		for _, a := range list {
			if a.Library {
				out[k.Project] = true
			}
		}
	}
	return out
}

// writeLayout tells what a layout inside another command did, as blocks
// each preceded by an empty line: the change of one worktree, or the number
// of free worktrees changed by project; then the conflicts and the failure.
func writeLayout(w io.Writer, o layoutOutcome, single bool, project string) {
	var b strings.Builder
	if len(o.changes) > 0 {
		b.WriteString("\n")
		switch {
		case single:
			fmt.Fprintln(&b, msg.Text(msg.AgentsWorktreeChanged, changesText(o.changes[0])))
		case project == "":
			fmt.Fprintln(&b, msg.Text(msg.AgentsFreeSyncedAll))
		default:
			fmt.Fprintln(&b, msg.Text(msg.AgentsFreeSynced, project))
		}
		fmt.Fprintln(&b, msg.Text(msg.AgentsNextSession))
	}
	if len(o.conflicts) > 0 {
		fmt.Fprintf(&b, "\n%s\n\n", msg.Text(msg.AgentsConflictWarning))
		writeConflicts(&b, o.conflicts)
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintAgentsConflictWarning))
	}
	if o.failed() {
		fmt.Fprintf(&b, "\n%s\n%s\n\n%s\n", msg.Text(msg.AgentsSyncFailed), msg.Text(msg.AgentsSyncFailedReason, o.reason()), msg.Text(msg.HintAgentsSyncFailed))
	}
	io.WriteString(w, b.String())
}

// writeFreeLayout tells a layout of free worktrees after a synchronization,
// by project.
func writeFreeLayout(w io.Writer, o layoutOutcome, p *layoutPool) {
	if len(o.changes) > 0 && p != nil {
		byProject := map[string]bool{}
		for _, c := range o.changes {
			for _, wt := range p.worktrees {
				if wt.Path == c.Worktree {
					byProject[wt.Project] = true
				}
			}
		}
		var b strings.Builder
		b.WriteString("\n")
		for _, id := range slices.Sorted(maps.Keys(byProject)) {
			fmt.Fprintln(&b, msg.Text(msg.AgentsFreeSynced, id))
		}
		fmt.Fprintln(&b, msg.Text(msg.AgentsNextSession))
		io.WriteString(w, b.String())
	}
	o.changes = nil
	writeLayout(w, o, false, "")
}

// changesText names what a layout changed in a worktree, such as
// «добавлены reviewer, tester; удалён linter».
func changesText(c agents.Change) string {
	var parts []string
	add := func(list []string, one, many msg.Key) {
		switch len(list) {
		case 0:
		case 1:
			parts = append(parts, msg.Text(one, list[0]))
		default:
			parts = append(parts, msg.Text(many, strings.Join(list, ", ")))
		}
	}
	add(c.Added, msg.AgentAdded, msg.AgentsAdded)
	add(c.Updated, msg.AgentUpdated, msg.AgentsUpdated)
	add(c.Removed, msg.AgentRemoved, msg.AgentsRemoved)
	return strings.Join(parts, "; ")
}

// writeConflicts prints the conflicts as a table.
func writeConflicts(b *strings.Builder, conflicts []agents.Conflict) {
	rows := [][]string{{msg.Text(msg.ColWorktree), msg.Text(msg.ColAgent), msg.Text(msg.ColFile), msg.Text(msg.ColReason)}}
	for _, c := range conflicts {
		reason := msg.Text(msg.AgentsReasonForeign)
		if c.Reason == agents.Tracked {
			reason = msg.Text(msg.AgentsReasonTracked)
		}
		rows = append(rows, []string{c.Worktree, c.Agent, c.File, reason})
	}
	writeTable(b, rows)
}

// agentsConflict is the refusal of a layout with conflicts.
func agentsConflict(conflicts []agents.Conflict) failure {
	var more strings.Builder
	more.WriteString("\n")
	writeConflicts(&more, conflicts)
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeAgentsConflict,
		message: msg.Text(msg.ErrAgentsConflict),
		more:    more.String(),
		hint:    msg.Text(msg.HintAgentsConflict),
		details: map[string]any{"conflicts": conflictsJSON(conflicts)},
	}
}

func conflictsJSON(conflicts []agents.Conflict) []contract.AgentsConflict {
	out := []contract.AgentsConflict{}
	for _, c := range conflicts {
		out = append(out, contract.AgentsConflict{Worktree: c.Worktree, Agent: c.Agent, File: c.File, Reason: c.Reason})
	}
	return out
}

func changeJSON(c agents.Change, p *layoutPool) contract.AgentsWorktree {
	out := contract.AgentsWorktree{Path: c.Worktree, Agents: nonNil(c.Agents), Added: nonNil(c.Added), Updated: nonNil(c.Updated), Removed: nonNil(c.Removed)}
	if c.Missing {
		yes := true
		out.Missing = &yes
	}
	if p != nil {
		if t, ok := p.held[c.Worktree]; ok {
			key := t.Key()
			out.Task = &key
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// runAgentsSync lays out the subagents in every worktree of the pool of a
// project: a worktree with a task by the snapshot of its flow, a free one by
// the active flow. A conflict in any worktree refuses the command, and
// nothing is written.
func runAgentsSync(args []string, env Env) int {
	f := newFlags("agents sync")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	id, bad := flowProject(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	p, bad := readLayoutPool()
	if bad != nil {
		return fail(env, *bad)
	}
	defer p.close()

	worktrees := p.of(id)
	sort.SliceStable(worktrees, func(i, j int) bool {
		a, b := worktrees[i], worktrees[j]
		if a.Main != b.Main {
			return a.Main
		}
		return a.Path < b.Path
	})
	active, activeOK, err := activeAgents(r, id)
	if err != nil {
		return fail(env, flowFailure(err, flow.PlacesOf(r, id)))
	}
	var targets []agents.Target
	laid := map[string]bool{} // worktrees whose subagents are known
	for _, w := range worktrees {
		list, ok := active, activeOK
		if t, held := p.held[w.Path]; held {
			if list, ok, err = p.taskAgents(t); err != nil {
				return fail(env, stateFailure(err))
			}
		}
		if ok {
			targets = append(targets, agents.Target{Path: w.Path, Agents: list})
			laid[w.Path] = true
		}
	}
	conflicts, err := agents.Check(agentsLayout, targets)
	if err != nil {
		return fail(env, layoutFailure(err))
	}
	if len(conflicts) > 0 {
		return fail(env, agentsConflict(conflicts))
	}
	res, err := agents.Sync(agentsLayout, targets, p.paths())
	if err != nil {
		return fail(env, layoutFailure(err))
	}
	changes := map[string]agents.Change{}
	for _, c := range res.Worktrees {
		changes[c.Worktree] = c
	}
	changed := len(res.Changed()) > 0

	if *asJSON {
		out := contract.AgentsSyncOutput{Project: id, Changed: changed, Worktrees: []contract.AgentsWorktree{}}
		for _, w := range worktrees {
			c, ok := changes[w.Path]
			if !ok {
				c = agents.Change{Worktree: w.Path}
			}
			out.Worktrees = append(out.Worktrees, changeJSON(c, p))
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	if changed {
		fmt.Fprintln(&b, msg.Text(msg.AgentsSynced, id))
		fmt.Fprintln(&b, msg.Text(msg.AgentsNextSession))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.AgentsInPlace, id))
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColWorktree), msg.Text(msg.ColTask), msg.Text(msg.ColAgents), msg.Text(msg.ColChanges)}}
	for _, w := range worktrees {
		c := changes[w.Path]
		key := ""
		if t, ok := p.held[w.Path]; ok {
			key = t.Key()
		}
		agentsCell, changesCell := strings.Join(c.Agents, ", "), changesText(c)
		if c.Missing {
			agentsCell, changesCell = "", msg.Text(msg.AgentsWorktreeMissing)
		}
		if !laid[w.Path] {
			agentsCell, changesCell = "", ""
		}
		rows = append(rows, []string{w.Path, orNone(key), orNone(agentsCell), orNone(changesCell)})
	}
	writeTable(&b, rows)
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// layoutFailure is a layout that failed: git failed, or a file could not be
// read or written.
func layoutFailure(err error) failure {
	if f, ok := gitFailure(err); ok {
		return f
	}
	var busy *agents.BusyError
	if errors.As(err, &busy) {
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeAgentsBusy,
			message: msg.Text(msg.ErrAgentsBusy),
			details: map[string]any{"path": busy.Path},
		}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return ioError(pe.Path, pe.Err)
	}
	return internal(err)
}

// layoutRefused lays out the free worktrees of a command refused after the
// synchronization or the apply changed active flows: of the projects of the
// synchronization and of also. In text, what it did goes to stderr before the
// refusal.
func layoutRefused(env Env, r *process.Repo, s *process.Sync, also ...string) {
	projects := syncProjects(r, s)
	if projects == nil {
		projects = map[string]bool{}
	}
	for _, id := range also {
		projects[id] = true
	}
	o, p := layoutFree(r, projects)
	if !env.json {
		writeFreeLayout(env.Stderr, o, p)
	}
}
