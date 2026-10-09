package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// exitFields are the fields of stage exit, in the order of the command spec.
var exitFields = textFields("kind", "text", "artifact", "to", "reason")

// skipFields are the fields of stage skip.
var skipFields = textFields("reason", "to")

// exitKinds are the kinds of an exit as --kind names them.
var exitKinds = []string{state.ExitArtifact, state.ExitResult, state.ExitNegative}

func runStageShow(args []string, env Env) int {
	f := newFlags("stage show")
	key := f.String("task")
	if code, done := f.parse(args, env); done {
		return code
	}
	w, bad := openWayTask(key, true)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	// A closed task has passed its scenario: it has no stage, and the
	// refusal of a passed scenario would offer to close it.
	if w.task.State == state.TaskClosed {
		return fail(env, taskFailure(&task.EndedError{Task: w.task.Key(), State: w.task.State}))
	}
	v, bad := w.view()
	if bad != nil {
		return w.fail(env, *bad)
	}
	decisions, err := w.st.Decisions(v.ID)
	if err != nil {
		return w.fail(env, stateFailure(err))
	}
	sv, err := task.StageOf(v, decisions)
	if err != nil {
		return w.fail(env, wayFailure("stage show", err))
	}

	out := contract.StageShowOutput{
		Task: v.Key(), Node: sv.Node.ID, Round: v.Round,
		Stage: contract.StageShowStage{
			Id: sv.Stage.ID, Title: sv.Stage.Title, Executor: sv.Stage.Executor, Exit: sv.Stage.Exit,
			Instruction: sv.Stage.Instruction, Parts: []contract.StageShowPart{},
		},
		Transitions: []contract.StageShowTransition{},
	}
	for _, p := range sv.Parts {
		out.Stage.Parts = append(out.Stage.Parts, contract.StageShowPart{Id: p.ID, Text: p.Text})
	}
	for _, t := range sv.Transitions {
		ct := contract.StageShowTransition{To: t.To}
		if t.To != flow.Finish {
			id, title := t.Stage.ID, t.Stage.Title
			ct.Stage, ct.Title = &id, &title
		}
		if t.If != "" {
			cond := t.If
			ct.If = &cond
		}
		if t.Return {
			limit, returns, allowed := t.Limit, t.Returns, t.Allowed
			ct.MaxReturns, ct.Returns, ct.AllowedReturns = &limit, &returns, &allowed
		}
		out.Transitions = append(out.Transitions, ct)
	}
	// The line of parts names every part the stage includes, as its file does.
	include := sv.Stage.Include
	return emit(env, out, func(p *page, out contract.StageShowOutput) { stageShowText(p, out, include) })
}

// stageShowText prints the current stage of a task: its fields, the
// transitions, the instruction and the texts of its parts; include names the
// parts the stage includes.
func stageShowText(p *page, out contract.StageShowOutput, include []string) {
	b := &p.Builder
	st := out.Stage
	fmt.Fprintln(b, msg.Text(msg.FlowStage, stageRound(st.Title, st.Id, out.Round, false)))
	fmt.Fprintln(b, msg.Text(msg.StageTask, out.Task))
	fmt.Fprintln(b, msg.Text(msg.FlowExecutor, st.Executor))
	fmt.Fprintln(b, msg.Text(msg.FlowExit, orNone(oneLine(st.Exit))))
	fmt.Fprintln(b, msg.Text(msg.FlowParts, joined(include)))
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColTransition), msg.Text(msg.ColStage), msg.Text(msg.ColCondition), msg.Text(msg.ColReturns)}}
	for _, t := range out.Transitions {
		title := msg.Text(msg.FlowEnd)
		if t.Stage != nil {
			title = stageName(flow.Stage{ID: *t.Stage, Title: *t.Title})
		}
		cond, returns := "", msg.Text(msg.ValueNone)
		if t.If != nil {
			cond = *t.If
		}
		if t.MaxReturns != nil {
			returns = msg.Text(msg.StageReturns, *t.Returns, *t.MaxReturns)
		}
		rows = append(rows, []string{t.To, title, orNone(oneLine(cond)), returns})
	}
	writeTable(b, rows)
	b.WriteString("\n")
	writeText(b, msg.Text(msg.FlowInstruction), st.Instruction)
	for _, part := range st.Parts {
		b.WriteString("\n")
		writeText(b, msg.Text(msg.StagePartText, part.Id), part.Text)
	}
}

