package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/project"
	"github.com/t8nax/gentry/internal/state"
)

// Objects of a flow the flags of flow show name, in the order of the spec.
var flowObjects = []struct {
	flag string
	kind string  // as the contract names it
	name msg.Key // as the refusal names it
}{
	{"scenario", "scenario", msg.FlowKindScenario},
	{"stage", "stage", msg.FlowKindStage},
	{"agent", "agent", msg.FlowKindAgent},
	{"part", "part", msg.FlowKindPart},
}

// flowProject finds the project a flow command works with: the one named by
// flag, or the one of the current directory. It only reads the state store
// and does not create it.
func flowProject(flag string) (string, *failure) {
	fail := func(f failure) (string, *failure) { return "", &f }
	path, err := state.Path()
	if err != nil {
		return fail(homeUnknown())
	}
	var projects []state.Project
	var worktrees []state.Worktree
	st, err := state.OpenRead(path)
	switch {
	case errors.Is(err, state.ErrNotExist):
		// No store, no projects: the project is not found.
	case err != nil:
		return fail(stateFailure(err))
	default:
		projects, err = st.Projects()
		if err == nil {
			worktrees, err = st.Worktrees()
		}
		st.Close()
		if err != nil {
			return fail(stateFailure(err))
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return fail(internal(err))
	}
	dir, err := paths.Canonical(wd)
	if err != nil {
		return fail(internal(err))
	}
	p, err := project.Resolve(projects, worktrees, dir, flag)
	if err != nil {
		if rf, ok := resolveFailure(err); ok {
			return fail(rf)
		}
		return fail(internal(err))
	}
	return p.ID, nil
}

// openFlow finds the project of a flow command and opens the process
// repository with its flow directory. The caller closes the repository.
func openFlow(flag string) (*process.Repo, flow.Places, *failure) {
	id, bad := flowProject(flag)
	if bad != nil {
		return nil, flow.Places{}, bad
	}
	r, bad := openProcess()
	if bad != nil {
		return nil, flow.Places{}, bad
	}
	if err := r.EnsureFlowDir(id); err != nil {
		r.Close()
		f := processFailure(err)
		return nil, flow.Places{}, &f
	}
	return r, flow.PlacesOf(r, id), nil
}

// syncFlow synchronizes the process inside a flow or library command that
// does not commit: it takes the changes of the remote repository and sends
// those of this machine.
func syncFlow(env Env, r *process.Repo) (*process.Sync, *failure) {
	s, bad := pull(r)
	if bad == nil {
		bad = push(r, s)
	}
	if bad != nil {
		return nil, bad
	}
	syncDone(env, r, s, false)
	return s, nil
}

// runFlowShow prints the active flow of a project or its draft, as a whole
// or one object of it. It only reads: the flow is checked anew at each call.
func runFlowShow(args []string, env Env) int {
	f := newFlags("flow show")
	objects := make([]*stringFlag, len(flowObjects))
	for i, o := range flowObjects {
		objects[i] = f.String(o.flag)
	}
	draft := f.Bool("draft")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	object, id := -1, ""
	var given []string
	for i, o := range flowObjects {
		v := objects[i]
		if !v.Set {
			continue
		}
		if v.Value == "" {
			return fail(env, flagValueMissing("--"+o.flag))
		}
		given = append(given, "--"+o.flag)
		object, id = i, v.Value
	}
	if len(given) > 1 {
		return fail(env, conflictingFlags("flow show", given[:2]))
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}

	r, places, bad := openFlow(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := syncFlow(env, r)
	if bad != nil {
		return fail(env, *bad)
	}
	k := process.FlowOf(places.Project)
	hasDraft, err := r.HasDraft(k)
	if err != nil {
		return fail(env, flowFailure(err, places))
	}

	var fl *flow.Flow
	out := contract.FlowShowOutput{Project: places.Project, Dir: places.Dir, Sync: syncJSON(s)}
	if a, ok, err := r.Applied(k); err != nil {
		return fail(env, flowFailure(err, places))
	} else if ok {
		out.Applied = appliedJSON(&a)
	}
	if *draft {
		res, ok, err := flow.Draft(r, places)
		if err != nil {
			return fail(env, flowFailure(err, places))
		}
		if !ok {
			return fail(env, flowFailure(&flow.NoDraftError{Project: places.Project, Dir: places.Dir}, places))
		}
		if len(res.Problems) > 0 {
			return fail(env, flowFailure(&flow.DraftInvalidError{Dir: places.Dir, Problems: res.Problems}, places))
		}
		fl = res.Flow
	} else {
		res, _, ok, err := flow.Active(r, places.Project)
		if err != nil {
			return fail(env, flowFailure(err, places))
		}
		if !ok {
			return fail(env, flowFailure(&flow.NoFlowError{Project: places.Project, Dir: places.Dir}, places))
		}
		if len(res.Problems) > 0 {
			return fail(env, flowInvalid(places, res.Problems))
		}
		fl = res.Flow
	}
	if hasDraft {
		out.Draft = &contract.FlowDraftInfo{}
		objects, conflict, err := flow.ConflictObjects(r, k)
		if err != nil {
			return fail(env, flowFailure(err, places))
		}
		if conflict {
			out.Draft.ConflictDir = &places.Conflict
			out.Draft.Conflicts = []contract.FlowObject{}
			for _, o := range objects {
				co := contract.FlowObject{Object: contract.FlowShowOutputDraftConflictsElemObject(o.Object)}
				if o.ID != "" {
					id := o.ID
					co.Id = &id
				}
				out.Draft.Conflicts = append(out.Draft.Conflicts, co)
			}
		}
	}
	var b strings.Builder
	if object >= 0 {
		narrowed, ok := writeFlowObject(&b, fl, flowObjects[object].flag, id)
		if !ok {
			return fail(env, objectNotFound(places.Project, object, id, *draft))
		}
		fl = narrowed
	}
	if *asJSON {
		return writeFlow(env, out, fl)
	}
	if object < 0 {
		writeFlowText(&b, places, out.Applied, hasDraft, fl, *draft)
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// writeFlowObject prints the object of flow fl named by flag and id, and
// returns the flow of that object alone; ok is false if there is none.
func writeFlowObject(b *strings.Builder, fl *flow.Flow, flag, id string) (*flow.Flow, bool) {
	switch flag {
	case "scenario":
		s, ok := fl.Scenario(id)
		if !ok {
			return nil, false
		}
		writeScenario(b, fl, s)
		return fl.OnlyScenario(s), true
	case "stage":
		st, ok := fl.Stage(id)
		if !ok {
			return nil, false
		}
		fmt.Fprintln(b, msg.Text(msg.FlowStage, st.ID))
		fmt.Fprintln(b, msg.Text(msg.FlowTitle, oneLine(st.Title)))
		fmt.Fprintln(b, msg.Text(msg.FlowExecutor, st.Executor))
		fmt.Fprintln(b, msg.Text(msg.FlowExit, oneLine(st.Exit)))
		fmt.Fprintln(b, msg.Text(msg.FlowParts, joined(st.Include)))
		fmt.Fprintln(b, msg.Text(msg.FlowScenarios, joined(fl.ScenariosOf(st.ID))))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowInstruction), st.Instruction)
		return fl.OnlyStage(st), true
	case "agent":
		a, ok := fl.Agent(id)
		if !ok {
			return nil, false
		}
		fmt.Fprintln(b, msg.Text(msg.FlowAgent, a.ID))
		fmt.Fprintln(b, msg.Text(msg.FlowSource, agentSource(a)))
		fmt.Fprintln(b, msg.Text(msg.FlowPurpose, oneLine(a.Purpose)))
		fmt.Fprintln(b, msg.Text(msg.FlowCapabilities, joined(a.Capabilities)))
		fmt.Fprintln(b, msg.Text(msg.FlowStages, joined(fl.StagesBy(a.ID))))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowInstruction), a.Instruction)
		return fl.OnlyAgent(a), true
	default:
		p, ok := fl.Part(id)
		if !ok {
			return nil, false
		}
		fmt.Fprintln(b, msg.Text(msg.FlowPart, p.ID))
		fmt.Fprintln(b, msg.Text(msg.FlowStages, joined(fl.StagesOf(p.ID))))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowText), p.Text)
		return fl.OnlyPart(p), true
	}
}

