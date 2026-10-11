package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/state"
)

// Events of the way of a task.
const (
	EventStageExited  = "stage.exited"
	EventStageSkipped = "stage.skipped"
)

// FinishedError means the scenario of the task is passed: it has no stage.
type FinishedError struct{ Task string }

func (e *FinishedError) Error() string { return e.Task + ": scenario passed" }

// FieldError means a field the stage needs is not given: the transition at
// a fork, or the reason of a transition at a fork or with a condition.
type FieldError struct {
	Field string // "to" or "reason"
	Fork  bool   // the node has more than one transition
}

func (e *FieldError) Error() string { return e.Field + ": required" }

// TransitionError means the node has no transition to the node named.
type TransitionError struct {
	Node, To    string
	Stage       flow.Stage // the stage of the node
	Transitions []string   // the nodes the node has transitions to
}

func (e *TransitionError) Error() string { return e.Node + ": no transition to " + e.To }

// ReturnLimitError means the returns by the transition are used up.
type ReturnLimitError struct {
	Node, To string
	Limit    int // the limit of the flow with the returns the operator allowed
	Allowed  int // returns of the limit the operator allowed
}

func (e *ReturnLimitError) Error() string {
	return fmt.Sprintf("%s → %s: %d returns made", e.Node, e.To, e.Limit)
}

// StepsEmptyError means the stage has no steps to close it by an exit.
type StepsEmptyError struct {
	Node  string
	Stage flow.Stage
}

func (e *StepsEmptyError) Error() string { return e.Node + ": no steps" }

// StepsOpenError means the stage has steps neither done nor dropped.
type StepsOpenError struct {
	Node  string
	Steps []state.Step
}

func (e *StepsOpenError) Error() string { return e.Node + ": steps not done" }

// ArtifactNotFoundError means the exit names an artifact the task has not saved.
type ArtifactNotFoundError struct{ Name string }

func (e *ArtifactNotFoundError) Error() string { return e.Name + ": no such artifact" }

// Close is the closing of the current stage of a task by an exit or a skip.
type Close struct {
	Task     int64 // the row of the task
	Skip     bool
	Kind     string // the kind of the exit, one of state.Exit*
	Text     string // the text of the exit
	Artifact string // the artifact of an exit of kind state.ExitArtifact
	To       string // the node to go to; required at a fork
	Reason   string // why the transition; why the skip
	Source   string
	// Progress, if set, is called while the commands of checks of the stage
	// run: when each starts and then every checks.ProgressEvery.
	Progress func()
}

// Closed is the result of closing a stage.
type Closed struct {
	Task    state.Task
	Pass    state.Pass // the pass closed
	Returns int        // which return by the transition this was; 0 if not a return
}

// closeAttempts limits how many times CloseStage starts anew when another
// command closed the stage while the commands of checks ran.
const closeAttempts = 10

// errChecksNeeded rolls back the closing of a stage whose commands of checks
// have not run for its pass yet.
var errChecksNeeded = errors.New("the commands of checks of the stage have not run")

// errPassChanged rolls back the closing of a stage whose current pass is not
// the one the commands of checks ran for.
var errPassChanged = errors.New("the current pass of the task changed")

// CloseStage closes the current stage of a task by an exit or a skip and moves
// the task by the transition, in one transaction with its event. The steps,
// the artifact and the limit of returns are checked in the transaction, so
// that of two commands closing one stage only one does.
//
// An exit of a stage with commands of checks runs them first, outside the
// transaction, in the worktree of the task: the transaction would hold the
// store for their whole time. The refusals that need no command come before
// them. The stage is then closed if its pass is still the current one;
// otherwise another command closed it, and the closing starts anew with the
// pass that is current now, as an exit right after that command would. When
// ctx is done, the command that runs is ended and the stage is not closed.
func CloseStage(ctx context.Context, st *state.Store, req Close) (Closed, error) {
	var checked int64 // the pass the commands of checks ran for
	for range closeAttempts {
		var res Closed
		var need closing
		err := st.Write(func(tx *state.Tx) error {
			c, err := prepare(tx, req)
			if err != nil {
				return err
			}
			switch {
			case checked != 0 && c.pass.ID != checked:
				return errPassChanged
			case !req.Skip && len(c.stage.Checks) > 0 && checked == 0:
				need = c
				return errChecksNeeded
			}
			res, err = c.close(tx, req)
			return err
		})
		switch {
		case errors.Is(err, errPassChanged):
			checked = 0
			continue
		case errors.Is(err, errChecksNeeded):
			if err := runChecks(ctx, need.task, need.pass, need.stage, req.Progress); err != nil {
				return Closed{}, err
			}
			checked = need.pass.ID
			continue
		case err != nil:
			return Closed{}, err
		}
		return res, nil
	}
	return Closed{}, errPassChanged
}

