package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

func TestCommandList(t *testing.T) {
	_, list, _ := run("help")
	if !strings.HasPrefix(list, msg.Text(msg.HelpIntro)+"\n\n"+msg.Text(msg.HelpUsage)+"\n") {
		t.Errorf("list does not start with the intro and usage:\n%s", list)
	}
	if !strings.HasSuffix(list, "\n"+msg.Text(msg.HelpMore)+"\n") {
		t.Errorf("list does not end with the hint about command help:\n%s", list)
	}
	// Every top-level command is listed under its section heading; actions
	// are not, they are in the help of their group.
	for _, a := range allCommands() {
		if strings.Contains(a.name, " ") && strings.Contains(list, a.name) {
			t.Errorf("action %s is in the command list:\n%s", a.name, list)
		}
	}
	for _, c := range commands() {
		if c.hidden() {
			if strings.Contains(list, "  "+c.name+" ") {
				t.Errorf("service command %s is listed:\n%s", c.name, list)
			}
			continue
		}
		section := strings.Index(list, "\n"+msg.Text(c.section)+"\n")
		line := strings.Index(list, "\n  "+c.name+" ")
		if section < 0 || line < section {
			t.Errorf("%s is not listed under %q:\n%s", c.name, msg.Text(c.section), list)
		}
		if !strings.Contains(list, " "+msg.Text(c.summary)+"\n") {
			t.Errorf("%s: summary missing:\n%s", c.name, list)
		}
	}
}

// TestCommandHelp checks that every operator command has a help naming each
// argument and flag it declares, and that all ways to ask for it agree.
func TestCommandHelp(t *testing.T) {
	for _, c := range allCommands() {
		if c.hidden() {
			continue
		}
		if c.desc == "" || c.summary == "" {
			t.Errorf("%s: no description or summary", c.name)
			continue
		}
		want := describe(c)
		name := strings.Fields(c.name)
		for _, args := range [][]string{append([]string{"help"}, name...), append(name, "--help"), append(name, "-h")} {
			code, stdout, stderr := run(args...)
			if code != contract.ExitOK || stderr != "" || stdout != want {
				t.Errorf("%v: exit code %d, stderr %q, output:\n%s\nwant:\n%s", args, code, stderr, stdout, want)
			}
		}
		if !strings.HasPrefix(want, msg.Text(c.desc)+"\n") {
			t.Errorf("%s: help does not start with the description:\n%s", c.name, want)
		}
		for _, a := range c.args {
			if a.name == "" || a.desc == nil || a.desc() == "" {
				t.Errorf("%s: argument without a name or description", c.name)
				continue
			}
			if !strings.Contains(want, "  "+msg.Text(a.name)+" ") || !containsLines(want, a.desc()) {
				t.Errorf("%s: argument %s is not described:\n%s", c.name, msg.Text(a.name), want)
			}
		}
		for _, f := range c.flags {
			if f.desc == nil || f.desc() == "" {
				t.Errorf("%s: flag --%s without a description", c.name, f.name)
				continue
			}
			if !strings.Contains(want, "  --"+f.name+" ") || !containsLines(want, f.desc()) {
				t.Errorf("%s: flag --%s is not described:\n%s", c.name, f.name, want)
			}
		}
	}
}

// containsLines reports whether every line of desc is in help: a description
// of several lines is printed with an indent.
func containsLines(help, desc string) bool {
	for _, line := range strings.Split(desc, "\n") {
		if !strings.Contains(help, line) {
			return false
		}
	}
	return true
}

// TestHelpMatchesFlags checks that every flag a spec declares is defined by
// the command. The opposite holds by construction: defining an undeclared flag
// panics, which running each command with --help would reveal here.
func TestHelpMatchesFlags(t *testing.T) {
	var got *flags
	onHelp = func(f *flags) { got = f }
	t.Cleanup(func() { onHelp = func(*flags) {} })
	for _, c := range allCommands() {
		got = nil
		run(append(strings.Fields(c.name), "--help")...)
		if got == nil {
			t.Errorf("%s --help did not reach the flag parser", c.name)
			continue
		}
		for _, s := range c.flags {
			_, b := got.bools[s.name]
			_, v := got.strings[s.name]
			if !b && !v {
				t.Errorf("%s declares --%s but does not define it", c.name, s.name)
			}
		}
	}
}