// objectNotFound is the refusal of flow show for an object the flow, or the
// draft, does not have.
func objectNotFound(projectID string, object int, id string, draft bool) failure {
	o := flowObjects[object]
	f := failure{
		exit:    contract.ExitError,
		code:    contract.CodeFlowObjectNotFound,
		message: msg.Text(msg.ErrFlowObjectNotFound, projectID, msg.Text(o.name), id),
		hint:    msg.Text(msg.HintFlowObjects),
		details: map[string]any{"project": projectID, "kind": o.kind, "id": id, "draft": draft},
	}
	if draft {
		f.message = msg.Text(msg.ErrDraftObjectNotFound, projectID, msg.Text(o.name), id)
		f.hint = msg.Text(msg.HintDraftObjects)
	}
	return f
}

// writeFlowText prints the summary of the flow, or of the draft, and its
// scenarios, stages and subagents as tables.
func writeFlowText(b *strings.Builder, places flow.Places, applied *contract.Applied, hasDraft bool, fl *flow.Flow, draft bool) {
	fmt.Fprintln(b, msg.Text(msg.FlowProject, places.Project))
	writeFlowApplied(b, applied)
	fmt.Fprintln(b, msg.Text(msg.FlowDir, places.Dir))
	if !draft && hasDraft {
		fmt.Fprintln(b, msg.Text(msg.FlowDraftOpened))
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColScenario), msg.Text(msg.ColTitle)}}
	for _, s := range fl.Scenarios {
		rows = append(rows, []string{s.ID, oneLine(s.Title)})
	}
	writeTable(b, rows)
	b.WriteString("\n")
	rows = [][]string{{msg.Text(msg.ColStage), msg.Text(msg.ColTitle), msg.Text(msg.ColExecutor), msg.Text(msg.ColExit)}}
	for _, st := range fl.Stages {
		rows = append(rows, []string{st.ID, oneLine(st.Title), st.Executor, oneLine(st.Exit)})
	}
	writeTable(b, rows)
	if len(fl.Agents) > 0 {
		b.WriteString("\n")
		rows = [][]string{{msg.Text(msg.ColAgent), msg.Text(msg.ColSource), msg.Text(msg.ColStages)}}
		for _, a := range fl.Agents {
			rows = append(rows, []string{a.ID, agentSource(a), joined(fl.StagesBy(a.ID))})
		}
		writeTable(b, rows)
	}
	b.WriteString("\n")
	switch {
	case draft:
		fmt.Fprintln(b, msg.Text(msg.HintFlowApply))
	case hasDraft:
		fmt.Fprintln(b, msg.Text(msg.HintFlowShowDraft))
	default:
		fmt.Fprintln(b, msg.Text(msg.HintFlowStage))
	}
}

