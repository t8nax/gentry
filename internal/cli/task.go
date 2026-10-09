package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/project"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// takeFields are the fields of task take: each is a flag and a field of
// --input of the same name, in the order of the command spec.
var takeFields = []string{"task", "scenario", "title", "statement", "worktree"}

// taskStates are the states of a task as --state names them, with their
// words for the operator.
var taskStates = []struct {
	id   string
	word msg.Key
}{
	{state.TaskActive, msg.TaskStateActive},
	{state.TaskWaiting, msg.TaskStateWaiting},
	{state.TaskClosed, msg.TaskStateClosed},
	{state.TaskCancelled, msg.TaskStateCancelled},
}

// stateWord names the state of a task for the operator.
func stateWord(id string) string {
	for _, s := range taskStates {
		if s.id == id {
			return msg.Text(s.word)
		}
	}
	return id
}

// sourceWord names who wrote the statement of a task.
func sourceWord(source string) string {
	if source == state.SourceAgent {
		return msg.Text(msg.TaskSourceAgent)
	}
	return msg.Text(msg.TaskSourceOperator)
}

func runTaskTake(args []string, env Env) int {
	f := newFlags("task take")
	values := map[string]*stringFlag{}
	for _, name := range takeFields {
		values[name] = f.String(name)
	}
	input := f.String("input")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	fields := map[string]string{}
	if input.Set {
		if input.Value == "" {
			return fail(env, flagValueMissing("--input"))
		}
		for _, name := range takeFields {
			if values[name].Set {
				return fail(env, conflictingFlags("task take", []string{"--" + name, "--input"}))
			}
		}
		var bad *failure
		if fields, bad = readInput("task take", input.Value, env.Stdin, takeFields); bad != nil {
			return fail(env, *bad)
		}
	} else {
		for _, name := range takeFields {
			v := values[name]
			if v.Set && v.Value == "" {
				return fail(env, flagValueMissing("--"+name))
			}
			fields[name] = v.Value
		}
	}
	if bad := checkTakeFields(fields); bad != nil {
		return fail(env, *bad)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fail(env, internal(err))
	}
	dir := wd
	if w := fields["worktree"]; w != "" {
		dir = w
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(wd, dir)
		}
	}
	statePath, err := state.Path()
	if err != nil {
		return fail(env, homeUnknown())
	}
	st, err := state.Open(statePath)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	defer st.Close()

	w, bad := takeWorktree(st, dir)
	if bad != nil {
		return fail(env, *bad)
	}
	var again *state.Task
	if key := fields["task"]; key != "" {
		t, bad := cancelledTask(st, key, w)
		if bad != nil {
			return fail(env, *bad)
		}
		again = &t
	}
	r, places, bad := openTaskFlow(w.Project)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := syncFlow(env, r)
	if bad != nil {
		return fail(env, *bad)
	}
	res, applied, ok, err := flow.Active(r, w.Project)
	if err != nil {
		return fail(env, flowFailure(err, places))
	}
	if !ok {
		return fail(env, flowFailure(&flow.NoFlowError{Project: w.Project, Dir: places.Dir}, places))
	}
	if len(res.Problems) > 0 {
		return fail(env, flowInvalid(places, res.Problems))
	}
	sc, ok := res.Flow.Scenario(fields["scenario"])
	if !ok {
		return fail(env, objectNotFound(w.Project, 0, fields["scenario"], false))
	}
	library, _, err := r.Applied(process.Library)
	if err != nil {
		return fail(env, processFailure(err))
	}
	// The flow is read: the process repository is free for other commands
	// before the task is recorded.
	r.Close()

	source := source(env)
	t, err := task.Take(st, task.TakeRequest{
		Project: w.Project, Worktree: w.Path, Scenario: sc,
		Title: fields["title"], Statement: fields["statement"], Source: source, Again: again,
		Snapshot: res.Snapshot, Flow: applied, Library: library,
	})
	if err != nil {
		return fail(env, taskFailure(err))
	}
	views, err := task.Views(st, []state.Task{t})
	if err != nil {
		return fail(env, stateFailure(err))
	}
	v := views[0]

	if *asJSON {
		if err := writeJSON(env, contract.TaskTakeOutput{Task: taskJSON(v), Sync: syncJSON(s)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	if again == nil {
		fmt.Fprintln(&b, msg.Text(msg.TaskTaken, t.Key()))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.TaskTakenAnew, t.Key()))
		fmt.Fprintln(&b, msg.Text(msg.AttemptLine, t.Attempt))
	}
	fmt.Fprintln(&b, msg.Text(msg.TaskTitle, t.Title))
	fmt.Fprintln(&b, msg.Text(msg.TaskScenario, orNone(v.ScenarioTitle)))
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, orNone(v.StageTitle)))
	fmt.Fprintln(&b, msg.Text(msg.TaskSource, sourceWord(t.Source)))
	fmt.Fprintln(&b, msg.Text(msg.TaskWorktree, t.Worktree))
	// task show without a number finds the task only from its worktree.
	hint := msg.Text(msg.HintTaskShowKey, t.Key())
	if d, err := paths.Canonical(wd); err == nil && paths.Within(d, t.Worktree) {
		hint = msg.Text(msg.HintTaskShow)
	}
	fmt.Fprintf(&b, "\n%s\n", hint)
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// checkTakeFields refuses a task without a scenario, a title or a statement,
// and a title that is not one line of up to 80 characters. A task taken anew
// keeps its own title and statement: they are refused with it.
func checkTakeFields(fields map[string]string) *failure {
	if fields["task"] != "" {
		for _, name := range []string{"title", "statement"} {
			if fields[name] != "" {
				f := conflictingFlags("task take", []string{"--task", "--" + name})
				return &f
			}
		}
		if strings.TrimSpace(fields["scenario"]) == "" {
			f := missingField("task take", "scenario", msg.Text(msg.ErrScenarioMissing), msg.Text(msg.HintFlowScenarios))
			return &f
		}
		return nil
	}
	missing := func(field string, message, hint msg.Key, args ...any) *failure {
		f := failure{
			exit:    contract.ExitUsage,
			code:    contract.CodeMissingField,
			message: msg.Text(message),
			hint:    msg.Text(hint, args...),
			details: map[string]any{"command": "task take", "field": field},
		}
		return &f
	}
	switch {
	case strings.TrimSpace(fields["scenario"]) == "":
		return missing("scenario", msg.ErrScenarioMissing, msg.HintFlowScenarios)
	case strings.TrimSpace(fields["title"]) == "":
		return missing("title", msg.ErrTitleMissing, msg.HintCommandHelp, "task take")
	case strings.TrimSpace(fields["statement"]) == "":
		return missing("statement", msg.ErrStatementMissing, msg.HintCommandHelp, "task take")
	}
	reason := task.CheckTitle(fields["title"])
	if reason == "" {
		return nil
	}
	k := msg.ErrTitleTooLong
	if reason == task.TitleMultiline {
		k = msg.ErrTitleMultiline
	}
	return &failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFieldInvalid,
		message: msg.Text(k),
		hint:    msg.Text(msg.HintTitle),
		details: map[string]any{"field": "title", "reason": reason},
	}
}

