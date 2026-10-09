// Package agents lays out the subagents of a flow in the worktrees of a pool,
// in the format of the tool the agent works in. Files are copied, hidden from
// git of the project by its info/exclude, and only files Gentry laid out are
// replaced or removed: a file is told as Gentry's by its mark. Tracked files
// of the project and files of the operator are never changed: a subagent
// whose file is one of them is a conflict.
package agents

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/t8nax/gentry/internal/filelock"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/paths"
)

// Layout is how a tool keeps subagents in a worktree; a tool adapter
// implements it.
type Layout interface {
	// Dir is the directory of subagents, relative to the worktree root with
	// forward slashes, such as .claude/agents.
	Dir() string
	// Name is the file name of subagent id in Dir.
	Name(id string) string
	// ID is the subagent of file name, if name is a file of a subagent.
	ID(name string) (string, bool)
	// File is the content of the file of subagent a, with the mark.
	File(a flow.Agent) []byte
	// Marked reports whether content bears the mark of Gentry.
	Marked(content []byte) bool
}

// Reasons of a conflict, as the contract names them.
const (
	Tracked = "tracked" // the file is tracked by git of the project
	Foreign = "foreign" // the file is not tracked and Gentry did not lay it out
)

// Conflict is a subagent whose file cannot be laid out.
type Conflict struct {
	Worktree string
	Agent    string
	File     string // relative to the worktree root, with forward slashes
	Reason   string // Tracked or Foreign
}

// Target is a worktree and the subagents it must have.
type Target struct {
	Path   string
	Agents []flow.Agent
}

// Change is what a layout did in a worktree, or would do.
type Change struct {
	Worktree string
	Missing  bool     // the directory of the worktree does not exist: it is skipped
	Agents   []string // the subagents of the worktree laid out, by identifier
	Added    []string
	Updated  []string
	Removed  []string
}

// Changed reports whether the layout changed the worktree.
func (c Change) Changed() bool { return len(c.Added)+len(c.Updated)+len(c.Removed) > 0 }

// Result is a layout of several worktrees.
type Result struct {
	Worktrees []Change // in the order of the targets
	Conflicts []Conflict
}

// Changed returns the worktrees the layout changed.
func (r Result) Changed() []Change {
	var out []Change
	for _, c := range r.Worktrees {
		if c.Changed() {
			out = append(out, c)
		}
	}
	return out
}

// plan is the layout of one worktree, worked out before anything is written.
type plan struct {
	change    Change
	write     map[string][]byte // by file name in the directory of subagents
	remove    []string          // file names
	conflicts []Conflict
}