// writeScenario prints a scenario: its default path from left to right, its
// conditional transitions and its nodes. A full drawing of the graph is left
// to the panel: arrows of forks and several loops tangle in a terminal.
func writeScenario(b *strings.Builder, fl *flow.Flow, s flow.Scenario) {
	fmt.Fprintln(b, msg.Text(msg.FlowScenario, s.ID))
	fmt.Fprintln(b, msg.Text(msg.FlowTitle, oneLine(s.Title)))
	b.WriteString("\n")
	var path []string
	for _, id := range s.DefaultPath() {
		path = append(path, nodeName(id))
	}
	writeList(b, msg.Text(msg.FlowDefaultPath), []string{strings.Join(path, " → ")})

	var conditional []string
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			switch {
			case t.If == "":
			case t.MaxRounds > 0:
				conditional = append(conditional, msg.Text(msg.FlowTransitionLimit, n.ID, nodeName(t.To),
					msg.Count(msg.FlowRounds, t.MaxRounds), oneLine(t.If)))
			default:
				conditional = append(conditional, msg.Text(msg.FlowTransition, n.ID, nodeName(t.To), oneLine(t.If)))
			}
		}
	}
	if len(conditional) > 0 {
		b.WriteString("\n")
		writeList(b, msg.Text(msg.FlowConditional), conditional)
	}

	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)}}
	for _, n := range s.Nodes {
		st, _ := fl.Stage(n.Stage)
		rows = append(rows, []string{n.ID, n.Stage, st.Executor})
	}
	writeTable(b, rows)
}

