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
	// Every command is listed under its section heading.
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
	for _, c := range commands() {
		if c.hidden() {
			continue
		}
		if c.desc == "" || c.summary == "" {
			t.Errorf("%s: no description or summary", c.name)
			continue
		}
		want := describe(c)
		for _, args := range [][]string{{"help", c.name}, {c.name, "--help"}, {c.name, "-h"}} {
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
			if !strings.Contains(want, "  "+msg.Text(a.name)+" ") || !strings.Contains(want, a.desc()) {
				t.Errorf("%s: argument %s is not described:\n%s", c.name, msg.Text(a.name), want)
			}
		}
		for _, f := range c.flags {
			if f.desc == nil || f.desc() == "" {
				t.Errorf("%s: flag --%s without a description", c.name, f.name)
				continue
			}
			if !strings.Contains(want, "  --"+f.name+" ") || !strings.Contains(want, f.desc()) {
				t.Errorf("%s: flag --%s is not described:\n%s", c.name, f.name, want)
			}
		}
	}
}

// TestHelpMatchesFlags checks that every flag a spec declares is defined by
// the command. The opposite holds by construction: defining an undeclared flag
// panics, which running each command with --help would reveal here.
func TestHelpMatchesFlags(t *testing.T) {
	var got *flags
	onHelp = func(f *flags) { got = f }
	t.Cleanup(func() { onHelp = func(*flags) {} })
	for _, c := range commands() {
		got = nil
		run(c.name, "--help")
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
		"  " + msg.Text(msg.ArgTool) + "   " + msg.Text(msg.ArgToolDesc, "claude") + "\n\n" +
		msg.Text(msg.HelpFlags) + "\n" +
		"  --json" + strings.Repeat(" ", len([]rune(msg.Text(msg.ArgTool)))-len("--json")+3) + msg.Text(msg.FlagJSON) + "\n"
	if got := describe(c); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestServiceCommandHelp(t *testing.T) {
	_, list, _ := run("help")
	for _, args := range [][]string{{"help", "events"}, {"events", "--help"}, {"help", "hook"}, {"hook", "-h"}} {
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
		{[]string{"help", "foo"}, msg.Text(msg.ErrUnknownCommand, "foo") + " " + msg.Text(msg.HintUnknownCommand)},
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
