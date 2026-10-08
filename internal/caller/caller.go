// Package caller tells a command called by the agent from one called by the
// operator (decision R122). The mark decides only the source of what the
// command records; it forbids nothing.
//
// The hooks of the agent tool mark a call of a shell tool of the agent before
// it runs and unmark it after: an empty file calls/<session>/<call> in the
// state directory of the data root. A command of the operator typed with ! in
// a session of Claude Code has the same environment as a command of the agent
// but passes no hook, so it finds no mark. The hooks touch neither the state
// store nor the knowledge.
package caller

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/t8nax/gentry/internal/home"
)

// Environment variables that name the session of the caller.
const (
	// SessionEnv is set by the driver for the sessions it runs (stage 9):
	// such a session is the agent's whatever it calls.
	SessionEnv = "GENTRY_SESSION"
	// ClaudeSessionEnv is the session of Claude Code a command runs in.
	ClaudeSessionEnv = "CLAUDE_CODE_SESSION_ID"
)

// namePattern is a session or call identifier that is safe as a file name.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// dir returns the directory of the marks of session.
func dir(session string) (string, error) {
	if !namePattern.MatchString(session) {
		return "", errors.New("caller: invalid session identifier")
	}
	root, err := home.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", "calls", session), nil
}

// Mark marks the call of session as a call of the agent.
func Mark(session, call string) error {
	d, err := dir(session)
	if err != nil {
		return err
	}
	if !namePattern.MatchString(call) {
		return errors.New("caller: invalid call identifier")
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, call), nil, 0o644)
}

// Unmark removes the mark of the call of session, and the directory of the
// session once it has no marks.
func Unmark(session, call string) error {
	d, err := dir(session)
	if err != nil {
		return err
	}
	if !namePattern.MatchString(call) {
		return errors.New("caller: invalid call identifier")
	}
	if err := os.Remove(filepath.Join(d, call)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Fails while another call of the session is marked: that is fine.
	os.Remove(d)
	return nil
}

// Clear removes all marks of session: a call may have been interrupted
// without its hook after the call.
func Clear(session string) error {
	d, err := dir(session)
	if err != nil {
		return err
	}
	return os.RemoveAll(d)
}

// ByAgent reports whether the agent calls the running command: a session of
// the driver, or a session of Claude Code with a marked call. Any failure to
// tell counts as the operator.
func ByAgent() bool {
	if os.Getenv(SessionEnv) != "" {
		return true
	}
	session := os.Getenv(ClaudeSessionEnv)
	if session == "" {
		return false
	}
	d, err := dir(session)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(d)
	return err == nil && len(entries) > 0
}
