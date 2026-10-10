package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// The tests of the output state the words of Gentry: each text of a command
// and each refusal is a literal in the test of the function that prints it,
// in the channel of the command line and in the channel of the agent. They
// call the function with a structure of the contract made by hand. Words that
// the test of another function states — what to do of a hint, the header of
// a column, a line that another command prints too — are taken from the
// catalog. The other tests build the text they expect from the catalog or
// check the code of a refusal, so a change of the words of the catalog
// changes one test.

// render returns the text that text prints in the channel ch.
func render(ch channel, text func(p *page)) string {
	p := &page{ch: ch}
	text(p)
	return p.String()
}

// failText returns the refusal f as a command prints it in the channel ch.
func failText(ch channel, f failure) string {
	var out, errOut bytes.Buffer
	fail(Env{Stdout: &out, Stderr: &errOut, agent: ch == agentChannel}, f)
	return errOut.String()
}

// wantText checks the text that text prints in the channel of the command
// line and in the channel of the agent. An empty agent states that the agent
// gets the text of the command line: it has no hints, or the same ones.
func wantText(t *testing.T, name string, text func(p *page), cli, agent string) {
	t.Helper()
	if agent == "" {
		agent = cli
	}
	wantIn(t, name, "command line", render(cliChannel, text), cli)
	wantIn(t, name, "agent", render(agentChannel, text), agent)
}

// wantFail checks the refusal f as a command prints it in the channel of the
// command line and in the channel of the agent; an empty agent, as wantText
// has it.
func wantFail(t *testing.T, name string, f failure, cli, agent string) {
	t.Helper()
	if agent == "" {
		agent = cli
	}
	wantIn(t, name, "command line", failText(cliChannel, f), cli)
	wantIn(t, name, "agent", failText(agentChannel, f), agent)
}

func wantIn(t *testing.T, name, ch, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s, %s:\ngot:\n%s\nwant:\n%s", name, ch, got, want)
	}
}

// lines joins lines of a text, each ended by a newline.
func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// inUTC makes the local time UTC for the test: the texts name times in the
// local time.
func inUTC(t *testing.T) {
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
}