// takeWorktree finds the worktree of the pool to take a task in and refuses
// one that holds a task or has uncommitted changes. The transaction checks
// the task again: another command may take one meanwhile.
func takeWorktree(st *state.Store, dir string) (state.Worktree, *failure) {
	fail := func(f failure) (state.Worktree, *failure) { return state.Worktree{}, &f }
	worktrees, err := st.Worktrees()
	if err != nil {
		return fail(stateFailure(err))
	}
	w, err := task.Worktree(worktrees, dir)
	if err != nil {
		return fail(taskFailure(err))
	}
	if t, ok, err := task.In(st, w.Path); err != nil {
		return fail(stateFailure(err))
	} else if ok {
		return fail(taskFailure(&task.BusyError{Path: w.Path, Task: t.Key()}))
	}
	if err := task.CheckClean(w.Path); err != nil {
		return fail(taskFailure(err))
	}
	return w, nil
}

// cancelledTask finds the task named by key to take anew in the worktree w:
// its last attempt must be cancelled, of the project of w. The transaction
// checks the attempt again: another command may take it meanwhile.
func cancelledTask(st *state.Store, key string, w state.Worktree) (state.Task, *failure) {
	fail := func(f failure) (state.Task, *failure) { return state.Task{}, &f }
	t, err := task.Find(st, key)
	var invalid *task.InvalidKeyError
	switch {
	case errors.As(err, &invalid):
		return fail(flagValueInvalid("--task", invalid.Value, msg.Text(msg.ErrTaskKeyInvalid, invalid.Value), msg.Text(msg.HintTaskKey)))
	case err != nil:
		return fail(lookupFailure(err))
	}
	if err := task.CheckAgain(t); err != nil {
		return fail(taskFailure(err))
	}
	if t.Project != w.Project {
		return fail(taskFailure(&task.ProjectMismatchError{Task: t.Key(), Project: t.Project, Worktree: w.Path, WorktreeProject: w.Project}))
	}
	return t, nil
}

