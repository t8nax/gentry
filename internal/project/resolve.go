package project

import (
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
)

// NotFoundError means --project names a project that is not connected.
type NotFoundError struct{ Project string }

func (e *NotFoundError) Error() string { return e.Project + ": not connected" }

// UndeterminedError means no project is named and none contains Dir.
type UndeterminedError struct{ Dir string }

func (e *UndeterminedError) Error() string { return e.Dir + ": no project" }

// Find returns the connected project with identifier id, or NotFoundError.
func Find(projects []state.Project, id string) (state.Project, error) {
	for _, p := range projects {
		if p.ID == id {
			return p, nil
		}
	}
	return state.Project{}, &NotFoundError{Project: id}
}

// Containing returns the project whose pool worktree or knowledge is the
// canonical directory dir or contains it. A worktree may lie inside another,
// as worktrees kept in a subdirectory of the main one: the deepest wins.
func Containing(projects []state.Project, worktrees []state.Worktree, dir string) (state.Project, bool) {
	id, depth := "", -1
	match := func(root, project string) {
		if paths.Within(dir, root) && len(root) > depth {
			id, depth = project, len(root)
		}
	}
	for _, w := range worktrees {
		match(w.Path, w.Project)
	}
	for _, p := range projects {
		match(p.Knowledge, p.ID)
	}
	if id == "" {
		return state.Project{}, false
	}
	p, err := Find(projects, id)
	return p, err == nil
}

// Resolve returns the project a command works with: the one named by flag if
// it is not empty, otherwise the one that contains the canonical directory
// dir the command runs in.
func Resolve(projects []state.Project, worktrees []state.Worktree, dir, flag string) (state.Project, error) {
	if flag != "" {
		return Find(projects, flag)
	}
	if p, ok := Containing(projects, worktrees, dir); ok {
		return p, nil
	}
	return state.Project{}, &UndeterminedError{Dir: dir}
}
