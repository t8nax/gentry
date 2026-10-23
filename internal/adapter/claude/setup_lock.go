package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/t8nax/gentry/internal/filelock"
	"github.com/t8nax/gentry/internal/paths"
)

// SetupLock holds the resources shared by setups of Claude: its
// configuration and the directory of the integration.
type SetupLock struct{ locks []*filelock.Lock }

// LockSetup serializes Gentry setups sharing either a Claude configuration
// or an integration directory. The caller holds it from reading the
// registration until a successful setup or its rollback has finished.
func LockSetup(ctx context.Context, dir string) (*SetupLock, error) {
	files, err := setupLockPaths(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(files[0]), 0o700); err != nil {
		return nil, err
	}
	l := &SetupLock{}
	// Every caller takes the configuration first, then the integration.
	// No holder of an integration lock waits for another configuration.
	for _, path := range files {
		held, err := filelock.AcquireContext(ctx, path)
		if err != nil {
			l.Release()
			return nil, &fs.PathError{Op: "lock", Path: path, Err: err}
		}
		l.locks = append(l.locks, held)
	}
	return l, nil
}

// Release unlocks the integration before the configuration. Files stay in
// place: removing one would let a new caller bypass an existing waiter.
func (l *SetupLock) Release() {
	for i := len(l.locks) - 1; i >= 0; i-- {
		l.locks[i].Release()
	}
}

// setupLockPaths identifies resources, independent of GENTRY_HOME and
// TMP/TEMP. The cache keeps the locks outside settings that can be
// unreadable or unwritable: those remain warnings of setup.
func setupLockPaths(dir string) ([]string, error) {
	config := "unknown-default-claude-configuration"
	if settings, err := SettingsPath(); err == nil {
		config, err = setupCanonical(filepath.Dir(settings))
		if err != nil {
			return nil, err
		}
	}
	integration, err := setupCanonical(dir)
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, resource := range []string{"configuration:" + config, "integration:" + integration} {
		sum := sha256.Sum256([]byte(resource))
		files = append(files, filepath.Join(cache, "gentry", "claude-setup", hex.EncodeToString(sum[:])+".lock"))
	}
	return files, nil
}

func setupCanonical(dir string) (string, error) {
	canonical, err := paths.Canonical(dir)
	if err != nil {
		// Resolving an inaccessible settings directory must not turn its
		// warning into a refusal before registration.
		canonical, err = filepath.Abs(dir)
		if err != nil {
			return "", err
		}
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		canonical = strings.ToLower(canonical)
	}
	return canonical, nil
}
