package checks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helperEnv makes the test binary a program for the commands of the tests:
// its first argument names what it does.
const helperEnv = "GENTRY_CHECKS_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(helper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// helper does what args name and returns the exit code:
//   - output <code>: writes to stdout and stderr in turn, exits with code;
//   - cwd: prints the current directory and the arguments after it;
//   - tree <marker>: starts a child that writes the marker after a second,
//     then sleeps 5 seconds;
//   - marker <path>: sleeps a second and writes the file path;
//   - sleep <seconds>.
func helper(args []string) int {
	switch args[0] {
	case "output":
		fmt.Fprintln(os.Stdout, "out 1")
		fmt.Fprintln(os.Stderr, "err 1")
		fmt.Fprintln(os.Stdout, "out 2")
		code, _ := strconv.Atoi(args[1])
		return code
	case "cwd":
		wd, _ := os.Getwd()
		fmt.Println(wd)
		fmt.Println(strings.Join(args[1:], "|"))
		return 0
	case "tree":
		child := exec.Command(os.Args[0], "marker", args[1])
		if err := child.Start(); err != nil {
			return 9
		}
		time.Sleep(5 * time.Second)
		return 0
	case "marker":
		time.Sleep(time.Second)
		os.WriteFile(args[1], nil, 0o644)
		return 0
	case "sleep":
		n, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(n) * time.Second)
		return 0
	}
	return 99
}

// command is a command of the helper in a temporary worktree, with its file
// of output.
func command(t *testing.T, args ...string) Command {
	t.Helper()
	t.Setenv(helperEnv, "1")
	dir := t.TempDir()
	return Command{Dir: dir, Args: append([]string{os.Args[0]}, args...), Log: filepath.Join(dir, "out", "work-1-1.log")}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// TestRun checks the exit code of a command, its output in the order it
// writes it to the file, which it replaces, the worktree it runs in and its
// arguments as they are.
func TestRun(t *testing.T) {
	ctx := context.Background()
	c := command(t, "output", "3")
	if err := os.MkdirAll(filepath.Dir(c.Log), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Log, []byte("an older attempt\nwith more lines\nthan now\nand more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Run(ctx, c)
	if err != nil || code != 3 {
		t.Fatalf("output: code %d, %v", code, err)
	}
	if got := read(t, c.Log); got != "out 1\nerr 1\nout 2\n" {
		t.Errorf("output: %q", got)
	}
	if code, err := Run(ctx, command(t, "output", "0")); err != nil || code != 0 {
		t.Errorf("output 0: code %d, %v", code, err)
	}

	c = command(t, "cwd", "a b", "c|d", "$HOME", "*")
	if code, err := Run(ctx, c); err != nil || code != 0 {
		t.Fatalf("cwd: code %d, %v", code, err)
	}
	want, _ := filepath.EvalSymlinks(c.Dir)
	lines := strings.Split(read(t, c.Log), "\n")
	if got, _ := filepath.EvalSymlinks(lines[0]); !strings.EqualFold(got, want) || lines[1] != "a b|c|d|$HOME|*" {
		t.Errorf("cwd: %q, want %q", lines, want)
	}
}

// TestRunNotFound checks that a program not found is named and has no file
// of output: on PATH, by a path in the worktree, and in the current
// directory, which is not the worktree.
func TestRunNotFound(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	name := "gentry-no-such-program"
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(cwd, name+".exe"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, program := range []string{name, "./" + name, filepath.Join("bin", name)} {
		c := command(t, "output", "0")
		c.Args[0] = program
		var nf *NotFoundError
		if _, err := Run(context.Background(), c); !errors.As(err, &nf) || nf.Program != program {
			t.Errorf("%s: %v", program, err)
		}
		if _, err := os.Stat(c.Log); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: a file of output: %v", program, err)
		}
	}
	// A path relative to the worktree finds the program there.
	c := command(t, "output", "0")
	exe := filepath.Join(c.Dir, "tools", filepath.Base(os.Args[0]))
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, b, 0o755); err != nil {
		t.Fatal(err)
	}
	c.Args[0] = "tools/" + filepath.Base(os.Args[0])
	if code, err := Run(context.Background(), c); err != nil || code != 0 {
		t.Errorf("a program of the worktree: code %d, %v", code, err)
	}
}

// TestRunTimeout checks that a command that does not end in time is ended
// with the process it started, which leaves no marker, and that Run returns
// at once with the error of the context.
func TestRunTimeout(t *testing.T) {
	c := command(t, "tree", "")
	marker := filepath.Join(c.Dir, "marker")
	c.Args[2] = marker
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, c)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Run returned after %v", d)
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the child of the command was not ended: %v", err)
	}
}

// TestRunCancel checks that a command that goes on reports its progress, and
// that it is ended when its context is cancelled: here at the second report,
// which is slow, so that the command ends while it is made. Run returns only
// after the report: the answer of a call comes after its last report.
func TestRunCancel(t *testing.T) {
	defer func(d time.Duration) { ProgressEvery = d }(ProgressEvery)
	ProgressEvery = 100 * time.Millisecond
	var reports atomic.Int32
	var returned, late atomic.Bool
	c := command(t, "sleep", "10")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Progress = func() {
		if reports.Add(1) == 2 {
			cancel()
			time.Sleep(500 * time.Millisecond)
		}
		if returned.Load() {
			late.Store(true)
		}
	}
	start := time.Now()
	_, err := Run(ctx, c)
	returned.Store(true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run returned after %v", d)
	}
	n := reports.Load()
	time.Sleep(time.Second)
	if late.Load() || reports.Load() != n {
		t.Error("progress is reported after Run returned")
	}
}

func TestTail(t *testing.T) {
	dir := t.TempDir()
	var long strings.Builder
	for i := 1; i <= 250; i++ {
		fmt.Fprintf(&long, "line %d\r\n", i)
	}
	big := strings.Repeat("x", 200<<10) + "\nlast\n"
	tests := []struct {
		name, text, want string
		n                int
	}{
		{"empty", "", "", 100},
		{"one line", "stop\n", "stop", 100},
		{"no line end", "a\nb", "a\nb", 100},
		{"empty lines kept", "\n\na\n\n", "\n\na\n", 100},
		{"last lines", long.String(), strings.TrimSuffix(long.String()[strings.Index(long.String(), "line 151"):], "\r\n"), 100},
		{"long line", big, strings.Repeat("x", 200<<10) + "\nlast", 100},
		{"two of three", "a\nb\nc\n", "b\nc", 2},
	}
	for _, tt := range tests {
		path := filepath.Join(dir, tt.name)
		if err := os.WriteFile(path, []byte(tt.text), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := Tail(path, tt.n); err != nil || got != tt.want {
			t.Errorf("%s: %q, %v; want %q", tt.name, trim(got), err, trim(tt.want))
		}
	}
}

// trim shortens a text for a message.
func trim(s string) string {
	if len(s) > 80 {
		return s[:40] + "…" + s[len(s)-40:]
	}
	return s
}
