//go:build unix

package checks

import (
	"os/exec"
	"syscall"
)

// tree is a command with the processes it starts: a process group of its
// own, ended as a whole.
type tree struct{ cmd *exec.Cmd }

func newTree(cmd *exec.Cmd) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &tree{cmd: cmd}
}

// start starts the command in a new process group.
func (t *tree) start() error { return t.cmd.Start() }

// kill ends the processes of the group.
func (t *tree) kill() error {
	if t.cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
}

// close has nothing to release.
func (t *tree) close() {}
