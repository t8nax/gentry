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

// runHelp prints the command list, or the help of the command named in args.
// A service command has no help of its own: the list is printed for it.
func runHelp(args []string, env Env) int {
	f := newFlags("help")
	if code, done := f.parse(args, env); done {
		return code
	}
	if len(f.args) == 0 {
		io.WriteString(env.Stdout, commandList())
		return contract.ExitOK
	}
	c, ok := lookup(f.args[0])
	if !ok {
		return fail(env, unknownCommand(f.args[0]))
	}
	return commandHelp(c, env)
}

// commandHelp prints the help of c, or the command list for a service command.
func commandHelp(c command, env Env) int {
	if c.hidden() {
		io.WriteString(env.Stdout, commandList())
	} else {
		io.WriteString(env.Stdout, describe(c))
	}
	return contract.ExitOK
}

// commandList is the list of commands by section, in the order of commands().
func commandList() string {
	var cmds []command
	var sections []msg.Key
	width := 0
	for _, c := range commands() {
		if c.hidden() {
			continue
		}
		cmds = append(cmds, c)
		if !slices.Contains(sections, c.section) {
			sections = append(sections, c.section)
		}
		width = max(width, len(c.name))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n", msg.Text(msg.HelpIntro), msg.Text(msg.HelpUsage))
	for _, s := range sections {
		fmt.Fprintf(&b, "\n%s\n", msg.Text(s))
		for _, c := range cmds {
			if c.section == s {
				fmt.Fprintf(&b, "  %-*s%s\n", width+helpGap, c.name, msg.Text(c.summary))
			}
		}
	}
	fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HelpMore))
	return b.String()
}

// describe is the help of one command: what it does, the call line, every
// argument and flag.
func describe(c command) string {
	type row struct{ name, desc string }
	var args, flags []row
	usage := []string{"gentry", c.name}
	for _, a := range c.args {
		name := msg.Text(a.name)
		args = append(args, row{name, a.desc()})
		if a.optional {
			name = "[" + name + "]"
		}
		usage = append(usage, name)
	}
	for _, f := range c.flags {
		name := "--" + f.name
		if f.value != "" {
			name += " " + msg.Text(f.value)
		}
		flags = append(flags, row{name, f.desc()})
		usage = append(usage, "["+name+"]")
	}

	width := 0
	for _, r := range slices.Concat(args, flags) {
		width = max(width, len([]rune(r.name)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n  %s\n", msg.Text(c.desc), msg.Text(msg.HelpUsageTitle), strings.Join(usage, " "))
	section := func(title msg.Key, rows []row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s\n", msg.Text(title))
		for _, r := range rows {
			// fmt pads by runes, so Cyrillic names line up.
			fmt.Fprintf(&b, "  %-*s%s\n", width+helpGap, r.name, r.desc)
		}
	}
	section(msg.HelpArgs, args)
	section(msg.HelpFlags, flags)
	return b.String()
}
