package codex

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/filelock"
)

// HomeEnv names the folder of the settings of Codex; without it the settings
// are in .codex in the home of the user.
const HomeEnv = "CODEX_HOME"

// The files of the settings of Codex in its folder.
const (
	configFile = "config.toml"
	hooksFile  = "hooks.json"
)

// Home returns the folder of the settings of Codex.
func Home() (string, error) {
	if dir := os.Getenv(HomeEnv); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// ConfigPath and HooksPath return the settings of Codex and the settings of
// its hooks in the folder of its settings home.
func ConfigPath(home string) string { return filepath.Join(home, configFile) }
func HooksPath(home string) string  { return filepath.Join(home, hooksFile) }

// Why the settings of Codex cannot be used, as the contract names it.
const (
	Unreadable = "unreadable" // the file is no TOML, or cannot be read
	Unwritable = "unwritable" // the file cannot be written
)

// SettingsError means the settings of Codex at Path cannot be read or
// written: Codex does not work with them, so Gentry changes nothing.
type SettingsError struct {
	Path   string
	Reason string // Unreadable or Unwritable
	Err    error
}

func (e *SettingsError) Error() string { return e.Path + ": " + e.Reason + ": " + e.Err.Error() }
func (e *SettingsError) Unwrap() error { return e.Err }

// config is the settings of Codex as read: the text and its values. A file
// that is not there is empty.
type config struct {
	path string
	text string
	doc  *tomlDoc
}

// readConfig reads the settings of Codex at path.
func readConfig(path string) (*config, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &SettingsError{Path: path, Reason: Unreadable, Err: err}
	}
	doc, err := parseTOML(string(b))
	if err != nil {
		return nil, &SettingsError{Path: path, Reason: Unreadable, Err: err}
	}
	return &config{path: path, text: string(b), doc: doc}, nil
}

// checkSettings reads the settings of Codex at path and returns the
// SettingsError of settings that cannot be read or written, or in which an
// edit setup of req may make cannot be made, such as one in an inline
// table: the edits are tried on a copy, the hooks taken for not turned off
// by the administrator, whose requirements only the program of Codex
// tells. Nothing is written.
func checkSettings(path string, req SetupRequest) (*config, error) {
	c, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	if err := checkWritable(path, Unwritable); err != nil {
		return nil, err
	}
	try := *c
	if _, _, _, err := try.edits(req, false, disabledIn(c)); err != nil {
		return nil, &SettingsError{Path: path, Reason: Unwritable, Err: err}
	}
	return c, nil
}

// lockName is the lock of the settings of Codex for the commands of Gentry,
// beside config.toml: two of them do not lose the edits of each other.
const lockName = configFile + ".gentry-lock"

// LockWait is how long a command waits for another to release the lock of
// the settings of Codex.
var LockWait = 10 * time.Second

// lockSettings takes the lock of the settings of Codex at path; a lock that
// cannot be taken makes the settings unwritable.
func lockSettings(path string) (*filelock.Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, &SettingsError{Path: path, Reason: Unwritable, Err: err}
	}
	l, err := filelock.Acquire(filepath.Join(filepath.Dir(path), lockName), LockWait)
	if err != nil {
		return nil, &SettingsError{Path: path, Reason: Unwritable, Err: err}
	}
	return l, nil
}

// checkWritable returns a SettingsError with reason if the file at path is
// there and cannot be opened for writing; nothing is written.
func checkWritable(path, reason string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &SettingsError{Path: path, Reason: reason, Err: err}
	}
	return f.Close()
}

// value returns the value at the path of keys.
func (c *config) value(keys ...string) (any, bool) { return tomlAt(c.doc.root, keys...) }

// edit applies the edit of the text and reads the text anew.
func (c *config) edit(f func(text string) (string, error)) error {
	text, err := f(c.text)
	if err != nil {
		return err
	}
	doc, err := parseTOML(text)
	if err != nil {
		return err
	}
	c.text, c.doc = text, doc
	return nil
}

// set sets the value at the path of keys to the TOML text lit.
func (c *config) set(lit string, keys ...string) error {
	return c.edit(func(text string) (string, error) { return tomlSet(text, keys, lit) })
}

