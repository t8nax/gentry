package checks

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// NotFoundError means the program of a command is not found: by the path of
// the worktree if the program names a path, or else on PATH.
type NotFoundError struct{ Program string }

func (e *NotFoundError) Error() string { return e.Program + ": program not found" }

// NotStartedError means the program of a command is found, but the system
// did not start it: for example, it may not be run, or the file is no
// program.
type NotStartedError struct {
	Program string
	Reason  string // what the system says, as it says it
}

func (e *NotStartedError) Error() string { return e.Program + ": " + e.Reason }

// Command is a command of checks to run.
type Command struct {
	Dir  string   // the worktree the command runs in
	Args []string // the program and its arguments, as Split gives them
	Log  string   // the file of the output of the command; replaced if it exists
	// Progress, if set, is called when the command starts and then every
	// ProgressEvery while it runs.
	Progress func()
	// Mark, if set, names the command while it runs, so that End ends it
	// from another process: on Windows its job object is named after the
	// path Mark, elsewhere the file Mark holds its process group.
	Mark string
	// Ready, if set, is asked once the command can be ended by End: if it
	// returns an error, the command is ended at once, and Run returns the
	// error. On Windows the command has not run yet then.
	Ready func() error
}

// ProgressEvery is how often Run reports that a command goes on; tests set
// it.
var ProgressEvery = 5 * time.Second

// Run runs the command c without a shell and returns its exit code. The
// command writes its standard output and error, in the order it writes
// them, to the file c.Log; its standard input is empty and its environment
// is that of Gentry. When ctx is done, the command is ended with every
// process it started, and Run returns the error of ctx; the processes the
// command leaves behind when it ends are ended too. A program not found
// gives *NotFoundError, and a program the system does not start gives
// *NotStartedError; the file of the output is left only once the program
// starts, and is not made if ctx is done already.
func Run(ctx context.Context, c Command) (code int, err error) {
	program, err := lookPath(c.Dir, c.Args[0])
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(c.Log), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(c.Log)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	cmd := exec.CommandContext(ctx, program, c.Args[1:]...)
	cmd.Args[0] = c.Args[0]
	cmd.Dir = c.Dir
	cmd.Stdout, cmd.Stderr = out, out
	tree := newTree(cmd, c.Mark, c.Ready)
	defer tree.close()
	cmd.Cancel = tree.kill
	if err := tree.start(); err != nil {
		// The context may end the command while it starts: on Windows a
		// command ended before it is resumed fails to resume.
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		var pe *os.PathError
		if errors.As(err, &pe) {
			out.Close()
			os.Remove(c.Log)
			return 0, &NotStartedError{Program: c.Args[0], Reason: pe.Err.Error()}
		}
		return 0, err
	}
	// The progress is reported only while Run runs: the answer of the call
	// that reports it must come after the last report.
	stop := func() {}
	if c.Progress != nil {
		c.Progress()
		stop = Report(c.Progress)
	}
	err = cmd.Wait()
	stop()
	tree.kill()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// Report calls progress every ProgressEvery until stop is called; stop
// returns once the last call has returned, so that no call comes after it.
func Report(progress func()) (stop func()) {
	done := make(chan struct{})
	var calls sync.WaitGroup
	calls.Add(1)
	go func() {
		defer calls.Done()
		t := time.NewTicker(ProgressEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				progress()
			}
		}
	}()
	return func() {
		close(done)
		calls.Wait()
	}
}

// lookPath finds the program of a command: a name without a path on PATH,
// as the command line does, and a path relative to the worktree dir. The
// current directory of Gentry is not looked in: it is not the worktree.
func lookPath(dir, program string) (string, error) {
	seps := "/"
	if runtime.GOOS == "windows" {
		seps = `/\`
	}
	name := program
	if strings.ContainsAny(program, seps) && !filepath.IsAbs(program) {
		name = filepath.Join(dir, program)
	}
	path, err := exec.LookPath(name)
	if errors.Is(err, exec.ErrDot) {
		// exec.LookPath looks in the current directory first on Windows
		// and in the relative directories of PATH, and gives ErrDot for a
		// program found there even if a directory of PATH after them has
		// it.
		path, err = lookAbsPath(program)
	}
	if err != nil {
		var ee *exec.Error
		switch {
		case !errors.As(err, &ee):
			return "", err
		case errors.Is(ee.Err, exec.ErrNotFound) || errors.Is(ee.Err, os.ErrNotExist):
			return "", &NotFoundError{Program: program}
		}
		// The file is there, but it may not be run, for example.
		return "", &NotStartedError{Program: program, Reason: ee.Err.Error()}
	}
	return path, nil
}

// lookAbsPath looks for a program without a path in the absolute
// directories of PATH, in their order.
func lookAbsPath(program string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if path, err := exec.LookPath(filepath.Join(dir, program)); err == nil {
			return path, nil
		}
	}
	return "", &exec.Error{Name: program, Err: exec.ErrNotFound}
}

// TailLines is how many last lines of the output Tail gives.
const TailLines = 100

// tailMax limits what Tail reads from the end of a file: lines longer than
// that are cut.
const tailMax = 4 << 20

// Tail returns the last n lines of the file at path as they are: the bytes
// from the start of the n-th line from the end to the end of the file,
// without the line end of the last line. An empty file gives "".
func Tail(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	const block = 64 << 10
	var buf []byte
	pos := size
	for pos > 0 && len(buf) < tailMax {
		step := min(int64(block), pos)
		pos -= step
		chunk := make([]byte, step)
		if _, err := f.ReadAt(chunk, pos); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		buf = append(chunk, buf...)
		// The line end before the first of n lines and the one after the
		// last are enough.
		if countLines(buf) > n+1 {
			break
		}
	}
	text := string(buf)
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")
	i := len(text)
	for k := 0; k < n; k++ {
		j := strings.LastIndexByte(text[:i], '\n')
		if j < 0 {
			return text, nil
		}
		i = j
	}
	return text[i+1:], nil
}

// countLines counts the line ends in b.
func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