// writeText prints a text written by the operator, such as an instruction,
// under heading as it is, indented (principle 12).
func writeText(b *strings.Builder, heading, text string) {
	fmt.Fprintln(b, heading)
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		fmt.Fprintf(b, "  %s\n", msg.Text(msg.ValueNone))
		return
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "  %s\n", strings.TrimRight(line, " \t"))
	}
}

// agentSource names where a subagent comes from.
func agentSource(a flow.Agent) string {
	if a.Library {
		return msg.Text(msg.FlowSourceLibrary)
	}
	return msg.Text(msg.FlowSourceProject)
}

// joined lists identifiers in one line, or marks that there are none.
func joined(ids []string) string { return orNone(strings.Join(ids, ", ")) }

// writeFlowApplied prints when the active flow was applied, or a dash if the
// project has none.
func writeFlowApplied(b *strings.Builder, applied *contract.Applied) {
	at := msg.Text(msg.ValueNone)
	if applied != nil {
		at = localTime(applied.Time)
	}
	fmt.Fprintln(b, msg.Text(msg.FlowAppliedAt, at))
}

// nodeName returns the node as the scenario text names it: the end of the
// scenario is a word, not the reserved identifier.
func nodeName(id string) string {
	if id == flow.Finish {
		return msg.Text(msg.FlowEnd)
	}
	return id
}

// oneLine joins the lines of a text written as a block, so that a table row
// or a list item stays on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// writeFlow prints fl as the output of flow show --json; out holds the
// project, the commit applied, the directory and the draft.
func writeFlow(env Env, out contract.FlowShowOutput, fl *flow.Flow) int {
	out.Scenarios = []contract.FlowScenario{}
	out.Stages = []contract.FlowStage{}
	out.Parts = []contract.FlowPart{}
	out.Agents = []contract.FlowAgent{}
	for _, s := range fl.Scenarios {
		cs := contract.FlowScenario{Id: s.ID, Title: s.Title, Start: s.Start, Nodes: []contract.FlowNode{}}
		for _, n := range s.Nodes {
			cn := contract.FlowNode{Id: n.ID, Stage: n.Stage, Next: []contract.FlowTransition{}}
			for _, t := range n.Next {
				ct := contract.FlowTransition{To: t.To}
				if t.If != "" {
					cond := t.If
					ct.If = &cond
				}
				if t.MaxRounds > 0 {
					rounds := t.MaxRounds
					ct.MaxRounds = &rounds
				}
				cn.Next = append(cn.Next, ct)
			}
			cs.Nodes = append(cs.Nodes, cn)
		}
		out.Scenarios = append(out.Scenarios, cs)
	}
	for _, st := range fl.Stages {
		out.Stages = append(out.Stages, contract.FlowStage{
			Id: st.ID, Title: st.Title, Exit: st.Exit, Executor: st.Executor,
			Include: append([]string{}, st.Include...), Instruction: st.Instruction,
		})
	}
	for _, p := range fl.Parts {
		out.Parts = append(out.Parts, contract.FlowPart{Id: p.ID, Text: p.Text})
	}
	for _, a := range fl.Agents {
		ca := contract.FlowAgent{
			Id: a.ID, Source: contract.FlowShowOutputAgentsElemSource(flowSource(a.Library)), Purpose: a.Purpose,
			Capabilities: []contract.FlowShowOutputAgentsElemCapabilitiesElem{}, Instruction: a.Instruction,
		}
		for _, c := range a.Capabilities {
			ca.Capabilities = append(ca.Capabilities, contract.FlowShowOutputAgentsElemCapabilitiesElem(c))
		}
		out.Agents = append(out.Agents, ca)
	}
	if err := writeJSON(env, out); err != nil {
		return fail(env, internal(err))
	}
	return contract.ExitOK
}

