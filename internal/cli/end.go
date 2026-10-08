package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// cancelFields are the fields of task cancel; the task is the argument.
var cancelFields = textFields("reason")

func runTaskClose(args []string, env Env) int {
	const cmd = "task close"
	f := newFlags(cmd)
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	return endTask(env, cmd, f.args, false, "", *asJSON)
}

func runTaskCancel(args []string, env Env) int {
	const cmd = "task cancel"
	f := newFlags(cmd)
	flags := map[string]*stringFlag{"reason": f.String("reason")}
	input := f.String("input")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	// The argument names the task: it is not a field of --input.
	values, bad := commandFields(cmd, input, flags, nil, env.Stdin, cancelFields)
	if bad != nil {
		return fail(env, *bad)
	}
	return endTask(env, cmd, f.args, true, strings.TrimSpace(text(values, "reason")), *asJSON)
}

// endTask closes or cancels the task named by args, or the task of the
// current worktree, and prints the result. Its worktree is checked for
// changes first: Gentry does not touch the code in it (R129).
func endTask(env Env, cmd string, args []string, cancel bool, reason string, asJSON bool) int {
	w, bad := openArgTask(cmd, args, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	if !cancel && w.task.Node != state.Finish {
		v, bad := w.view()
		if bad != nil {
			return fail(env, *bad)
		}
		return w.fail(env, notFinished(v))
	}
	if w.task.Worktree != "" {
		if err := task.CheckClean(w.task.Worktree); err != nil {
			return fail(env, taskFailure(err))
		}
	}
	ended, err := task.EndTask(w.st, task.End{Task: w.task.ID, Cancel: cancel, Reason: reason, Source: source()})
	var nf *task.NotFinishedError
	switch {
	case errors.As(err, &nf):
		// Another command moved the task meanwhile.
		v, bad := w.view()
		if bad != nil {
			return fail(env, *bad)
		}
		return w.fail(env, notFinished(v))
	case err != nil:
		return fail(env, taskFailure(err))
	}
	v, bad := w.view()
	if bad != nil {
		return fail(env, *bad)
	}
	// The worktree is free: it gets the subagents of the active flow.
	var layout layoutOutcome
	var laidPool *layoutPool
	if ended.Worktree != "" {
		layout, laidPool = layoutReleased(w.task.Project, ended.Worktree)
	}
	if asJSON {
		var out any = contract.TaskCloseOutput{Task: taskJSON(v), Worktree: ended.Worktree, Agents: layout.json(laidPool)}
		if cancel {
			out = contract.TaskCancelOutput{Task: taskJSON(v), Worktree: ended.Worktree, Agents: layout.json(laidPool)}
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	if !cancel {
		fmt.Fprintln(&b, msg.Text(msg.TaskClosed, v.Key()))
		fmt.Fprintln(&b, msg.Text(msg.WorktreeReleased, ended.Worktree))
		writeLayout(&b, layout, true, "")
		fmt.Fprint(env.Stdout, b.String())
		return contract.ExitOK
	}
	fmt.Fprintln(&b, msg.Text(msg.TaskCancelled, v.Key()))
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)))
	if v.Reason != "" {
		fmt.Fprintln(&b, msg.Text(msg.ReasonLine, oneLine(v.Reason)))
	}
	fmt.Fprintln(&b, msg.Text(msg.WorktreeReleased, ended.Worktree))
	writeLayout(&b, layout, true, "")
	fmt.Fprintf(&b,"\n%s\n", msg.Text(msg.HintTaskAgain, v.Key()))
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// notFinished is the refusal to close a task whose scenario is not passed.
func notFinished(v task.View) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeScenarioNotFinished,
		message: msg.Text(msg.ErrScenarioNotFinished, v.Key()),
		more:    msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)) + "\n",
		hint:    msg.Text(msg.HintStageShow),
		details: map[string]any{"task": v.Key(), "node": v.Node},
	}
}

func runTaskAttempts(args []string, env Env) int {
	const cmd = "task attempts"
	f := newFlags(cmd)
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	// One argument of digits alone is an attempt of the task of the current
	// worktree: an identifier of a task has the prefix.
	keyArgs, number := f.args, ""
	switch {
	case len(f.args) == 2:
		keyArgs, number = f.args[:1], f.args[1]
	case len(f.args) == 1 && isDigits(f.args[0]):
		keyArgs, number = nil, f.args[0]
	}
	w, bad := openArgTask(cmd, keyArgs, true)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	attempts, err := w.st.TaskAttempts(w.task.Prefix, w.task.Number)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	if number == "" {
		return listAttempts(env, w, attempts, *asJSON)
	}
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 || n > len(attempts) {
		f := failure{
			exit:    contract.ExitError,
			code:    contract.CodeAttemptNotFound,
			message: msg.Text(msg.ErrAttemptNotFound, w.task.Key(), number),
			more:    msg.Text(msg.AttemptsCountLine, len(attempts)) + "\n",
			hint:    w.hint(msg.Text(msg.HintAttemptsList)),
			details: map[string]any{"task": w.task.Key(), "attempt": number, "attempts": len(attempts)},
		}
		if err != nil || n < 1 {
			f.code, f.exit = contract.CodeInvalidArgument, contract.ExitUsage
			f.details = map[string]any{"command": cmd, "argument": "attempt", "value": number}
		} else {
			f.details["attempt"] = n
		}
		return fail(env, f)
	}
	return showAttempt(env, w.st, attempts[n-1], *asJSON)
}

