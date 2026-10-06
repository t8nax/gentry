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
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	w, bad := openWayTask(key, true)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	v, bad := w.view()
	if bad != nil {
		return w.fail(env, *bad)
	}
	sv, err := task.StageOf(v)
	if err != nil {
		return w.fail(env, wayFailure("stage show", err))
	}

	if *asJSON {
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
				limit, returns := t.MaxRounds, t.Returns
				ct.MaxReturns, ct.Returns = &limit, &returns
			}
			out.Transitions = append(out.Transitions, ct)
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.FlowStage, stageRound(sv.Stage.Title, sv.Stage.ID, v.Round, false)))
	fmt.Fprintln(&b, msg.Text(msg.StageTask, v.Key()))
	fmt.Fprintln(&b, msg.Text(msg.FlowExecutor, sv.Stage.Executor))
	fmt.Fprintln(&b, msg.Text(msg.FlowExit, orNone(oneLine(sv.Stage.Exit))))
	fmt.Fprintln(&b, msg.Text(msg.FlowParts, joined(sv.Stage.Include)))
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColTransition), msg.Text(msg.ColStage), msg.Text(msg.ColCondition), msg.Text(msg.ColReturns)}}
	for _, t := range sv.Transitions {
		title := msg.Text(msg.FlowEnd)
		if t.To != flow.Finish {
			title = stageName(t.Stage)
		}
		returns := msg.Text(msg.ValueNone)
		if t.Return {
			returns = msg.Text(msg.StageReturns, t.Returns, t.MaxRounds)
		}
		rows = append(rows, []string{t.To, title, orNone(oneLine(t.If)), returns})
	}
	writeTable(&b, rows)
	b.WriteString("\n")
	writeText(&b, msg.Text(msg.FlowInstruction), sv.Stage.Instruction)
	for _, p := range sv.Parts {
		b.WriteString("\n")
		writeText(&b, msg.Text(msg.StagePartText, p.ID), p.Text)
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
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
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, flags, nil, env.Stdin, fields)
	if bad != nil {
		return fail(env, *bad)
	}
	req := task.Close{
		Skip: skip, Kind: text(values, "kind"), Text: text(values, "text"), Artifact: text(values, "artifact"),
		To: text(values, "to"), Reason: text(values, "reason"), Source: source(),
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

	if *asJSON {
		if err := writeJSON(env, contract.StageCloseOutput{Task: taskJSON(v), Closed: passJSON(res.Pass)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	p := res.Pass
	var b strings.Builder
	title := stageName(flow.Stage{ID: p.Stage, Title: v.StageTitleOf(p.Stage)})
	if skip {
		fmt.Fprintln(&b, msg.Text(msg.StageSkipped, title))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.StageExited, title))
		fmt.Fprintln(&b, msg.Text(msg.ExitKindLine, exitKindWord(p.ExitKind, p.Artifact)))
		fmt.Fprintln(&b, msg.Text(msg.ExitTextLine, oneLine(p.ExitText)))
	}
	if skip {
		fmt.Fprintln(&b, msg.Text(msg.ReasonLine, oneLine(p.Reason)))
	}
	fmt.Fprintln(&b, msg.Text(msg.TransitionLine, nodeName(p.Next)))
	if !skip && p.Reason != "" {
		fmt.Fprintln(&b, msg.Text(msg.ReasonLine, oneLine(p.Reason)))
	}
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)))
	fmt.Fprintln(&b, msg.Text(msg.ProgressLine, progressText(v.Progress)))
	hint := msg.Text(msg.HintStageShow)
	if v.Finished {
		hint = msg.Text(msg.HintTaskShow)
	}
	fmt.Fprintf(&b, "\n%s\n", w.hint(hint))
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// checkClose refuses the fields of stage exit or stage skip that are missing
// or wrong before the task is looked at: the transition and its reason at a
// fork are checked with the stage.
func checkClose(cmd string, req task.Close, fromInput bool) *failure {
	refuse := func(f failure) *failure { return &f }
	help := msg.Text(msg.HintCommandHelp, cmd)
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
			exit:    contract.ExitUsage,
			code:    contract.CodeInputInvalid,
			message: msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputBadValue, "kind")),
			hint:    help,
			details: map[string]any{"field": "kind"},
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
			hint:    help,
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
