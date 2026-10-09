// Package hook answers the hook calls of the agent: the agent tool runs
// `gentry hook <event>` at certain points of a session and passes what gentry
// prints to the agent.
package hook

import (
	"encoding/json"
	"io"
)

// Hook events.
const (
	SessionStart = "session-start" // a session starts, resumes, is cleared or compacted
)

// Events lists the hook events the integration installs.
var Events = []string{SessionStart}

// Retired are hook events a plugin of an earlier Gentry still runs until
// `gentry setup` replaces it. They do nothing, and never fail: a failing hook
// before a tool call would stop the shell of the agent.
var Retired = []string{"pre-tool", "post-tool", "stop"}

// MaxOutput is the limit of hook output in characters: a limit of Codex,
// kept for all agents.
const MaxOutput = 10000

// Input is what the agent tool passes to a hook on its standard input; the
// fields Gentry does not use are skipped.
type Input struct {
	Session string `json:"session_id"`
	Dir     string `json:"cwd"` // the directory the session runs in
	Tool    string `json:"-"`   // the tool that runs the hook, from its command line; empty if unknown
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
