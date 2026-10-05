package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
)

// Event types written when a project is connected.
const (
	EventProjectAdded  = "project.added"
	EventWorktreeAdded = "worktree.added"
)

// Reasons of KnowledgeError. The list is open, as in the contract.
const (
	ReasonNotEmpty    = "not_empty"    // a non-empty directory that is not a git repository
	ReasonForeignRepo = "foreign_repo" // a repository with files but without project information
	ReasonNested      = "nested"       // the knowledge and a worktree lie one inside the other
	ReasonBadFile     = "bad_file"     // gentry.yaml cannot be read
)

var prefixPattern = regexp.MustCompile(`^[A-Z]{2,10}$`)

// ValidPrefix reports whether p is a valid prefix of task numbers: 2-10
// uppercase Latin letters.
func ValidPrefix(p string) bool { return prefixPattern.MatchString(p) }

// DefaultPrefix returns the letters of the project identifier in upper case,
// or false if they are not 2-10 letters: Gentry does not abbreviate on its own.
func DefaultPrefix(id string) (string, bool) {
	p := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, id)
	return p, ValidPrefix(p)
}

// AddRequest is a request to connect a project.
type AddRequest struct {
	Dir       string // the directory the command runs in
	ID        string // project identifier; may be empty if the knowledge has one
	Knowledge string // knowledge directory, absolute or relative to Dir
	Prefix    string // prefix of task numbers; empty for the default
}

// AddResult is what Add did.
type AddResult struct {
	Project          state.Project
	KnowledgeCreated bool     // the project information was written and committed now
	RepoCreated      bool     // the knowledge repository was created now
	Added            []string // worktrees added to the pool, the main one first
	Unchanged        bool     // the project was already connected so
	Unpooled         []string // worktrees of the repository in no pool, as git lists them
}

// NotRepoError means the command does not run in a git worktree.
type NotRepoError struct{ Path string }

func (e *NotRepoError) Error() string { return e.Path + ": not a git worktree" }

// KnowledgeError means the directory cannot hold the project knowledge.
type KnowledgeError struct {
	Path     string
	Reason   string
	Worktree string // the worktree it is nested with, for ReasonNested
	Cause    string // what is wrong with gentry.yaml, for ReasonBadFile
}

func (e *KnowledgeError) Error() string { return e.Path + ": " + e.Reason }

// IDMissingError means new knowledge is created but no identifier is given.
type IDMissingError struct{}

func (e *IDMissingError) Error() string { return "project identifier required" }

// MismatchError means the identifier given differs from the knowledge.
type MismatchError struct{ Value, KnowledgeValue string }

func (e *MismatchError) Error() string { return e.Value + " != " + e.KnowledgeValue }

// PrefixUnderivableError means the default prefix cannot be built from ID.
type PrefixUnderivableError struct{ ID string }

func (e *PrefixUnderivableError) Error() string { return e.ID + ": no default prefix" }

// ExistsError means the project is connected with another knowledge or,
// when Clone is set, with another main worktree.
type ExistsError struct {
	Project, Knowledge, MainWorktree string
	Clone                            bool
}

func (e *ExistsError) Error() string { return e.Project + ": already connected" }

// PrefixTakenError means another project of the machine has the prefix.
type PrefixTakenError struct{ Prefix, Project string }

func (e *PrefixTakenError) Error() string { return e.Prefix + ": taken by " + e.Project }

// WorktreeTakenError means the worktree is in the pool of another project.
type WorktreeTakenError struct{ Path, Project string }

func (e *WorktreeTakenError) Error() string { return e.Path + ": in the pool of " + e.Project }

// knowledgeKind is what the knowledge directory needs to become knowledge.
type knowledgeKind int

const (
	kindExisting   knowledgeKind = iota // knowledge of a colleague or of an earlier connection
	kindNewRepo                         // no directory or an empty one: create the repository
	kindEmptyRepo                       // a fresh repository, such as a new one from GitHub
	kindUnfinished                      // gentry.yaml written, the first commit failed
)

