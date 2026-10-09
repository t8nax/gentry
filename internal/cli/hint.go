package cli

import (
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/msg"
)

// A hint tells what to do next: «Посмотреть этап: gentry stage show». It is
// an element of the output of its own: what to do is a text of the catalog,
// the command and its values are data. The channel decides the form: the
// command line names the command, the channel of the agent names the tool
// and its fields: «Посмотреть этап: stage_show». So a change of the words in
// the catalog changes the hint in both channels.

// hint is a hint of an output.
type hint struct {
	label msg.Key // what to do; the whole hint if there is no command
	args  []any   // values of label
	cmd   string  // the command, such as "stage exit"; empty for a text alone
	help  bool    // the hint names the help: gentry [cmd] --help
	// params are the arguments and flags of the command in the order of the
	// hint.
	params []param
	// agent is the text the agent gets instead, with the values of args, for
	// a text without a command that has other words for the agent.
	agent msg.Key
}

// param is an argument or a flag of the command of a hint, named as the field
// of its tool: "task", "allow_return". Without a value the hint shows the
// placeholder of the command spec; a boolean flag has no value.
type param struct {
	field string
	flag  bool
	value string
}

// hintSpecs are the hints that name a command, by what to do: the command
// and its arguments and flags. Values are set where the hint is given.
var hintSpecs = map[msg.Key]hint{
	msg.HintPluginElsewhere:       {cmd: "setup", params: []param{arg("tool"), flag("switch")}},
	msg.HintToolNotFound:          {cmd: "setup", params: []param{arg("tool")}},
	msg.HintIntroPluginStale:      {cmd: "setup", params: []param{arg("tool")}},
	msg.HintProjectUnpooled:       {cmd: "worktree add", params: []param{arg("path")}},
	msg.HintProjectClone:          {cmd: "worktree add", params: []param{flag("project")}},
	msg.HintProjectNotFound:       {cmd: "project list"},
	msg.HintProjectIDMissing:      {cmd: "project add", params: []param{arg("project"), flag("knowledge")}},
	msg.HintProjectAdd:            {cmd: "project add"},
	msg.HintFlowShowDraft:         {cmd: "flow show", params: []param{flag("draft")}},
	msg.HintFlowStage:             {cmd: "flow show", params: []param{flag("stage")}},
	msg.HintFlowApply:             {cmd: "flow apply"},
	msg.HintFlowObjects:           {cmd: "flow show"},
	msg.HintDraftObjects:          {cmd: "flow show", params: []param{flag("draft")}},
	msg.HintLibraryApply:          {cmd: "library apply"},
	msg.HintProcessStatus:         {cmd: "process status"},
	msg.HintProcessRemote:         {cmd: "process remote", params: []param{arg("remote")}},
	msg.HintProcessSync:           {cmd: "process sync"},
	msg.HintFlowDiffProject:       {cmd: "flow diff", params: []param{flag("project")}},
	msg.HintLibraryDiff:           {cmd: "library diff"},
	msg.HintTaskShow:              {cmd: "task show"},
	msg.HintWorktreeAdd:           {cmd: "worktree add"},
	msg.HintFlowScenarios:         {cmd: "flow show"},
	msg.HintTaskListAll:           {cmd: "task list", params: []param{flag("all")}},
	msg.HintTaskList:              {cmd: "task list"},
	msg.HintStageExit:             {cmd: "stage exit", params: []param{flag("kind"), flag("text")}},
	msg.HintStageShow:             {cmd: "stage show"},
	msg.HintStatement:             {cmd: "task show", params: []param{flag("statement")}},
	msg.HintNotes:                 {cmd: "note list"},
	msg.HintStageTransitions:      {cmd: "stage show"},
	msg.HintOtherTransitions:      {cmd: "stage show"},
	msg.HintStepAdd:               {cmd: "step add", params: []param{arg("steps")}},
	msg.HintStepDone:              {cmd: "step done", params: []param{arg("step")}},
	msg.HintStepDrop:              {cmd: "step drop", params: []param{arg("step"), flag("reason")}},
	msg.HintSteps:                 {cmd: "task show"},
	msg.HintArtifactSave:          {cmd: "artifact save", params: []param{arg("name"), flag("file")}},
	msg.HintArtifactLink:          {cmd: "artifact save", params: []param{arg("name"), flag("url")}},
	msg.HintStatementDecisions:    {cmd: "task show", params: []param{flag("statement")}},
	msg.HintAllowReturn:           {cmd: "operator record", params: []param{flag("answer"), flag("allow_return")}},
	msg.HintTaskClose:             {cmd: "task close"},
	msg.HintTaskAgain:             {cmd: "task take", params: []param{flag("task"), flag("scenario")}},
	msg.HintAttempts:              {cmd: "task attempts"},
	msg.HintAttemptsList:          {cmd: "task attempts"},
	msg.HintAttempt:               {cmd: "task attempts", params: []param{arg("attempt")}},
	msg.HintWorktreeListProject:   {cmd: "worktree list", params: []param{flag("project")}},
	msg.HintDiffLibraryApply:      {cmd: "library apply"},
	msg.HintAgentsConflictWarning: {cmd: "agents sync"},
	msg.HintAgentsSyncFailed:      {cmd: "agents sync"},
}