// closing is the closing of the current stage of a task as checked in a
// transaction.
type closing struct {
	task    state.Task
	sc      flow.Scenario
	node    flow.Node
	stage   flow.Stage
	pass    state.Pass
	passes  []state.Pass
	to      string // the node the transition goes to
	returns int    // which return by the transition this is; 0 if not a return
}

// prepare checks in tx that the current stage of the task of req can be
// closed as req asks: the task is active, the transition exists and its
// returns are not used up, the steps are done or dropped and the artifact of
// the exit is saved.
func prepare(tx *state.Tx, req Close) (closing, error) {
	t, err := active(tx, req.Task)
	if err != nil {
		return closing{}, err
	}
	fl, err := txFlow(tx, t)
	if err != nil {
		return closing{}, err
	}
	sc, node, stage, pass, err := current(tx, t, fl)
	if err != nil {
		return closing{}, err
	}
	tr, err := pick(node, stage, req)
	if err != nil {
		return closing{}, err
	}
	passes, err := tx.TaskPath(t.ID)
	if err != nil {
		return closing{}, err
	}
	loops := sc.Loops()
	counts := countReturns(loops, passes)
	returns := 0
	if l, ok := loopOf(loops, node.ID, tr.To); ok {
		ds, err := tx.Decisions(t.ID)
		if err != nil {
			return closing{}, err
		}
		allowed := allowedReturns(ds)[loopKey(l)]
		if counts[loopKey(l)] >= l.Max+allowed {
			return closing{}, &ReturnLimitError{Node: node.ID, To: tr.To, Limit: l.Max + allowed, Allowed: allowed}
		}
		returns = counts[loopKey(l)] + 1
	}
	if err := checkSteps(pass, stage, req.Skip); err != nil {
		return closing{}, err
	}
	if !req.Skip && req.Kind == state.ExitArtifact {
		if _, ok, err := tx.Artifact(t.ID, req.Artifact); err != nil {
			return closing{}, err
		} else if !ok {
			return closing{}, &ArtifactNotFoundError{Name: req.Artifact}
		}
	}
	return closing{task: t, sc: sc, node: node, stage: stage, pass: pass, passes: passes, to: tr.To, returns: returns}, nil
}

// close closes the pass of c as req asks, moves the task by the transition
// and records the event.
func (c closing) close(tx *state.Tx, req Close) (Closed, error) {
	pass := c.pass
	pass.Next, pass.Reason, pass.Source = c.to, req.Reason, req.Source
	if req.Skip {
		pass.Outcome = state.OutcomeSkip
	} else {
		pass.Outcome, pass.ExitKind, pass.ExitText = state.OutcomeExit, req.Kind, req.Text
		if req.Kind == state.ExitArtifact {
			pass.Artifact = req.Artifact
		}
	}
	pass, err := tx.ClosePass(pass)
	if err != nil {
		return Closed{}, err
	}
	if err := enter(tx, c.task, c.sc, c.passes, c.to); err != nil {
		return Closed{}, err
	}
	t, err := tx.Task(c.task.ID)
	if err != nil {
		return Closed{}, err
	}
	_, err = tx.AddEvent(closeEvent(req.Skip), t.Project, t.Key(), closeData(pass, c.returns, t.Node == state.Finish))
	if err != nil {
		return Closed{}, err
	}
	return Closed{Task: t, Pass: pass, Returns: c.returns}, nil
}

// enter moves the task to node: a new pass of the node, or the end of the
// scenario without one.
func enter(tx *state.Tx, t state.Task, sc flow.Scenario, passes []state.Pass, node string) error {
	if err := tx.SetNode(t.ID, node); err != nil {
		return err
	}
	if node == flow.Finish {
		return nil
	}
	round := 1
	for _, p := range passes {
		if p.Node == node {
			round++
		}
	}
	_, err := tx.AddPass(state.Pass{Task: t.ID, Node: node, Stage: stageOfNode(sc, node), Round: round})
	return err
}

