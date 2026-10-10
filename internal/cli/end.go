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
	if code, done := f.parse(args, env); done {
		return code
	}
	return endTask(env, cmd, f.args, false, "")
}

func runTaskCancel(args []string, env Env) int {
	const cmd = "task cancel"
	f := newFlags(cmd)
	flags := map[string]*stringFlag{"reason": f.String("reason")}
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	// The argument names the task: it is not a field of --input.
	values, bad := commandFields(cmd, input, flags, nil, env.Stdin, cancelFields)
	if bad != nil {
		return fail(env, *bad)
	}
	return endTask(env, cmd, f.args, true, strings.TrimSpace(text(values, "reason")))
}

// endTask closes or cancels the task named by args, or the task of the
// current worktree, and prints the result. Its worktree is checked for
// changes first: Gentry does not touch the code in it (R129).
func endTask(env Env, cmd string, args []string, cancel bool, reason string) int {
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
	ended, err := task.EndTask(w.st, task.End{Task: w.task.ID, Cancel: cancel, Reason: reason, Source: source(env)})
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
		layout, laidPool = layoutReleased(w.st, w.task.Project, ended.Worktree)
	}
	if !cancel {
		out := contract.TaskCloseOutput{Task: taskJSON(v), Worktree: ended.Worktree, Agents: layout.json(laidPool)}
		return emit(env, out, taskCloseText)
	}
	out := contract.TaskCancelOutput{Task: taskJSON(v), Worktree: ended.Worktree, Agents: layout.json(laidPool)}
	return emit(env, out, taskCancelText)
}

// taskCloseText prints the task closed and the worktree it released.
func taskCloseText(p *page, out contract.TaskCloseOutput) {
	fmt.Fprintln(p, msg.Text(msg.TaskClosed, out.Task.Id))
	fmt.Fprintln(p, msg.Text(msg.WorktreeReleased, out.Worktree))
	writeLayout(p, out.Agents, true, "")
}

// taskCancelText prints the task cancelled, the stage it stopped at, the
// reason and the worktree it released, with the hint to take it anew.
func taskCancelText(p *page, out contract.TaskCancelOutput) {
	t := out.Task
	fmt.Fprintln(p, msg.Text(msg.TaskCancelled, t.Id))
	fmt.Fprintln(p, msg.Text(msg.TaskStage, stageLine(t)))
	if t.Ended != nil && t.Ended.Reason != nil {
		fmt.Fprintln(p, msg.Text(msg.ReasonLine, oneLine(*t.Ended.Reason)))
	}
	fmt.Fprintln(p, msg.Text(msg.WorktreeReleased, out.Worktree))
	writeLayout(p, out.Agents, true, "")
	p.hints(hintOf(msg.HintTaskAgain).set("task", t.Id))
}

// notFinished is the refusal to close a task whose scenario is not passed.
func notFinished(v task.View) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeScenarioNotFinished,
		message: msg.Text(msg.ErrScenarioNotFinished, v.Key()),
		more:    msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)) + "\n",
		hints:   []hint{hintOf(msg.HintStageShow)},
		details: map[string]any{"task": v.Key(), "node": v.Node},
	}
}

