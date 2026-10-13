//go:build unix

package checks

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// tree is a command with the processes it starts: a process group of its
// own, ended as a whole. The file of the mark of a command with one holds
// its group while it runs, so that End finds it from another process.
type tree struct {
	cmd   *exec.Cmd
	mark  string
	ready func() error
}

func newTree(cmd *exec.Cmd, mark string, ready func() error) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	endWithGentry(cmd.SysProcAttr)
	return &tree{cmd: cmd, mark: mark, ready: ready}
}

// start starts the command in a new process group and writes the mark.
// Then, if ready is set and does not allow the command, the command is ended
// at once: it starts running before ready is asked.
func (t *tree) start() error {
	if err := t.cmd.Start(); err != nil {
		return err
	}
	err := t.writeMark()
	if err == nil && t.ready != nil {
		err = t.ready()
	}
	if err != nil {
		t.kill()
		t.cmd.Wait()
		return err
	}
	return nil
}

// writeMark writes the process group of the command to the file of its
// mark, whole: the file is replaced by another.
func (t *tree) writeMark() error {
	if t.mark == "" {
		return nil
	}
	tmp := markTemp(t.mark)
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(t.cmd.Process.Pid)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.mark)
}

// kill ends the processes of the group.
func (t *tree) kill() error {
	if t.cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
}

// close removes the mark: the command no longer runs.
func (t *tree) close() {
	if t.mark != "" {
		os.Remove(t.mark)
	}
}

// End ends the command that runs with mark, in this process or another, with
// every process it started: it kills the process group the file of the mark
// names. If no command runs with mark, End does nothing.
func End(mark string) error {
	b, err := os.ReadFile(mark)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	group, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || group <= 1 {
		return nil
	}
	if err := syscall.Kill(-group, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
