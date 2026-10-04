// Package hook answers the hook calls of the agent: the agent tool runs
// `gentry hook <event>` at certain points of a session and passes what gentry
// prints to the agent.
package hook

import "io"

// SessionStart is the event of a session start.
const SessionStart = "session-start"

// Events lists the supported hook events.
var Events = []string{SessionStart}

// MaxOutput is the limit of hook output in characters: a limit of Codex,
// kept for all agents.
const MaxOutput = 10000

// RunSessionStart writes the introduction for the agent at session start:
// project, task, stage, knowledge index, agent commands. Outside a project
// under Gentry it writes nothing. Projects come in stage 6, so for now it
// always writes nothing.
func RunSessionStart(w io.Writer) error {
	return nil
}
