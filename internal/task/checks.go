package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/t8nax/gentry/internal/checks"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/state"
)

// ChecksFailedError means a command of checks of the stage ended with an
// exit code other than 0.
type ChecksFailedError struct {
	Node    string
	Command string // as the flow writes it
	Code    int
	Log     string // the file of the output of the command
	Output  string // the last lines of the output, as they are
}

func (e *ChecksFailedError) Error() string {
	return fmt.Sprintf("%s: %s: exit code %d", e.Node, e.Command, e.Code)
}

// ChecksTimeoutError means the commands of checks of the stage did not end
// in the time of the stage: the command that ran was ended.
type ChecksTimeoutError struct {
	Node    string
	Timeout string // as the flow writes it, or the default
	Command string // the command that was ended
	Log     string
	Output  string // the last lines of what it wrote, as they are
}

func (e *ChecksTimeoutError) Error() string {
	return fmt.Sprintf("%s: checks did not end in %s", e.Node, e.Timeout)
}

// CheckProgramError means the program of a command of checks is not found.
type CheckProgramError struct {
	Node    string
	Program string
	Command string
}

func (e *CheckProgramError) Error() string { return e.Command + ": " + e.Program + " not found" }

// taskDir returns the directory of the files of task t in the data root:
// state/<project>/tasks/<task> for the first attempt of the task,
// …/<task>/attempts/<attempt> for a later one.
func taskDir(t state.Task) (string, error) {
	root, err := home.Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "state", t.Project, "tasks", t.Key())
	if t.Attempt > 1 {
		dir = filepath.Join(dir, "attempts", strconv.Itoa(t.Attempt))
	}
	return dir, nil
}

// CheckLogPath returns the file of the output of command n, from 1, of the
// checks of the pass of node in round: checks/<node>-<round>-<n>.log in the
// directory of the task.
func CheckLogPath(t state.Task, node string, round, n int) (string, error) {
	dir, err := taskDir(t)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "checks", fmt.Sprintf("%s-%d-%d.log", node, round, n)), nil
}

// CheckLogs returns the files of the output of the commands of checks run
// in the last attempt to close pass p of task t, in the order of the
// commands; none if no command ran.
func CheckLogs(t state.Task, p state.Pass) []string {
	var logs []string
	for n := 1; ; n++ {
		path, err := CheckLogPath(t, p.Node, p.Round, n)
		if err != nil {
			return logs
		}
		if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
			return logs
		}
		logs = append(logs, path)
	}
}

// runChecks runs the commands of checks of stage st for pass p of task t in
// order in the worktree of the task, within the time of the stage, and
// reports the progress while they run. The files of the output of the last
// attempt go first, so that the files of the pass are those of this one. The
// first command that fails or is ended stops the others.
func runChecks(ctx context.Context, t state.Task, p state.Pass, st flow.Stage, progress func()) error {
	limit, ok := checks.ParseTimeout(st.CheckTimeout())
	if !ok {
		return fmt.Errorf("stage %s: the time of checks %q", st.ID, st.CheckTimeout())
	}
	for _, path := range CheckLogs(t, p) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	run, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	for i, command := range st.Checks {
		args, err := checks.Split(command)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			return fmt.Errorf("stage %s: an empty command of checks", st.ID)
		}
		log, err := CheckLogPath(t, p.Node, p.Round, i+1)
		if err != nil {
			return err
		}
		code, err := checks.Run(run, checks.Command{Dir: t.Worktree, Args: args, Log: log, Progress: progress})
		var nf *checks.NotFoundError
		switch {
		case errors.As(err, &nf):
			return &CheckProgramError{Node: p.Node, Program: nf.Program, Command: command}
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			output, err := checks.Tail(log, checks.TailLines)
			if err != nil {
				return err
			}
			return &ChecksTimeoutError{Node: p.Node, Timeout: st.CheckTimeout(), Command: command, Log: log, Output: output}
		case err != nil:
			return err
		case code != 0:
			output, err := checks.Tail(log, checks.TailLines)
			if err != nil {
				return err
			}
			return &ChecksFailedError{Node: p.Node, Command: command, Code: code, Log: log, Output: output}
		}
	}
	return nil
}