func TestDescribeLayout(t *testing.T) {
	c, _ := lookup("setup")
	want := msg.Text(msg.CmdSetupDesc) + "\n\n" +
		msg.Text(msg.HelpUsageTitle) + "\n" +
		"  gentry setup " + msg.Text(msg.ArgTool) + " [--json]\n\n" +
		msg.Text(msg.HelpArgs) + "\n" +
		// The second line of the description keeps the column.
		"  " + msg.Text(msg.ArgTool) + "   " + strings.ReplaceAll(msg.Text(msg.ArgToolDesc, "claude"), "\n", "\n"+strings.Repeat(" ", 2+len([]rune(msg.Text(msg.ArgTool)))+3)) + "\n\n" +
		msg.Text(msg.HelpFlags) + "\n" +
		"  --json" + strings.Repeat(" ", len([]rune(msg.Text(msg.ArgTool)))-len("--json")+3) + msg.Text(msg.FlagJSON) + "\n"
	if got := describe(c); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDescribeProjectAdd(t *testing.T) {
	c, _ := lookup("project add")
	got := describe(c)
	usage := "  gentry project add [" + msg.Text(msg.ArgProjectID) + "] --knowledge " + msg.Text(msg.ArgPath) +
		" [--prefix " + msg.Text(msg.ArgPrefix) + "] [--json]\n"
	if !strings.Contains(got, usage) {
		t.Errorf("usage line %q missing:\n%s", usage, got)
	}
	// The second line of a description keeps the column.
	first, second, _ := strings.Cut(msg.Text(msg.FlagPrefixDesc), "\n")
	at := strings.Index(got, first)
	column := len([]rune(got[strings.LastIndex(got[:at], "\n")+1 : at]))
	if !strings.Contains(got, "\n"+strings.Repeat(" ", column)+second+"\n") {
		t.Errorf("continuation line not indented to column %d:\n%s", column, got)
	}
}

func TestGroupHelp(t *testing.T) {
	g, _ := topLevel("project")
	want := groupHelp(g)
	for _, args := range [][]string{{"help", "project"}, {"project", "--help"}, {"project", "-h"}} {
		code, stdout, stderr := run(args...)
		if code != contract.ExitOK || stderr != "" || stdout != want {
			t.Errorf("%v: exit code %d, stderr %q, output:\n%s", args, code, stderr, stdout)
		}
	}
	for _, a := range g.actionNames() {
		if !strings.Contains(want, "\n  "+a+" ") {
			t.Errorf("action %s is not listed:\n%s", a, want)
		}
	}

	tests := []struct {
		args   []string
		stderr string
	}{
		{[]string{"project"}, msg.Text(msg.ErrActionMissing, "project") + "\n" + "Посмотреть перечень действий: gentry project --help"},
		{[]string{"project", "--prefix", "X"}, msg.Text(msg.ErrActionMissing, "project") + "\n" + "Посмотреть перечень действий: gentry project --help"},
		{[]string{"project", "foo"}, msg.Text(msg.ErrActionUnknown, "foo", "project") + "\n" + "Посмотреть перечень действий: gentry project --help"},
		{[]string{"help", "project", "foo"}, msg.Text(msg.ErrActionUnknown, "foo", "project") + "\n" + "Посмотреть перечень действий: gentry project --help"},
		{[]string{"project", "list", "extra"}, msg.Text(msg.ErrUnexpectedArgs, "project list")},
		{[]string{"project", "list", "--foo"}, msg.Text(msg.ErrUnknownFlag, "project list", "--foo")},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != contract.ExitUsage || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q, want %q", tt.args, code, stdout, stderr, tt.stderr)
		}
	}
	// An unknown action fails in JSON when asked, as an unknown command.
	code, stdout, _ := run("project", "foo", "--json")
	if code != contract.ExitUsage || !strings.Contains(stdout, `"code":"`+contract.CodeInvalidArgument+`"`) {
		t.Errorf("project foo --json: exit code %d, output %q", code, stdout)
	}
}

func TestServiceCommandHelp(t *testing.T) {
	_, list, _ := run("help")
	for _, args := range [][]string{{"help", "events"}, {"events", "--help"}, {"help", "hook"}, {"hook", "-h"}, {"help", "help"}, {"help", "--help"}} {
		code, stdout, stderr := run(args...)
		if code != contract.ExitOK || stderr != "" || stdout != list {
			t.Errorf("%v: exit code %d, stderr %q; want the command list, got:\n%s", args, code, stderr, stdout)
		}
	}
}

func TestHelpDoesNotRunCommand(t *testing.T) {
	code, stdout, _ := run("setup", "claude", "--help")
	c, _ := lookup("setup")
	if code != contract.ExitOK || stdout != describe(c) {
		t.Errorf("setup claude --help: exit code %d, output:\n%s", code, stdout)
	}
}

func TestHelpErrors(t *testing.T) {
	tests := []struct {
		args   []string
		stderr string
	}{
		{[]string{"help", "foo"}, msg.Text(msg.ErrUnknownCommand, "foo") + "\n" + msg.Text(msg.HintUnknownCommand)},
		{[]string{"help", "setup", "claude"}, msg.Text(msg.ErrExtraArgs, "help", "claude")},
		{[]string{"help", "--foo"}, msg.Text(msg.ErrUnknownFlag, "help", "--foo")},
		// help has no --json, so it refuses the flag in text, not in JSON.
		{[]string{"help", "--json"}, msg.Text(msg.ErrUnknownFlag, "help", "--json")},
		{[]string{"help", "foo", "--json"}, msg.Text(msg.ErrUnknownFlag, "help", "--json")},
		{[]string{"setup", "claude", "extra"}, msg.Text(msg.ErrExtraArgs, "setup", "extra")},
		{[]string{"version", "extra"}, msg.Text(msg.ErrUnexpectedArgs, "version")},
		{[]string{"events", "--", "extra"}, msg.Text(msg.ErrUnexpectedArgs, "events")},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != contract.ExitUsage || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q, want %q", tt.args, code, stdout, stderr, tt.stderr)
		}
	}

}