// Add connects a project: it makes or reads the knowledge, records the project
// and adds the main worktree and the current one to the pool. Connecting a
// connected project again changes nothing.
func Add(st *state.Store, req AddRequest) (AddResult, error) {
	if err := git.Find(); err != nil {
		return AddResult{}, err
	}
	dir, err := paths.Canonical(req.Dir)
	if err != nil {
		return AddResult{}, err
	}
	current, err := git.Root(dir)
	if errors.Is(err, git.ErrNotRepo) {
		return AddResult{}, &NotRepoError{Path: dir}
	}
	if err != nil {
		return AddResult{}, err
	}
	list, err := git.Worktrees(current)
	if err != nil {
		return AddResult{}, err
	}
	main := current
	if len(list) > 0 && list[0].Main {
		main = list[0].Path
	}

	k := req.Knowledge
	if !filepath.IsAbs(k) {
		k = filepath.Join(dir, k)
	}
	know, err := paths.Canonical(k)
	if err != nil {
		return AddResult{}, err
	}
	for _, w := range append(list, git.Worktree{Path: current}) {
		if paths.Within(know, w.Path) || paths.Within(w.Path, know) {
			return AddResult{}, &KnowledgeError{Path: know, Reason: ReasonNested, Worktree: w.Path}
		}
	}

	kind, info, err := inspect(know)
	if err != nil {
		return AddResult{}, err
	}
	id := req.ID
	lang := DefaultLanguage
	if kind == kindExisting || kind == kindUnfinished {
		if id != "" && id != info.Project {
			return AddResult{}, &MismatchError{Value: id, KnowledgeValue: info.Project}
		}
		id, lang = info.Project, info.Language
	} else if id == "" {
		return AddResult{}, &IDMissingError{}
	}
	prefix := req.Prefix
	if prefix == "" {
		var ok bool
		if prefix, ok = DefaultPrefix(id); !ok {
			return AddResult{}, &PrefixUnderivableError{ID: id}
		}
	}

	p := state.Project{ID: id, Prefix: prefix, Knowledge: know, MainWorktree: main}
	pool := []string{main}
	if !paths.Same(current, main) {
		pool = append(pool, current)
	}

	// Check before touching the knowledge, so that a refusal leaves no trace;
	// the transaction checks again.
	var existing state.Project
	var unpooled []string
	unchanged := false
	if err := st.Write(func(tx *state.Tx) error {
		var err error
		existing, unchanged, err = check(tx, p, pool)
		if err != nil || !unchanged {
			return err
		}
		unpooled, err = outside(tx, list)
		return err
	}); err != nil {
		return AddResult{}, err
	}
	if unchanged {
		return AddResult{Project: existing, Unchanged: true, Unpooled: unpooled}, nil
	}

	res := AddResult{Project: p, KnowledgeCreated: kind != kindExisting, RepoCreated: kind == kindNewRepo}
	if err := makeKnowledge(know, kind, Info{Format: KnowledgeFormat, Project: id, Language: lang}); err != nil {
		return AddResult{}, err
	}

	err = st.Write(func(tx *state.Tx) error {
		existing, unchanged, err = check(tx, p, pool)
		if err != nil {
			return err
		}
		if unchanged {
			unpooled, err = outside(tx, list)
			return err
		}
		if err := tx.AddProject(p); err != nil {
			return err
		}
		data := contract.ProjectAddedData{Knowledge: know, Prefix: prefix, KnowledgeCreated: res.KnowledgeCreated}
		if _, err := tx.AddEvent(EventProjectAdded, id, "", data); err != nil {
			return err
		}
		for i, path := range pool {
			w := state.Worktree{Path: path, Project: id, Main: i == 0}
			if err := tx.AddWorktree(w); err != nil {
				return err
			}
			if _, err := tx.AddEvent(EventWorktreeAdded, id, "", contract.WorktreeAddedData{Path: path, Main: w.Main}); err != nil {
				return err
			}
		}
		unpooled, err = outside(tx, list)
		return err
	})
	if err != nil {
		return AddResult{}, err
	}
	if unchanged {
		// Connected by another gentry meanwhile.
		return AddResult{Project: existing, Unchanged: true, Unpooled: unpooled}, nil
	}
	res.Added = pool
	res.Unpooled = unpooled
	return res, nil
}

