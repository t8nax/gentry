package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/task"
)

// Fields of the step commands; the steps and the number are arguments.
var (
	stepAddFields  = []field{{name: "steps", kind: listField}}
	stepDoneFields = []field{{name: "step", kind: numberField}, {name: "check"}}
	stepDropFields = []field{{name: "step", kind: numberField}, {name: "reason"}}
)

func runStepAdd(args []string, env Env) int {
	const cmd = "step add"
	f := newFlags(cmd)
	key := f.String("task")
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, nil, f.args, env.Stdin, stepAddFields)
	if bad != nil {
		return fail(env, *bad)
	}
	steps, _ := values["steps"].([]string)
	if !input.Set {
		steps = f.args
	}
	if len(steps) == 0 {
		return fail(env, missingField(cmd, "steps", msg.Text(msg.ErrStepsMissing), helpHint(msg.HintCommandHelp, cmd)))
	}
	for _, s := range steps {
		if strings.TrimSpace(s) == "" {
			return fail(env, missingField(cmd, "steps", msg.Text(msg.ErrStepsMissing), helpHint(msg.HintCommandHelp, cmd)))
		}
		if reason := task.CheckStep(s); reason != "" {
			k := msg.ErrStepTooLong
			if reason == task.TitleMultiline {
				k = msg.ErrStepMultiline
			}
			return fail(env, fieldInvalid("steps", reason, msg.Text(k), hintOf(msg.HintStep)))
		}
	}
	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	res, err := task.AddSteps(w.st, w.task.ID, steps, source(env))
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}
	return writeSteps(env, w, res, stepChange{})
}

func runStepDone(args []string, env Env) int {
	return runStepClose("step done", args, env)
}

func runStepDrop(args []string, env Env) int {
	return runStepClose("step drop", args, env)
}

// runStepClose marks a step done for step done, or drops it for step drop.
func runStepClose(cmd string, args []string, env Env) int {
	done := cmd == "step done"
	fields, textField := stepDoneFields, "check"
	if !done {
		fields, textField = stepDropFields, "reason"
	}
	f := newFlags(cmd)
	flags := map[string]*stringFlag{textField: f.String(textField)}
	key := f.String("task")
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, flags, f.args, env.Stdin, fields)
	if bad != nil {
		return fail(env, *bad)
	}
	number, ok := values["step"].(int)
	if !input.Set && len(f.args) > 0 {
		n, err := strconv.Atoi(f.args[0])
		if err != nil {
			return fail(env, invalidArgument(cmd, "step", f.args[0], msg.Text(msg.ErrStepNumberInvalid, f.args[0]), hintOf(msg.HintSteps)))
		}
		number, ok = n, true
	}
	if !ok {
		return fail(env, missingField(cmd, "step", msg.Text(msg.ErrStepNumberMissing), helpHint(msg.HintCommandHelp, cmd)))
	}
	note := text(values, textField)
	if !done && strings.TrimSpace(note) == "" {
		return fail(env, missingField(cmd, "reason", msg.Text(msg.ErrDropReasonMissing), helpHint(msg.HintCommandHelp, cmd)))
	}
	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	res, err := task.CloseStep(w.st, w.task.ID, number, done, note, source(env))
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}
	return writeSteps(env, w, res, stepChange{step: number, done: done})
}

// writeSteps prints the result of a change of the steps of the current pass:
// the message and the numbers of the steps added, or the message and the
// number of steps left with, once none is left, the hint to close the stage.
// The table of steps is in task show.
func writeSteps(env Env, w wayTask, res task.Steps, change stepChange) int {
	out := contract.StepListOutput{Task: res.Task.Key(), Node: res.Pass.Node, Round: res.Pass.Round, Steps: stepsJSON(res.Pass.Steps)}
	if res.Added != nil {
		out.Added = res.Added
	}
	hints := w.hints(hintOf(msg.HintStageExit))
	return emit(env, out, func(p *page, out contract.StepListOutput) { stepsText(p, out, change, hints) })
}

// stepChange is the step a command marked, which the contract has not: its
// number, and whether it is done or dropped. Without a number the steps are
// added.
type stepChange struct {
	step int
	done bool
}

// stepsText prints what changed, then the numbers of the steps added or the
// number of steps left; hints are the hints for the task.
func stepsText(p *page, out contract.StepListOutput, change stepChange, hints []hint) {
	switch {
	case change.step == 0:
		fmt.Fprintln(p, msg.Text(msg.StepsAdded, len(out.Added)))
	case change.done:
		fmt.Fprintln(p, msg.Text(msg.StepMarkedDone, change.step))
	default:
		fmt.Fprintln(p, msg.Text(msg.StepMarkedDropped, change.step))
	}
	if n := len(out.Added); n > 0 {
		if n == 1 {
			fmt.Fprintln(p, msg.Text(msg.StepNumber, out.Added[0]))
		} else {
			fmt.Fprintln(p, msg.Text(msg.StepNumbers, out.Added[0], out.Added[n-1]))
		}
		return
	}
	left := 0
	for _, s := range out.Steps {
		if s.State == contract.TaskStepStatePlanned {
			left++
		}
	}
	fmt.Fprintln(p, msg.Text(msg.StepsLeft, left))
	if left == 0 {
		p.hints(hints...)
	}
}