// flowSource is where a subagent comes from as the contract names it.
func flowSource(library bool) string {
	if library {
		return "library"
	}
	return "project"
}

func runFlowDiff(args []string, env Env) int {
	f := newFlags("flow diff")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	r, places, bad := openFlow(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := syncFlow(env, r)
	if bad != nil {
		return fail(env, *bad)
	}
	res, err := flow.DiffDraft(r, places)
	if err != nil {
		return fail(env, flowFailure(err, places))
	}
	applied := appliedJSON(res.Applied)
	if *asJSON {
		out := contract.FlowDiffOutput{Project: places.Project, Applied: applied, Changes: []contract.FlowChange{}, Sync: syncJSON(s)}
		for _, c := range res.Changes {
			cc := contract.FlowChange{
				Object: contract.FlowDiffOutputChangesElemObject(c.Object),
				Change: contract.FlowDiffOutputChangesElemChange(c.Change),
			}
			if c.ID != "" {
				id := c.ID
				cc.Id = &id
			}
			out.Changes = append(out.Changes, cc)
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.FlowProject, places.Project))
	writeFlowApplied(&b, applied)
	b.WriteString("\n")
	var lines []string
	for _, c := range res.Changes {
		lines = append(lines, msg.Text(msg.FlowChange, changeObject(c), changeWord(c)))
	}
	writeList(&b, msg.Text(msg.FlowChanges), lines)
	fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintFlowApply))
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// changeObject names the object of a change.
func changeObject(c flow.Change) string {
	switch c.Object {
	case flow.ObjectCommon:
		return msg.Text(msg.FlowObjCommon)
	case flow.ObjectScenario:
		return msg.Text(msg.FlowObjScenario, c.ID)
	case flow.ObjectStage:
		return msg.Text(msg.FlowObjStage, c.ID)
	case flow.ObjectPart:
		return msg.Text(msg.FlowObjPart, c.ID)
	case flow.ObjectAgent:
		if c.Library {
			return msg.Text(msg.FlowObjLibraryAgent, c.ID)
		}
		return msg.Text(msg.FlowObjAgent, c.ID)
	}
	return c.ID
}

// changeWord names what happened to the object of a change; the common rules
// are plural.
func changeWord(c flow.Change) string {
	words := map[string]msg.Key{flow.Added: msg.FlowChangeAdded, flow.Modified: msg.FlowChangeModified, flow.Removed: msg.FlowChangeRemoved}
	if c.Object == flow.ObjectCommon {
		words = map[string]msg.Key{flow.Added: msg.FlowCommonAdded, flow.Modified: msg.FlowCommonModified, flow.Removed: msg.FlowCommonRemoved}
	}
	return msg.Text(words[c.Change])
}