// pick returns the transition the closing takes. At a fork, a node with more
// than one transition, the transition and its reason are required; a single
// transition with a condition needs a reason too, as the agent judges the
// condition.
func pick(node flow.Node, stage flow.Stage, req Close) (flow.Transition, error) {
	fork := len(node.Next) > 1
	if fork && req.To == "" {
		return flow.Transition{}, &FieldError{Field: "to", Fork: true}
	}
	var tr flow.Transition
	found := false
	for _, t := range node.Next {
		if t.To == req.To || !fork && req.To == "" {
			tr, found = t, true
			break
		}
	}
	if !found {
		targets := make([]string, len(node.Next))
		for i, t := range node.Next {
			targets[i] = t.To
		}
		return flow.Transition{}, &TransitionError{Node: node.ID, To: req.To, Stage: stage, Transitions: targets}
	}
	if req.Reason == "" && (fork || tr.If != "") {
		return flow.Transition{}, &FieldError{Field: "reason", Fork: fork}
	}
	return tr, nil
}

// checkSteps refuses to close a pass with steps neither done nor dropped, and
// an exit of a pass without steps, unless the operator does the stage.
func checkSteps(pass state.Pass, stage flow.Stage, skip bool) error {
	var open []state.Step
	for _, s := range pass.Steps {
		if s.State == state.StepPlanned {
			open = append(open, s)
		}
	}
	if len(open) > 0 {
		return &StepsOpenError{Node: pass.Node, Steps: open}
	}
	if !skip && len(pass.Steps) == 0 && stage.Executor != flow.Operator {
		return &StepsEmptyError{Node: pass.Node, Stage: stage}
	}
	return nil
}

// loopKey identifies a loop by its return.
func loopKey(l flow.Loop) [2]string { return [2]string{l.From, l.To} }

// loopOf returns the loop the transition from → to closes, if it is a return.
func loopOf(loops []flow.Loop, from, to string) (flow.Loop, bool) {
	for _, l := range loops {
		if l.From == from && l.To == to {
			return l, true
		}
	}
	return flow.Loop{}, false
}

// countReturns counts the returns made along the passes, by loop. A return
// by a loop starts anew the counts of the loops within it; the count of an
// outer loop and of a loop it partly overlaps do not change.
func countReturns(loops []flow.Loop, passes []state.Pass) map[[2]string]int {
	counts := map[[2]string]int{}
	for _, p := range passes {
		if p.Current() {
			continue
		}
		l, ok := loopOf(loops, p.Node, p.Next)
		if !ok {
			continue
		}
		counts[loopKey(l)]++
		for _, o := range loops {
			if o.Within(l) {
				counts[loopKey(o)] = 0
			}
		}
	}
	return counts
}

// current returns the scenario of the task, the node and the stage it is at
// and its current pass.
func current(tx *state.Tx, t state.Task, fl *flow.Flow) (flow.Scenario, flow.Node, flow.Stage, state.Pass, error) {
	fail := func(err error) (flow.Scenario, flow.Node, flow.Stage, state.Pass, error) {
		return flow.Scenario{}, flow.Node{}, flow.Stage{}, state.Pass{}, err
	}
	pass, ok, err := tx.CurrentPass(t.ID)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return fail(&FinishedError{Task: t.Key()})
	}
	sc, node, stage, err := position(fl, t.Scenario, pass.Node)
	if err != nil {
		return fail(err)
	}
	return sc, node, stage, pass, nil
}

// position finds the scenario, a node of it and the stage of the node in fl.
func position(fl *flow.Flow, scenario, node string) (flow.Scenario, flow.Node, flow.Stage, error) {
	sc, ok := fl.Scenario(scenario)
	if !ok {
		return flow.Scenario{}, flow.Node{}, flow.Stage{}, fmt.Errorf("the flow of the task has no scenario %s", scenario)
	}
	for _, n := range sc.Nodes {
		if n.ID == node {
			st, _ := fl.Stage(n.Stage)
			return sc, n, st, nil
		}
	}
	return flow.Scenario{}, flow.Node{}, flow.Stage{}, fmt.Errorf("the scenario %s has no node %s", scenario, node)
}