func runStageExit(args []string, env Env) int {
	return runStageClose("stage exit", args, env)
}

func runStageSkip(args []string, env Env) int {
	return runStageClose("stage skip", args, env)
}

// runStageClose closes the current stage of a task: by an exit for stage
// exit, by a skip for stage skip.
func runStageClose(cmd string, args []string, env Env) int {
	skip := cmd == "stage skip"
	fields := exitFields
	if skip {
		fields = skipFields
	}
	f := newFlags(cmd)
	flags := map[string]*stringFlag{}
	for _, fl := range fields {
		flags[fl.name] = f.String(fl.name)
	}
	key := f.String("task")
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, flags, nil, env.Stdin, fields)
	if bad != nil {
		return fail(env, *bad)
	}
	req := task.Close{
		Skip: skip, Kind: text(values, "kind"), Text: text(values, "text"), Artifact: text(values, "artifact"),
		To: text(values, "to"), Reason: text(values, "reason"), Source: source(env),
	}
	if bad := checkClose(cmd, req, input.Set); bad != nil {
		return fail(env, *bad)
	}

	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	req.Task = w.task.ID
	res, err := task.CloseStage(w.st, req)
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}
	v, bad := w.view()
	if bad != nil {
		return w.fail(env, *bad)
	}

	out := contract.StageCloseOutput{Task: taskJSON(v), Closed: passJSON(res.Pass)}
	x := stageCloseExtra{title: v.StageTitleOf(res.Pass.Stage), hints: w.hints(hintOf(msg.HintStageShow))}
	if v.Finished {
		x.hints = w.hints(hintOf(msg.HintTaskClose))
	}
	return emit(env, out, func(p *page, out contract.StageCloseOutput) { stageCloseText(p, out, x) })
}

// stageCloseExtra is what the text of stage exit and stage skip has apart
// from the contract: the title of the stage closed, by the snapshot, and the
// hints for the task.
type stageCloseExtra struct {
	title string
	hints []hint
}

// stageCloseText prints the stage closed: by its exit or its skip, the
// transition, the stage the task is at now and its progress.
func stageCloseText(p *page, out contract.StageCloseOutput, x stageCloseExtra) {
	c := out.Closed
	title := stageName(flow.Stage{ID: c.Stage, Title: x.title})
	reason := ""
	if c.Reason != nil {
		reason = *c.Reason
	}
	if c.Exit == nil {
		fmt.Fprintln(p, msg.Text(msg.StageSkipped, title))
		fmt.Fprintln(p, msg.Text(msg.ReasonLine, oneLine(reason)))
	} else {
		fmt.Fprintln(p, msg.Text(msg.StageExited, title))
		fmt.Fprintln(p, msg.Text(msg.ExitKindLine, exitWord(*c.Exit)))
		fmt.Fprintln(p, msg.Text(msg.ExitTextLine, oneLine(c.Exit.Text)))
	}
	fmt.Fprintln(p, msg.Text(msg.TransitionLine, nodeName(*c.To)))
	if c.Exit != nil && reason != "" {
		fmt.Fprintln(p, msg.Text(msg.ReasonLine, oneLine(reason)))
	}
	fmt.Fprintln(p, msg.Text(msg.TaskStage, stageLine(out.Task)))
	fmt.Fprintln(p, msg.Text(msg.ProgressLine, progressLine(out.Task.Progress)))
	p.hints(x.hints...)
}

