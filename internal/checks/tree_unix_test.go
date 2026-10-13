//go:build unix

package checks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestRunMarkNotWritten checks that a mark Gentry cannot write is an error
// of Gentry, not a program that does not start.
func TestRunMarkNotWritten(t *testing.T) {
	c := command(t, "output", "0")
	c.Mark = filepath.Join(t.TempDir(), "no-such-directory", ".running")
	_, err := Run(context.Background(), c)
	var ns *NotStartedError
	if err == nil || errors.As(err, &ns) {
		t.Errorf("Run: %v", err)
	}
}