// openTaskFlow opens the process repository with the places of the flow of
// project. The caller closes the repository.
func openTaskFlow(projectID string) (*process.Repo, flow.Places, *failure) {
	r, bad := openProcess()
	if bad != nil {
		return nil, flow.Places{}, bad
	}
	return r, flow.PlacesOf(r, projectID), nil
}

func runTaskShow(args []string, env Env) int {
	f := newFlags("task show")
	full := f.Bool("path")
	onlyStatement := f.Bool("statement")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if *full && *onlyStatement {
		return fail(env, conflictingFlags("task show", []string{"--path", "--statement"}))
	}
	st, bad := openTasks()
	if bad != nil {
		return fail(env, *bad)
	}
	if st != nil {
		defer st.Close()
	}
	var t state.Task
	var err error
	if len(f.args) > 0 {
		t, err = task.Find(st, f.args[0])
	} else {
		wd, werr := os.Getwd()
		if werr != nil {
			return fail(env, internal(werr))
		}
		worktrees, bad := readWorktrees(st)
		if bad != nil {
			return fail(env, *bad)
		}
		t, err = task.Current(st, worktrees, wd)
	}
	if err != nil {
		return fail(env, lookupFailure(err))
	}
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

	if *asJSON {
		if err := writeJSON(env, taskShowJSON(v, notes, artifacts, decisions)); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	// task show without a number finds the task only from its worktree.
	here := false
	if wd, err := os.Getwd(); err == nil && t.Worktree != "" {
		if d, err := paths.Canonical(wd); err == nil {
			here = paths.Within(d, t.Worktree)
		}
	}
	var b strings.Builder
	if *onlyStatement {
		writeText(&b, msg.Text(msg.StatementHeading, t.Key()), t.Statement)
		b.WriteString("\n")
		fmt.Fprintln(&b, msg.Text(msg.TaskSource, sourceWord(t.Source)))
		if len(decisions) > 0 {
			b.WriteString("\n")
			writeDecisions(&b, v, decisions)
		}
		fmt.Fprint(env.Stdout, b.String())
		return contract.ExitOK
	}
	fmt.Fprintln(&b, msg.Text(msg.TaskHeading, t.Key(), t.Title))
	b.WriteString("\n")
	writeTaskFields(&b, v)
	if *full {
		writePasses(&b, v)
	} else {
		writePath(&b, v)
	}
	if len(artifacts) > 0 {
		b.WriteString("\n")
		writeArtifacts(&b, t, artifacts)
	}
	b.WriteString("\n")
	hint := wayTask{task: t, here: here}.hint
	fmt.Fprintln(&b, statementHint(t, here, len(decisions) > 0))
	if len(notes) > 0 {
		fmt.Fprintln(&b, hint(msg.Text(msg.HintNotes)))
	}
	if t.Attempt > 1 {
		fmt.Fprintln(&b, hint(msg.Text(msg.HintAttempts)))
	}
	switch {
	case t.State == state.TaskCancelled:
		fmt.Fprintln(&b, msg.Text(msg.HintTaskAgain, t.Key()))
	case !t.IsEnded() && v.Finished:
		fmt.Fprintln(&b, hint(msg.Text(msg.HintTaskClose)))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// writeTaskFields prints the fields of the task of v, one «name: value» a
// line: its state, scenario, stage, progress, times and worktree.
func writeTaskFields(b *strings.Builder, v task.View) {
	t := v.Task
	fmt.Fprintln(b, msg.Text(msg.TaskProject, t.Project))
	fmt.Fprintln(b, msg.Text(msg.TaskState, stateWord(t.State)))
	fmt.Fprintln(b, msg.Text(msg.TaskScenario, named(v.ScenarioTitle, t.Scenario)))
	fmt.Fprintln(b, msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)))
	if v.Flow != nil {
		fmt.Fprintln(b, msg.Text(msg.ProgressLine, progressText(v.Progress)))
	}
	fmt.Fprintln(b, msg.Text(msg.TaskTakenAt, localTime(t.Taken)))
	if !t.Ended.IsZero() {
		fmt.Fprintln(b, endedLine(t))
	}
	fmt.Fprintln(b, msg.Text(msg.TaskFlowApplied, localTime(t.FlowApplied)))
	if t.Reason != "" {
		fmt.Fprintln(b, msg.Text(msg.CancelReasonLine, oneLine(t.Reason)))
	}
	if t.Worktree != "" {
		fmt.Fprintln(b, msg.Text(msg.TaskWorktree, t.Worktree))
	}
}

// taskShowJSON returns the task of v with its path, notes, artifacts and
// decisions of the operator as task show --json gives them.
func taskShowJSON(v task.View, notes []state.Note, artifacts []state.Artifact, decisions []state.Decision) contract.TaskShowOutput {
	out := contract.TaskShowOutput{Task: taskJSON(v), Path: []contract.TaskPass{}, Notes: []contract.TaskNote{},
		Artifacts: []contract.TaskArtifact{}, OperatorDecisions: []contract.OperatorDecision{}}
	for _, p := range v.Path {
		out.Path = append(out.Path, passJSON(p))
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, noteJSON(n))
	}
	for _, a := range artifacts {
		path, _ := task.ArtifactPath(v.Task, a.Name)
		out.Artifacts = append(out.Artifacts, artifactJSON(a, path))
	}
	for _, d := range decisions {
		out.OperatorDecisions = append(out.OperatorDecisions, decisionJSON(d))
	}
	return out
}