// checkClose refuses the fields of stage exit or stage skip that are missing
// or wrong before the task is looked at: the transition and its reason at a
// fork are checked with the stage.
func checkClose(cmd string, req task.Close, fromInput bool) *failure {
	refuse := func(f failure) *failure { return &f }
	help := helpHint(msg.HintCommandHelp, cmd)
	if req.Skip {
		if strings.TrimSpace(req.Reason) == "" {
			return refuse(missingField(cmd, "reason", msg.Text(msg.ErrSkipReasonMissing), help))
		}
		return nil
	}
	switch {
	case req.Kind == "":
		return refuse(missingField(cmd, "kind", msg.Text(msg.ErrExitKindMissing), help))
	case !slices.Contains(exitKinds, req.Kind) && fromInput:
		return refuse(failure{
			exit:         contract.ExitUsage,
			code:         contract.CodeInputInvalid,
			message:      msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputBadValue, "kind")),
			agentMessage: msg.Text(msg.ErrToolFields, msg.Text(msg.InputBadValue, "kind")),
			hints:        []hint{help},
			details:      map[string]any{"field": "kind"},
		})
	case !slices.Contains(exitKinds, req.Kind):
		return refuse(flagValueInvalid("--kind", req.Kind, msg.Text(msg.ErrFlagValueInvalid, "--kind", req.Kind), help))
	case strings.TrimSpace(req.Text) == "":
		return refuse(missingField(cmd, "text", msg.Text(msg.ErrExitTextMissing), help))
	case req.Kind == state.ExitArtifact && req.Artifact == "":
		return refuse(missingField(cmd, "artifact", msg.Text(msg.ErrExitArtifactMissing), help))
	case req.Kind != state.ExitArtifact && req.Artifact != "":
		return refuse(failure{
			exit:    contract.ExitUsage,
			code:    contract.CodeConflictingFlags,
			message: msg.Text(msg.ErrArtifactForKind),
			hints:   []hint{help},
			details: map[string]any{"command": cmd, "flags": []string{"--kind", "--artifact"}},
		})
	}
	return nil
}

// exitKindWord names the kind of an exit for the operator.
func exitKindWord(kind, artifact string) string {
	switch kind {
	case state.ExitArtifact:
		return msg.Text(msg.ExitKindArtifact, artifact)
	case state.ExitNegative:
		return msg.Text(msg.ExitKindNegative)
	}
	return msg.Text(msg.ExitKindResult)
}

// passJSON returns a pass as the contract has it.
func passJSON(p state.Pass) contract.TaskPass {
	cp := contract.TaskPass{Node: p.Node, Stage: p.Stage, Round: p.Round, Entered: p.Entered, Steps: stepsJSON(p.Steps)}
	if p.Current() {
		return cp
	}
	outcome, src, to, closed := contract.TaskPassOutcome(p.Outcome), contract.TaskPassSource(p.Source), p.Next, p.Closed
	cp.Outcome, cp.Source, cp.To, cp.Closed = &outcome, &src, &to, &closed
	if p.Reason != "" {
		r := p.Reason
		cp.Reason = &r
	}
	if p.Outcome == state.OutcomeExit {
		cp.Exit = &contract.TaskPassExit{Kind: contract.TaskPassExitKind(p.ExitKind), Text: p.ExitText}
		if p.Artifact != "" {
			a := p.Artifact
			cp.Exit.Artifact = &a
		}
	}
	return cp
}

// stepsJSON returns steps as the contract has them.
func stepsJSON(steps []state.Step) []contract.TaskStep {
	out := []contract.TaskStep{}
	for _, s := range steps {
		cs := contract.TaskStep{Number: s.Number, Title: s.Title, State: contract.TaskStepState(s.State),
			Source: contract.TaskStepSource(s.Source), Added: s.Added}
		if s.Check != "" {
			c := s.Check
			cs.Check = &c
		}
		if s.Reason != "" {
			r := s.Reason
			cs.Reason = &r
		}
		if s.State != state.StepPlanned {
			src, closed := contract.TaskStepClosedSource(s.ClosedSource), s.Closed
			cs.ClosedSource, cs.Closed = &src, &closed
		}
		out = append(out, cs)
	}
	return out
}
