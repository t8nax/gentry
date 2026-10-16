package install_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The guard of the real home. The stand gives each script a home of its own,
// but a script that took the home from the system rather than from HOME or
// USERPROFILE, or wrote the variable Path of the user, would change the
// machine of the user who runs the tests. The guard takes the state of the
// real home before the tests, checks it after each run of a script, puts it
// back when a run has changed it and fails every later run of a script
// without running it.
var guard struct {
	mu       sync.Mutex
	watched  []*watch
	userPath *userPath // the variable Path of the user; nil: not guarded
	broken   string    // what a run changed; "" while nothing
}

// A watch is a file of the real home, or a folder of files, with its content
// before the tests.
type watch struct {
	path    string
	dir     bool
	content map[string][]byte // by name in the folder, "" for a file; nil: absent
}

// guardRealHome takes the state of the real home: the profiles of the shells
// and the folder of the program on Linux and macOS, the folder of the program
// on Windows, and the variable Path of the user on a Windows machine that is
// not a runner of CI. Call it from TestMain before Main moves the home.
func guardRealHome() error {
	var home string
	var files, dirs []string
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
		dirs = []string{".gentry/bin"}
	} else {
		home = os.Getenv("HOME")
		files = []string{".profile", ".bashrc", ".zshrc", ".config/fish/config.fish"}
		dirs = []string{".gentry/bin"}
	}
	if home != "" {
		for _, f := range files {
			guard.watched = append(guard.watched, &watch{path: filepath.Join(home, filepath.FromSlash(f))})
		}
		for _, d := range dirs {
			guard.watched = append(guard.watched, &watch{path: filepath.Join(home, filepath.FromSlash(d)), dir: true})
		}
	}
	for _, w := range guard.watched {
		var err error
		if w.content, err = w.read(); err != nil {
			return err
		}
	}
	if runtime.GOOS == "windows" && !onCIRunner() {
		p, err := readUserPath()
		if err != nil {
			return err
		}
		guard.userPath = &p
	}
	return nil
}

// read returns the content of the watched file or folder now.
func (w *watch) read() (map[string][]byte, error) {
	if !w.dir {
		b, err := os.ReadFile(w.path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return map[string][]byte{"": b}, nil
	}
	entries, err := os.ReadDir(w.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	content := map[string][]byte{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(w.path, e.Name()))
		if err != nil {
			return nil, err
		}
		content[e.Name()] = b
	}
	return content, nil
}

// restore puts back the content the watched file or folder had before the
// tests.
func (w *watch) restore(now map[string][]byte) error {
	var errs []error
	switch {
	case w.content == nil && w.dir:
		errs = append(errs, os.RemoveAll(w.path))
	case w.content == nil:
		errs = append(errs, os.Remove(w.path))
	case !w.dir:
		errs = append(errs, os.WriteFile(w.path, w.content[""], 0o644))
	default:
		errs = append(errs, os.MkdirAll(w.path, 0o755))
		for name := range now {
			if _, ok := w.content[name]; !ok {
				errs = append(errs, os.Remove(filepath.Join(w.path, name)))
			}
		}
		for name, b := range w.content {
			if !bytes.Equal(now[name], b) {
				errs = append(errs, os.WriteFile(filepath.Join(w.path, name), b, 0o755))
			}
		}
	}
	return errors.Join(errs...)
}

// stopIfBroken fails the test before it runs a script once a run has changed
// the real home.
func stopIfBroken(t *testing.T) {
	t.Helper()
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.broken != "" {
		t.Fatalf("an earlier run of a script changed the real home (%s): the scripts run no more", guard.broken)
	}
}

// checkRealHome fails the test if the run of a script has changed the real
// home, and puts the real home back.
func checkRealHome(t *testing.T) {
	t.Helper()
	guard.mu.Lock()
	defer guard.mu.Unlock()
	var changed, failed []string
	for _, w := range guard.watched {
		now, err := w.read()
		if err == nil && maps.EqualFunc(now, w.content, bytes.Equal) && (now == nil) == (w.content == nil) {
			continue
		}
		changed = append(changed, w.path)
		if err := w.restore(now); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", w.path, err))
		}
	}
	if guard.userPath != nil {
		now, err := readUserPath()
		if err != nil || now != *guard.userPath {
			changed = append(changed, "the variable Path of the user")
			if err := writeUserPath(*guard.userPath); err != nil {
				failed = append(failed, fmt.Sprintf("the variable Path of the user: %v", err))
			}
		}
	}
	if len(changed) == 0 {
		return
	}
	if guard.broken == "" {
		guard.broken = strings.Join(changed, ", ")
	}
	if len(failed) > 0 {
		t.Errorf("the script changed the real home: %s; could not put back: %s", strings.Join(changed, ", "), strings.Join(failed, "; "))
		return
	}
	t.Errorf("the script changed the real home: %s; put back as it was before the tests", strings.Join(changed, ", "))
}