// named is an object of a flow by its title and identifier, or by its
// identifier alone if it has no title.
func named(title, id string) string {
	if strings.TrimSpace(title) == "" {
		return id
	}
	return msg.Text(msg.TaskNamed, oneLine(title), id)
}

func runTaskList(args []string, env Env) int {
	f := newFlags("task list")
	all := f.Bool("all")
	only := f.String("state")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if *all && only.Set {
		return fail(env, conflictingFlags("task list", []string{"--all", "--state"}))
	}
	if only.Set {
		if only.Value == "" {
			return fail(env, flagValueMissing("--state"))
		}
		known := false
		for _, s := range taskStates {
			known = known || s.id == only.Value
		}
		if !known {
			return fail(env, flagValueInvalid("--state", only.Value,
				msg.Text(msg.ErrFlagValueInvalid, "--state", only.Value), msg.Text(msg.HintCommandHelp, "task list")))
		}
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}

	st, bad := openTasks()
	if bad != nil {
		return fail(env, *bad)
	}
	if st != nil {
		defer st.Close()
	}
	projects, worktrees, bad := readPool()
	if bad != nil {
		return fail(env, *bad)
	}
	tasks, _, bad := readTasks(st)
	if bad != nil {
		return fail(env, *bad)
	}
	// The project as worktree list has it: the one named, the one of the
	// directory, or all.
	var scope string
	if proj.Value != "" {
		p, err := project.Find(projects, proj.Value)
		if err != nil {
			return fail(env, taskFailure(err))
		}
		scope = p.ID
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return fail(env, internal(err))
		}
		d, err := paths.Canonical(wd)
		if err != nil {
			return fail(env, internal(err))
		}
		if p, ok := project.Containing(projects, worktrees, d); ok {
			scope = p.ID
		}
	}
	var picked []state.Task
	for _, t := range task.Latest(tasks) {
		switch {
		case scope != "" && t.Project != scope:
		case only.Set && t.State != only.Value:
		case !*all && !only.Set && t.State != state.TaskActive && t.State != state.TaskWaiting:
		default:
			picked = append(picked, t)
		}
	}
	views, err := task.Views(st, picked)
	if err != nil {
		return fail(env, stateFailure(err))
	}

	if *asJSON {
		out := contract.TaskListOutput{Tasks: []contract.TaskListItem{}}
		for _, v := range views {
			item := contract.TaskListItem{
				Id: v.Key(), Attempt: v.Attempt, Project: v.Project, Title: v.Title, State: contract.TaskListOutputTasksElemState(v.State),
				Scenario: contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle},
				Stage:    stageJSON(v), Finished: v.Finished, Progress: progressJSON(v.Progress),
				Taken: v.Taken,
			}
			if v.Worktree != "" {
				w := v.Worktree
				item.Worktree = &w
			}
			out.Tasks = append(out.Tasks, item)
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if len(views) == 0 {
		k := msg.TasksNoneOpen
		if *all || only.Set {
			k = msg.TasksNone
		}
		fmt.Fprintln(env.Stdout, msg.Text(k))
		return contract.ExitOK
	}
	header := []string{msg.Text(msg.ColNumber), msg.Text(msg.ColTitle), msg.Text(msg.ColState),
		msg.Text(msg.ColScenario), msg.Text(msg.ColStage), msg.Text(msg.ColWorktree)}
	if scope == "" {
		header = append([]string{msg.Text(msg.ColProject)}, header...)
	}
	rows := [][]string{header}
	for _, v := range views {
		stage := orNone(oneLine(v.StageTitle))
		if v.Finished {
			stage = msg.Text(msg.StageFinished)
		}
		row := []string{v.Key(), v.Title, stateWord(v.State), orNone(oneLine(v.ScenarioTitle)),
			stage, orNone(v.Worktree)}
		if scope == "" {
			row = append([]string{v.Project}, row...)
		}
		rows = append(rows, row)
	}
	var b strings.Builder
	writeTable(&b, rows)
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// openTasks opens the state store for reading; nil without a store, which
// has no tasks and is not created.
func openTasks() (*state.Store, *failure) {
	path, err := state.Path()
	if err != nil {
		f := homeUnknown()
		return nil, &f
	}
	st, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	return st, nil
}

// readWorktrees reads the pool of st; none for nil.
func readWorktrees(st *state.Store) ([]state.Worktree, *failure) {
	if st == nil {
		return nil, nil
	}
	worktrees, err := st.Worktrees()
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	return worktrees, nil
}

// lookupFailure turns an error of finding a task into a failure: a store
// that cannot be read, or a refusal of taskFailure.
func lookupFailure(err error) failure {
	var se *state.UnavailableError
	var ne *state.NewerError
	if errors.As(err, &se) || errors.As(err, &ne) {
		return stateFailure(err)
	}
	return taskFailure(err)
}

// readTasks reads the tasks and the worktrees of st; none for nil.
func readTasks(st *state.Store) ([]state.Task, []state.Worktree, *failure) {
	if st == nil {
		return nil, nil, nil
	}
	tasks, err := st.Tasks()
	if err == nil {
		var worktrees []state.Worktree
		if worktrees, err = st.Worktrees(); err == nil {
			return tasks, worktrees, nil
		}
	}
	f := stateFailure(err)
	return nil, nil, &f
}

// taskJSON returns the task of v as the contract has it.
func taskJSON(v task.View) contract.Task {
	t := contract.Task{
		Id: v.Key(), Attempt: v.Attempt, Project: v.Project, Title: v.Title, State: contract.TaskState(v.State),
		Scenario:  contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle},
		Stage:     stageJSON(v),
		Finished:  v.Finished,
		Progress:  progressJSON(v.Progress),
		Flow:      contract.Applied{Commit: v.FlowCommit, Time: v.FlowApplied},
		Statement: contract.TaskStatement{Text: v.Statement, Source: contract.TaskStatementSource(v.Source)},
		Taken:     v.Taken,
		Ended:     endedJSON(v.Task),
	}
	if v.Worktree != "" {
		w := v.Worktree
		t.Worktree = &w
	}
	return t
}

