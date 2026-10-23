package claude

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupLockHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "user"))
	t.Setenv("USERPROFILE", filepath.Join(base, "user"))
	t.Setenv("LOCALAPPDATA", filepath.Join(base, "cache"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	return base
}

// TestSetupLockConfiguration exercises the real lock, without sleeps: a
// cancelled waiter still tries to acquire once, so a different key would
// incorrectly succeed instead of reporting the cancellation.
func TestSetupLockConfiguration(t *testing.T) {
	base := setupLockHome(t)
	config := filepath.Join(base, "real", "config")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigDirEnv, config)
	t.Setenv("GENTRY_HOME", filepath.Join(base, "first"))
	firstDir := filepath.Join(base, "first", "integrations", "claude")
	secondDir := filepath.Join(base, "second", "integrations", "claude")
	first, err := LockSetup(context.Background(), firstDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if first != nil {
			first.Release()
		}
	}()
	files, err := setupLockPaths(firstDir)
	if err != nil {
		t.Fatal(err)
	}
	path := files[0]
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("locking created the settings directory: %v", err)
	}
	// Creating settings during the first setup must not change its key.
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GENTRY_HOME", filepath.Join(base, "second"))
	t.Setenv("TMP", filepath.Join(base, "other-tmp"))
	t.Setenv("TEMP", filepath.Join(base, "other-tmp"))
	alias := filepath.Dir(config) + string(filepath.Separator) + ".." + string(filepath.Separator) + "real" + string(filepath.Separator) + "config"
	if runtime.GOOS == "windows" {
		alias = strings.ToUpper(alias)
	}
	t.Setenv(ConfigDirEnv, alias)
	assertBlocked := func(t *testing.T) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		second, err := LockSetup(ctx, secondDir)
		if second != nil {
			second.Release()
			t.Fatal("another data root/config alias/tmp acquired the held setup lock")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting setup returned %v; want context.Canceled", err)
		}
	}
	assertBlocked(t)
	t.Run("symbolic-link", func(t *testing.T) {
		link := filepath.Join(base, "link")
		if err := os.Symlink(filepath.Dir(config), link); err != nil {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		t.Setenv(ConfigDirEnv, filepath.Join(link, "config"))
		assertBlocked(t)
	})
	first.Release()
	first = nil
	second, err := LockSetup(context.Background(), secondDir)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("setup lock file was removed or replaced on release: %v", err)
	}
	// An unrelated Claude configuration does not share the lock.
	t.Setenv(ConfigDirEnv, filepath.Join(base, "unrelated"))
	otherPaths, err := setupLockPaths(secondDir)
	if err != nil || otherPaths[0] == path {
		t.Fatalf("unrelated configuration has the same lock: %v, %v", otherPaths, err)
	}
}

func TestSetupLockUnavailableSettings(t *testing.T) {
	base := setupLockHome(t)
	blocked := filepath.Join(base, "file")
	if err := os.WriteFile(blocked, []byte("settings parent is a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigDirEnv, filepath.Join(blocked, "config"))
	l, err := LockSetup(context.Background(), filepath.Join(base, "integration"))
	if err != nil {
		t.Fatalf("unavailable settings prevent taking the setup lock: %v", err)
	}
	l.Release()
}

// Different Claude configurations can still share the integration folder.
// A failed waiter must also release the configuration lock it took first.
func TestSetupLockDirectory(t *testing.T) {
	base := setupLockHome(t)
	dir := filepath.Join(base, "integration")
	t.Setenv(ConfigDirEnv, filepath.Join(base, "config-first"))
	first, err := LockSetup(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	t.Setenv(ConfigDirEnv, filepath.Join(base, "config-second"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second, err := LockSetup(ctx, dir)
	if second != nil {
		second.Release()
		t.Fatal("another configuration acquired the held integration lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting setup returned %v; want context.Canceled", err)
	}
	// The failed waiter released config-second, and a setup using it with
	// another integration can acquire both otherwise uncontended locks.
	independent, err := LockSetup(ctx, filepath.Join(base, "independent"))
	if err != nil {
		t.Fatalf("failed waiter kept its configuration lock: %v", err)
	}
	independent.Release()
}
