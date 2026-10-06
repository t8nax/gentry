package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
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
var takeFields = []string{"scenario", "title", "statement", "worktree"}

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

	source := state.SourceOperator
	if caller.ByAgent() {
		source = state.SourceAgent
	}
	t, err := task.Take(st, task.TakeRequest{
		Project: w.Project, Worktree: w.Path, Scenario: sc,
		Title: fields["title"], Statement: fields["statement"], Source: source,
		Snapshot: res.Snapshot, Flow: applied, Library: library,
	})
	if err != nil {
		return fail(env, taskFailure(err))
	}
	v := task.ViewOf(t, res.Flow)

	if *asJSON {
		if err := writeJSON(env, contract.TaskTakeOutput{Task: taskJSON(v), Sync: syncJSON(s)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.TaskTaken, t.Key()))
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
// and a title that is not one line of up to 80 characters.
func checkTakeFields(fields map[string]string) *failure {
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
	tasks, err := st.Tasks()
	if err != nil {
		return fail(stateFailure(err))
	}
	if t, ok := task.In(tasks, w.Path); ok {
		return fail(taskFailure(&task.BusyError{Path: w.Path, Task: t.Key()}))
	}
	if err := task.CheckClean(w.Path); err != nil {
		return fail(taskFailure(err))
	}
	return w, nil
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
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	st, bad := openTasks()
	if bad != nil {
		return fail(env, *bad)
	}
	if st != nil {
		defer st.Close()
	}
	tasks, worktrees, bad := readTasks(st)
	if bad != nil {
		return fail(env, *bad)
	}
	var t state.Task
	var err error
	if len(f.args) > 0 {
		t, err = task.Find(tasks, f.args[0])
	} else {
		wd, werr := os.Getwd()
		if werr != nil {
			return fail(env, internal(werr))
		}
		t, err = task.Current(tasks, worktrees, wd)
	}
	if err != nil {
		return fail(env, taskFailure(err))
	}
	views, err := task.Views(st, []state.Task{t})
	if err != nil {
		return fail(env, stateFailure(err))
	}
	v := views[0]

	if *asJSON {
		if err := writeJSON(env, contract.TaskShowOutput{Task: taskJSON(v)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.TaskHeading, t.Key(), t.Title))
	b.WriteString("\n")
	fmt.Fprintln(&b, msg.Text(msg.TaskProject, t.Project))
	fmt.Fprintln(&b, msg.Text(msg.TaskState, stateWord(t.State)))
	fmt.Fprintln(&b, msg.Text(msg.TaskScenario, named(v.ScenarioTitle, t.Scenario)))
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, named(v.StageTitle, v.Stage)))
	fmt.Fprintln(&b, msg.Text(msg.TaskTakenAt, localTime(t.Taken)))
	fmt.Fprintln(&b, msg.Text(msg.TaskFlowApplied, localTime(t.FlowApplied)))
	fmt.Fprintln(&b, msg.Text(msg.TaskSource, sourceWord(t.Source)))
	if t.Worktree != "" {
		fmt.Fprintln(&b, msg.Text(msg.TaskWorktree, t.Worktree))
	}
	b.WriteString("\n")
	writeText(&b, msg.Text(msg.TaskStatement), t.Statement)
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
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
	for _, t := range tasks {
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
				Id: v.Key(), Project: v.Project, Title: v.Title, State: contract.TaskListOutputTasksElemState(v.State),
				Scenario: contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle},
				Stage:    contract.TaskStage{Node: v.Node, Id: v.Stage, Title: v.StageTitle},
				Taken:    v.Taken,
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
		row := []string{v.Key(), v.Title, stateWord(v.State), orNone(oneLine(v.ScenarioTitle)),
			orNone(oneLine(v.StageTitle)), orNone(v.Worktree)}
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
		Id: v.Key(), Project: v.Project, Title: v.Title, State: contract.TaskState(v.State),
		Scenario:  contract.TaskScenario{Id: v.Scenario, Title: v.ScenarioTitle},
		Stage:     contract.TaskStage{Node: v.Node, Id: v.Stage, Title: v.StageTitle},
		Flow:      contract.Applied{Commit: v.FlowCommit, Time: v.FlowApplied},
		Statement: contract.TaskStatement{Text: v.Statement, Source: contract.TaskStatementSource(v.Source)},
		Taken:     v.Taken,
	}
	if v.Worktree != "" {
		w := v.Worktree
		t.Worktree = &w
	}
	return t
}

// taskFailure turns an error of a task command into a failure.
func taskFailure(err error) failure {
	if f, ok := resolveFailure(err); ok {
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
