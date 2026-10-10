package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// helpGap separates a name from its description in the help columns.
const helpGap = 3

// runHelp prints the command list, or the help of the command (and action)
// named in args, as --help does. A service command has no help of its own:
// the list is printed for it.
func runHelp(args []string, env Env) int {
	f := newFlags("help")
	if code, done := f.parse(args, env); done {
		return code
	}
	if len(f.args) == 0 {
		io.WriteString(env.Stdout, commandList())
		return contract.ExitOK
	}
	c, ok := topLevel(f.args[0])
	if !ok || c.agentOnly {
		return fail(env, unknownCommand(f.args[0]))
	}
	if len(f.args) == 2 {
		if c.actions == nil {
			return fail(env, extraArgs("help", f.args[1:]))
		}
		a, ok := c.action(f.args[1])
		if !ok || a.agentOnly {
			return fail(env, unknownAction(c, f.args[1]))
		}
		c = a
	}
	return commandHelp(c, env)
}

// commandHelp prints the help of c: the description of a command, the action
// list of a group, or the command list for a service command.
func commandHelp(c command, env Env) int {
	switch {
	case c.hidden():
		io.WriteString(env.Stdout, commandList())
	case c.actions != nil:
		io.WriteString(env.Stdout, groupHelp(c))
	default:
		io.WriteString(env.Stdout, describe(c))
	}
	return contract.ExitOK
}

// row is a name and its description in a help column.
type row struct{ name, desc string }

// nameWidth is the width of the widest name of rows, in runes.
func nameWidth(rows []row) int {
	width := 0
	for _, r := range rows {
		width = max(width, len([]rune(r.name)))
	}
	return width
}

// writeRows prints rows as two columns, names padded to width; a description
// of several lines keeps its indent.
func writeRows(b *strings.Builder, rows []row, width int) {
	indent := "\n" + strings.Repeat(" ", 2+width+helpGap)
	for _, r := range rows {
		// fmt pads by runes, so Cyrillic names line up.
		fmt.Fprintf(b, "  %-*s%s\n", width+helpGap, r.name, strings.ReplaceAll(r.desc, "\n", indent))
	}
}

// commandList is the list of top-level commands by section, in the order of
// commands(); the actions of a group are in the help of the group.
func commandList() string {
	var sections []msg.Key
	bySection := map[msg.Key][]row{}
	for _, c := range commands() {
		if c.hidden() || c.agentOnly {
			continue
		}
		if !slices.Contains(sections, c.section) {
			sections = append(sections, c.section)
		}
		bySection[c.section] = append(bySection[c.section], row{c.name, msg.Text(c.summary)})
	}
	// One column width for all sections, as one table.
	width := 0
	for _, rows := range bySection {
		width = max(width, nameWidth(rows))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n", msg.Text(msg.HelpIntro), msg.Text(msg.HelpUsage))
	for _, s := range sections {
		fmt.Fprintf(&b, "\n%s\n", msg.Text(s))
		writeRows(&b, bySection[s], width)
	}
	fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HelpMore))
	return b.String()
}

// groupHelp is the help of a group: what it is for and its actions.
func groupHelp(g command) string {
	var rows []row
	for _, a := range g.actions {
		if a.agentOnly {
			continue
		}
		rows = append(rows, row{strings.TrimPrefix(a.name, g.name+" "), msg.Text(a.summary)})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n  %s\n\n%s\n", msg.Text(g.desc), msg.Text(msg.HelpUsageTitle),
		msg.Text(msg.HelpGroupUsage, g.name), msg.Text(msg.HelpActions))
	writeRows(&b, rows, nameWidth(rows))
	fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HelpGroupMore, g.name))
	return b.String()
}

// describe is the help of one command: what it does, the call line, every
// argument and flag.
func describe(c command) string {
	var args, flags []row
	usage := []string{"gentry", c.name}
	for _, a := range c.args {
		name := msg.Text(a.name)
		if a.many {
			name += "..."
		}
		args = append(args, row{name, a.desc()})
		if a.optional {
			name = "[" + name + "]"
		}
		usage = append(usage, name)
	}
	for i, f := range c.flags {
		name := "--" + f.name
		if f.value != "" {
			name += " " + msg.Text(f.value)
		}
		flags = append(flags, row{name, f.help()})
		// A flag of the group of the previous one is another choice in its
		// brackets: [--a <x> | --b <y>].
		if f.group != "" && i > 0 && c.flags[i-1].group == f.group {
			last := &usage[len(usage)-1]
			*last = strings.TrimSuffix(*last, "]") + " | " + name + "]"
			continue
		}
		if !f.required {
			name = "[" + name + "]"
		}
		usage = append(usage, name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n  %s\n", msg.Text(c.desc), msg.Text(msg.HelpUsageTitle), strings.Join(usage, " "))
	// Arguments and flags share one column width.
	width := nameWidth(slices.Concat(args, flags))
	section := func(title msg.Key, rows []row) {
		if len(rows) > 0 {
			fmt.Fprintf(&b, "\n%s\n", msg.Text(title))
			writeRows(&b, rows, width)
		}
	}
	section(msg.HelpArgs, args)
	section(msg.HelpFlags, flags)
	return b.String()
}