// agentHints are the texts without a command that the agent gets in words of
// its own: a server of the agent runs on after Gentry is updated, and only a
// new session starts the new one.
var agentHints = map[msg.Key]msg.Key{
	msg.HintStateNewer: msg.HintStateNewerAgent,
}

func arg(field string) param  { return param{field: field} }
func flag(field string) param { return param{field: field, flag: true} }

// hintOf returns the hint k with the values of its text args. A hint of
// hintSpecs names its command; any other is a text alone.
func hintOf(k msg.Key, args ...any) hint {
	h, ok := hintSpecs[k]
	if !ok {
		return hint{label: k, args: args, agent: agentHints[k]}
	}
	h.label, h.args = k, args
	h.params = slices.Clone(h.params)
	return h
}

// helpHint returns the hint k that names the help of the command cmd, or of
// gentry if cmd is empty.
func helpHint(k msg.Key, cmd string) hint {
	return hint{label: k, cmd: cmd, help: true}
}

// set gives the param field of h the value v.
func (h hint) set(field, v string) hint {
	for i, p := range h.params {
		if p.field == field {
			h.params[i].value = v
			return h
		}
	}
	panic("cli: hint " + string(h.label) + " has no field " + field)
}

// forTask names the task key in the command of h, unless it names a task
// already: by the argument of the command if it takes one, as task show
// does, or else by --task. A hint without a command, a hint of the help and
// a command that takes no task are left as they are.
func (h hint) forTask(key string) hint {
	if h.cmd == "" || h.help || slices.ContainsFunc(h.params, func(p param) bool { return p.field == "task" }) {
		return h
	}
	c, ok := lookup(h.cmd)
	if !ok {
		return h
	}
	if slices.ContainsFunc(c.args, func(a argSpec) bool { return a.field == "task" }) {
		h.params = append([]param{{field: "task", value: key}}, h.params...)
		return h
	}
	if slices.ContainsFunc(c.flags, func(f flagSpec) bool { return f.name == "task" }) {
		h.params = append(h.params, param{field: "task", flag: true, value: key})
	}
	return h
}

// channel is where an output goes: the command line, the agent, or JSON of
// a refusal, which has the hints as the operator types them.
type channel int

const (
	cliChannel channel = iota
	agentChannel
	jsonChannel
)

// channelOf returns the channel of the text output of env.
func channelOf(env Env) channel {
	if env.agent {
		return agentChannel
	}
	return cliChannel
}

