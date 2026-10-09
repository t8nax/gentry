package cli

import (
	"fmt"
	"strings"

	"github.com/t8nax/gentry/contract"
)

// The output of a command has one source: the structure of the contract. The
// command builds it once; --json prints it as it is, and the text for the
// operator and the agent is built from it by one function of the command.
// What only the text shows, such as the title of a stage the contract names
// by its identifier or the worktree the command runs in, is passed to that
// function apart from the structure: the contract stays as it is.

// page is the text of an output in a channel: of the agent, or of the
// command line.
type page struct {
	strings.Builder
	ch channel
}

// hints prints a block of hints after a blank line: the lines the channel
// has of hints. If the channel has none, nothing is printed, not even the
// blank line.
func (p *page) hints(hints ...hint) {
	lines := renderHints(hints, p.ch)
	if len(lines) == 0 {
		return
	}
	p.WriteString("\n")
	for _, l := range lines {
		fmt.Fprintln(p, l)
	}
}

// emit prints the output of a command: out as JSON with --json, or else the
// text that text builds from it.
func emit[T any](env Env, out T, text func(p *page, out T)) int {
	if env.json {
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	p := &page{ch: channelOf(env)}
	text(p, out)
	fmt.Fprint(env.Stdout, p.String())
	return contract.ExitOK
}

// warn prints a warning of a command on its standard error, as text built
// by text; with --json the warning is a part of the output of the command and
// is not printed.
func warn(env Env, text func(p *page)) {
	if env.json {
		return
	}
	p := &page{ch: channelOf(env)}
	text(p)
	fmt.Fprint(env.Stderr, p.String())
}