// statementHint is the hint to the statement of t and its decisions of the
// operator, if it has any; outside its worktree it names the task, as task
// show takes it.
func statementHint(t state.Task, here, decisions bool) string {
	h := msg.Text(msg.HintStatement)
	if decisions {
		h = msg.Text(msg.HintStatementDecisions)
	}
	if !here {
		h, _ = withArg(h, t.Key())
	}
	return h
}

// stageJSON returns the current stage of v as the contract has it; nil once
// the scenario is passed.
func stageJSON(v task.View) *contract.TaskStage {
	if v.Finished {
		return nil
	}
	return &contract.TaskStage{Node: v.Node, Id: v.Stage, Title: v.StageTitle, Round: max(v.Round, 1)}
}

// progressJSON returns a progress as the contract has it.
func progressJSON(p task.Progress) contract.TaskProgress {
	return contract.TaskProgress{Passed: p.Passed, TotalMin: p.Min, TotalMax: p.Max}
}

// writePath prints the path of a task as a table, one pass per row, and the
// steps of its current stage.
func writePath(b *strings.Builder, v task.View) {
	if len(v.Path) == 0 {
		return
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)}}
	for _, p := range v.Path {
		next := msg.Text(msg.ValueNone)
		if !p.Current() {
			next = nodeName(p.Next)
		}
		rows = append(rows, []string{passStage(v, p), strconv.Itoa(p.Round), outcomeWord(v, p), next})
	}
	writeTable(b, rows)
	if n := len(v.Path); v.Path[n-1].Current() {
		b.WriteString("\n")
		writeStepTable(b, v.Path[n-1].Steps)
	}
}

