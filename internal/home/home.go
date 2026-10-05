// Package home locates the data root of Gentry: the directory that holds the
// operator's local data — process, state, the binary and tool integrations.
package home

import (
	"errors"
	"os"
	"path/filepath"
)

// EnvVar overrides the data root.
const EnvVar = "GENTRY_HOME"

// ErrUnknown means neither GENTRY_HOME nor the user's home directory is known.
var ErrUnknown = errors.New("data root unknown")

// Root returns the data root: GENTRY_HOME as an absolute path if set,
// otherwise ~/.gentry. It does not create the directory.
func Root() (string, error) {
	if dir := os.Getenv(EnvVar); dir != "" {
		return filepath.Abs(dir)
	}
	user, err := os.UserHomeDir()
	if err != nil || user == "" {
		return "", ErrUnknown
	}
	return filepath.Join(user, ".gentry"), nil
}

// Integration returns the directory of the integration with a tool,
// such as ~/.gentry/integrations/claude.
func Integration(tool string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "integrations", tool), nil
}

// Process returns the directory of the operator's process: flow drafts and
// the library of subagents, such as ~/.gentry/process. The process of a
// project is in its subdirectory named by the project identifier.
func Process() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "process"), nil
}
