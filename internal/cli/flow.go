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
func syncFlow(env Env, r *process.Repo) (*process.Sync, *contract.AgentsLayout, *failure) {
	s, bad := pull(r)
	if bad == nil {
		bad = push(r, s)
		if bad != nil {
			// What the pull took is active all the same.
			layoutRefused(env, r, s)
		}
	}
	if bad != nil {
		return nil, nil, bad
	}
	return s, syncDone(env, r, s, false), nil
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
	s, synced, bad := syncFlow(env, r)
	if bad != nil {
		return fail(env, *bad)
	}
	k := process.FlowOf(places.Project)
	hasDraft, err := r.HasDraft(k)
	if err != nil {
		return fail(env, flowFailure(err, places))
	}

	var fl *flow.Flow
	out := contract.FlowShowOutput{Project: places.Project, Dir: places.Dir, Sync: syncJSON(s, synced)}
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
	x := flowShowExtra{draft: *draft}
	if object >= 0 {
		narrowed, uses, ok := flowObject(fl, flowObjects[object].flag, id)
		if !ok {
			return fail(env, objectNotFound(places.Project, object, id, *draft))
		}
		fl, x.object, x.uses = narrowed, flowObjects[object].flag, uses
	}
	return emit(env, flowJSON(out, fl), func(p *page, out contract.FlowShowOutput) { flowShowText(p, out, x) })
}

// flowShowExtra is what the text of flow show has apart from the contract:
// whether the draft is shown, the flag of the object shown alone and where
// that object is used, which the flow of the object alone has not.
type flowShowExtra struct {
	draft  bool
	object string   // scenario, stage, agent or part; empty for the whole flow
	uses   []string // the scenarios of a stage, the stages of a subagent or of a part
}

// flowObject returns the flow of the object of fl named by flag and id alone,
// and where the object is used: the scenarios of a stage, the stages of a
// subagent or of a part. ok is false if there is no such object.
func flowObject(fl *flow.Flow, flag, id string) (*flow.Flow, []string, bool) {
	switch flag {
	case "scenario":
		s, ok := fl.Scenario(id)
		if !ok {
			return nil, nil, false
		}
		return fl.OnlyScenario(s), nil, true
	case "stage":
		st, ok := fl.Stage(id)
		if !ok {
			return nil, nil, false
		}
		return fl.OnlyStage(st), fl.ScenariosOf(st.ID), true
	case "agent":
		a, ok := fl.Agent(id)
		if !ok {
			return nil, nil, false
		}
		return fl.OnlyAgent(a), fl.StagesBy(a.ID), true
	default:
		p, ok := fl.Part(id)
		if !ok {
			return nil, nil, false
		}
		return fl.OnlyPart(p), fl.StagesOf(p.ID), true
	}
}