// writePasses prints the path of a task in full, each pass a block: its
// outcome, exit, transition, reason, who closed it and its steps.
func writePasses(b *strings.Builder, v task.View) {
	for _, p := range v.Path {
		b.WriteString("\n")
		fmt.Fprintln(b, msg.Text(msg.StageRound, named(v.StageTitleOf(p.Stage), p.Stage), p.Round))
		fmt.Fprintln(b, msg.Text(msg.OutcomeLine, outcomeWord(v, p)))
		if p.Current() {
			b.WriteString("\n")
			writeStepTable(b, p.Steps)
			continue
		}
		if p.Outcome == state.OutcomeExit {
			fmt.Fprintln(b, msg.Text(msg.ExitTextLine, oneLine(p.ExitText)))
		}
		fmt.Fprintln(b, msg.Text(msg.TransitionLine, nodeName(p.Next)))
		if p.Reason != "" {
			fmt.Fprintln(b, msg.Text(msg.ReasonLine, oneLine(p.Reason)))
		}
		fmt.Fprintln(b, msg.Text(msg.RecordedLine, recordedWord(v, p)))
		fmt.Fprintln(b, msg.Text(msg.ClosedLine, localTime(p.Closed)))
		if len(p.Steps) > 0 {
			b.WriteString("\n")
			writeStepTable(b, p.Steps)
		}
	}
}

// passStage names the stage of a pass by its title, or by its identifier.
func passStage(v task.View, p state.Pass) string {
	if t := oneLine(v.StageTitleOf(p.Stage)); t != "" {
		return t
	}
	return p.Stage
}

