package flow

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/state"
)

// Event types of the flow draft.
const (
	EventDraftCreated   = "flow.draft_created"
	EventApplied        = "flow.applied"
	EventDraftDiscarded = "flow.draft_discarded"
)

// Places is where the flow of a project lives in the process directory.
type Places struct {
	Project string
	Draft   string // the draft directory
	Library string // the library of subagents
}

// PlacesOf returns the places of the flow of project in the process
// directory process.
func PlacesOf(process, project string) Places {
	return Places{Project: project, Draft: DraftDir(process, project), Library: LibraryDir(process)}
}

// NoFlowError means the project has no flow version.
type NoFlowError struct{ Project string }

func (e *NoFlowError) Error() string { return e.Project + ": no flow" }

// NoDraftError means the project has no flow draft.
type NoDraftError struct{ Project string }

func (e *NoDraftError) Error() string { return e.Project + ": no flow draft" }

// DraftInvalidError means the draft has problems, in the order of files and
// lines.
type DraftInvalidError struct {
	Dir      string
	Problems []Problem
}

func (e *DraftInvalidError) Error() string { return e.Dir + ": problems in the flow draft" }

// UnchangedError means the draft is the same as the active version.
type UnchangedError struct{ Version int }

func (e *UnchangedError) Error() string { return "the flow draft is the same as the active version" }

// Draft operations on files.
const (
	OpRead   = "read"
	OpWrite  = "write"
	OpDelete = "delete"
)

// DraftIOError means the files of the draft, or of the library, cannot be
// read, written or deleted.
type DraftIOError struct {
	Op      string // OpRead, OpWrite or OpDelete
	Path    string
	Library bool // the library of subagents, not the draft
	Err     error
}

func (e *DraftIOError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }
func (e *DraftIOError) Unwrap() error { return e.Err }

// Version is the active flow of a project as stored.
type Version struct {
	Number   int
	Snapshot Snapshot
}

// Active returns the active flow version of project; ok is false if there is
// none.
func Active(st *state.Store, project string) (v Version, ok bool, err error) {
	fv, ok, err := st.LatestFlow(project)
	if err != nil || !ok {
		return Version{}, false, err
	}
	snap, err := DecodeSnapshot(fv.Content)
	if err != nil {
		return Version{}, false, err
	}
	return Version{Number: fv.Version, Snapshot: snap}, true, nil
}

// ReadOpenDraft reads and checks the draft of a project whose record is in
// the store; the caller has checked that it is.
func ReadOpenDraft(p Places) (*Result, error) {
	res, err := ReadDraft(p.Draft, p.Library)
	if err != nil {
		return nil, ioError(OpRead, p, err)
	}
	return res, nil
}

// ioError wraps an error of the files of the draft or the library.
func ioError(op string, p Places, err error) error {
	var pe *fs.PathError
	if !errors.As(err, &pe) {
		return &DraftIOError{Op: op, Path: p.Draft, Err: err}
	}
	lib := pe.Path == p.Library || filepath.Dir(pe.Path) == p.Library
	return &DraftIOError{Op: op, Path: pe.Path, Library: lib, Err: err}
}

// EditResult is what Edit did.
type EditResult struct {
	Dir     string
	Base    int  // the version the draft was made from; 0 if the project had no flow
	Created bool // false if the draft was open already
}

// Edit opens the draft of the flow of a project: lays it out from the active
// version, or empty if the project has no flow. An open draft is continued;
// if its directory is gone, it is laid out again from the version it was
// made from. A directory without a record is a leftover of an earlier draft
// and is replaced.
func Edit(st *state.Store, p Places) (EditResult, error) {
	res := EditResult{Dir: p.Draft}
	var base Snapshot
	err := st.Write(func(tx *state.Tx) error {
		res.Created, res.Base, base = false, 0, Snapshot{}
		d, open, err := tx.FlowDraft(p.Project)
		if err != nil {
			return err
		}
		if open {
			res.Base = d.BaseVersion
			if res.Base == 0 {
				return nil
			}
			v, ok, err := tx.FlowVersion(p.Project, res.Base)
			if err != nil || !ok {
				return err
			}
			base, err = DecodeSnapshot(v.Content)
			return err
		}
		v, ok, err := tx.LatestFlow(p.Project)
		if err != nil {
			return err
		}
		if ok {
			res.Base = v.Version
			if base, err = DecodeSnapshot(v.Content); err != nil {
				return err
			}
		}
		if err := tx.AddFlowDraft(p.Project, res.Base); err != nil {
			return err
		}
		res.Created = true
		var data contract.FlowDraftCreatedData
		if res.Base > 0 {
			data.BaseVersion = &res.Base
		}
		_, err = tx.AddEvent(EventDraftCreated, p.Project, "", data)
		return err
	})
	if err != nil {
		return EditResult{}, err
	}

	if !res.Created {
		if _, err := os.Stat(p.Draft); err == nil {
			return res, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return EditResult{}, ioError(OpRead, p, err)
		}
	}
	if err := layOut(p.Draft, base); err != nil {
		return EditResult{}, ioError(OpWrite, p, err)
	}
	return res, nil
}

