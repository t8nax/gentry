package cli

import (
	"bytes"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/t8nax/gentry/internal/msg"
)

// Hints name commands: «Посмотреть этап: gentry stage show». The catalog has
// one text per hint, for the operator; the channel of the agent names the
// tool and its fields instead: «Посмотреть этап: stage_show», and the command
// line has no hints of commands only the agent runs. A hint is a line that
// starts as a line of a hint of the catalog: the words of the operator and the
// agent, such as a note or a statement, pass as they are.

// hintLine matches a line that ends with a command: what to do, a colon, then
// the command.
var hintLine = regexp.MustCompile(`^(.*: )gentry (.+)$`)

// verbs are the places of values in a text of the catalog.
var verbs = regexp.MustCompile(`%[sdvq]`)

// hintLabels match what to do in the lines of the catalog that end with a
// command, such as «Посмотреть попытку: » or «Установите .+ и повторите: ».
var hintLabels = sync.OnceValue(func() []*regexp.Regexp {
	seen := map[string]bool{}
	var labels []*regexp.Regexp
	for _, k := range msg.Keys() {
		for _, line := range strings.Split(msg.Text(k), "\n") {
			m := hintLine.FindStringSubmatch(line)
			if m == nil || seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			parts := verbs.Split(m[1], -1)
			for i, p := range parts {
				parts[i] = regexp.QuoteMeta(p)
			}
			labels = append(labels, regexp.MustCompile(`^`+strings.Join(parts, `.+`)+`$`))
		}
	}
	return labels
})

// isHintLabel reports whether label is what to do in a hint of the catalog.
func isHintLabel(label string) bool {
	for _, l := range hintLabels() {
		if l.MatchString(label) {
			return true
		}
	}
	return false
}

// hintAction is what to do with a line of output in a channel.
type hintAction int

const (
	keepLine hintAction = iota
	dropLine
	replaceLine
)

// agentTexts are texts without a command that the agent gets in words of its
// own: a server of the agent runs on after Gentry is updated, and only a new
// session starts the new one; the fields of a tool are no --input to the
// agent. The values of a text pass to the text of the agent.
var agentTexts = map[msg.Key]msg.Key{
	msg.HintStateNewer:  msg.HintStateNewerAgent,
	msg.ErrInputInvalid: msg.ErrToolFields,
}

// agentPatterns match the texts of agentTexts, their values as groups.
var agentPatterns = sync.OnceValue(func() map[msg.Key]*regexp.Regexp {
	patterns := map[msg.Key]*regexp.Regexp{}
	for k := range agentTexts {
		parts := verbs.Split(msg.Text(k), -1)
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		patterns[k] = regexp.MustCompile(`^` + strings.Join(parts, `(.+)`) + `$`)
	}
	return patterns
})

// agentText returns the text of the agent for line, if line is a text of
// agentTexts. The values pass as strings: the texts of agentTexts have only
// %s.
func agentText(line string) (string, bool) {
	for k, a := range agentTexts {
		m := agentPatterns()[k].FindStringSubmatch(line)
		if m == nil {
			continue
		}
		args := make([]any, len(m)-1)
		for i, v := range m[1:] {
			args[i] = v
		}
		return msg.Text(a, args...), true
	}
	return "", false
}

// hintFor tells what to do with line in the channel of the agent, or of the
// command line if agent is false, and the line to put instead.
func hintFor(line string, agent bool) (hintAction, string) {
	if agent {
		if text, ok := agentText(line); ok {
			return replaceLine, text
		}
	}
	m := hintLine.FindStringSubmatch(line)
	if m == nil || !isHintLabel(m[1]) {
		return keepLine, ""
	}
	words := commandWords(m[2])
	// The help of the command line has no tool: the agent has the
	// descriptions of the tools.
	if agent && containsHelp(words) {
		return dropLine, ""
	}
	var c command
	var ok bool
	if len(words) > 1 {
		c, ok = lookup(words[0] + " " + words[1])
		if ok {
			words = words[2:]
		}
	}
	if !ok {
		if c, ok = topLevel(words[0]); ok {
			words = words[1:]
		}
	}
	if !ok {
		return keepLine, ""
	}
	if !agent {
		if c.agentOnly {
			return dropLine, ""
		}
		return keepLine, ""
	}
	// The help of a command, a group or a service command has no tool: the
	// agent has the description of the tool.
	if c.actions != nil || c.hidden() || isService(c) || containsHelp(words) {
		return dropLine, ""
	}
	return replaceLine, m[1] + toolHint(c, words)
}

