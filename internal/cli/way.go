package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// The commands of the way of a task — stage, step, note and artifact — work
// with the task named by --task or with the task of the current worktree,
// so that the panel and the operator can call them from any directory.

// taskFlag is the flag that names the task of a command.
var taskFlag = flagSpec{name: "task", value: msg.ArgTask, desc: descText(msg.ArgTaskDesc)}

// inputFlag is --input of a command whose fields are names; withArgs tells
// that some of them are arguments.
func inputFlag(names string, withArgs bool) flagSpec {
	k := msg.FlagInputFields
	if withArgs {
		k = msg.FlagInputFieldsArgs
	}
	return flagSpec{name: "input", value: msg.ArgFile, desc: func() string { return msg.Text(k, names) }}
}

// inputFlagWith is inputFlag with a line on the fields after the first line,
// for fields that are objects.
func inputFlagWith(names string, withArgs bool, fields msg.Key) flagSpec {
	f := inputFlag(names, withArgs)
	desc := f.desc
	f.desc = func() string {
		first, rest, _ := strings.Cut(desc(), "\n")
		return first + "\n" + msg.Text(fields) + "\n" + rest
	}
	return f
}

// wayTask is the task a command works with.
type wayTask struct {
	st   *state.Store
	task state.Task
	here bool // the current directory is in the worktree of the task: hints need no --task
}

// openWayTask opens the state store, for writing unless read is set, and
// finds the task named by key, or the task of the current worktree if key is
// not set. A command that writes refuses a closed or cancelled task. The
// caller closes the store; without a store there is no task.
func openWayTask(key *stringFlag, read bool) (wayTask, *failure) {
	if key.Set && key.Value == "" {
		f := flagValueMissing("--task")
		return wayTask{}, &f
	}
	return openTask(key.Value, key.Set, read, func(v string) failure {
		return flagValueInvalid("--task", v, msg.Text(msg.ErrTaskKeyInvalid, v), msg.Text(msg.HintTaskKey))
	})
}

// openArgTask is openWayTask for a command of the group task, which names
// the task by its first argument.
func openArgTask(cmd string, args []string, read bool) (wayTask, *failure) {
	var key string
	if len(args) > 0 {
		key = args[0]
	}
	return openTask(key, len(args) > 0, read, func(v string) failure {
		return invalidArgument(cmd, "task", v, msg.Text(msg.ErrTaskKeyInvalid, v), msg.Text(msg.HintTaskKey))
	})
}

// openTask finds the task named by key if named is set, or the task of the
// current worktree; badKey is the refusal of a key that names no task.
func openTask(key string, named, read bool, badKey func(string) failure) (wayTask, *failure) {
	fail := func(f failure) (wayTask, *failure) { return wayTask{}, &f }
	// A store that does not exist has no tasks: it is not created to say so.
	st, bad := openTasks()
	if bad != nil {
		return wayTask{}, bad
	}
	if st != nil && !read {
		path := st.Path()
		st.Close()
		var err error
		if st, err = state.Open(path); err != nil {
			return fail(stateFailure(err))
		}
	}
	closeOnFail := func(f failure) (wayTask, *failure) {
		if st != nil {
			st.Close()
		}
		return fail(f)
	}
	wd, err := os.Getwd()
	if err != nil {
		return closeOnFail(internal(err))
	}
	var t state.Task
	if named {
		t, err = task.Find(st, key)
	} else {
		var worktrees []state.Worktree
		if worktrees, bad = readWorktrees(st); bad != nil {
			return closeOnFail(*bad)
		}
		t, err = task.Current(st, worktrees, wd)
	}
	var invalid *task.InvalidKeyError
	switch {
	case errors.As(err, &invalid):
		return closeOnFail(badKey(invalid.Value))
	case err != nil:
		return closeOnFail(lookupFailure(err))
	case !read && t.IsEnded():
		return closeOnFail(taskFailure(&task.EndedError{Task: t.Key(), State: t.State}))
	}
	here := false
	if d, err := paths.Canonical(wd); err == nil && t.Worktree != "" {
		here = paths.Within(d, t.Worktree)
	}
	return wayTask{st: st, task: t, here: here}, nil
}

// close closes the store of the task.
func (w wayTask) close() {
	if w.st != nil {
		w.st.Close()
	}
}

