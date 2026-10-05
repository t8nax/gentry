// Package paths brings file paths to one form, so that the same directory is
// recorded and compared the same way however it was named: relative or
// absolute, through a symbolic link, with forward slashes from git, or in
// another letter case on Windows and macOS.
package paths

import (
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
)

// Canonical returns the absolute path of p with symbolic links resolved. On
// Windows the letter case and short names are those of the file system. A path
// that does not exist yet is resolved up to its nearest existing parent.
func Canonical(p string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return "", err
	}
	var rest []string
	for dir := abs; ; {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
	}
}

// caseless reports whether file names differ only by case on this system by
// default.
var caseless = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// Same reports whether two canonical paths name the same directory.
func Same(a, b string) bool {
	if caseless {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Within reports whether canonical path p is dir or lies inside it.
func Within(p, dir string) bool {
	if Same(p, dir) {
		return true
	}
	prefix := strings.TrimSuffix(dir, string(filepath.Separator)) + string(filepath.Separator)
	if len(p) <= len(prefix) {
		return false
	}
	return Same(p[:len(prefix)], prefix)
}
