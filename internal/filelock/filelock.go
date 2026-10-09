// Package filelock takes exclusive locks of files between processes of
// Gentry: a lock is held by an open file and released by the system when
// the process ends, so a crash leaves no stale lock.
package filelock

import (
	"errors"
	"os"
	"time"
)

// ErrBusy means another process held the lock for the whole wait.
var ErrBusy = errors.New("lock busy")

// Lock is a lock taken by Acquire.
type Lock struct{ f *os.File }

// Acquire takes the lock of the file at path, creating it, and waits up to
// wait for another process to release it.
func Acquire(path string, wait time.Duration) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		ok, err := TryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return &Lock{f: f}, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrBusy
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release releases the lock.
func (l *Lock) Release() {
	Unlock(l.f)
	l.f.Close()
}
