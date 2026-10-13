package checks

import "syscall"

// endWithGentry makes the system kill the command when the process of
// Gentry that started it dies, for one killed with no chance to end its
// commands. Only the command itself is killed, not the processes it
// started; Linux sends the signal when the thread that started the command
// ends, which the Go runtime does not do while Gentry runs.
func endWithGentry(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
