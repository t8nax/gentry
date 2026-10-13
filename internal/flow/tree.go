package flow

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/process"
)

// tree is the files of a flow, read from the flow directory or taken from a
// commit, so that both are checked by the same code. Paths are relative to
// the flow with forward slashes; texts are normalized by process.Normalize.
type tree struct {
	files map[string][]byte
	dirs  map[string]bool // directories, those implied by files included
}

// treeOf returns the tree of files given by path.
func treeOf(files map[string]string) *tree {
	t := &tree{files: map[string][]byte{}, dirs: map[string]bool{}}
	for p, text := range files {
		t.files[p] = []byte(text)
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			t.dirs[d] = true
		}
	}
	return t
}

// entries returns the entries of directory rel of the tree by name; rel is
// empty for the root.
func (t *tree) entries(rel string) []entry {
	seen := map[string]bool{}
	var out []entry
	add := func(p string, dir bool) {
		parent := path.Dir(p)
		if parent == "." {
			parent = ""
		}
		if parent != rel || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, entry{name: path.Base(p), rel: p, dir: dir})
	}
	for p := range t.files {
		add(p, false)
	}
	for p := range t.dirs {
		add(p, true)
	}
	slices.SortFunc(out, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	return out
}

// sharedSource is where the shared subagents are read from: their directory
// or the copies in a snapshot. Only the subagents a flow names are read.
type sharedSource interface {
	// file returns the text of file name of the shared subagents, normalized;
	// ok is false if there is no such file.
	file(name string) (text []byte, ok bool, err error)
}

// dirShared is the directory of the shared subagents, process/agents.
type dirShared string

func (d dirShared) file(name string) ([]byte, bool, error) {
	b, err := os.ReadFile(filepath.Join(string(d), name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return process.Normalize(b), true, nil
}

// mapShared is the copies of shared subagents in a snapshot, by file name.
type mapShared map[string]string

func (m mapShared) file(name string) ([]byte, bool, error) {
	text, ok := m[name]
	return []byte(text), ok, nil
}