// layOut writes the files of snapshot s to directory dir, replacing what is
// there. The files are written to a new directory beside it first, so that a
// failure to write them leaves no half-written draft.
func layOut(dir string, s Snapshot) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// The directories of the flow are there even when empty, as a prompt of
	// the layout.
	for _, d := range []string{scenariosDir, stagesDir, partsDir, agentsDir} {
		if err := os.Mkdir(filepath.Join(tmp, d), 0o755); err != nil {
			return err
		}
	}
	for p, text := range s.Files {
		full := filepath.Join(tmp, filepath.FromSlash(p))
		if d := path.Dir(p); d != "." {
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(tmp, dir)
	}
	// A directory that is there may be the current directory of a terminal,
	// which Windows does not let delete: it is emptied and filled instead.
	if err := emptyDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(tmp, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// emptyDir deletes everything in directory dir, but not dir itself.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// remove deletes the draft directory dir. If dir itself cannot be deleted,
// as the current directory of a terminal in Windows, it stays empty: without
// the record of the draft it is a leftover the next edit fills.
func remove(dir string) error {
	if err := emptyDir(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	os.Remove(dir)
	return nil
}

// DiffResult is how the draft differs from the active flow.
type DiffResult struct {
	Version int // the active version; 0 if the project has no flow
	Changes []Change
}

// DiffDraft compares the draft of a project with its active flow. The draft
// may have problems: what can be read is compared.
func DiffDraft(st *state.Store, p Places) (DiffResult, error) {
	if _, open, err := st.FlowDraft(p.Project); err != nil {
		return DiffResult{}, err
	} else if !open {
		return DiffResult{}, &NoDraftError{Project: p.Project}
	}
	res, err := ReadOpenDraft(p)
	if err != nil {
		return DiffResult{}, err
	}
	var out DiffResult
	old := Snapshot{}
	if v, ok, err := Active(st, p.Project); err != nil {
		return DiffResult{}, err
	} else if ok {
		out.Version, old = v.Number, v.Snapshot
	}
	out.Changes = Diff(old, res.Snapshot)
	return out, nil
}

// Apply makes the draft of a project the next version of its flow and
// deletes the draft. A draft with problems, or the same as the active
// version, is not applied, and nothing changes.
func Apply(st *state.Store, p Places) (int, error) {
	if _, open, err := st.FlowDraft(p.Project); err != nil {
		return 0, err
	} else if !open {
		return 0, &NoDraftError{Project: p.Project}
	}
	res, err := ReadOpenDraft(p)
	if err != nil {
		return 0, err
	}
	if len(res.Problems) > 0 {
		return 0, &DraftInvalidError{Dir: p.Draft, Problems: res.Problems}
	}
	content, err := res.Snapshot.Encode()
	if err != nil {
		return 0, err
	}
	var version int
	err = st.Write(func(tx *state.Tx) error {
		// Another gentry may have applied or deleted the draft meanwhile.
		if _, open, err := tx.FlowDraft(p.Project); err != nil {
			return err
		} else if !open {
			return &NoDraftError{Project: p.Project}
		}
		version = 1
		v, ok, err := tx.LatestFlow(p.Project)
		if err != nil {
			return err
		}
		if ok {
			old, err := DecodeSnapshot(v.Content)
			if err != nil {
				return err
			}
			if len(Diff(old, res.Snapshot)) == 0 {
				return &UnchangedError{Version: v.Version}
			}
			version = v.Version + 1
		}
		if err := tx.AddFlowVersion(p.Project, version, content); err != nil {
			return err
		}
		if err := tx.DeleteFlowDraft(p.Project); err != nil {
			return err
		}
		_, err = tx.AddEvent(EventApplied, p.Project, "", contract.FlowAppliedData{Version: version})
		return err
	})
	if err != nil {
		return 0, err
	}
	// Without its record the directory is a leftover: the next edit replaces
	// it, so a failure to delete it now changes nothing.
	remove(p.Draft)
	return version, nil
}

// Discard deletes the draft of a project; the active flow is unchanged. The
// files go first: if they cannot be deleted, the draft stays open.
func Discard(st *state.Store, p Places) error {
	if _, open, err := st.FlowDraft(p.Project); err != nil {
		return err
	} else if !open {
		return &NoDraftError{Project: p.Project}
	}
	if err := remove(p.Draft); err != nil {
		return ioError(OpDelete, p, err)
	}
	return st.Write(func(tx *state.Tx) error {
		if _, open, err := tx.FlowDraft(p.Project); err != nil {
			return err
		} else if !open {
			return &NoDraftError{Project: p.Project}
		}
		if err := tx.DeleteFlowDraft(p.Project); err != nil {
			return err
		}
		_, err := tx.AddEvent(EventDraftDiscarded, p.Project, "", nil)
		return err
	})
}