func isService(c command) bool {
	for _, s := range serviceCommands {
		if c.name == s {
			return true
		}
	}
	return false
}

func containsHelp(words []string) bool {
	for _, w := range words {
		if w == "--help" || w == "-h" {
			return true
		}
	}
	return false
}

// commandWords splits the arguments of a command in a hint into words; a
// placeholder such as <как проверено> is one word.
func commandWords(s string) []string {
	var words []string
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
				words = append(words, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

// toolHint names the tool of c with the fields of the arguments words:
// stage_exit (kind, text) for --kind <вид> --text <текст>, task_show (task:
// SHOP-1) for SHOP-1. A placeholder names the field alone.
func toolHint(c command, words []string) string {
	var fields []string
	field := func(name, value string) {
		if value == "" || strings.HasPrefix(value, "<") {
			fields = append(fields, name)
		} else {
			fields = append(fields, name+": "+value)
		}
	}
	pos := 0
	for i := 0; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "--") {
			name, value, hasValue := strings.Cut(w[2:], "=")
			takes := false
			for _, f := range c.flags {
				if f.name == name {
					takes = f.value != ""
				}
			}
			if takes && !hasValue && i+1 < len(words) && !strings.HasPrefix(words[i+1], "--") {
				i++
				value = words[i]
			}
			field(fieldName(name), value)
			continue
		}
		// A placeholder names its argument: «<попытка>» of task attempts is
		// the attempt, though the task comes first.
		if i := slices.IndexFunc(c.args, func(a argSpec) bool { return msg.Text(a.name) == w }); i >= 0 {
			field(c.args[i].field, w)
			continue
		}
		if pos < len(c.args) {
			a := c.args[pos]
			if !a.many {
				pos++
			}
			if a.field != "" {
				field(a.field, w)
			}
		}
	}
	name := toolName(c.name)
	if len(fields) == 0 {
		return name
	}
	return name + " (" + strings.Join(fields, ", ") + ")"
}

// hintFilter applies the hints of a channel to the lines of a text. A line
// is passed on whole; a blank line waits for the next line it separates, so
// that a hint block dropped at the end leaves no blank line behind. A blank
// line at the end with nothing dropped after it stays.
type hintFilter struct {
	out     io.Writer
	agent   bool
	line    []byte // the start of a line not yet ended
	blanks  int    // blank lines waiting for a line
	dropped bool   // a line after the waiting blank lines was dropped
}

func (f *hintFilter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			f.line = append(f.line, p...)
			break
		}
		f.line = append(f.line, p[:i]...)
		p = p[i+1:]
		if err := f.emit(string(f.line)); err != nil {
			return 0, err
		}
		f.line = f.line[:0]
	}
	return n, nil
}

func (f *hintFilter) emit(line string) error {
	if line == "" {
		f.blanks++
		return nil
	}
	action, other := hintFor(line, f.agent)
	switch action {
	case dropLine:
		f.dropped = true
		return nil
	case replaceLine:
		line = other
	}
	b := strings.Repeat("\n", f.blanks) + line + "\n"
	f.blanks, f.dropped = 0, false
	_, err := io.WriteString(f.out, b)
	return err
}

// flush writes a line left without its end, and blank lines at the end
// unless a line after them was dropped.
func (f *hintFilter) flush() error {
	if len(f.line) == 0 {
		if f.blanks > 0 && !f.dropped {
			_, err := io.WriteString(f.out, strings.Repeat("\n", f.blanks))
			f.blanks = 0
			return err
		}
		return nil
	}
	line := string(f.line)
	f.line = nil
	action, other := hintFor(line, f.agent)
	switch action {
	case dropLine:
		return nil
	case replaceLine:
		line = other
	}
	_, err := io.WriteString(f.out, strings.Repeat("\n", f.blanks)+line)
	f.blanks = 0
	return err
}

// Hints applies the hints of a channel to text: of the agent, or of the
// command line if agent is false. Tests use it to state what a command
// prints in its channel.
func Hints(text string, agent bool) string {
	var b strings.Builder
	f := &hintFilter{out: &b, agent: agent}
	f.Write([]byte(text))
	f.flush()
	return b.String()
}