// isDigits reports whether s is a number of decimal digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// listAttempts prints the attempts of the task of w as a table.
func listAttempts(env Env, w wayTask, attempts []state.Task, asJSON bool) int {
	views, err := task.Views(w.st, attempts)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	if asJSON {
		out := contract.TaskAttemptsOutput{Task: w.task.Key(), Attempts: []contract.TaskAttempt{}}
		for _, v := range views {
			out.Attempts = append(out.Attempts, contract.TaskAttempt{
				Attempt: v.Attempt, State: contract.TaskAttemptsOutputAttemptsElemState(v.State),
				Scenario: contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle}, Taken: v.Taken, Ended: endedJSON(v.Task),
			})
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	rows := [][]string{{msg.Text(msg.ColAttempt), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
		msg.Text(msg.ColTaken), msg.Text(msg.ColEnded), msg.Text(msg.ColCancelReason)}}
	for _, v := range views {
		ended := msg.Text(msg.ValueNone)
		if !v.Ended.IsZero() {
			ended = localTime(v.Ended)
		}
		rows = append(rows, []string{strconv.Itoa(v.Attempt), stateWord(v.State), orNone(oneLine(v.ScenarioTitle)),
			localTime(v.Taken), ended, orNone(oneLine(v.Reason))})
	}
	var b strings.Builder
	writeTable(&b, rows)
	fmt.Fprintf(&b, "\n%s\n", w.hint(msg.Text(msg.HintAttempt)))
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// showAttempt prints one attempt of a task in full: its fields, its path
// with steps, artifacts, notes and decisions of the operator.
func showAttempt(env Env, st *state.Store, t state.Task, asJSON bool) int {
	views, err := task.Views(st, []state.Task{t})
	if err != nil {
		return fail(env, stateFailure(err))
	}
	v := views[0]
	notes, err := st.Notes(t.ID)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	artifacts, err := st.Artifacts(t.ID)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	decisions, err := st.Decisions(t.ID)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	if asJSON {
		if err := writeJSON(env, taskShowJSON(v, notes, artifacts, decisions)); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.AttemptHeading, t.Key(), t.Attempt, t.Title))
	b.WriteString("\n")
	writeTaskFields(&b, v)
	writePasses(&b, v)
	if len(artifacts) > 0 {
		b.WriteString("\n")
		writeArtifacts(&b, t, artifacts)
	}
	if len(notes) > 0 {
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.NotesHeading))
		writeNotes(&b, v, notes, "  ")
	}
	if len(decisions) > 0 {
		b.WriteString("\n")
		writeDecisions(&b, v, decisions)
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// endedJSON returns the closing or the cancelling of t as the contract has
// it; nil while t is in work.
func endedJSON(t state.Task) *contract.TaskEnded {
	if t.Ended.IsZero() {
		return nil
	}
	e := &contract.TaskEnded{Time: t.Ended, Source: contract.EndedSource(t.EndedSource)}
	if t.Reason != "" {
		r := t.Reason
		e.Reason = &r
	}
	return e
}

// endedLine names who closed or cancelled t and when.
func endedLine(t state.Task) string {
	k := msg.TaskClosedByOperator
	switch {
	case t.State == state.TaskClosed && t.EndedSource == state.SourceAgent:
		k = msg.TaskClosedByAgent
	case t.State == state.TaskCancelled && t.EndedSource == state.SourceAgent:
		k = msg.TaskCancelledByAgent
	case t.State == state.TaskCancelled:
		k = msg.TaskCancelledByOperator
	}
	return msg.Text(k, localTime(t.Ended))
}

// endFailure turns the refusals of a task that cannot be changed or taken
// anew into failures.
func endFailure(err error) (failure, bool) {
	var (
		ended    *task.EndedError
		inWork   *task.InWorkError
		mismatch *task.ProjectMismatchError
	)
	switch {
	case errors.As(err, &ended):
		f := failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskEnded,
			message: msg.Text(msg.TaskClosed, ended.Task),
			hint:    msg.Text(msg.HintTaskShowKey, ended.Task),
			details: map[string]any{"task": ended.Task, "state": ended.State},
		}
		if ended.State == state.TaskCancelled {
			f.message, f.hint = msg.Text(msg.TaskCancelled, ended.Task), msg.Text(msg.HintTaskAgain, ended.Task)
		}
		return f, true
	case errors.As(err, &inWork):
		var more string
		if inWork.Worktree != "" {
			more = msg.Text(msg.TaskWorktree, inWork.Worktree) + "\n"
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskInWork,
			message: msg.Text(msg.ErrTaskInWork, inWork.Task),
			more:    more,
			hint:    msg.Text(msg.HintTaskShowKey, inWork.Task),
			details: map[string]any{"task": inWork.Task, "worktree": inWork.Worktree},
		}, true
	case errors.As(err, &mismatch):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskProjectMismatch,
			message: msg.Text(msg.ErrTaskProjectMismatch, mismatch.Task),
			more: msg.Text(msg.TaskProjectLine, mismatch.Project) + "\n" +
				msg.Text(msg.WorktreeProjectLine, mismatch.WorktreeProject) + "\n",
			hint: msg.Text(msg.HintWorktreeListProject, mismatch.Project),
			details: map[string]any{"task": mismatch.Task, "project": mismatch.Project,
				"worktree": mismatch.Worktree, "worktree_project": mismatch.WorktreeProject},
		}, true
	}
	return failure{}, false
}