// remove removes the value at the path of keys.
func (c *config) remove(keys ...string) error {
	return c.edit(func(text string) (string, error) { return tomlRemove(text, keys) })
}

// appendTo adds the TOML text lit to the array at the path of keys.
func (c *config) appendTo(lit string, keys ...string) error {
	return c.edit(func(text string) (string, error) { return tomlAppend(text, keys, lit) })
}

// writeFile replaces the file at path with data through a file beside it,
// so that a failure midway leaves the old file whole. A symbolic link is
// followed, and the file keeps its mode. A file that cannot be opened for
// writing is not replaced: on Linux and macOS a file closed for writing
// could be replaced in a folder open for writing.
func writeFile(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		f.Close()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	tmp := path + ".tmp-" + hex.EncodeToString(suffix)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// SamePath tells whether a path of the settings of Codex, such as a key of
// projects, names the path p by the rules of Codex: cleaned, without the
// prefix \\?\ Codex writes on Windows, and on Windows without regard to the
// case.
func SamePath(a, b string) bool {
	a, b = filepath.Clean(withoutVerbatim(a)), filepath.Clean(withoutVerbatim(b))
	return a == b || runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

// Trust levels of a project in the settings of Codex.
const (
	trusted   = "trusted"
	untrusted = "untrusted"
)

// trustRecord returns the record of trust of the settings of Codex that
// applies to the folder dir by the rules of Codex: the record of dir, else
// of the root of its project by the markers of Codex, else of the root of
// its main repository; false if there is none.
func (c *config) trustRecord(dir string) (key, level string, ok bool) {
	return trustRecordIn(c.doc.root, dir, c.markers())
}

// trustRecordIn returns the record of trust of the settings of Codex of
// the values that applies to the folder dir, as trustRecord does. Of two
// records of one place, such as keys in another case, the one of the key
// as the place is written is taken, else the first by key: the choice does
// not change from a call to another.
func trustRecordIn(values map[string]any, dir string, markers []string) (key, level string, ok bool) {
	projects, _ := tomlAt(values, "projects")
	records, _ := projects.(map[string]any)
	if len(records) == 0 {
		return "", "", false
	}
	keys := slices.Sorted(maps.Keys(records))
	root := projectRoot(dir, markers)
	for _, place := range []string{dir, root, mainRoot(root)} {
		if place == "" {
			continue
		}
		k, found := "", false
		if _, exact := records[place]; exact {
			k, found = place, true
		} else if i := slices.IndexFunc(keys, func(k string) bool { return SamePath(k, place) }); i >= 0 {
			k, found = keys[i], true
		}
		if !found {
			continue
		}
		m, _ := records[k].(map[string]any)
		l, _ := m["trust_level"].(string)
		return k, l, true
	}
	return "", "", false
}

// markers returns the markers of the root of a project of Codex: those of
// the settings, or .git.
func (c *config) markers() []string {
	v, ok := c.value("project_root_markers")
	list, isList := v.([]any)
	if !ok || !isList {
		return []string{".git"}
	}
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// projectRoot returns the nearest folder from dir up that has one of the
// markers; "" if there is none.
func projectRoot(dir string, markers []string) string {
	for d := filepath.Clean(dir); ; {
		for _, m := range markers {
			if _, err := os.Lstat(filepath.Join(d, m)); err == nil {
				return d
			}
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// mainRoot returns the root of the main repository of the worktree of git at
// root, read from its files without git: the root itself for a main
// worktree or a folder that is no worktree; "" for "".
func mainRoot(root string) string {
	if root == "" {
		return ""
	}
	dotGit := filepath.Join(root, ".git")
	fi, err := os.Stat(dotGit)
	if err != nil || fi.IsDir() {
		return root
	}
	gitdir := readGitdir(dotGit)
	if gitdir == "" {
		return root
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	b, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return root
	}
	common := strings.TrimSpace(string(b))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return root
	}
	return filepath.Dir(common)
}

// readGitdir reads the line gitdir: of the file .git of a worktree.
func readGitdir(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "gitdir:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// A Distrusted is a worktree whose record of trust in the settings of Codex
// the operator set other than trusted, and the key of the record.
type Distrusted struct {
	Path string
	Key  string
}

// A Trust is what the trust of worktrees did in the settings of Codex.
type Trust struct {
	// Trusted are the worktrees whose trust was written: those without a
	// record that applies to them, trusted by the record of the root of
	// their main repository.
	Trusted []string
	// Distrusted are the worktrees with a record that applies to them and
	// is not trusted: they are left as they are.
	Distrusted []Distrusted
}

// trust trusts the worktrees without a record of trust that applies to
// them by a record of the root of their main repository, one for the
// worktrees of one repository; a worktree whose folder is not there is
// skipped. The records that apply are those of the settings before the
// command: a worktree trusted by the record it writes for another is one it
// trusted.
func (c *config) trust(worktrees []string) (Trust, error) {
	res := Trust{Trusted: []string{}, Distrusted: []Distrusted{}}
	var written []string
	before, markers := c.doc.root, c.markers()
	for _, w := range worktrees {
		if fi, err := os.Stat(w); err != nil || !fi.IsDir() {
			continue
		}
		key, level, ok := trustRecordIn(before, w, markers)
		switch {
		case ok && level == trusted:
			continue
		case ok:
			res.Distrusted = append(res.Distrusted, Distrusted{Path: w, Key: key})
			continue
		}
		root := mainRoot(projectRoot(w, c.markers()))
		if root == "" {
			root = w
		}
		already := false
		for _, k := range written {
			already = already || SamePath(k, root)
		}
		if !already {
			if err := c.set(`"`+trusted+`"`, "projects", root, "trust_level"); err != nil {
				return Trust{}, err
			}
			written = append(written, root)
		}
		res.Trusted = append(res.Trusted, w)
	}
	return res, nil
}

// TrustWorktrees trusts in Codex the worktrees without a record of trust
// that applies to them, as setup codex does, in the settings of Codex at
// path; worktrees with a record not trusted are left as they are and told.
// Settings that cannot be read or written are left as they are: a
// SettingsError.
func TrustWorktrees(path string, worktrees []string) (Trust, error) {
	lock, err := lockSettings(path)
	if err != nil {
		return Trust{}, err
	}
	defer lock.Release()
	c, err := readConfig(path)
	if err != nil {
		return Trust{}, err
	}
	before := c.text
	res, err := c.trust(worktrees)
	if err != nil {
		return Trust{}, &SettingsError{Path: path, Reason: Unwritable, Err: err}
	}
	if c.text != before {
		if err := writeFile(path, []byte(c.text)); err != nil {
			return Trust{}, &SettingsError{Path: path, Reason: Unwritable, Err: err}
		}
	}
	return res, nil
}

// distrusted returns the worktrees whose record of trust in the settings c
// of Codex is not trusted; nothing is written.
func distrusted(c *config, worktrees []string) []Distrusted {
	out := []Distrusted{}
	for _, w := range worktrees {
		if fi, err := os.Stat(w); err != nil || !fi.IsDir() {
			continue
		}
		if key, level, ok := c.trustRecord(w); ok && level != trusted {
			out = append(out, Distrusted{Path: w, Key: key})
		}
	}
	return out
}

// Trusted reports whether the folder dir of a session is trusted in Codex
// by the record of trust that applies to it in the settings of Codex at
// path; ok is false if the settings cannot be read.
func Trusted(path, dir string) (yes, ok bool) {
	c, err := readConfig(path)
	if err != nil {
		return false, false
	}
	_, level, found := c.trustRecord(dir)
	return found && level == trusted, true
}

// samePlace tells whether two paths name one file or folder: by the rules
// of Codex, or one reached by different paths, such as through a symbolic
// link or a short name.
func samePlace(a, b string) bool {
	if SamePath(a, b) {
		return true
	}
	ia, errA := os.Stat(withoutVerbatim(a))
	ib, errB := os.Stat(withoutVerbatim(b))
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// verbatim is the prefix Codex writes before a local path on Windows.
const verbatim = `\\?\`

// withoutVerbatim returns the path p without the prefix verbatim.
func withoutVerbatim(p string) string { return strings.TrimPrefix(p, verbatim) }
