//go:build !windows

package process

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes the lock of f without waiting; ok is false if another
// process holds it.
func tryLock(f *os.File) (ok bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// unlock releases the lock of f taken by tryLock.
func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
