package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
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
	asJSON := f.Bool("json")
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
		return fail(env, missingField(cmd, "steps", msg.Text(msg.ErrStepsMissing), msg.Text(msg.HintCommandHelp, cmd)))
	}
	for _, s := range steps {
		if strings.TrimSpace(s) == "" {
			return fail(env, missingField(cmd, "steps", msg.Text(msg.ErrStepsMissing), msg.Text(msg.HintCommandHelp, cmd)))
		}
		if reason := task.CheckStep(s); reason != "" {
			k := msg.ErrStepTooLong
			if reason == task.TitleMultiline {
				k = msg.ErrStepMultiline
			}
			return fail(env, fieldInvalid("steps", reason, msg.Text(k), msg.Text(msg.HintStep)))
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
	return writeSteps(env, w, res, *asJSON, msg.Text(msg.StepsAdded, len(steps)))
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
	asJSON := f.Bool("json")
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
			return fail(env, invalidArgument(cmd, "step", f.args[0], msg.Text(msg.ErrStepNumberInvalid, f.args[0]), msg.Text(msg.HintSteps)))
		}
		number, ok = n, true
	}
	if !ok {
		return fail(env, missingField(cmd, "step", msg.Text(msg.ErrStepNumberMissing), msg.Text(msg.HintCommandHelp, cmd)))
	}
	note := text(values, textField)
	if !done && strings.TrimSpace(note) == "" {
		return fail(env, missingField(cmd, "reason", msg.Text(msg.ErrDropReasonMissing), msg.Text(msg.HintCommandHelp, cmd)))
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
	message := msg.Text(msg.StepMarkedDropped, number)
	if done {
		message = msg.Text(msg.StepMarkedDone, number)
	}
	return writeSteps(env, w, res, *asJSON, message)
}

// writeSteps prints the result of a change of the steps of the current pass:
// the message and the numbers of the steps added, or the message and the
// number of steps left with, once none is left, the hint to close the stage.
// The table of steps is in task show.
func writeSteps(env Env, w wayTask, res task.Steps, asJSON bool, message string) int {
	if asJSON {
		out := contract.StepListOutput{Task: res.Task.Key(), Node: res.Pass.Node, Round: res.Pass.Round, Steps: stepsJSON(res.Pass.Steps)}
		if res.Added != nil {
			out.Added = res.Added
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, message)
	if n := len(res.Added); n > 0 {
		if n == 1 {
			fmt.Fprintln(&b, msg.Text(msg.StepNumber, res.Added[0]))
		} else {
			fmt.Fprintln(&b, msg.Text(msg.StepNumbers, res.Added[0], res.Added[n-1]))
		}
		fmt.Fprint(env.Stdout, b.String())
		return contract.ExitOK
	}
	left := 0
	for _, s := range res.Pass.Steps {
		if s.State == state.StepPlanned {
			left++
		}
	}
	fmt.Fprintln(&b, msg.Text(msg.StepsLeft, left))
	if left == 0 {
		fmt.Fprintf(&b, "\n%s\n", w.hint(msg.Text(msg.HintStageExit)))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// writeStepTable prints steps as a table, or a line that there are none.
func writeStepTable(b *strings.Builder, steps []state.Step) {
	if len(steps) == 0 {
		fmt.Fprintln(b, msg.Text(msg.StepsNone))
		return
	}
	rows := [][]string{{msg.Text(msg.ColStepNumber), msg.Text(msg.ColState), msg.Text(msg.ColStep), msg.Text(msg.ColComment)}}
	for _, s := range steps {
		comment := s.Check
		if s.State == state.StepDropped {
			comment = s.Reason
		}
		rows = append(rows, []string{strconv.Itoa(s.Number), stepStateWord(s.State), s.Title, orNone(oneLine(comment))})
	}
	writeTable(b, rows)
}

// stepStateWord names the state of a step for the operator.
func stepStateWord(s string) string {
	switch s {
	case state.StepDone:
		return msg.Text(msg.StepStateDone)
	case state.StepDropped:
		return msg.Text(msg.StepStateDropped)
	}
	return msg.Text(msg.StepStatePlanned)
}
