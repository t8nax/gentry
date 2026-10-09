package claude

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// tools maps the capabilities of a subagent to the tools of Claude Code.
// Claude Code gives a subagent the tools that exist on its system and skips
// the rest, so run names the shells of every system. task and progress are
// tools of the server of Gentry: progress leads the work of a stage, the
// main session closes it.
var tools = map[string][]string{
	"read":     {"Read"},
	"search":   {"Grep", "Glob"},
	"edit":     {"Edit", "Write", "NotebookEdit"},
	"run":      {"Bash", "PowerShell"},
	"web":      {"WebFetch", "WebSearch"},
	"task":     gentryTools(taskTools...),
	"progress": gentryTools(append(taskTools, "step_add", "step_done", "step_drop", "note_add", "artifact_save")...),
}

// taskTools are the tools that show the task.
var taskTools = []string{"stage_show", "task_show", "note_list"}

// gentryTools names the tools of the server of Gentry the way of Claude Code.
func gentryTools(names ...string) []string {
	full := make([]string, len(names))
	for i, n := range names {
		full[i] = PermissionRule + "__" + n
	}
	return full
}

// Agents lays out subagents as Claude Code reads them from a project:
// .claude/agents/<id>.md, a YAML front matter with the mark of Gentry, the
// name, the description and the tools, then the instruction.
type Agents struct{}

var _ agents.Layout = Agents{}

const agentExt = ".md"

func (Agents) Dir() string { return ".claude/agents" }

func (Agents) Name(id string) string { return id + agentExt }

func (Agents) ID(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, agentExt)
	return id, ok && id != ""
}

// File is the file of subagent a. An empty list of tools is written as []:
// with an empty value of the field Claude Code 2.1 does not finish starting
// the subagent. A Go quoted string is a valid YAML double-quoted scalar for
// the purpose: it is one line of text.
func (Agents) File(a flow.Agent) []byte {
	var names []string
	for _, c := range a.Capabilities {
		for _, t := range tools[c] {
			if !slices.Contains(names, t) {
				names = append(names, t)
			}
		}
	}
	list := "[]"
	if len(names) > 0 {
		list = strings.Join(names, ", ")
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(agents.MarkPrefix + " " + msg.Text(msg.AgentFileMark) + "\n")
	b.WriteString("name: " + a.ID + "\n")
	b.WriteString("description: " + strconv.Quote(a.Purpose) + "\n")
	b.WriteString("tools: " + list + "\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(a.Instruction, "\n") + "\n")
	return []byte(b.String())
}

// Marked reports whether the front matter of the file starts with the mark.
func (Agents) Marked(content []byte) bool {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(content, []byte("---\n"))
	return ok && bytes.HasPrefix(rest, []byte(agents.MarkPrefix))
}