func runFlowApply(args []string, env Env) int {
	f := newFlags("flow apply")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	r, places, bad := openFlow(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := pull(r)
	if bad != nil {
		return fail(env, *bad)
	}
	if code, refused := refuseOnConflict(env, r, s, process.FlowOf(places.Project)); refused {
		return code
	}
	applied, err := flow.Apply(r, places)
	if err != nil {
		syncDone(env, r, s, false)
		return fail(env, flowFailure(err, places))
	}
	if bad := push(r, s); bad != nil {
		return fail(env, *bad)
	}
	record(event{typ: flow.EventApplied, project: places.Project, data: contract.FlowAppliedData{Commit: applied.Commit}})
	syncDone(env, r, s, true)
	if *asJSON {
		out := contract.FlowApplyOutput{Project: places.Project, Applied: *appliedJSON(&applied), Sent: s.Remote && !s.Unavailable, Sync: syncJSON(s)}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(msg.FlowApplied))
	fmt.Fprintln(env.Stdout, msg.Text(msg.FlowProject, places.Project))
	fmt.Fprintln(env.Stdout, msg.Text(msg.FlowAppliedAt, localTime(applied.Time)))
	return contract.ExitOK
}

func runFlowDiscard(args []string, env Env) int {
	f := newFlags("flow discard")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	r, places, bad := openFlow(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	if err := flow.Discard(r, places); err != nil {
		return fail(env, flowFailure(err, places))
	}
	record(event{typ: flow.EventDraftDiscarded, project: places.Project})
	if *asJSON {
		if err := writeJSON(env, contract.FlowDiscardOutput{Project: places.Project, Dir: places.Dir}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(msg.FlowDiscarded))
	fmt.Fprintln(env.Stdout, msg.Text(msg.FlowProject, places.Project))
	return contract.ExitOK
}

// flowInvalid is the failure of an active flow with problems: a later Gentry
// may check more than the one that applied it.
func flowInvalid(places flow.Places, problems []flow.Problem) failure {
	var more strings.Builder
	fmt.Fprintln(&more, msg.Text(msg.FlowDir, places.Dir))
	more.WriteString("\n")
	cps := writeProblems(&more, problems)
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeFlowInvalid,
		message: msg.Text(msg.ErrFlowInvalid, places.Project),
		more:    more.String(),
		details: map[string]any{"project": places.Project, "dir": places.Dir, "problems": cps},
	}
}

// writeProblems prints the problems as a list and returns them as the
// contract has them.
func writeProblems(b *strings.Builder, problems []flow.Problem) []contract.FlowProblem {
	return writeProblemsAs(b, msg.Text(msg.FlowProblems), problems)
}

// writeProblemsAs prints the problems as a list under heading and returns
// them as the contract has them.
func writeProblemsAs(b *strings.Builder, heading string, problems []flow.Problem) []contract.FlowProblem {
	cps := make([]contract.FlowProblem, 0, len(problems))
	var lines []string
	for _, p := range problems {
		cp := contract.FlowProblem{Code: p.Code, Message: p.Message}
		if p.File != "" {
			file := p.File
			cp.File = &file
		}
		if p.Line > 0 {
			line := p.Line
			cp.Line = &line
		}
		cps = append(cps, cp)
		lines = append(lines, p.Message)
	}
	writeList(b, heading, lines)
	return cps
}

// flowFailure turns an error of a flow command for the project at places into
// a failure.
func flowFailure(err error, places flow.Places) failure {
	var (
		noFlow  *flow.NoFlowError
		noDraft *flow.NoDraftError
		invalid *flow.DraftInvalidError
		ioErr   *flow.DraftIOError
	)
	id := places.Project
	switch {
	case errors.As(err, &noFlow):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowNotFound,
			message: msg.Text(msg.ErrFlowNotFound, id),
			more:    msg.Text(msg.FlowDir, noFlow.Dir) + "\n",
			details: map[string]any{"project": id, "dir": noFlow.Dir},
		}
	case errors.As(err, &noDraft):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowDraftNotFound,
			message: msg.Text(msg.ErrDraftNotFound, id),
			more:    msg.Text(msg.FlowDir, noDraft.Dir) + "\n",
			details: map[string]any{"project": id, "dir": noDraft.Dir},
		}
	case errors.As(err, &invalid):
		var more strings.Builder
		fmt.Fprintln(&more, msg.Text(msg.FlowDir, invalid.Dir))
		more.WriteString("\n")
		cps := writeProblems(&more, invalid.Problems)
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowDraftInvalid,
			message: msg.Text(msg.ErrDraftInvalid, id),
			more:    more.String(),
			details: map[string]any{"project": id, "dir": invalid.Dir, "problems": cps},
		}
	case errors.As(err, &ioErr):
		k := map[string]msg.Key{flow.OpRead: msg.ErrDraftRead, flow.OpWrite: msg.ErrDraftWrite}[ioErr.Op]
		if ioErr.Library && ioErr.Op == flow.OpRead {
			k = msg.ErrLibraryRead
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeIOError,
			message: msg.Text(k, ioErr.Path),
			details: map[string]any{"path": ioErr.Path},
		}
	}
	return processFailure(err)
}