// outside returns the worktrees of list that are in no pool: the operator is
// told about them, as Add does not add them on its own.
func outside(tx *state.Tx, list []git.Worktree) ([]string, error) {
	worktrees, err := tx.Worktrees()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, g := range list {
		if !slices.ContainsFunc(worktrees, func(w state.Worktree) bool { return paths.Same(w.Path, g.Path) }) {
			out = append(out, g.Path)
		}
	}
	return out, nil
}

// check tells whether p can be recorded with the worktrees of pool, or is
// recorded already.
func check(tx *state.Tx, p state.Project, pool []string) (existing state.Project, unchanged bool, err error) {
	projects, err := tx.Projects()
	if err != nil {
		return state.Project{}, false, err
	}
	for _, q := range projects {
		switch {
		case q.ID == p.ID && !paths.Same(q.Knowledge, p.Knowledge):
			return q, false, &ExistsError{Project: q.ID, Knowledge: q.Knowledge, MainWorktree: q.MainWorktree}
		case q.ID == p.ID && !paths.Same(q.MainWorktree, p.MainWorktree):
			return q, false, &ExistsError{Project: q.ID, Knowledge: q.Knowledge, MainWorktree: q.MainWorktree, Clone: true}
		case q.ID == p.ID:
			return q, true, nil
		case paths.Same(q.Knowledge, p.Knowledge):
			return q, false, &ExistsError{Project: q.ID, Knowledge: q.Knowledge, MainWorktree: q.MainWorktree}
		case q.Prefix == p.Prefix:
			return q, false, &PrefixTakenError{Prefix: p.Prefix, Project: q.ID}
		}
	}
	worktrees, err := tx.Worktrees()
	if err != nil {
		return state.Project{}, false, err
	}
	for _, w := range worktrees {
		for _, path := range pool {
			if paths.Same(w.Path, path) {
				return state.Project{}, false, &WorktreeTakenError{Path: w.Path, Project: w.Project}
			}
		}
	}
	return state.Project{}, false, nil
}

// inspect tells what the knowledge directory needs.
func inspect(dir string) (knowledgeKind, Info, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || err == nil && len(entries) == 0 {
		return kindNewRepo, Info{}, nil
	}
	if err != nil {
		return 0, Info{}, err
	}
	root, err := git.Root(dir)
	if errors.Is(err, git.ErrNotRepo) || err == nil && !paths.Same(root, dir) {
		return 0, Info{}, &KnowledgeError{Path: dir, Reason: ReasonNotEmpty}
	}
	if err != nil {
		return 0, Info{}, err
	}
	if !hasInfo(dir) {
		for _, e := range entries {
			if !serviceFile(e.Name()) {
				return 0, Info{}, &KnowledgeError{Path: dir, Reason: ReasonForeignRepo}
			}
		}
		return kindEmptyRepo, Info{}, nil
	}
	info, err := ReadInfo(dir)
	var ie *InvalidInfoError
	if errors.As(err, &ie) {
		return 0, Info{}, &KnowledgeError{Path: dir, Reason: ReasonBadFile, Cause: ie.Cause}
	}
	if err != nil {
		return 0, Info{}, err
	}
	committed, err := git.HasCommits(dir)
	if err != nil {
		return 0, Info{}, err
	}
	if !committed {
		return kindUnfinished, info, nil
	}
	return kindExisting, info, nil
}

// serviceFile reports whether a file may be in a fresh repository that is
// taken for knowledge: git files, a README and a LICENSE.
func serviceFile(name string) bool {
	n := strings.ToUpper(name)
	return n == ".GIT" || n == ".GITIGNORE" || n == ".GITATTRIBUTES" ||
		strings.HasPrefix(n, "README") || strings.HasPrefix(n, "LICENSE")
}

// makeKnowledge brings the knowledge directory to a committed gentry.yaml.
func makeKnowledge(dir string, kind knowledgeKind, info Info) error {
	switch kind {
	case kindExisting:
		return nil
	case kindNewRepo:
		if err := git.Init(dir); err != nil {
			return err
		}
	}
	if kind != kindUnfinished {
		if err := WriteInfo(dir, info); err != nil {
			return err
		}
	}
	return git.Commit(dir, InfoFile, msg.Knowledge(info.Language, msg.CommitProjectAdded, info.Project))
}
