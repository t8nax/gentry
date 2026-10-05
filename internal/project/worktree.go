package project

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
)

// WorktreeRequest is a request to add a worktree to the pool of a project.
type WorktreeRequest struct {
	Dir     string // the directory the command runs in
	Path    string // the worktree, absolute or relative to Dir; empty for Dir
	Project string // project identifier; empty to determine it
}

// WorktreeResult is what AddWorktree did.
type WorktreeResult struct {
	Worktree  state.Worktree
	Unchanged bool // the worktree was in the pool of the project already
}

// InvalidWorktreeError means the directory is the knowledge of a project or
// lies within it, and so cannot be a worktree.
type InvalidWorktreeError struct{ Path, Project string }

func (e *InvalidWorktreeError) Error() string { return e.Path + ": knowledge of " + e.Project }

// AddWorktree adds a worktree to the pool of a project: a worktree made after
// the project was connected or a separate clone. The worktree root is added
// when a directory inside it is given. The project is the one named in the
// request; otherwise the one whose pool has another worktree of the same
// repository; otherwise the one the command runs in. Adding a worktree again
// changes nothing.
func AddWorktree(st *state.Store, req WorktreeRequest) (WorktreeResult, error) {
	if err := git.Find(); err != nil {
		return WorktreeResult{}, err
	}
	dir, err := paths.Canonical(req.Dir)
	if err != nil {
		return WorktreeResult{}, err
	}
	target := dir
	if req.Path != "" {
		p := req.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if target, err = paths.Canonical(p); err != nil {
			return WorktreeResult{}, err
		}
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		return WorktreeResult{}, &NotRepoError{Path: target}
	}
	root, err := git.Root(target)
	if errors.Is(err, git.ErrNotRepo) {
		return WorktreeResult{}, &NotRepoError{Path: target}
	}
	if err != nil {
		return WorktreeResult{}, err
	}
	siblings, err := git.Worktrees(root)
	if err != nil {
		return WorktreeResult{}, err
	}

	var res WorktreeResult
	err = st.Write(func(tx *state.Tx) error {
		res = WorktreeResult{}
		projects, err := tx.Projects()
		if err != nil {
			return err
		}
		worktrees, err := tx.Worktrees()
		if err != nil {
			return err
		}
		if req.Project != "" {
			if _, err := Find(projects, req.Project); err != nil {
				return err
			}
		}
		for _, w := range worktrees {
			if !paths.Same(w.Path, root) {
				continue
			}
			if req.Project != "" && req.Project != w.Project {
				return &WorktreeTakenError{Path: w.Path, Project: w.Project}
			}
			res = WorktreeResult{Worktree: w, Unchanged: true}
			return nil
		}
		for _, p := range projects {
			if paths.Within(root, p.Knowledge) || paths.Within(p.Knowledge, root) {
				return &InvalidWorktreeError{Path: root, Project: p.ID}
			}
		}
		p, err := worktreeProject(projects, worktrees, siblings, dir, req.Project)
		if err != nil {
			return err
		}
		w := state.Worktree{Path: root, Project: p.ID}
		if err := tx.AddWorktree(w); err != nil {
			return err
		}
		if _, err := tx.AddEvent(EventWorktreeAdded, p.ID, "", contract.WorktreeAddedData{Path: root}); err != nil {
			return err
		}
		res.Worktree = w
		return nil
	})
	if err != nil {
		return WorktreeResult{}, err
	}
	return res, nil
}

// worktreeProject determines the project of a new worktree. A worktree made
// with git worktree add belongs to the project whose pool has another
// worktree of its repository, unless the pools of several projects have them.
func worktreeProject(projects []state.Project, worktrees []state.Worktree, siblings []git.Worktree, dir, flag string) (state.Project, error) {
	if flag != "" {
		return Find(projects, flag)
	}
	ids := map[string]bool{}
	for _, s := range siblings {
		for _, w := range worktrees {
			if paths.Same(s.Path, w.Path) {
				ids[w.Project] = true
			}
		}
	}
	if len(ids) == 1 {
		for id := range ids {
			return Find(projects, id)
		}
	}
	return Resolve(projects, worktrees, dir, "")
}

// WorktreeState is a worktree of a pool as it is on disk now.
type WorktreeState struct {
	state.Worktree
	Exists bool   // the directory exists
	Branch string // the branch checked out; empty if none or Exists is false
}

// ListWorktrees returns the worktrees of the project named by flag, or of the
// project that contains the directory dir, or of all projects if none does:
// by project, the main worktree first, then by path. The branch is read from
// git now: the operator switches branches on their own.
func ListWorktrees(projects []state.Project, worktrees []state.Worktree, dir, flag string) ([]WorktreeState, error) {
	var only string
	if flag != "" {
		p, err := Find(projects, flag)
		if err != nil {
			return nil, err
		}
		only = p.ID
	} else {
		d, err := paths.Canonical(dir)
		if err != nil {
			return nil, err
		}
		if p, ok := Containing(projects, worktrees, d); ok {
			only = p.ID
		}
	}

	var list []WorktreeState
	for _, w := range worktrees {
		if only == "" || w.Project == only {
			list = append(list, WorktreeState{Worktree: w})
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Main != b.Main {
			return a.Main
		}
		return a.Path < b.Path
	})
	for i := range list {
		fi, err := os.Stat(list[i].Path)
		if err != nil || !fi.IsDir() {
			continue
		}
		list[i].Exists = true
		branch, err := git.Branch(list[i].Path)
		var ce *git.CommandError
		if errors.As(err, &ce) {
			// The directory is no longer a worktree: show it without a branch.
			continue
		}
		if err != nil {
			return nil, err
		}
		list[i].Branch = branch
	}
	return list, nil
}