// render returns the hint in channel ch; false if the channel has no such
// hint: the command line has no command only the agent runs, and the agent
// has no help, which the descriptions of the tools take the place of. JSON
// has every hint as the operator types it.
func (h hint) render(ch channel) (string, bool) {
	if h.cmd == "" && !h.help {
		if ch == agentChannel && h.agent != "" {
			return msg.Text(h.agent, h.args...), true
		}
		return msg.Text(h.label, h.args...), true
	}
	label := msg.Text(h.label, h.args...)
	if h.help {
		if ch == agentChannel {
			return "", false
		}
		cmd := "gentry --help"
		if h.cmd != "" {
			cmd = "gentry " + h.cmd + " --help"
		}
		return msg.Text(msg.HintLine, label, cmd), true
	}
	c, ok := lookup(h.cmd)
	if !ok {
		panic("cli: hint " + string(h.label) + " names no command " + h.cmd)
	}
	switch {
	case ch == agentChannel && (c.hidden() || isService(c)):
		return "", false
	case ch == agentChannel:
		return msg.Text(msg.HintLine, label, h.tool()), true
	case ch == cliChannel && c.agentOnly:
		return "", false
	}
	return msg.Text(msg.HintLine, label, h.commandLine(c)), true
}

// commandLine is the command of h as the operator types it: gentry stage
// exit --kind <вид> --text <текст>.
func (h hint) commandLine(c command) string {
	words := []string{"gentry", h.cmd}
	for _, p := range h.params {
		if p.flag {
			f := c.flagOf(p.field)
			words = append(words, "--"+f.name)
			if f.value == "" {
				continue
			}
			words = append(words, valueOr(p.value, f.value))
			continue
		}
		words = append(words, valueOr(p.value, c.argOf(p.field).name))
	}
	return strings.Join(words, " ")
}

// valueOr returns v, or the placeholder k if v is empty.
func valueOr(v string, k msg.Key) string {
	if v == "" {
		return msg.Text(k)
	}
	return v
}

// tool is the command of h as the tool of the agent with its fields:
// stage_exit (kind, text), task_show (task: SHOP-1, statement). A field
// without a value is named alone.
func (h hint) tool() string {
	var fields []string
	for _, p := range h.params {
		if p.value == "" {
			fields = append(fields, p.field)
		} else {
			fields = append(fields, p.field+": "+p.value)
		}
	}
	name := toolName(h.cmd)
	if len(fields) == 0 {
		return name
	}
	return name + " (" + strings.Join(fields, ", ") + ")"
}

// flagOf returns the flag of c that is the field of its tool named field.
func (c command) flagOf(field string) flagSpec {
	for _, f := range c.flags {
		if fieldName(f.name) == field {
			return f
		}
	}
	panic("cli: command " + c.name + " has no flag of field " + field)
}

// argOf returns the argument of c that is the field of its tool named field.
func (c command) argOf(field string) argSpec {
	for _, a := range c.args {
		if a.field == field {
			return a
		}
	}
	panic("cli: command " + c.name + " has no argument of field " + field)
}

// renderHints returns the lines of hints in a channel, without the hints the
// channel has not.
func renderHints(hints []hint, ch channel) []string {
	var lines []string
	for _, h := range hints {
		if line, ok := h.render(ch); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

// HintText returns the hint k with the values of its text args as the
// operator types it, a command only the agent runs too. A hint of the help
// takes the command as its arg. Tests use it to state the hints of the
// output, which they turn into the channel of the command.
func HintText(k msg.Key, args ...any) string {
	var h hint
	switch k {
	case msg.HintCommandHelp, msg.HintActions:
		h = helpHint(k, args[0].(string))
	case msg.HintUnknownCommand:
		h = helpHint(k, "")
	default:
		h = hintOf(k, args...)
	}
	line, _ := h.render(jsonChannel)
	return line
}

// HintFor is HintText with the value of the field of its command: the task
// is named as the hint names it outside the worktree of the task.
func HintFor(k msg.Key, field, value string, args ...any) string {
	h := hintOf(k, args...)
	if field == "task" && !slices.ContainsFunc(h.params, func(p param) bool { return p.field == "task" }) {
		h = h.forTask(value)
	} else {
		h = h.set(field, value)
	}
	line, _ := h.render(jsonChannel)
	return line
}