func runTaskAttempts(args []string, env Env) int {
	const cmd = "task attempts"
	f := newFlags(cmd)
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
		return listAttempts(env, w, attempts)
	}
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 || n > len(attempts) {
		f := failure{
			exit:    contract.ExitError,
			code:    contract.CodeAttemptNotFound,
			message: msg.Text(msg.ErrAttemptNotFound, w.task.Key(), number),
			more:    msg.Text(msg.AttemptsCountLine, len(attempts)) + "\n",
			hints:   w.hints(hintOf(msg.HintAttemptsList)),
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
	return showAttempt(env, w.st, attempts[n-1])
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
func listAttempts(env Env, w wayTask, attempts []state.Task) int {
	views, err := task.Views(w.st, attempts)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	out := contract.TaskAttemptsOutput{Task: w.task.Key(), Attempts: []contract.TaskAttempt{}}
	for _, v := range views {
		out.Attempts = append(out.Attempts, contract.TaskAttempt{
			Attempt: v.Attempt, State: contract.TaskAttemptsOutputAttemptsElemState(v.State),
			Scenario: contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle}, Taken: v.Taken, Ended: endedJSON(v.Task),
		})
	}
	hints := w.hints(hintOf(msg.HintAttempt))
	return emit(env, out, func(p *page, out contract.TaskAttemptsOutput) { attemptsText(p, out, hints) })
}

// attemptsText prints the attempts of a task as a table with the hint to one
// attempt; hints are the hints for the task.
func attemptsText(p *page, out contract.TaskAttemptsOutput, hints []hint) {
	rows := [][]string{{msg.Text(msg.ColAttempt), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
		msg.Text(msg.ColTaken), msg.Text(msg.ColEnded), msg.Text(msg.ColCancelReason)}}
	for _, a := range out.Attempts {
		ended, reason := msg.Text(msg.ValueNone), ""
		if a.Ended != nil {
			ended = localTime(a.Ended.Time)
			if a.Ended.Reason != nil {
				reason = *a.Ended.Reason
			}
		}
		rows = append(rows, []string{strconv.Itoa(a.Attempt), stateWord(string(a.State)), orNone(oneLine(a.Scenario.Title)),
			localTime(a.Taken), ended, orNone(oneLine(reason))})
	}
	writeTable(&p.Builder, rows)
	p.hints(hints...)
}

// showAttempt prints one attempt of a task in full: its fields, its path
// with steps, artifacts, notes and decisions of the operator.
func showAttempt(env Env, st *state.Store, t state.Task) int {
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
	n := namesOf(v)
	return emit(env, taskShowJSON(v, notes, artifacts, decisions), func(p *page, out contract.TaskShowOutput) {
		attemptText(p, out, n)
	})
}

// attemptText prints an attempt of a task in full.
func attemptText(p *page, out contract.TaskShowOutput, n names) {
	t := out.Task
	b := &p.Builder
	fmt.Fprintln(b, msg.Text(msg.AttemptHeading, t.Id, t.Attempt, t.Title))
	b.WriteString("\n")
	writeTaskFields(b, t, n)
	writePasses(b, t, out.Path, n)
	if len(out.Artifacts) > 0 {
		b.WriteString("\n")
		writeArtifacts(b, out.Artifacts)
	}
	if len(out.Notes) > 0 {
		fmt.Fprintf(b, "\n%s\n", msg.Text(msg.NotesHeading))
		writeNotes(b, out.Notes, n, "  ")
	}
	if len(out.OperatorDecisions) > 0 {
		b.WriteString("\n")
		writeDecisions(b, out.OperatorDecisions, n)
	}
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
			hints:   []hint{hintOf(msg.HintTaskShow).forTask(ended.Task)},
			details: map[string]any{"task": ended.Task, "state": ended.State},
		}
		if ended.State == state.TaskCancelled {
			f.message, f.hints = msg.Text(msg.TaskCancelled, ended.Task), []hint{hintOf(msg.HintTaskAgain).set("task", ended.Task)}
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
			hints:   []hint{hintOf(msg.HintTaskShow).forTask(inWork.Task)},
			details: map[string]any{"task": inWork.Task, "worktree": inWork.Worktree},
		}, true
	case errors.As(err, &mismatch):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskProjectMismatch,
			message: msg.Text(msg.ErrTaskProjectMismatch, mismatch.Task),
			more: msg.Text(msg.TaskProjectLine, mismatch.Project) + "\n" +
				msg.Text(msg.WorktreeProjectLine, mismatch.WorktreeProject) + "\n",
			hints: []hint{hintOf(msg.HintWorktreeListProject).set("project", mismatch.Project)},
			details: map[string]any{"task": mismatch.Task, "project": mismatch.Project,
				"worktree": mismatch.Worktree, "worktree_project": mismatch.WorktreeProject},
		}, true
	}
	return failure{}, false
}
