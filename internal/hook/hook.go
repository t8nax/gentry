// Package hook answers the hook calls of the agent: the agent tool runs
// `gentry hook <event>` at certain points of a session and passes what gentry
// prints to the agent.
package hook

import (
	"encoding/json"
	"io"

	"github.com/t8nax/gentry/internal/caller"
)

// Hook events.
const (
	SessionStart = "session-start" // a session starts, resumes, is cleared or compacted
	PreTool      = "pre-tool"      // a shell tool of the agent is about to run a command
	PostTool     = "post-tool"     // the command of a shell tool ended, with or without success
	Stop         = "stop"          // the agent ended its turn
)

// Events lists the supported hook events.
var Events = []string{SessionStart, PreTool, PostTool, Stop}

// MaxOutput is the limit of hook output in characters: a limit of Codex,
// kept for all agents.
const MaxOutput = 10000

// Input is what the agent tool passes to a hook on its standard input; the
// fields Gentry does not use are skipped.
type Input struct {
	Session string `json:"session_id"`
	Call    string `json:"tool_use_id"` // only of the tool events
	Dir     string `json:"cwd"`         // the directory the session runs in
	Tool    string `json:"-"`           // the tool that runs the hook, from its command line; empty if unknown
}

// ReadInput reads the input of a hook. An input that cannot be read is empty:
// a hook never breaks the agent session.
func ReadInput(r io.Reader) Input {
	var in Input
	if r != nil {
		json.NewDecoder(r).Decode(&in)
	}
	return in
}

// RunSessionStart removes the marks of calls left by an interrupted session
// that resumes. The introduction for the agent, which needs the state of
// tasks, is written by the caller.
func RunSessionStart(in Input) error {
	if in.Session != "" {
		caller.Clear(in.Session)
	}
	return nil
}

// RunPreTool marks the call of a shell tool as a call of the agent, so that
// gentry run by it records the agent as the source.
func RunPreTool(in Input) error {
	if in.Session == "" || in.Call == "" {
		return nil
	}
	return caller.Mark(in.Session, in.Call)
}

// RunPostTool removes the mark of the call of a shell tool.
func RunPostTool(in Input) error {
	if in.Session == "" || in.Call == "" {
		return nil
	}
	return caller.Unmark(in.Session, in.Call)
}

// RunStop removes all marks of the session at the end of a turn, in case a
// call was interrupted without its hook after the call.
func RunStop(in Input) error {
	if in.Session == "" {
		return nil
	}
	return caller.Clear(in.Session)
}
