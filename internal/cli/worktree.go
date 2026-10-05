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
	asJSON := f.Bool("json")
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
	if *asJSON {
		out := contract.WorktreeAddOutput{Path: w.Path, Project: w.Project, Main: w.Main, Action: actionAdded}
		if res.Unchanged {
			out.Action = actionUnchanged
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if res.Unchanged {
		fmt.Fprintln(env.Stdout, msg.Text(msg.WorktreeUnchanged, w.Project, w.Path))
	} else {
		fmt.Fprintln(env.Stdout, msg.Text(msg.WorktreeAdded, w.Project, w.Path))
	}
	return contract.ExitOK
}

// runWorktreeList prints the worktrees of the pools. It only reads: without a
// state store there are no worktrees, and no store is created.
func runWorktreeList(args []string, env Env) int {
	f := newFlags("worktree list")
	proj := f.String("project")
	asJSON := f.Bool("json")
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

	if *asJSON {
		out := contract.WorktreeListOutput{Worktrees: []contract.WorktreeListItem{}}
		for _, w := range list {
			item := contract.WorktreeListItem{Path: w.Path, Project: w.Project, Main: w.Main, Exists: w.Exists}
			if w.Branch != "" {
				item.Branch = &w.Branch
			}
			out.Worktrees = append(out.Worktrees, item)
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, msg.Text(msg.WorktreesNone))
		return contract.ExitOK
	}
	rows := [][]string{{msg.Text(msg.ColProject), msg.Text(msg.ColWorktree), msg.Text(msg.ColBranch), msg.Text(msg.ColState)}}
	for _, w := range list {
		rows = append(rows, []string{w.Project, w.Path, orNone(w.Branch), worktreeState(w)})
	}
	var b strings.Builder
	writeTable(&b, rows)
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// worktreeState is the state column of the list: whether the worktree is the
// main one, and whether it is free. A worktree without its directory is not
// free.
func worktreeState(w project.WorktreeState) string {
	var labels []string
	if w.Main {
		labels = append(labels, msg.Text(msg.WorktreeMain))
	}
	if w.Exists {
		labels = append(labels, msg.Text(msg.WorktreeFree))
	} else {
		labels = append(labels, msg.Text(msg.WorktreeMissing))
	}
	return strings.Join(labels, ", ")
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
	var (
		notRepo  *project.NotRepoError
		invalid  *project.InvalidWorktreeError
		notFound *project.NotFoundError
		undet    *project.UndeterminedError
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
	case errors.As(err, &notFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectNotFound,
			message: msg.Text(msg.ErrProjectNotFound, notFound.Project),
			hint:    msg.Text(msg.HintProjectNotFound),
			details: map[string]any{"project": notFound.Project},
		}
	case errors.As(err, &undet):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectUndetermined,
			message: msg.Text(msg.ErrProjectUndetermined, undet.Dir),
			hint:    msg.Text(msg.HintProjectUndetermined),
			details: map[string]any{"dir": undet.Dir},
		}
	}
	return projectFailure(err)
}