// flowShowText prints the object shown alone, or the summary of the flow,
// or of the draft, and its scenarios, stages and subagents as tables.
func flowShowText(p *page, out contract.FlowShowOutput, x flowShowExtra) {
	b := &p.Builder
	switch x.object {
	case "scenario":
		writeScenario(b, out, out.Scenarios[0])
		return
	case "stage":
		st := out.Stages[0]
		fmt.Fprintln(b, msg.Text(msg.FlowStage, st.Id))
		fmt.Fprintln(b, msg.Text(msg.FlowTitle, oneLine(st.Title)))
		fmt.Fprintln(b, msg.Text(msg.FlowExecutor, st.Executor))
		fmt.Fprintln(b, msg.Text(msg.FlowExit, oneLine(st.Exit)))
		fmt.Fprintln(b, msg.Text(msg.FlowParts, joined(st.Include)))
		fmt.Fprintln(b, msg.Text(msg.FlowScenarios, joined(x.uses)))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowInstruction), st.Instruction)
		return
	case "agent":
		a := out.Agents[0]
		fmt.Fprintln(b, msg.Text(msg.FlowAgent, a.Id))
		fmt.Fprintln(b, msg.Text(msg.FlowSource, agentSource(a)))
		fmt.Fprintln(b, msg.Text(msg.FlowPurpose, oneLine(a.Purpose)))
		var capabilities []string
		for _, c := range a.Capabilities {
			capabilities = append(capabilities, string(c))
		}
		fmt.Fprintln(b, msg.Text(msg.FlowCapabilities, joined(capabilities)))
		fmt.Fprintln(b, msg.Text(msg.FlowStages, joined(x.uses)))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowInstruction), a.Instruction)
		return
	case "part":
		part := out.Parts[0]
		fmt.Fprintln(b, msg.Text(msg.FlowPart, part.Id))
		fmt.Fprintln(b, msg.Text(msg.FlowStages, joined(x.uses)))
		b.WriteString("\n")
		writeText(b, msg.Text(msg.FlowText), part.Text)
		return
	}
	fmt.Fprintln(b, msg.Text(msg.FlowProject, out.Project))
	writeFlowApplied(b, out.Applied)
	fmt.Fprintln(b, msg.Text(msg.FlowDir, out.Dir))
	if !x.draft && out.Draft != nil {
		fmt.Fprintln(b, msg.Text(msg.FlowDraftOpened))
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColScenario), msg.Text(msg.ColTitle)}}
	for _, s := range out.Scenarios {
		rows = append(rows, []string{s.Id, oneLine(s.Title)})
	}
	writeTable(b, rows)
	b.WriteString("\n")
	rows = [][]string{{msg.Text(msg.ColStage), msg.Text(msg.ColTitle), msg.Text(msg.ColExecutor), msg.Text(msg.ColExit)}}
	for _, st := range out.Stages {
		rows = append(rows, []string{st.Id, oneLine(st.Title), st.Executor, oneLine(st.Exit)})
	}
	writeTable(b, rows)
	if len(out.Agents) > 0 {
		b.WriteString("\n")
		rows = [][]string{{msg.Text(msg.ColAgent), msg.Text(msg.ColSource), msg.Text(msg.ColStages)}}
		for _, a := range out.Agents {
			var stages []string
			for _, st := range out.Stages {
				if st.Executor == a.Id {
					stages = append(stages, st.Id)
				}
			}
			rows = append(rows, []string{a.Id, agentSource(a), joined(stages)})
		}
		writeTable(b, rows)
	}
	switch {
	case x.draft:
		p.hints(hintOf(msg.HintFlowApply))
	case out.Draft != nil:
		p.hints(hintOf(msg.HintFlowShowDraft))
	default:
		p.hints(hintOf(msg.HintFlowStage))
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
		hints:   []hint{hintOf(msg.HintFlowObjects)},
		details: map[string]any{"project": projectID, "kind": o.kind, "id": id, "draft": draft},
	}
	if draft {
		f.message = msg.Text(msg.ErrDraftObjectNotFound, projectID, msg.Text(o.name), id)
		f.hints = []hint{hintOf(msg.HintDraftObjects)}
	}
	return f
}

// writeScenario prints scenario s of the flow out: its default path from
// left to right, its conditional transitions and its nodes. A full drawing
// of the graph is left to the panel: arrows of forks and several loops
// tangle in a terminal.
func writeScenario(b *strings.Builder, out contract.FlowShowOutput, s contract.FlowScenario) {
	fmt.Fprintln(b, msg.Text(msg.FlowScenario, s.Id))
	fmt.Fprintln(b, msg.Text(msg.FlowTitle, oneLine(s.Title)))
	b.WriteString("\n")
	var path []string
	for _, id := range scenarioOf(s).DefaultPath() {
		path = append(path, nodeName(id))
	}
	writeList(b, msg.Text(msg.FlowDefaultPath), []string{strings.Join(path, " → ")})

	var conditional []string
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			switch {
			case t.If == nil:
			case t.MaxRounds != nil:
				conditional = append(conditional, msg.Text(msg.FlowTransitionLimit, n.Id, nodeName(t.To),
					msg.Count(msg.FlowRounds, *t.MaxRounds), oneLine(*t.If)))
			default:
				conditional = append(conditional, msg.Text(msg.FlowTransition, n.Id, nodeName(t.To), oneLine(*t.If)))
			}
		}
	}
	if len(conditional) > 0 {
		b.WriteString("\n")
		writeList(b, msg.Text(msg.FlowConditional), conditional)
	}

	b.WriteString("\n")
	executors := map[string]string{}
	for _, st := range out.Stages {
		executors[st.Id] = st.Executor
	}
	rows := [][]string{{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)}}
	for _, n := range s.Nodes {
		rows = append(rows, []string{n.Id, n.Stage, executors[n.Stage]})
	}
	writeTable(b, rows)
}

