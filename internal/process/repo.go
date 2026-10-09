// Package process is the git repository of the operator's process, by
// decision R117: the flows of projects and the library of subagents as
// files, their versions as commits. What is committed is active; what differs
// in the working directory is a draft. Gentry alone commits to it, exchanges
// it with a remote repository, so that the process is the same on every
// machine of the operator, and checks changes made around it.
package process

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/t8nax/gentry/internal/filelock"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/paths"
)

// Format is the version of the format of the process repository, in
// process.yaml. A repository of a newer format is left alone.
const Format = 1

// Files and references of the repository.
const (
	formatFile  = "process.yaml"
	ignoreFile  = ".gitignore"
	libraryDir  = "agents"
	flowDir     = "flow"
	conflictDir = "conflict"

	remoteName = "origin"
	branchRef  = "refs/heads/main"
	remoteRef  = "refs/remotes/origin/main"
	// checkedRef is the last commit whose changes Gentry has checked: a change
	// after it made around Gentry is checked before it is used.
	checkedRef  = "refs/gentry/checked"
	incomingRef = "refs/gentry/incoming"

	lockFile   = "gentry.lock"
	syncedFile = "gentry-synced"
	indexFile  = "gentry-index"

	// trailer starts the service line of the commits of Gentry.
	trailer = "Gentry: "
)

// ignored keeps the conflict directories out of the repository: they hold
// the variants of another machine for the operator to compare.
const ignored = "/" + conflictDir + "/\n/*/" + conflictDir + "/\n"

// LockWait is how long a command waits for another one to release the
// repository.
var LockWait = 30 * time.Second

// Reasons of InvalidError and RemoteInvalidError.
const (
	ReasonNotProcess  = "not_process"  // a git repository of something else
	ReasonNewerFormat = "newer_format" // written by a newer Gentry
)

// InvalidError means the process directory is a git repository Gentry cannot
// use.
type InvalidError struct{ Path, Reason string }

func (e *InvalidError) Error() string { return e.Path + ": " + e.Reason }

// BusyError means another command of Gentry holds the repository longer than
// LockWait.
type BusyError struct{ Path string }

func (e *BusyError) Error() string { return e.Path + ": busy" }

// Repo is the open process repository. Only one command of Gentry holds it at
// a time: the agent and the operator do not interleave their git.
type Repo struct {
	Dir string // canonical

	lock         *os.File
	blobs        map[string]string // contents of blobs read, by hash
	trees        map[string]tree   // files of commits read, by hash
	headKnown    bool              // head holds HEAD: only Gentry moves it while the repository is open
	headHash     string
	ident        []string // environment of the identity of commits; empty if git has the operator's
	identKnown   bool
	checkedHash  string // the commit of checkedRef: only Gentry moves it while the repository is open
	checkedKnown bool
	config       map[string]string // the settings of git that Gentry reads, by key
}

