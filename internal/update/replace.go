package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/t8nax/gentry/internal/filelock"
)

// lockName is the lock of the updates of the program, in its folder: two
// updates at once do not replace the program together.
const lockName = ".update.lock"

// Lock takes the lock of the updates of the program and waits for another
// update to release it until ctx is done, then returns ctx.Err(). A folder of
// the program that cannot be written is a WriteError: the program could not
// be replaced either. The file of the lock stays in the folder after the
// update, as the locks of filelock do: the system releases the lock with the
// process, and a file left holds no lock.
func Lock(ctx context.Context, program string) (*filelock.Lock, error) {
	l, err := filelock.AcquireContext(ctx, filepath.Join(filepath.Dir(program), lockName))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &WriteError{Program: program, Err: err}
	}
	return l, nil
}

// Old returns the name a running program on Windows is renamed to when it is
// replaced.
func Old(program string) string { return program + ".old" }

// RemoveOld removes the programs earlier updates renamed on Windows,
// <program>.old and those moved aside from it, unless a process still holds
// them.
func RemoveOld(program string) {
	if runtime.GOOS != "windows" {
		return
	}
	entries, err := os.ReadDir(filepath.Dir(program))
	if err != nil {
		return
	}
	base := filepath.Base(program)
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, base+".") && strings.HasSuffix(n, ".old") && e.Type().IsRegular() {
			os.Remove(filepath.Join(filepath.Dir(program), n))
		}
	}
}

// clearOld makes room for <program>.old: a free one is removed, one a
// process still holds, such as the server of tools of a session that started
// before an earlier update, is moved aside to <program>.<random>.old, as a
// running program can be renamed.
func clearOld(program string) error {
	old := Old(program)
	err := os.Remove(old)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	return os.Rename(old, program+"."+hex.EncodeToString(suffix)+".old")
}

// rename renames a file; the tests make it fail.
var rename = os.Rename

// Replace puts the program downloaded in place of program: the new one comes
// next to it as <program>.new and takes its place by a rename over it, so on
// Linux and macOS no step leaves the folder without the program. On Windows
// a running program cannot be replaced but can be renamed: it goes to
// <program>.old, and the new one takes its place by a second rename; between
// the two the folder has no program for a moment. If the second rename
// fails, the program gets its name back. <program>.old stays only while a
// process holds it. A failure is a WriteError, and the program is left as it
// was, unless its name could not be given back: then the WriteError tells
// that too, and the program is <program>.old.
func Replace(downloaded, program string) error {
	staged := program + ".new"
	if err := stage(downloaded, staged); err != nil {
		os.Remove(staged)
		return &WriteError{Program: program, Err: err}
	}
	err := rename(staged, program)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		os.Remove(staged)
		return &WriteError{Program: program, Err: err}
	}
	// A running process holds the program: on Windows the update itself.
	if err := clearOld(program); err != nil {
		os.Remove(staged)
		return &WriteError{Program: program, Err: err}
	}
	if err := rename(program, Old(program)); err != nil {
		os.Remove(staged)
		return &WriteError{Program: program, Err: err}
	}
	if err := rename(staged, program); err != nil {
		restore := rename(Old(program), program)
		os.Remove(staged)
		return &WriteError{Program: program, Err: err, Restore: restore}
	}
	os.Remove(Old(program))
	return nil
}

// stage copies the program downloaded to the file staged, which can be run.
func stage(downloaded, staged string) error {
	in, err := os.Open(downloaded)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The mode of a file created is cut by the umask.
	return os.Chmod(staged, 0o755)
}
