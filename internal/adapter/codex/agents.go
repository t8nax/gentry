package codex

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// models maps the model levels of a subagent to the models of Codex. Codex
// has no alias of the latest model of a level, so the table names the
// models and changes with a new generation of them. The level of reasoning
// is not set: the subagent runs with the one of the session.
var models = map[string]string{
	"light":    "gpt-6-luna",
	"standard": "gpt-6.1-sol",
	"strong":   "gpt-6-astra",
}

// capabilities are the phrases of the capabilities of a subagent in the
// order of flow.Capabilities, but task, which progress includes.
var capabilities = map[string]msg.Key{
	"read":     msg.CapabilityRead,
	"search":   msg.CapabilitySearch,
	"edit":     msg.CapabilityEdit,
	"run":      msg.CapabilityRun,
	"web":      msg.CapabilityWeb,
	"task":     msg.CapabilityTask,
	"progress": msg.CapabilityProgress,
}

// tools are the capabilities of a subagent that are no tool of Gentry.
var tools = []string{"read", "search", "edit", "run", "web"}

// Agents lays out subagents as Codex reads them from a project:
// .codex/agents/<id>.toml with the mark of Gentry in its first line, the
// name, the description, the model if the subagent has a level, and the
// developer instructions. Codex limits no tool of a subagent by its file, so
// its capabilities are told in words at the start of its instruction: what
// it may do and what it does not.
type Agents struct{}

var _ agents.Layout = Agents{}

const agentExt = ".toml"

func (Agents) Dir() string { return ".codex/agents" }

func (Agents) Name(id string) string { return id + agentExt }

func (Agents) ID(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, agentExt)
	return id, ok && id != ""
}

// File is the file of subagent a.
func (Agents) File(a flow.Agent) []byte {
	var b strings.Builder
	b.WriteString(agents.MarkPrefix + " " + msg.Text(msg.AgentFileMark) + "\n")
	b.WriteString("name = " + tomlBasic(a.ID) + "\n")
	b.WriteString("description = " + tomlBasic(a.Purpose) + "\n")
	if m, ok := models[a.Model]; ok {
		b.WriteString("model = " + tomlBasic(m) + "\n")
	}
	text := instruction(a)
	b.WriteString("developer_instructions = \"\"\"\n" + multiline(text) + "\"\"\"\n")
	return []byte(b.String())
}

// instruction is the developer instructions of subagent a: the lines of
// what it may do and of what it does not, an empty line and its
// instruction, which ends with a line end.
func instruction(a flow.Agent) string {
	has := func(c string) bool { return slices.Contains(a.Capabilities, c) }
	var can, cannot []string
	for _, c := range flow.Capabilities {
		if c == "task" && has("progress") {
			continue
		}
		switch {
		case has(c):
			can = append(can, msg.Text(capabilities[c]))
		case slices.Contains(tools, c):
			cannot = append(cannot, msg.Text(capabilities[c]))
		}
	}
	switch {
	case !has("task") && !has("progress"):
		cannot = append(cannot, msg.Text(msg.CapabilityNoGentry))
	case !has("progress"):
		cannot = append(cannot, msg.Text(msg.CapabilityNoProgress))
	}
	var b strings.Builder
	if len(can) > 0 {
		b.WriteString(msg.Text(msg.AgentCodexCan, strings.Join(can, ", ")) + "\n")
	}
	if len(cannot) > 0 {
		b.WriteString(msg.Text(msg.AgentCodexCannot, strings.Join(cannot, ", ")) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(strings.ReplaceAll(a.Instruction, "\r\n", "\n"), "\n") + "\n")
	return b.String()
}

// multiline writes s as the content of a multi-line basic string of TOML:
// a backslash and a quote are escaped, so that no """ ends the string, and
// so are the control characters but the line end and the tab.
func multiline(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Marked reports whether the file starts with the mark.
func (Agents) Marked(content []byte) bool {
	content = bytes.TrimPrefix(content, []byte(bom))
	return bytes.HasPrefix(content, []byte(agents.MarkPrefix))
}
