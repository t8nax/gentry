package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireContext checks that a lock held is waited for until ctx is
// done, that Held tells it is held, and that the lock is taken once it is
// released. Each open file holds a lock of its own, as another process
// would.
func TestAcquireContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	if held, err := Held(path); held || err != nil {
		t.Errorf("Held of no file: %v, %v", held, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Held made the file: %v", err)
	}
	first, err := AcquireContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if held, err := Held(path); !held || err != nil {
		t.Errorf("Held of a lock held: %v, %v", held, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := AcquireContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("AcquireContext of a lock held: %v", err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		first.Release()
	}()
	second, err := AcquireContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	if held, err := Held(path); held || err != nil {
		t.Errorf("Held of a lock released: %v, %v", held, err)
	}
}
