// Package caller tells a command called by the agent from one called by the
// operator. The source of a record is the channel: the tools of gentry mcp are
// the agent's, the command line is the operator's. A command line run in a
// session of an AI tool, by the agent through its shell or by the operator
// with ! in Claude Code, counts as the agent's: the two cannot be told apart,
// and a record of the agent must never pass for the operator's.
package caller

import "os"

// Environment variables that tell a session of an AI tool.
const (
	// SessionEnv is set by the driver for the sessions it runs.
	SessionEnv = "GENTRY_SESSION"
	// ClaudeCodeEnv is set by Claude Code for the commands of its sessions.
	ClaudeCodeEnv = "CLAUDECODE"
)

// ByAgent reports whether a command of the command line runs in a session of
// the driver or of Claude Code. The tools of gentry mcp are the agent's
// whatever this says.
func ByAgent() bool {
	return os.Getenv(SessionEnv) != "" || os.Getenv(ClaudeCodeEnv) != ""
}
