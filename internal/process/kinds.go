package process

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Applied is the commit that made the active content of a kind.
type Applied struct {
	Commit string
	Time   time.Time
}

// Active returns the files of kind k at HEAD: its active content.
func (r *Repo) Active(k Kind) (Files, error) {
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	t, err := r.treeOf(head)
	if err != nil {
		return nil, err
	}
	return r.contents(t.kind(k))
}

// Working returns the files of kind k in its directory: the active content
// and the draft over it.
func (r *Repo) Working(k Kind) (Files, error) { return ReadDir(r.KindDir(k)) }

// HasDraft reports whether the working files of kind k differ from its
// active content.
func (r *Repo) HasDraft(k Kind) (bool, error) {
	active, err := r.Active(k)
	if err != nil {
		return false, err
	}
	working, err := r.Working(k)
	if err != nil {
		return false, err
	}
	return !active.Equal(working), nil
}

// Applied returns the commit that made the active content of kind k; ok is
// false if k has no files at HEAD.
func (r *Repo) Applied(k Kind) (a Applied, ok bool, err error) {
	active, err := r.Active(k)
	if err != nil || len(active) == 0 {
		return Applied{}, false, err
	}
	out, err := r.git("log", "-1", "--format=%H %cI", "HEAD", "--", k.root())
	if err != nil {
		return Applied{}, false, err
	}
	return parseApplied(out)
}

// LastApplied returns the latest of the commits that made the active content
// of kinds ks: the last commit at HEAD that changed any of them. The order of
// the history tells which is later, not the time of the commits: it is to the
// second, and two kinds may be applied within one. ok is false if none of ks
// has files at HEAD.
func (r *Repo) LastApplied(ks ...Kind) (a Applied, ok bool, err error) {
	head, err := r.head()
	if err != nil {
		return Applied{}, false, err
	}
	t, err := r.treeOf(head)
	if err != nil {
		return Applied{}, false, err
	}
	args := []string{"log", "-1", "--format=%H %cI", "HEAD", "--"}
	for _, k := range ks {
		if len(t.kind(k)) > 0 {
			args = append(args, k.root())
		}
	}
	if len(args) == 5 {
		return Applied{}, false, nil
	}
	out, err := r.git(args...)
	if err != nil {
		return Applied{}, false, err
	}
	return parseApplied(out)
}

func parseApplied(out string) (Applied, bool, error) {
	hash, at, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok {
		return Applied{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return Applied{}, false, err
	}
	return Applied{Commit: hash, Time: t}, true, nil
}

// Apply commits files as the content of kind k with a commit of subject for
// action, and removes the conflict directory of k and the files of changes it
// makes stale. The working files stay as they are: they now have no draft
// over them.
func (r *Repo) Apply(k Kind, files Files, subject, action string) (Applied, error) {
	return r.ApplyKinds([]KindFiles{{Kind: k, Files: files}}, subject, action)
}

// KindFiles are the files of a kind to apply.
type KindFiles struct {
	Kind  Kind
	Files Files
}

// ApplyKinds commits the files of several kinds as their content with one
// commit of subject for action, as Apply does for one: one application is
// one commit, such as the flow of a project with the shared subagents.
func (r *Repo) ApplyKinds(kinds []KindFiles, subject, action string) (Applied, error) {
	head, err := r.head()
	if err != nil {
		return Applied{}, err
	}
	t, err := r.treeOf(head)
	if err != nil {
		return Applied{}, err
	}
	changes := map[string]*entry{}
	texts := map[string]string{}
	for _, kf := range kinds {
		k := kf.Kind
		old := t.kind(k)
		current, err := r.contents(old)
		if err != nil {
			return Applied{}, err
		}
		for rel := range old {
			if _, ok := kf.Files[rel]; !ok {
				changes[k.root()+"/"+rel] = nil
			}
		}
		for rel, text := range kf.Files {
			if cur, ok := current[rel]; !ok || cur != text {
				texts[k.root()+"/"+rel] = text
			}
		}
	}
	written, err := r.hashAll(texts)
	if err != nil {
		return Applied{}, err
	}
	for p, e := range written {
		changes[p] = e
	}
	c, err := r.commit(head, changes, message(subject, action), nil)
	if err != nil {
		return Applied{}, err
	}
	if err := r.moveHead(c, head); err != nil {
		return Applied{}, err
	}
	if err := r.setChecked(c); err != nil {
		return Applied{}, err
	}
	for _, kf := range kinds {
		if err := os.RemoveAll(r.ConflictDir(kf.Kind)); err != nil {
			return Applied{}, err
		}
		if err := r.removeChanges(kf.Kind); err != nil {
			return Applied{}, err
		}
	}
	out, err := r.git("log", "-1", "--format=%H %cI", c)
	if err != nil {
		return Applied{}, err
	}
	a, _, err := parseApplied(out)
	return a, err
}

// Discard brings the working files of kind k back to its active content and
// removes its conflict directory and the files of changes it makes stale.
// Hidden files are left alone.
func (r *Repo) Discard(k Kind) error {
	active, err := r.Active(k)
	if err != nil {
		return err
	}
	dir := r.KindDir(k)
	working, err := ReadDir(dir)
	if err != nil {
		return err
	}
	for rel := range working {
		if _, ok := active[rel]; !ok {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	for rel, text := range active {
		if cur, ok := working[rel]; ok && cur == text {
			continue
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(rel)), text); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(r.ConflictDir(k)); err != nil {
		return err
	}
	return r.removeChanges(k)
}

// Conflict returns the variants of another machine of kind k in conflict;
// ok is false if k is not in conflict.
func (r *Repo) Conflict(k Kind) (files Files, ok bool, err error) {
	dir := r.ConflictDir(k)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	files, err = ReadDir(dir)
	return files, err == nil, err
}

// Kinds returns the kinds the process has, committed or only in the working
// directory: flows by project, the library last.
func (r *Repo) Kinds() ([]Kind, error) {
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	t, err := r.treeOf(head)
	if err != nil {
		return nil, err
	}
	seen := map[Kind]bool{}
	for _, k := range t.kinds() {
		seen[k] = true
	}
	if _, err := os.Stat(r.KindDir(Library)); err == nil {
		seen[Library] = true
	}
	des, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, err
	}
	for _, de := range des {
		name := de.Name()
		if !de.IsDir() || strings.HasPrefix(name, ".") || name == libraryDir || name == conflictDir {
			continue
		}
		if fi, err := os.Stat(filepath.Join(r.Dir, name, flowDir)); err == nil && fi.IsDir() {
			seen[FlowOf(name)] = true
		}
	}
	return sortKinds(seen), nil
}

// EnsureFlowDir creates the flow directory of project with the directories
// of a flow, as a prompt of the layout, if there is none.
func (r *Repo) EnsureFlowDir(project string) error {
	dir := r.KindDir(FlowOf(project))
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, sub := range []string{"scenarios", "stages", "parts", "agents"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes text to file p, creating its directory.
func writeFile(p, text string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}