// stageOfNode returns the stage of node in sc.
func stageOfNode(sc flow.Scenario, node string) string {
	for _, n := range sc.Nodes {
		if n.ID == node {
			return n.Stage
		}
	}
	return node
}

// errUnreadable means the snapshot of the flow of a task cannot be read.
var errUnreadable = errors.New("the flow of the task cannot be read")

// txFlow reads the flow of the task from its snapshot in the transaction.
func txFlow(tx *state.Tx, t state.Task) (*flow.Flow, error) {
	s, ok, err := tx.Snapshot(t.Project, t.FlowCommit)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errUnreadable
	}
	return readSnapshot(s.Content)
}

// readSnapshot reads a flow from the content of a stored snapshot.
func readSnapshot(content string) (*flow.Flow, error) {
	snap, err := flow.DecodeSnapshot([]byte(content))
	if err != nil {
		return nil, errUnreadable
	}
	res, err := flow.ReadSnapshot(snap)
	if err != nil {
		return nil, err
	}
	if res.Flow == nil {
		return nil, errUnreadable
	}
	return res.Flow, nil
}

func closeEvent(skip bool) string {
	if skip {
		return EventStageSkipped
	}
	return EventStageExited
}

// closeData returns the data of the event of closing pass.
func closeData(p state.Pass, returns int, finished bool) any {
	var ret *int
	if returns > 0 {
		ret = &returns
	}
	if p.Outcome == state.OutcomeSkip {
		return contract.StageSkippedData{
			Node: p.Node, Stage: p.Stage, Round: p.Round, Reason: p.Reason, To: p.Next, Returns: ret,
			Finished: finished, Source: contract.StageSkippedDataSource(p.Source),
		}
	}
	d := contract.StageExitedData{
		Node: p.Node, Stage: p.Stage, Round: p.Round, Kind: contract.StageExitedDataKind(p.ExitKind), Text: p.ExitText,
		To: p.Next, Returns: ret, Finished: finished, Source: contract.StageExitedDataSource(p.Source),
	}
	if p.Artifact != "" {
		a := p.Artifact
		d.Artifact = &a
	}
	if p.Reason != "" {
		r := p.Reason
		d.Reason = &r
	}
	return d
}

// StageView is the current stage of a task, as stage show gives it.
type StageView struct {
	View
	Node        flow.Node
	Stage       flow.Stage
	Parts       []flow.Part // in the order of include
	Transitions []TransitionView
}

// TransitionView is a transition from the current node of a task.
type TransitionView struct {
	flow.Transition
	Stage   flow.Stage // the stage of the target; zero for the end of the scenario
	Return  bool       // the transition is a return, with a limit
	Returns int        // returns made by it, counted anew after a return by an outer loop
	Limit   int        // the limit of the flow with the returns the operator allowed
	Allowed int        // returns of the limit the operator allowed
}

// StageOf returns the current stage of the task of v from the snapshot of
// its flow; decisions are the decisions of the operator of the task, which
// raise the limits of returns.
func StageOf(v View, decisions []state.Decision) (StageView, error) {
	if v.Finished {
		return StageView{}, &FinishedError{Task: v.Key()}
	}
	if v.Flow == nil {
		return StageView{}, errUnreadable
	}
	sc, node, stage, err := position(v.Flow, v.Scenario, v.Node)
	if err != nil {
		return StageView{}, err
	}
	sv := StageView{View: v, Node: node, Stage: stage}
	for _, id := range stage.Include {
		if p, ok := v.Flow.Part(id); ok {
			sv.Parts = append(sv.Parts, p)
		}
	}
	loops := sc.Loops()
	counts := countReturns(loops, v.Path)
	allowed := allowedReturns(decisions)
	for _, t := range node.Next {
		tv := TransitionView{Transition: t}
		if t.To != flow.Finish {
			tv.Stage, _ = v.Flow.Stage(stageOfNode(sc, t.To))
		}
		if l, ok := loopOf(loops, node.ID, t.To); ok {
			tv.Return, tv.Returns, tv.Allowed = true, counts[loopKey(l)], allowed[loopKey(l)]
			tv.Limit = l.Max + tv.Allowed
		}
		sv.Transitions = append(sv.Transitions, tv)
	}
	return sv, nil
}
