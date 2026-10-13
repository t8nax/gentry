package checks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRunEndedAtStart checks that a command whose context is done after it
// starts and before it is in its job does not run: the job does not end it
// then, so it is ended by itself.
func TestRunEndedAtStart(t *testing.T) {
	c := command(t, "tree", "")
	marker := filepath.Join(c.Dir, "marker")
	c.Args[2] = marker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startedHook = func() {
		cancel()
		// exec ends the command in a goroutine of its own once the context
		// is done.
		time.Sleep(200 * time.Millisecond)
	}
	defer func() { startedHook = nil }()
	start := time.Now()
	if _, err := Run(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Run returned after %v", d)
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the command ran: %v", err)
	}
}