// Open opens the process repository in directory dir, creating it if needed,
// and waits for other commands to release it. The caller closes it.
func Open(dir string) (*Repo, error) {
	if err := git.Find(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dir, err := paths.Canonical(dir)
	if err != nil {
		return nil, err
	}
	r := &Repo{Dir: dir, blobs: map[string]string{}, trees: map[string]tree{}}
	dotGit := filepath.Join(dir, ".git")
	fi, err := os.Stat(dotGit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, err := git.Run(dir, git.Opts{}, "init", "--quiet", "--initial-branch=main"); err != nil {
			return nil, err
		}
		// Gentry writes \n line ends itself and reads any: git converting them
		// only warns the operator who commits by hand.
		if _, err := git.Run(dir, git.Opts{}, "config", "core.autocrlf", "false"); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case !fi.IsDir():
		// A worktree or a submodule of another repository.
		return nil, &InvalidError{Path: dir, Reason: ReasonNotProcess}
	}
	if err := r.takeLock(dotGit); err != nil {
		return nil, err
	}
	if err := r.prepare(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the repository.
func (r *Repo) Close() {
	if r.lock != nil {
		filelock.Unlock(r.lock)
		r.lock.Close()
		r.lock = nil
	}
}

func (r *Repo) takeLock(dotGit string) error {
	f, err := os.OpenFile(filepath.Join(dotGit, lockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(LockWait)
	for {
		ok, err := filelock.TryLock(f)
		if err != nil {
			f.Close()
			return err
		}
		if ok {
			r.lock = f
			return nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return &BusyError{Path: r.Dir}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// prepare makes the first commit of a new repository, or checks that an
// existing one is a process repository of a format this Gentry knows.
func (r *Repo) prepare() error {
	head, err := r.head()
	if err != nil {
		return err
	}
	if head == "" {
		files := map[string]string{formatFile: "format: " + strconv.Itoa(Format) + "\n", ignoreFile: ignored}
		for p, text := range files {
			if err := os.WriteFile(filepath.Join(r.Dir, p), []byte(text), 0o644); err != nil {
				return err
			}
		}
		changes, err := r.hashAll(files)
		if err != nil {
			return err
		}
		c, err := r.commit("", changes, message("Create the process repository", "init"), nil)
		if err != nil {
			return err
		}
		if err := r.moveHead(c, ""); err != nil {
			return err
		}
		return r.setChecked(c)
	}
	format, ok, err := r.formatAt(head)
	if err != nil {
		return err
	}
	switch {
	case !ok:
		return &InvalidError{Path: r.Dir, Reason: ReasonNotProcess}
	case format > Format:
		return &InvalidError{Path: r.Dir, Reason: ReasonNewerFormat}
	}
	if checked, err := r.checked(); err != nil {
		return err
	} else if checked == "" {
		return r.setChecked(head)
	}
	return nil
}

// formatAt returns the format of the process at commit c; ok is false if c
// has no valid process.yaml.
func (r *Repo) formatAt(c string) (format int, ok bool, err error) {
	t, err := r.treeOf(c)
	if err != nil {
		return 0, false, err
	}
	e, found := t[formatFile]
	if !found {
		return 0, false, nil
	}
	if err := r.read([]string{e.hash}); err != nil {
		return 0, false, err
	}
	var v struct {
		Format int `yaml:"format"`
	}
	if err := yaml.Unmarshal([]byte(r.blobs[e.hash]), &v); err != nil || v.Format < 1 {
		return 0, false, nil
	}
	return v.Format, true, nil
}

// git runs git in the repository.
func (r *Repo) git(args ...string) (string, error) { return git.Run(r.Dir, git.Opts{}, args...) }

// head returns the commit of HEAD, or "" if the repository has none.
func (r *Repo) head() (string, error) {
	if r.headKnown {
		return r.headHash, nil
	}
	h, err := r.rev("HEAD")
	if err == nil {
		r.headHash, r.headKnown = h, true
	}
	return h, err
}

// rev returns the commit ref names, or "" if there is none.
func (r *Repo) rev(ref string) (string, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", ref+"^{commit}")
	var ce *git.CommandError
	if errors.As(err, &ce) {
		return "", nil
	}
	return strings.TrimSpace(out), err
}

// mergeBase returns the best common ancestor of commits a and b, or "" if
// they have none.
func (r *Repo) mergeBase(a, b string) (string, error) {
	out, err := r.git("merge-base", a, b)
	var ce *git.CommandError
	if errors.As(err, &ce) {
		return "", nil
	}
	return strings.TrimSpace(out), err
}

// checked returns the commit of checkedRef, or "" if there is none.
func (r *Repo) checked() (string, error) {
	if r.checkedKnown {
		return r.checkedHash, nil
	}
	c, err := r.rev(checkedRef)
	if err == nil {
		r.checkedHash, r.checkedKnown = c, true
	}
	return c, err
}

func (r *Repo) setChecked(c string) error {
	r.checkedKnown = false
	if _, err := r.git("update-ref", checkedRef, c); err != nil {
		return err
	}
	r.checkedHash, r.checkedKnown = c, true
	return nil
}

// configKeys are the settings of git that Gentry reads.
const configKeys = `^(user\.name|user\.email|remote\.` + remoteName + `\.url)$`

// configValue returns a setting of git named in configKeys, or "" if it is
// not set. The settings are read by one call of git; setRemote reads them
// anew.
func (r *Repo) configValue(key string) (string, error) {
	if r.config == nil {
		out, err := r.git("config", "-z", "--get-regexp", configKeys)
		var ce *git.CommandError
		if err != nil && !errors.As(err, &ce) {
			return "", err
		}
		// git exits with an error when nothing matches.
		r.config = map[string]string{}
		for _, rec := range strings.Split(out, "\x00") {
			k, v, _ := strings.Cut(rec, "\n")
			if k != "" {
				r.config[strings.ToLower(k)] = v // the last value wins, as for git config --get
			}
		}
	}
	return r.config[key], nil
}

// setRemote changes the remote repository by git remote with args.
func (r *Repo) setRemote(args ...string) error {
	r.config = nil
	_, err := r.git(append([]string{"remote"}, args...)...)
	return err
}

// moveHead moves HEAD from commit old to commit c and brings the index along;
// the working directory is left as it is. old is "" for the first commit.
func (r *Repo) moveHead(c, old string) error {
	args := []string{"update-ref", "HEAD", c}
	if old != "" {
		args = append(args, old)
	}
	r.headKnown = false
	if _, err := r.git(args...); err != nil {
		return err
	}
	r.headHash, r.headKnown = c, true
	_, err := r.git("reset", "--quiet")
	return err
}

// entry is a file of a commit.
type entry struct{ mode, hash string }

// tree is the files of a commit by path.
type tree map[string]entry

// treeOf returns the files of commit c; an empty tree for "".
func (r *Repo) treeOf(c string) (tree, error) {
	t := tree{}
	if c == "" {
		return t, nil
	}
	if known, ok := r.trees[c]; ok {
		return known, nil
	}
	out, err := r.git("ls-tree", "-r", "-z", "--full-tree", c)
	if err != nil {
		return nil, err
	}
	// A commit never changes; its files are read once.
	if len(c) == 40 || len(c) == 64 {
		defer func() { r.trees[c] = t }()
	}
	for _, rec := range strings.Split(out, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" {
			continue
		}
		t[p] = entry{mode: f[0], hash: f[2]}
	}
	return t, nil
}

// kind returns the files of kind k in t by path inside its directory.
func (t tree) kind(k Kind) map[string]entry {
	out := map[string]entry{}
	prefix := k.root() + "/"
	for p, e := range t {
		if rel, ok := strings.CutPrefix(p, prefix); ok {
			out[rel] = e
		}
	}
	return out
}

// kinds returns the kinds that have files in t.
func (t tree) kinds() []Kind {
	seen := map[Kind]bool{}
	for p := range t {
		if k, _, ok := kindOf(p); ok {
			seen[k] = true
		}
	}
	return sortKinds(seen)
}

// changedKinds returns the kinds whose files differ from a to b.
func changedKinds(a, b tree) []Kind {
	seen := map[Kind]bool{}
	for _, p := range changedPaths(a, b) {
		if k, _, ok := kindOf(p); ok {
			seen[k] = true
		}
	}
	return sortKinds(seen)
}

// changedPaths returns the paths whose files differ from a to b, in order.
func changedPaths[E comparable](a, b map[string]E) []string {
	var out []string
	for p, e := range a {
		if f, ok := b[p]; !ok || f != e {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// read reads the blobs of hashes into the cache.
func (r *Repo) read(hashes []string) error {
	var in strings.Builder
	for _, h := range hashes {
		if _, ok := r.blobs[h]; !ok {
			in.WriteString(h + "\n")
		}
	}
	if in.Len() == 0 {
		return nil
	}
	out, err := git.Run(r.Dir, git.Opts{Stdin: in.String()}, "cat-file", "--batch")
	if err != nil {
		return err
	}
	for out != "" {
		header, rest, ok := strings.Cut(out, "\n")
		f := strings.Fields(header)
		if !ok || len(f) != 3 {
			return fmt.Errorf("git cat-file: unexpected output %q", header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size+1 > len(rest) {
			return fmt.Errorf("git cat-file: unexpected output %q", header)
		}
		r.blobs[f[0]] = rest[:size]
		out = rest[size+1:]
	}
	return nil
}

// contents returns the normalized texts of files.
func (r *Repo) contents(files map[string]entry) (Files, error) {
	hashes := make([]string, 0, len(files))
	for _, e := range files {
		hashes = append(hashes, e.hash)
	}
	if err := r.read(hashes); err != nil {
		return nil, err
	}
	out := Files{}
	for p, e := range files {
		out[p] = string(Normalize([]byte(r.blobs[e.hash])))
	}
	return out, nil
}

// hashAll writes texts as blobs in one run of git and returns their entries
// by the same keys.
func (r *Repo) hashAll(texts map[string]string) (map[string]*entry, error) {
	out := map[string]*entry{}
	if len(texts) == 0 {
		return out, nil
	}
	dir, err := os.MkdirTemp(filepath.Join(r.Dir, ".git"), "gentry-blobs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	keys := slices.Sorted(mapKeys(texts))
	var list strings.Builder
	for i, k := range keys {
		p := filepath.Join(dir, strconv.Itoa(i))
		if err := os.WriteFile(p, []byte(texts[k]), 0o644); err != nil {
			return nil, err
		}
		list.WriteString(p + "\n")
	}
	res, err := git.Run(r.Dir, git.Opts{Stdin: list.String()}, "hash-object", "-w", "--no-filters", "--stdin-paths")
	if err != nil {
		return nil, err
	}
	hashes := strings.Fields(res)
	if len(hashes) != len(keys) {
		return nil, fmt.Errorf("git hash-object: %d hashes for %d files", len(hashes), len(keys))
	}
	for i, k := range keys {
		out[k] = &entry{mode: "100644", hash: hashes[i]}
	}
	return out, nil
}

// zeroHash removes a path in git update-index --index-info.
const zeroHash = "0000000000000000000000000000000000000000"

// commit makes a commit whose tree is the tree of parent with changes, a nil
// entry removing a path, and returns it; parent is "" for the first commit.
// env sets the author of a commit carried over from another; the committer
// is the identity of Gentry. HEAD does not move.
func (r *Repo) commit(parent string, changes map[string]*entry, msg string, env []string) (string, error) {
	index := filepath.Join(r.Dir, ".git", indexFile)
	os.Remove(index)
	defer os.Remove(index)
	o := git.Opts{Env: []string{"GIT_INDEX_FILE=" + index}}
	read := []string{"read-tree", "--empty"}
	if parent != "" {
		read = []string{"read-tree", parent}
	}
	if _, err := git.Run(r.Dir, o, read...); err != nil {
		return "", err
	}
	var info strings.Builder
	for _, p := range slices.Sorted(mapKeys(changes)) {
		if e := changes[p]; e != nil {
			fmt.Fprintf(&info, "%s %s\t%s\n", e.mode, e.hash, p)
		} else {
			fmt.Fprintf(&info, "0 %s\t%s\n", zeroHash, p)
		}
	}
	if info.Len() > 0 {
		o.Stdin = info.String()
		if _, err := git.Run(r.Dir, o, "update-index", "--index-info"); err != nil {
			return "", err
		}
		o.Stdin = ""
	}
	t, err := git.Run(r.Dir, o, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(t)}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	ident, err := r.identity()
	if err != nil {
		return "", err
	}
	out, err := git.Run(r.Dir, git.Opts{Stdin: msg, Env: append(ident, env...)}, append(args, "-F", "-")...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// identity returns the environment that names the author and the committer of
// the commits of Gentry: the operator's identity of git if there is one,
// otherwise Gentry itself.
func (r *Repo) identity() ([]string, error) {
	if r.identKnown {
		return r.ident, nil
	}
	known := true
	for _, key := range []string{"user.name", "user.email"} {
		v, err := r.configValue(key)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(v) == "" {
			known = false
		}
	}
	r.identKnown = true
	if !known {
		r.ident = []string{
			"GIT_AUTHOR_NAME=Gentry", "GIT_AUTHOR_EMAIL=gentry@localhost",
			"GIT_COMMITTER_NAME=Gentry", "GIT_COMMITTER_EMAIL=gentry@localhost",
		}
	}
	return r.ident, nil
}

// message returns the message of a commit of Gentry: the subject and the
// service line naming the action.
func message(subject, action string) string {
	return subject + "\n\n" + trailer + action + "\n"
}

// byGentry reports whether a commit message has the service line of Gentry.
func byGentry(msg string) bool {
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, trailer) {
			return true
		}
	}
	return false
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// RemoteURL returns the address of the remote repository, or "" if none is
// set.
func (r *Repo) RemoteURL() (string, error) {
	url, err := r.configValue("remote." + remoteName + ".url")
	return strings.TrimSpace(url), err
}

// Synced returns the time of the last successful synchronization; ok is
// false if there was none.
func (r *Repo) Synced() (t time.Time, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(r.Dir, ".git", syncedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}, false, nil
	}
	return t, true, nil
}

func (r *Repo) markSynced() error {
	return os.WriteFile(filepath.Join(r.Dir, ".git", syncedFile), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o644)
}

// Kind is a kind of the process: the flow of a project or the library of
// subagents.
type Kind struct {
	Project string // empty for the library
}

// Library is the library of subagents.
var Library = Kind{}

// FlowOf returns the flow of project.
func FlowOf(project string) Kind { return Kind{Project: project} }

// IsLibrary reports whether k is the library of subagents.
func (k Kind) IsLibrary() bool { return k.Project == "" }

// root is the directory of k in the repository.
func (k Kind) root() string {
	if k.IsLibrary() {
		return libraryDir
	}
	return k.Project + "/" + flowDir
}

// conflictRoot is the directory of the variants of another machine of k.
func (k Kind) conflictRoot() string {
	if k.IsLibrary() {
		return conflictDir
	}
	return k.Project + "/" + conflictDir
}

// KindDir returns the directory of k: its working files.
func (r *Repo) KindDir(k Kind) string { return filepath.Join(r.Dir, filepath.FromSlash(k.root())) }

// ConflictDir returns the directory of the variants of another machine of k,
// which exists only while k is in conflict.
func (r *Repo) ConflictDir(k Kind) string {
	return filepath.Join(r.Dir, filepath.FromSlash(k.conflictRoot()))
}

// kindOf returns the kind path p of the repository belongs to and the path
// inside its directory; ok is false for other files.
func kindOf(p string) (k Kind, rel string, ok bool) {
	if rel, ok := strings.CutPrefix(p, libraryDir+"/"); ok {
		return Library, rel, true
	}
	project, rest, ok := strings.Cut(p, "/")
	if !ok || project == "" || strings.HasPrefix(project, ".") {
		return Kind{}, "", false
	}
	if rel, ok := strings.CutPrefix(rest, flowDir+"/"); ok {
		return FlowOf(project), rel, true
	}
	return Kind{}, "", false
}

// sortKinds returns the kinds of set in order: flows by project, the library
// last.
func sortKinds(set map[Kind]bool) []Kind {
	out := make([]Kind, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b Kind) int {
		switch {
		case a.IsLibrary() == b.IsLibrary():
			return strings.Compare(a.Project, b.Project)
		case a.IsLibrary():
			return 1
		}
		return -1
	})
	return out
}

// Files are the texts of a kind by path inside its directory, with forward
// slashes, normalized by Normalize.
type Files map[string]string

// Equal reports whether f and g have the same files.
func (f Files) Equal(g Files) bool {
	if len(f) != len(g) {
		return false
	}
	for p, text := range f {
		if other, ok := g[p]; !ok || other != text {
			return false
		}
	}
	return true
}

// Normalize removes a byte order mark and brings line ends to \n, so that an
// editor that writes \r\n does not make an unchanged file look changed.
func Normalize(b []byte) []byte {
	b = slices.Clone(b)
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	return []byte(strings.ReplaceAll(string(b), "\r\n", "\n"))
}

// ReadDir reads the files in directory dir and below it, normalized. Hidden
// files and directories, whose name starts with a dot, are left out. A
// directory that does not exist has no files. It returns an error satisfying
// errors.As(err, *fs.PathError) if something cannot be read.
func ReadDir(dir string) (Files, error) {
	out := Files{}
	var walk func(rel string) error
	walk = func(rel string) error {
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
			if isDir {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			b, err := os.ReadFile(filepath.Join(full, name))
			if err != nil {
				return err
			}
			out[p] = string(Normalize(b))
		}
		return nil
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	// path.Join(".", name) is name: the root is not part of the paths.
	if err := walk("."); err != nil {
		return nil, err
	}
	return out, nil
}
