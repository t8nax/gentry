//go:build windows

package filelock

import (
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes the lock of f without waiting; ok is false if another
// process holds it.
func TryLock(f *os.File) (ok bool, err error) {
	var o windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &o)
	if err == windows.ERROR_LOCK_VIOLATION || err == windows.ERROR_IO_PENDING {
		return false, nil
	}
	return err == nil, err
}

// Unlock releases the lock of f taken by TryLock.
func Unlock(f *os.File) error {
	var o windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &o)
}
