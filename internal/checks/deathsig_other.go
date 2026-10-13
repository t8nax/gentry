//go:build unix && !linux

package checks

import "syscall"

// endWithGentry does nothing: the system has no signal for the death of the
// parent.
func endWithGentry(*syscall.SysProcAttr) {}