// planOf works out the layout of target t.
func planOf(l Layout, t Target) (plan, error) {
	p := plan{change: Change{Worktree: t.Path, Agents: []string{}}, write: map[string][]byte{}}
	if fi, err := os.Stat(t.Path); err != nil || !fi.IsDir() {
		p.change.Missing = true
		return p, nil
	}
	dir := filepath.Join(t.Path, filepath.FromSlash(l.Dir()))
	present, err := marked(l, dir)
	if err != nil {
		return plan{}, err
	}
	want := map[string]bool{}
	var files []string
	for _, a := range t.Agents {
		files = append(files, path.Join(l.Dir(), l.Name(a.ID)))
	}
	for name := range present {
		files = append(files, path.Join(l.Dir(), name))
	}
	tracked, err := git.Tracked(t.Path, files)
	if err != nil {
		return plan{}, err
	}
	agents := slices.Clone(t.Agents)
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	for _, a := range agents {
		name := l.Name(a.ID)
		want[name] = true
		file := path.Join(l.Dir(), name)
		if tracked[file] {
			p.conflicts = append(p.conflicts, Conflict{Worktree: t.Path, Agent: a.ID, File: file, Reason: Tracked})
			continue
		}
		content := l.File(a)
		old, err := os.ReadFile(filepath.Join(dir, name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			p.write[name] = content
			p.change.Added = append(p.change.Added, a.ID)
		case err != nil:
			return plan{}, err
		case !present[name]:
			p.conflicts = append(p.conflicts, Conflict{Worktree: t.Path, Agent: a.ID, File: file, Reason: Foreign})
			continue
		case !bytes.Equal(old, content):
			p.write[name] = content
			p.change.Updated = append(p.change.Updated, a.ID)
		}
		p.change.Agents = append(p.change.Agents, a.ID)
	}
	for _, name := range slices.Sorted(maps.Keys(present)) {
		// A marked file the project tracks is the project's: it stays.
		if want[name] || tracked[path.Join(l.Dir(), name)] {
			continue
		}
		id, _ := l.ID(name)
		p.remove = append(p.remove, name)
		p.change.Removed = append(p.change.Removed, id)
	}
	return p, nil
}

// marked returns the files of subagents in dir that Gentry laid out.
func marked(l Layout, dir string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, ok := l.ID(e.Name()); !ok {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if l.Marked(content) {
			out[e.Name()] = true
		}
	}
	return out, nil
}

// Check returns the conflicts of laying out targets; nothing is written.
func Check(l Layout, targets []Target) ([]Conflict, error) {
	var out []Conflict
	for _, t := range targets {
		p, err := planOf(l, t)
		if err != nil {
			return nil, err
		}
		out = append(out, p.conflicts...)
	}
	return out, nil
}

// Sync lays out targets: the files of their subagents are written unless
// they are the same, the files Gentry laid out for subagents no more there
// are removed, and the subagents in conflict are skipped. The block of
// Gentry in the info/exclude of each repository lists the files Gentry laid
// out in all worktrees of pool that share it; pool is every worktree of the
// pools, as a worktree outside the targets may have files too. The block
// gets the files to write before they are written and loses the files
// removed after they are, so a failure between leaves a pattern too many
// rather than a file git shows.
func Sync(l Layout, targets []Target, pool []string) (Result, error) {
	var res Result
	var plans []plan
	for _, t := range targets {
		p, err := planOf(l, t)
		if err != nil {
			return Result{}, err
		}
		res.Conflicts = append(res.Conflicts, p.conflicts...)
		res.Worktrees = append(res.Worktrees, p.change)
		if !p.change.Missing {
			plans = append(plans, p)
		}
	}
	if len(plans) == 0 {
		return res, nil
	}
	files, err := excludeFiles(plans, pool)
	if err != nil {
		return Result{}, err
	}
	if err := exclude(l, files, plans, true); err != nil {
		return Result{}, err
	}
	for _, p := range plans {
		if err := apply(l, p); err != nil {
			return Result{}, err
		}
	}
	return res, exclude(l, files, plans, false)
}

// apply writes and removes the files of plan p.
func apply(l Layout, p plan) error {
	dir := filepath.Join(p.change.Worktree, filepath.FromSlash(l.Dir()))
	if len(p.write) > 0 {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.write)) {
		if err := os.WriteFile(filepath.Join(dir, name), p.write[name], 0o644); err != nil {
			return err
		}
	}
	for _, name := range p.remove {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// excludeFiles returns the info/exclude of the worktrees of plans and of
// pool, by worktree. A worktree of the pool that is not there, or that git
// does not take for a worktree, is left out: it has no files to hide, and it
// must not stop the layout of the others.
func excludeFiles(plans []plan, pool []string) (map[string]string, error) {
	files := map[string]string{}
	for _, p := range plans {
		f, err := git.ExcludeFile(p.change.Worktree)
		if err != nil {
			return nil, err
		}
		files[p.change.Worktree] = f
	}
	for _, w := range pool {
		if _, ok := files[w]; ok {
			continue
		}
		if fi, err := os.Stat(w); err != nil || !fi.IsDir() {
			continue
		}
		if f, err := git.ExcludeFile(w); err == nil {
			files[w] = f
		}
	}
	return files, nil
}

// exclude rewrites the block of Gentry in the info/exclude of the
// repositories of plans with the files Gentry laid out in every worktree that
// shares the file; before the files are written, adding also lists those
// plans write. The block is worked out and written under a lock, so that two
// commands in worktrees of one repository do not lose each other's files.
func exclude(l Layout, files map[string]string, plans []plan, adding bool) error {
	done := map[string]bool{}
	for _, p := range plans {
		file := files[p.change.Worktree]
		if done[file] {
			continue
		}
		done[file] = true
		lock, err := filelock.Acquire(filepath.Join(filepath.Dir(file), lockName), LockWait)
		if errors.Is(err, filelock.ErrBusy) {
			return &BusyError{Path: file}
		}
		if err != nil {
			return err
		}
		err = func() error {
			defer lock.Release()
			laid := map[string]bool{}
			for w, f := range files {
				if !paths.Same(f, file) {
					continue
				}
				names, err := marked(l, filepath.Join(w, filepath.FromSlash(l.Dir())))
				if err != nil {
					return err
				}
				for name := range names {
					laid["/"+path.Join(l.Dir(), name)] = true
				}
			}
			if adding {
				for _, q := range plans {
					if paths.Same(files[q.change.Worktree], file) {
						for name := range q.write {
							laid["/"+path.Join(l.Dir(), name)] = true
						}
					}
				}
			}
			return writeBlock(file, slices.Sorted(maps.Keys(laid)))
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// BusyError means another command held the lock of the block of Gentry in
// the info/exclude at Path for the whole wait.
type BusyError struct{ Path string }

func (e *BusyError) Error() string { return e.Path + ": the block of Gentry is busy" }

// lockName is the lock of the block of Gentry, beside info/exclude.
const lockName = "gentry-exclude.lock"

// LockWait is how long a layout waits for another to release the lock of the
// block.
var LockWait = 10 * time.Second
