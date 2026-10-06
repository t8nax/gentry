package task

import (
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/state"
)

// EventArtifactSaved is the event of an artifact saved.
const EventArtifactSaved = "artifact.saved"

// ArtifactMaxSize is the limit of a file kept as an artifact.
const ArtifactMaxSize = 10 << 20

// Reasons a file of an artifact is refused, as the contract names them.
const (
	FileNotFound = "not_found"
	FileTooLarge = "too_large"
)

// artifactName is a name of an artifact: a file name on every system.
var artifactName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidArtifactName reports whether name can name an artifact.
func ValidArtifactName(name string) bool { return artifactName.MatchString(name) }

// ValidURL reports whether u is an address of an artifact: http or https,
// with a host, on one line.
func ValidURL(u string) bool {
	if strings.ContainsAny(u, " \t\r\n") {
		return false
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	s := strings.ToLower(p.Scheme)
	return (s == "http" || s == "https") && p.Host != ""
}

// FileError means the file of an artifact cannot be kept.
type FileError struct{ Path, Reason string }

func (e *FileError) Error() string { return e.Path + ": " + e.Reason }

// ArtifactPath returns where the copy of the file of an artifact lies:
// state/<project>/tasks/<task>/artifacts/<name> in the data root.
func ArtifactPath(t state.Task, name string) (string, error) {
	root, err := home.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", t.Project, "tasks", t.Key(), "artifacts", name), nil
}

// SaveArtifact saves an artifact of a task: a copy of file, or the address
// u. The file is copied before the transaction, through a temporary file
// renamed into place; an artifact of the same name is replaced, and the copy
// of a file it replaces by a link is removed.
func SaveArtifact(st *state.Store, t state.Task, name, file, u, source string) (state.Artifact, bool, error) {
	a := state.Artifact{Name: name, Kind: state.ArtifactLink, URL: u, Source: source}
	dst, err := ArtifactPath(t, name)
	if err != nil {
		return state.Artifact{}, false, err
	}
	if file != "" {
		a.Kind = state.ArtifactFile
		if err := copyFile(file, dst); err != nil {
			return state.Artifact{}, false, err
		}
	}
	var replaced bool
	var old state.Artifact
	err = st.Write(func(tx *state.Tx) error {
		var err error
		if old, _, err = tx.Artifact(t.ID, name); err != nil {
			return err
		}
		if a, replaced, err = tx.SaveArtifact(t.ID, a); err != nil {
			return err
		}
		d := contract.ArtifactSavedData{Name: name, Kind: contract.ArtifactSavedDataKind(a.Kind), Replaced: replaced,
			Source: contract.ArtifactSavedDataSource(source)}
		if a.URL != "" {
			d.Url = &a.URL
		}
		_, err = tx.AddEvent(EventArtifactSaved, t.Project, t.Key(), d)
		return err
	})
	if err != nil {
		return state.Artifact{}, false, err
	}
	if old.Kind == state.ArtifactFile && a.Kind == state.ArtifactLink {
		// The record no longer names the copy; a copy left behind is harmless.
		os.Remove(dst)
	}
	return a, replaced, nil
}

// copyFile copies the regular file src to dst through a temporary file in
// the directory of dst.
func copyFile(src, dst string) error {
	fi, err := os.Stat(src)
	switch {
	case errors.Is(err, fs.ErrNotExist) || err == nil && !fi.Mode().IsRegular():
		return &FileError{Path: src, Reason: FileNotFound}
	case err != nil:
		return err
	case fi.Size() > ArtifactMaxSize:
		return &FileError{Path: src, Reason: FileTooLarge}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".save-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, io.LimitReader(in, ArtifactMaxSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
