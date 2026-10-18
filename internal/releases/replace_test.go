package releases

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// holdEnv makes the program of the tests hold its file: it runs until its
// standard input is closed, as a server of tools of a session does.
const holdEnv = "GENTRY_TEST_HOLD"

func TestMain(m *testing.M) {
	if os.Getenv(holdEnv) != "" {
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// running puts a copy of the program of the tests at program and runs it
// until the test ends or stop is called.
func running(t *testing.T, program string) (stop func()) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, b, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(program)
	cmd.Env = append(os.Environ(), holdEnv+"=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	stop = func() {
		if !done {
			done = true
			in.Close()
			cmd.Wait()
		}
	}
	t.Cleanup(stop)
	return stop
}

// TestReplaceRunning checks that on Windows the program a process runs is
// renamed to .old, the new one takes its place, and the .old the process
// holds stays until RemoveOld finds it free.
func TestReplaceRunning(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a running program holds its file only on Windows")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, Program())
	stop := running(t, program)
	downloaded := filepath.Join(t.TempDir(), "gentry-new")
	os.WriteFile(downloaded, []byte("new"), 0o644)
	if err := Replace(downloaded, program); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(program); string(b) != "new" {
		t.Errorf("the program after Replace: %q", b)
	}
	if _, err := os.Stat(Old(program)); err != nil {
		t.Errorf("the running program is not at .old: %v", err)
	}
	stop()
	RemoveOld(program)
	if _, err := os.Stat(Old(program)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("RemoveOld left the free .old: %v", err)
	}
}

// TestReplaceRestore checks that on Windows a failure of the new program to
// take the place of the one renamed to .old gives the old one its name back,
// and that a failure of that too is told by the WriteError.
func TestReplaceRestore(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a program is renamed to .old only on Windows")
	}
	defer func(r func(string, string) error) { rename = r }(rename)
	for _, restoreFails := range []bool{false, true} {
		dir := t.TempDir()
		program := filepath.Join(dir, Program())
		os.WriteFile(program, []byte("old"), 0o755)
		downloaded := filepath.Join(t.TempDir(), "gentry-new")
		os.WriteFile(downloaded, []byte("new"), 0o644)
		busy, denied := errors.New("busy"), errors.New("denied")
		rename = func(from, to string) error {
			switch {
			case from == program+".new":
				return busy // the program is held, then the new one cannot take its place
			case from == Old(program) && restoreFails:
				return denied
			}
			return os.Rename(from, to)
		}
		var we *WriteError
		err := Replace(downloaded, program)
		if !errors.As(err, &we) || !errors.Is(we.Err, busy) {
			t.Fatalf("Replace: %v, want WriteError of the rename", err)
		}
		if restoreFails {
			if !errors.Is(we.Restore, denied) {
				t.Errorf("Restore %v, want the failure to give the name back", we.Restore)
			}
			if b, _ := os.ReadFile(Old(program)); string(b) != "old" {
				t.Errorf("the program is not at .old: %q", b)
			}
			continue
		}
		if b, _ := os.ReadFile(program); we.Restore != nil || string(b) != "old" {
			t.Errorf("the program after a failed Replace: %q, Restore %v; want it as it was", b, we.Restore)
		}
	}
}

// TestLockWaits checks that a second Lock waits for the first one to be
// released, and gives up when its ctx is done.
func TestLockWaits(t *testing.T) {
	program := filepath.Join(t.TempDir(), Program())
	first, err := Lock(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Lock(ctx, program); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Lock while another holds it, until ctx is done: %v, want %v", err, context.DeadlineExceeded)
	}
	start := time.Now()
	go func() {
		time.Sleep(200 * time.Millisecond)
		first.Release()
	}()
	second, err := Lock(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Errorf("the second Lock took %s: it did not wait for the first", d)
	}
}