// hint returns a hint for the task: outside its worktree each command in it
// names the task, as the commands of the group task do by their argument and
// other commands by --task. A command that names the task already is left
// as it is.
func (w wayTask) hint(h string) string {
	if w.here || h == "" {
		return h
	}
	key := w.task.Key()
	lines := strings.Split(h, "\n")
	for i, l := range lines {
		switch {
		case !strings.Contains(l, "gentry "), strings.HasSuffix(l, "--help"), strings.Contains(l, " "+key):
		default:
			var ok bool
			if lines[i], ok = withArg(l, key); !ok {
				lines[i] = l + " --task " + key
			}
		}
	}
	return strings.Join(lines, "\n")
}

// argCommands are the commands that name the task by their argument.
var argCommands = []string{"gentry task show", "gentry task close", "gentry task cancel", "gentry task attempts"}

// withArg names the task in a line of a hint with a command that takes it
// by its argument, right after the command; ok is false if the line has no
// such command.
func withArg(line, key string) (string, bool) {
	for _, c := range argCommands {
		i := strings.Index(line, c)
		if i < 0 {
			continue
		}
		if rest := line[i+len(c):]; rest == "" || rest[0] == ' ' {
			return line[:i+len(c)] + " " + key + rest, true
		}
	}
	return line, false
}

// fail prints a failure with its hint for the task.
func (w wayTask) fail(env Env, f failure) int {
	f.hint = w.hint(f.hint)
	return fail(env, f)
}

// view returns the view of the task as the store has it now.
func (w wayTask) view() (task.View, *failure) {
	t, ok, err := w.st.Task(w.task.ID)
	if err == nil && ok {
		var views []task.View
		if views, err = task.Views(w.st, []state.Task{t}); err == nil {
			return views[0], nil
		}
	}
	f := stateFailure(err)
	if err == nil {
		f = internal(fmt.Errorf("task %s is gone", w.task.Key()))
	}
	return task.View{}, &f
}

// source is who calls the command: the agent or the operator (R122).
func source() string {
	if caller.ByAgent() {
		return state.SourceAgent
	}
	return state.SourceOperator
}

// missingField is the refusal of a command for a required field not given.
func missingField(cmd, field string, message, hint string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeMissingField,
		message: message,
		hint:    hint,
		details: map[string]any{"command": cmd, "field": field},
	}
}

// fieldInvalid is the refusal of a command for a field it cannot accept.
func fieldInvalid(field, reason, message, hint string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFieldInvalid,
		message: message,
		hint:    hint,
		details: map[string]any{"field": field, "reason": reason},
	}
}

// stepNumbers returns the numbers of steps.
func stepNumbers(steps []state.Step) []int {
	ns := make([]int, len(steps))
	for i, s := range steps {
		ns[i] = s.Number
	}
	return ns
}

