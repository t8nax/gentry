package clitest

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/t8nax/gentry/internal/msg"
)

// The tests state the output of a command as the command line prints it.
// InChannel turns such a text into the channel of the agent: a hint names the
// tool and its fields instead of the command, and the hints of the help are
// left out. The command line has no hints of commands only the agent runs.
// Gentry builds both channels from one hint; the tests recognize the hints
// in the text, so that they check the channel of the agent against the words
// of the command line.

// hintLine matches a line that ends with a command: what to do, a colon, then
// the command.
var hintLine = regexp.MustCompile(`^(.*): gentry (.+)$`)

// hintLabels match what to do in the hints of the catalog, such as
// «Посмотреть попытку» or «Установите .+ и повторите».
var hintLabels = sync.OnceValue(func() []*regexp.Regexp {
	var labels []*regexp.Regexp
	for _, k := range msg.Keys() {
		if !strings.HasPrefix(string(k), "hint.") {
			continue
		}
		labels = append(labels, pattern(msg.Text(k), `.+`))
	}
	return labels
})

// pattern matches text with any value in place of its verbs.
func pattern(text, value string) *regexp.Regexp {
	parts := regexp.MustCompile(`%[sdvq]`).Split(text, -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile(`^` + strings.Join(parts, value) + `$`)
}

// agentTexts are the texts the agent gets in words of its own.
var agentTexts = map[msg.Key]msg.Key{
	msg.HintStateNewer:  msg.HintStateNewerAgent,
	msg.ErrInputInvalid: msg.ErrToolFields,
}

// argFields are the fields of the arguments of the commands, in order.
var argFields = map[string][]string{
	"task show": {"task"}, "task close": {"task"}, "task cancel": {"task"}, "task attempts": {"task", "attempt"},
	"step add": {"steps"}, "step done": {"step"}, "step drop": {"step"}, "artifact save": {"name"},
	"note add": {"text"}, "setup": {"tool"}, "worktree add": {"path"}, "process remote": {"remote"},
	"project add": {"project"},
}

// boolFlags are the flags without a value.
var boolFlags = []string{"draft", "statement", "path", "all", "switch", "json"}

// services are the commands that are no tools.
var services = []string{"help", "hook", "events", "mcp", "version"}

// lineFor returns line in the channel of the agent, or of the command line;
// false if the channel has no such line.
func lineFor(line string, agent bool) (string, bool) {
	if agent {
		for k, a := range agentTexts {
			if m := pattern(msg.Text(k), `(.+)`).FindStringSubmatch(line); m != nil {
				args := make([]any, len(m)-1)
				for i, v := range m[1:] {
					args[i] = v
				}
				return msg.Text(a, args...), true
			}
		}
	}
	m := hintLine.FindStringSubmatch(line)
	if m == nil || !slices.ContainsFunc(hintLabels(), func(l *regexp.Regexp) bool { return l.MatchString(m[1]) }) {
		return line, true
	}
	words := words(m[2])
	if slices.Contains(words, "--help") {
		return line, !agent
	}
	cmd := words[0]
	if len(words) > 1 && !strings.HasPrefix(words[1], "-") && !strings.HasPrefix(words[1], "<") {
		if _, ok := argFields[words[0]+" "+words[1]]; ok || isGroup(words[0]) {
			cmd, words = words[0]+" "+words[1], words[2:]
		} else {
			words = words[1:]
		}
	} else {
		words = words[1:]
	}
	if !agent {
		return line, !slices.Contains(agentOnly, cmd)
	}
	if slices.Contains(services, cmd) {
		return "", false
	}
	return m[1] + ": " + tool(cmd, words), true
}

// isGroup reports whether name is a group of commands.
func isGroup(name string) bool {
	return slices.Contains([]string{"project", "worktree", "agents", "flow", "library", "process", "task", "stage", "step", "note", "artifact", "operator"}, name)
}

// words splits a command into words; a placeholder such as <как проверено>
// is one word.
func words(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case r == ' ' && depth == 0:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// tool names the tool of cmd with the fields of the arguments words:
// stage_exit (kind, text), task_show (task: SHOP-1). A placeholder names the
// field alone.
func tool(cmd string, words []string) string {
	var fields []string
	field := func(name, value string) {
		if value == "" || strings.HasPrefix(value, "<") {
			fields = append(fields, name)
		} else {
			fields = append(fields, name+": "+value)
		}
	}
	args, pos := argFields[cmd], 0
	for i := 0; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "--") {
			name, value := w[2:], ""
			if !slices.Contains(boolFlags, name) && i+1 < len(words) {
				i++
				value = words[i]
			}
			field(strings.ReplaceAll(name, "-", "_"), value)
			continue
		}
		// A placeholder names its argument: «<попытка>» of task attempts is
		// the attempt, though the task comes first.
		if w == msg.Text(msg.ArgAttempt) {
			field("attempt", w)
			continue
		}
		if pos < len(args) {
			field(args[pos], w)
			pos++
		}
	}
	name := strings.ReplaceAll(cmd, " ", "_")
	if len(fields) == 0 {
		return name
	}
	return name + " (" + strings.Join(fields, ", ") + ")"
}

// InChannel states text as the command of args prints it in its channel. A
// blank line before lines left out at the end is left out too.
func InChannel(text string, args []string) string {
	agent := byAgent(args)
	var b strings.Builder
	blanks, dropped := 0, false
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == len(lines)-1 {
			if line == "" {
				break
			}
		}
		if line == "" {
			blanks++
			continue
		}
		out, ok := lineFor(line, agent)
		if !ok {
			dropped = true
			continue
		}
		b.WriteString(strings.Repeat("\n", blanks) + out)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
		blanks, dropped = 0, false
	}
	if blanks > 0 && !dropped {
		b.WriteString(strings.Repeat("\n", blanks))
	}
	return b.String()
}