// outcomeWord names how a pass of the task of v was closed, or that it goes
// on; the pass a cancelled task stopped at has no outcome.
func outcomeWord(v task.View, p state.Pass) string {
	switch {
	case p.Current() && v.State == state.TaskCancelled:
		return msg.Text(msg.ValueNone)
	case p.Current():
		return msg.Text(msg.OutcomeCurrent)
	case p.Outcome == state.OutcomeSkip:
		return msg.Text(msg.OutcomeSkip)
	}
	return exitKindWord(p.ExitKind, p.Artifact)
}

// recordedWord names who closed a pass: a stage of the operator closed by
// the agent is recorded from the operator's words.
func recordedWord(v task.View, p state.Pass) string {
	if p.Source != state.SourceAgent {
		return msg.Text(msg.TaskSourceOperator)
	}
	if v.Flow != nil {
		if st, ok := v.Flow.Stage(p.Stage); ok && st.Executor == flow.Operator {
			return msg.Text(msg.TaskSourceAgent)
		}
	}
	return msg.Text(msg.RecordedByAgent)
}

// writeArtifacts prints the artifacts of a task as a table.
func writeArtifacts(b *strings.Builder, t state.Task, artifacts []state.Artifact) {
	rows := [][]string{{msg.Text(msg.ColArtifact), msg.Text(msg.ColKind), msg.Text(msg.ColSaved), msg.Text(msg.ColPlace)}}
	for _, a := range artifacts {
		kind, place := msg.Text(msg.ArtifactKindLink), a.URL
		if a.Kind == state.ArtifactFile {
			kind = msg.Text(msg.ArtifactKindFile)
			place, _ = task.ArtifactPath(t, a.Name)
		}
		rows = append(rows, []string{a.Name, kind, localTime(a.Saved), place})
	}
	writeTable(b, rows)
}

// taskFailure turns an error of a task command into a failure.
func taskFailure(err error) failure {
	if f, ok := resolveFailure(err); ok {
		return f
	}
	if f, ok := endFailure(err); ok {
		return f
	}
	var (
		notPooled *task.NotPooledError
		busy      *task.BusyError
		dirty     *task.DirtyError
		notFound  *task.NotFoundError
		undet     *task.UndeterminedError
		badKey    *task.InvalidKeyError
	)
	switch {
	case errors.As(err, &notPooled):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeNotPooled,
			message: msg.Text(msg.ErrWorktreeNotPooled, notPooled.Path),
			hint:    msg.Text(msg.HintWorktreeAdd),
			details: map[string]any{"path": notPooled.Path},
		}
	case errors.As(err, &busy):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeBusy,
			message: msg.Text(msg.ErrWorktreeBusy, busy.Task, busy.Path),
			hint:    msg.Text(msg.HintTaskShowKey, busy.Task),
			details: map[string]any{"path": busy.Path, "task": busy.Task},
		}
	case errors.As(err, &dirty):
		var more strings.Builder
		more.WriteString("\n")
		writeList(&more, msg.Text(msg.WorktreeChangedFiles), dirty.Files)
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeDirty,
			message: msg.Text(msg.ErrWorktreeDirty, dirty.Path),
			more:    more.String(),
			hint:    msg.Text(msg.HintWorktreeDirty),
			details: map[string]any{"path": dirty.Path, "files": dirty.Files},
		}
	case errors.As(err, &notFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskNotFound,
			message: msg.Text(msg.ErrTaskNotFound, notFound.Task),
			hint:    msg.Text(msg.HintTaskListAll),
			details: map[string]any{"task": notFound.Task},
		}
	case errors.As(err, &undet):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeTaskUndetermined,
			message: msg.Text(msg.ErrTaskUndetermined, undet.Dir),
			hint:    msg.Text(msg.HintTaskList),
			details: map[string]any{"dir": undet.Dir},
		}
	case errors.As(err, &badKey):
		return invalidArgument("task show", "task", badKey.Value,
			msg.Text(msg.ErrTaskKeyInvalid, badKey.Value), msg.Text(msg.HintTaskKey))
	}
	return processFailure(err)
}
