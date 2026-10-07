package process

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// changesFile is the file of changes of a project, beside its flow directory:
// what gentry flow diff shows in full, for the operator to read. It is not a
// part of the process: it exists only while there is a draft, and git does
// not see it.
const changesFile = "changes.md"

// excludeEntries keep the files of changes out of git status: they are
// local to the machine, so the entries go to .git/info/exclude and not to the
// committed .gitignore. The instruction of a subagent of the library named
// changes stays seen.
var excludeEntries = []string{"/*/" + changesFile, "!/" + libraryDir + "/" + changesFile}

// ChangesFile returns the path of the file of changes of project.
func (r *Repo) ChangesFile(project string) string {
	return filepath.Join(r.Dir, project, changesFile)
}

// WriteChanges replaces the file of changes of project with text at once, so
// that a reader never sees it half written.
func (r *Repo) WriteChanges(project, text string) error {
	if err := r.excludeChanges(); err != nil {
		return err
	}
	p := r.ChangesFile(project)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+changesFile+".*")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(text)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// excludeChanges adds the files of changes to .git/info/exclude unless they
// are there.
func (r *Repo) excludeChanges() error {
	p := filepath.Join(r.Dir, ".git", "info", "exclude")
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if string(bytes.TrimSpace(line)) == excludeEntries[0] {
			return nil
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	for _, e := range excludeEntries {
		b = append(b, e+"\n"...)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// removeChanges removes the files of changes that kind k makes stale: of its
// project for a flow, of every project for the library, which every flow
// may name.
func (r *Repo) removeChanges(k Kind) error {
	if !k.IsLibrary() {
		return removeFile(r.ChangesFile(k.Project))
	}
	des, err := os.ReadDir(r.Dir)
	if err != nil {
		return err
	}
	for _, de := range des {
		name := de.Name()
		if !de.IsDir() || strings.HasPrefix(name, ".") || name == libraryDir || name == conflictDir {
			continue
		}
		if err := removeFile(r.ChangesFile(name)); err != nil {
			return err
		}
	}
	return nil
}

// removeFile removes file p if there is one.
func removeFile(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