// scenarioOf returns the graph of scenario s as the flow has it.
func scenarioOf(s contract.FlowScenario) flow.Scenario {
	out := flow.Scenario{ID: s.Id, Title: s.Title, Start: s.Start}
	for _, n := range s.Nodes {
		node := flow.Node{ID: n.Id, Stage: n.Stage}
		for _, t := range n.Next {
			tr := flow.Transition{To: t.To}
			if t.If != nil {
				tr.If = *t.If
			}
			if t.MaxRounds != nil {
				tr.MaxRounds = *t.MaxRounds
			}
			node.Next = append(node.Next, tr)
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out
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
func agentSource(a contract.FlowAgent) string {
	if a.Source == contract.FlowShowOutputAgentsElemSourceLibrary {
		return msg.Text(msg.FlowSourceLibrary)
	}
	return msg.Text(msg.FlowSourceProject)
}

// joined lists identifiers in one line, or marks that there are none.
func joined(ids []string) string { return orNone(strings.Join(ids, ", ")) }

// writeFlowApplied prints when the active flow was applied, or a dash if the
// project has none.
func writeFlowApplied(b *strings.Builder, applied *contract.Applied) {
	fmt.Fprintln(b, msg.Text(msg.FlowAppliedAt, appliedText(applied)))
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

// flowJSON returns out, which holds the project, the commit applied, the
// directory and the draft, with the objects of fl as flow show gives them.
func flowJSON(out contract.FlowShowOutput, fl *flow.Flow) contract.FlowShowOutput {
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
	return out
}

// flowSource is where a subagent comes from as the contract names it.
func flowSource(library bool) string {
	if library {
		return "library"
	}
	return "project"
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
		layoutRefused(env, r, s, places.Project)
		return fail(env, *bad)
	}
	record(event{typ: flow.EventApplied, project: places.Project, data: contract.FlowAppliedData{Commit: applied.Commit}})
	synced := syncDone(env, r, s, true)
	layout, laidPool := layoutFree(r, map[string]bool{places.Project: true})
	out := contract.FlowApplyOutput{Project: places.Project, Applied: *appliedJSON(&applied), Sent: s.Remote && !s.Unavailable, Sync: syncJSON(s, synced), Agents: layout.json(laidPool)}
	return emit(env, out, flowApplyText)
}

// flowApplyText prints the flow applied and the layout of the free
// worktrees of its project.
func flowApplyText(p *page, out contract.FlowApplyOutput) {
	fmt.Fprintln(p, msg.Text(msg.FlowApplied))
	fmt.Fprintln(p, msg.Text(msg.FlowProject, out.Project))
	fmt.Fprintln(p, msg.Text(msg.FlowAppliedAt, localTime(out.Applied.Time)))
	writeLayout(p, out.Agents, false, out.Project)
}

func runFlowDiscard(args []string, env Env) int {
	f := newFlags("flow discard")
	proj := f.String("project")
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
	return emit(env, contract.FlowDiscardOutput{Project: places.Project, Dir: places.Dir}, flowDiscardText)
}

// flowDiscardText prints the draft discarded and its project.
func flowDiscardText(p *page, out contract.FlowDiscardOutput) {
	fmt.Fprintln(p, msg.Text(msg.FlowDiscarded))
	fmt.Fprintln(p, msg.Text(msg.FlowProject, out.Project))
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
