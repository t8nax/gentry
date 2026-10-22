package cli

import (
	"os"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/home"
)

// tools lists the AI tools `gentry setup` supports, in the order of the
// help, of the integrations of gentry update and of the formats of the
// layout of subagents.
var tools = []string{claude.Tool, codex.Tool}

// toolTitles are the names of the tools for the operator.
var toolTitles = map[string]string{claude.Tool: "Claude Code", codex.Tool: "Codex"}

// toolLayouts are the formats of subagents of the tools.
var toolLayouts = map[string]agents.Layout{claude.Tool: claude.Agents{}, codex.Tool: codex.Agents{}}

// connectedTools returns the AI tools connected in the data root, in the
// order of tools: those whose folder of the integration is there, as setup
// makes it.
func connectedTools() []string {
	var out []string
	for _, t := range tools {
		dir, err := home.Integration(t)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			out = append(out, t)
		}
	}
	return out
}

// connected reports whether the AI tool is connected in the data root.
func connected(tool string) bool {
	for _, t := range connectedTools() {
		if t == tool {
			return true
		}
	}
	return false
}

// layoutFormats returns the formats of the layout of subagents: those of
// the AI tools connected are written, and the files Gentry laid out in the
// format of any tool stay hidden from git; without a tool connected
// nothing is laid out.
func layoutFormats() agents.Formats { return formatsOf(connectedTools()) }

// formatsOf returns the formats of the layout with the AI tools connected
// given.
func formatsOf(connected []string) agents.Formats {
	f := agents.Formats{}
	for _, t := range tools {
		f.Known = append(f.Known, toolLayouts[t])
	}
	for _, t := range connected {
		f.Write = append(f.Write, toolLayouts[t])
	}
	return f
}
