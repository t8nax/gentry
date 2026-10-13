//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestMCPSignalEndsChecks checks that SIGTERM to gentry mcp ends the command
// of checks of the call that runs, with the process it started, which leaves
// no marker, and the server.
func TestMCPSignalEndsChecks(t *testing.T) {
	bin := buildGentry(t)
	shop, files := checksShop(t, bin, "tree")
	s := startServer(t, bin, shop)
	s.send(t, "stage_exit", map[string]any{"kind": "result", "text": "Ветка"}, nil)
	waitFile(t, filepath.Join(files, "spans"))
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() {
		s.cmd.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("gentry mcp did not end on SIGTERM")
	}
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(filepath.Join(files, "marker")); err == nil {
		t.Error("the child of the command was not ended")
	}
	if got := pathNodes(t, bin, shop); got != "branch:" {
		t.Errorf("path: %s", got)
	}
}
