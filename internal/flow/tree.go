package flow

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// tree is the files of a flow, read from the draft directory or taken from a
// snapshot, so that both are checked by the same code. Paths are relative to
// the flow with forward slashes; texts are normalized by normalize.
type tree struct {
	files map[string][]byte
	dirs  map[string]bool // directories, those implied by files included
}

// normalize removes a byte order mark and brings line ends to \n, so that an
// editor that writes \r\n does not make an unchanged file look changed.
func normalize(b []byte) []byte {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
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

// readTree reads the flow in directory dir: the files at its root and in the
// directories of the flow. Other directories are noted but not read, as they
// do not belong to the flow. Hidden files, those whose name starts with a dot,
// are not part of the flow. It returns an error satisfying
// errors.As(err, *fs.PathError) if something cannot be read.
func readTree(dir string) (*tree, error) {
	t := &tree{files: map[string][]byte{}, dirs: map[string]bool{}}
	var walk func(rel string, deep bool) error
	walk = func(rel string, deep bool) error {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		des, err := os.ReadDir(full)
		if err != nil {
			return err
		}
		for _, de := range des {
			name := de.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			p := path.Join(rel, name)
			isDir := de.IsDir()
			// A symbolic link counts as what it points to.
			if de.Type()&fs.ModeSymlink != 0 {
				fi, err := os.Stat(filepath.Join(full, name))
				if err != nil {
					return err
				}
				isDir = fi.IsDir()
			}
			switch {
			case isDir:
				t.dirs[p] = true
				if deep && slices.Contains([]string{scenariosDir, stagesDir, partsDir, agentsDir}, name) {
					if err := walk(p, false); err != nil {
						return err
					}
				}
			default:
				b, err := os.ReadFile(filepath.Join(full, name))
				if err != nil {
					return err
				}
				t.files[p] = normalize(b)
			}
		}
		return nil
	}
	if err := walk(".", true); err != nil {
		return nil, err
	}
	// path.Join(".", name) is name: the root is not a directory of the tree.
	delete(t.dirs, ".")
	return t, nil
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

// library is where the subagents of the library are read from: its directory
// or the copies in a snapshot. Only the subagents a flow names are read.
type library interface {
	// file returns the text of file name of the library, normalized; ok is
	// false if there is no such file.
	file(name string) (text []byte, ok bool, err error)
}

// dirLibrary is the library directory, process/agents.
type dirLibrary string

func (d dirLibrary) file(name string) ([]byte, bool, error) {
	b, err := os.ReadFile(filepath.Join(string(d), name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return normalize(b), true, nil
}

// mapLibrary is the copies of library subagents in a snapshot, by file name.
type mapLibrary map[string]string

func (m mapLibrary) file(name string) ([]byte, bool, error) {
	text, ok := m[name]
	return []byte(text), ok, nil
}
