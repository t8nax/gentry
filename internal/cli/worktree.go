package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/project"
	"github.com/t8nax/gentry/internal/state"
)

func runWorktreeAdd(args []string, env Env) int {
	f := newFlags("worktree add")
	proj := f.String("project")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	var path string
	if len(f.args) > 0 {
		path = f.args[0]
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail(env, internal(err))
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
	res, err := project.AddWorktree(st, project.WorktreeRequest{Dir: dir, Path: path, Project: proj.Value})
	if err != nil {
		return fail(env, worktreeFailure(err))
	}

	w := res.Worktree
	// The worktree is in the pool: a layout that fails or meets a conflict is
	// told, and the worktree stays there.
	var layout layoutOutcome
	var laidPool *layoutPool
	if r, bad := openProcess(); bad != nil {
		layout.err = errors.New(bad.message)
	} else {
		layout, laidPool = layoutAdded(st, r, w.Project, []string{w.Path})
		r.Close()
	}
	out := contract.WorktreeAddOutput{Path: w.Path, Project: w.Project, Main: w.Main, Action: actionAdded, Agents: layout.json(laidPool)}
	if res.Unchanged {
		out.Action = actionUnchanged
	}
	return emit(env, out, worktreeAddText)
}

// worktreeAddText prints the worktree added to the pool and the layout of
// its subagents.
func worktreeAddText(p *page, out contract.WorktreeAddOutput) {
	if out.Action == actionUnchanged {
		fmt.Fprintln(p, msg.Text(msg.WorktreeUnchanged, out.Project, out.Path))
	} else {
		fmt.Fprintln(p, msg.Text(msg.WorktreeAdded, out.Project, out.Path))
	}
	writeLayout(p, out.Agents, true, "")
}

// runWorktreeList prints the worktrees of the pools. It only reads: without a
// state store there are no worktrees, and no store is created.
func runWorktreeList(args []string, env Env) int {
	f := newFlags("worktree list")
	proj := f.String("project")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail(env, internal(err))
	}
	projects, worktrees, bad := readPool()
	if bad != nil {
		return fail(env, *bad)
	}
	list, err := project.ListWorktrees(projects, worktrees, dir, proj.Value)
	if err != nil {
		return fail(env, worktreeFailure(err))
	}
	held, bad := worktreeTasks()
	if bad != nil {
		return fail(env, *bad)
	}
	out := contract.WorktreeListOutput{Worktrees: []contract.WorktreeListItem{}}
	for _, w := range list {
		item := contract.WorktreeListItem{Path: w.Path, Project: w.Project, Main: w.Main, Exists: w.Exists}
		if w.Branch != "" {
			item.Branch = &w.Branch
		}
		if key, ok := held[w.Path]; ok {
			item.Task = &key
		}
		out.Worktrees = append(out.Worktrees, item)
	}
	return emit(env, out, worktreeListText)
}

// worktreeListText prints the worktrees as a table, or a line that there are
// none.
func worktreeListText(p *page, out contract.WorktreeListOutput) {
	if len(out.Worktrees) == 0 {
		fmt.Fprintln(p, msg.Text(msg.WorktreesNone))
		return
	}
	rows := [][]string{{msg.Text(msg.ColProject), msg.Text(msg.ColWorktree), msg.Text(msg.ColBranch), msg.Text(msg.ColState)}}
	for _, w := range out.Worktrees {
		branch := ""
		if w.Branch != nil {
			branch = *w.Branch
		}
		rows = append(rows, []string{w.Project, w.Path, orNone(branch), worktreeState(w)})
	}
	writeTable(&p.Builder, rows)
}

// worktreeState is the state column of the list: whether the worktree is the
// main one, and whether it is free or holds a task. A worktree without its
// directory is not free.
func worktreeState(w contract.WorktreeListItem) string {
	var labels []string
	if w.Main {
		labels = append(labels, msg.Text(msg.WorktreeMain))
	}
	switch {
	case w.Task != nil:
		labels = append(labels, msg.Text(msg.WorktreeTask, *w.Task))
	case w.Exists:
		labels = append(labels, msg.Text(msg.WorktreeFree))
	}
	if !w.Exists {
		labels = append(labels, msg.Text(msg.WorktreeMissing))
	}
	return strings.Join(labels, ", ")
}

// worktreeTasks returns the tasks that hold worktrees by path of the
// worktree, such as SHOP-1.
func worktreeTasks() (map[string]string, *failure) {
	st, bad := openTasks()
	if bad != nil || st == nil {
		return nil, bad
	}
	defer st.Close()
	tasks, _, bad := readTasks(st)
	if bad != nil {
		return nil, bad
	}
	held := map[string]string{}
	for _, t := range tasks {
		if t.Worktree != "" {
			held[t.Worktree] = t.Key()
		}
	}
	return held, nil
}

// readPool reads the projects and their worktrees; without a state store
// there are none.
func readPool() ([]state.Project, []state.Worktree, *failure) {
	path, err := state.Path()
	if err != nil {
		f := homeUnknown()
		return nil, nil, &f
	}
	st, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		f := stateFailure(err)
		return nil, nil, &f
	}
	defer st.Close()
	projects, err := st.Projects()
	if err != nil {
		f := stateFailure(err)
		return nil, nil, &f
	}
	worktrees, err := st.Worktrees()
	if err != nil {
		f := stateFailure(err)
		return nil, nil, &f
	}
	return projects, worktrees, nil
}

// worktreeFailure turns an error of a worktree command into a failure.
func worktreeFailure(err error) failure {
	if f, ok := resolveFailure(err); ok {
		return f
	}
	var (
		notRepo *project.NotRepoError
		invalid *project.InvalidWorktreeError
	)
	switch {
	case errors.As(err, &notRepo):
		// Without the hint of project add: it names that command.
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeNotGitRepo,
			message: msg.Text(msg.ErrNotGitRepo, notRepo.Path),
			details: map[string]any{"path": notRepo.Path},
		}
	case errors.As(err, &invalid):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeInvalid,
			message: msg.Text(msg.ErrWorktreeKnowledge, invalid.Project, invalid.Path),
			details: map[string]any{"path": invalid.Path},
		}
	}
	return projectFailure(err)
}

// resolveFailure turns an error of determining the project of a command into
// a failure; ok is false for other errors.
func resolveFailure(err error) (f failure, ok bool) {
	var (
		notFound *project.NotFoundError
		undet    *project.UndeterminedError
	)
	switch {
	case errors.As(err, &notFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectNotFound,
			message: msg.Text(msg.ErrProjectNotFound, notFound.Project),
			hints:   []hint{hintOf(msg.HintProjectNotFound)},
			details: map[string]any{"project": notFound.Project},
		}, true
	case errors.As(err, &undet):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectUndetermined,
			message: msg.Text(msg.ErrProjectUndetermined, undet.Dir),
			hints:   []hint{hintOf(msg.HintProjectUndetermined)},
			details: map[string]any{"dir": undet.Dir},
		}, true
	}
	return failure{}, false
}
