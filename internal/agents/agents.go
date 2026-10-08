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
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"

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
	for _, name := range sortedKeys(present) {
		if want[name] {
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
// are removed, and the subagents in conflict are skipped. Then the block of
// Gentry in the info/exclude of each repository is made to list the files
// Gentry laid out in all worktrees of pool that share it; pool is every
// worktree of the pools, as a worktree outside the targets may have files
// too.
func Sync(l Layout, targets []Target, pool []string) (Result, error) {
	var res Result
	touched := map[string]bool{}
	for _, t := range targets {
		p, err := planOf(l, t)
		if err != nil {
			return Result{}, err
		}
		res.Conflicts = append(res.Conflicts, p.conflicts...)
		if !p.change.Missing {
			if err := apply(l, t.Path, p); err != nil {
				return Result{}, err
			}
			touched[t.Path] = true
		}
		res.Worktrees = append(res.Worktrees, p.change)
	}
	if len(touched) == 0 {
		return res, nil
	}
	return res, exclude(l, touched, pool)
}

// apply writes and removes the files of plan p in worktree root.
func apply(l Layout, root string, p plan) error {
	dir := filepath.Join(root, filepath.FromSlash(l.Dir()))
	if len(p.write) > 0 {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(p.write) {
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

// exclude rewrites the block of Gentry in the info/exclude of the
// repositories of the worktrees touched, with the files Gentry laid out in
// every worktree of pool that shares the file.
func exclude(l Layout, touched map[string]bool, pool []string) error {
	files := map[string]string{} // worktree → its info/exclude
	for _, w := range pool {
		if fi, err := os.Stat(w); err != nil || !fi.IsDir() {
			continue
		}
		f, err := git.ExcludeFile(w)
		if err != nil {
			return err
		}
		files[w] = f
	}
	for w := range touched {
		if _, ok := files[w]; !ok {
			f, err := git.ExcludeFile(w)
			if err != nil {
				return err
			}
			files[w] = f
		}
	}
	done := map[string]bool{}
	for w := range touched {
		file := files[w]
		if done[file] {
			continue
		}
		done[file] = true
		laid := map[string]bool{}
		for other, f := range files {
			if !paths.Same(f, file) {
				continue
			}
			names, err := marked(l, filepath.Join(other, filepath.FromSlash(l.Dir())))
			if err != nil {
				return err
			}
			for name := range names {
				laid["/"+path.Join(l.Dir(), name)] = true
			}
		}
		if err := writeBlock(file, sortedKeys(laid)); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
