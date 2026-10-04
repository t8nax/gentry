// Package integration describes, independently of any tool, what Gentry
// installs into the tool the agent works in. Tool adapters turn this
// description into the tool's own format, such as a Claude Code plugin.
package integration

import "github.com/t8nax/gentry/internal/hook"

// Name is the name of the integration in every tool.
const Name = "gentry"

// Description is the neutral description of the integration.
type Description struct {
	Name        string
	Description string // operator-facing, in the operator's language
	Hooks       []Hook
}

// Hook is a command the tool runs on a hook event.
type Hook struct {
	Event   string   // a neutral event from package hook, such as hook.SessionStart
	Command []string // program and arguments
}

// Gentry returns the integration description for the gentry binary at exe.
func Gentry(exe, description string) Description {
	return Description{
		Name:        Name,
		Description: description,
		Hooks: []Hook{
			{Event: hook.SessionStart, Command: []string{exe, "hook", hook.SessionStart}},
		},
	}
}