// wayFailure turns an error of a command of the way of a task into a
// failure; cmd is the command, for the hint to its help.
func wayFailure(cmd string, err error) failure {
	if f, ok := endFailure(err); ok {
		return f
	}
	var (
		finished   *task.FinishedError
		field      *task.FieldError
		transition *task.TransitionError
		limit      *task.ReturnLimitError
		noReturn   *task.ReturnNotFoundError
		empty      *task.StepsEmptyError
		open       *task.StepsOpenError
		noStep     *task.StepNotFoundError
		closed     *task.StepClosedError
		noArtifact *task.ArtifactNotFoundError
		file       *task.FileError
	)
	switch {
	case errors.As(err, &finished):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeScenarioFinished,
			message: msg.Text(msg.ErrScenarioFinished, finished.Task),
			hint:    msg.Text(msg.HintTaskClose),
			details: map[string]any{"task": finished.Task},
		}
	case errors.As(err, &field) && field.Field == "to":
		return missingField(cmd, "to", msg.Text(msg.ErrForkToMissing), msg.Text(msg.HintStageTransitions))
	case errors.As(err, &field):
		k := msg.ErrReasonMissing
		if field.Fork {
			k = msg.ErrForkReasonMissing
		}
		return missingField(cmd, "reason", msg.Text(k), msg.Text(msg.HintCommandHelp, cmd))
	case errors.As(err, &transition):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTransitionNotFound,
			message: msg.Text(msg.ErrTransitionNotFound, stageName(transition.Stage), transition.To),
			hint:    msg.Text(msg.HintStageTransitions),
			details: map[string]any{"node": transition.Node, "to": transition.To, "transitions": transition.Transitions},
		}
	case errors.As(err, &limit):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeReturnLimit,
			message: msg.Text(msg.ErrReturnLimit, limit.To),
			more:    msg.Text(msg.ReturnsLine, limit.Limit, limit.Limit) + "\n",
			hint:    msg.Text(msg.HintOtherTransitions) + "\n" + msg.Text(msg.HintAllowReturn, limit.To),
			details: map[string]any{"node": limit.Node, "to": limit.To, "limit": limit.Limit, "allowed": limit.Allowed},
		}
	case errors.As(err, &noReturn):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeReturnNotFound,
			message: msg.Text(msg.ErrReturnNotFound, stageName(noReturn.Stage), noReturn.To),
			hint:    msg.Text(msg.HintStageTransitions),
			details: map[string]any{"node": noReturn.Node, "to": noReturn.To, "returns": noReturn.Returns},
		}
	case errors.As(err, &empty):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStepsEmpty,
			message: msg.Text(msg.ErrStepsEmpty, stageName(empty.Stage)),
			hint:    msg.Text(msg.HintStepAdd),
			details: map[string]any{"node": empty.Node, "stage": empty.Stage.ID},
		}
	case errors.As(err, &open):
		var items []string
		for _, s := range open.Steps {
			items = append(items, fmt.Sprintf("%d. %s", s.Number, s.Title))
		}
		var more strings.Builder
		more.WriteString("\n")
		writeList(&more, msg.Text(msg.StepsOpenHeading), items)
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStepsOpen,
			message: msg.Text(msg.ErrStepsOpen),
			more:    more.String(),
			hint:    msg.Text(msg.HintStepsOpen),
			details: map[string]any{"node": open.Node, "steps": stepNumbers(open.Steps)},
		}
	case errors.As(err, &noStep):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStepNotFound,
			message: msg.Text(msg.ErrStepNotFound, noStep.Step),
			hint:    msg.Text(msg.HintSteps),
			details: map[string]any{"step": noStep.Step},
		}
	case errors.As(err, &closed):
		k := msg.ErrStepDroppedAlready
		if closed.State == state.StepDone {
			k = msg.ErrStepDoneAlready
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStepClosed,
			message: msg.Text(k, closed.Step),
			hint:    msg.Text(msg.HintSteps),
			details: map[string]any{"step": closed.Step, "state": closed.State},
		}
	case errors.As(err, &noArtifact):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeArtifactNotFound,
			message: msg.Text(msg.ErrArtifactNotFound, noArtifact.Name),
			hint:    msg.Text(msg.HintArtifactSave, noArtifact.Name),
			details: map[string]any{"artifact": noArtifact.Name},
		}
	case errors.As(err, &file) && file.Reason == task.FileTooLarge:
		return fieldInvalid("file", file.Reason, msg.Text(msg.ErrArtifactFileTooLarge, file.Path), msg.Text(msg.HintArtifactLink))
	case errors.As(err, &file):
		return fieldInvalid("file", file.Reason, msg.Text(msg.ErrArtifactFileNotFound, file.Path), msg.Text(msg.HintArtifactFile))
	}
	var se *state.UnavailableError
	var ne *state.NewerError
	if errors.As(err, &se) || errors.As(err, &ne) {
		return stateFailure(err)
	}
	return internal(err)
}

// stageName names a stage for the operator: by its title, or by its
// identifier if it has none.
func stageName(st flow.Stage) string {
	if t := oneLine(st.Title); t != "" {
		return t
	}
	return st.ID
}

// stageRound is a stage by its title and identifier with the round of its
// pass, or a word that the scenario is passed.
func stageRound(title, stage string, round int, finished bool) string {
	if finished {
		return msg.Text(msg.StageFinished)
	}
	if round == 0 {
		return named(title, stage)
	}
	return msg.Text(msg.StageRound, named(title, stage), round)
}

// progressText is the progress of a task: 4 из 5, or 2 из 4–5 ahead of a fork.
func progressText(p task.Progress) string {
	if p.Min == p.Max {
		return msg.Text(msg.ProgressValue, p.Passed, p.Max)
	}
	return msg.Text(msg.ProgressRange, p.Passed, p.Min, p.Max)
}
