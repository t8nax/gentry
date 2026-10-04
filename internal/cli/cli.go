// Package cli parses the gentry command line and dispatches commands.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/msg"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Env is the environment a command runs in.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
}

type command struct {
	name    string
	summary msg.Key
	run     func(args []string, env Env) int
}

func commands() []command {
	return []command{
		{name: "version", summary: msg.CmdVersionSummary, run: runVersion},
		{name: "help", summary: msg.CmdHelpSummary, run: runHelp},
	}
}

// Run executes the command given by args (without the program name)
// and returns the exit code.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		return runHelp(nil, env)
	}
	name := args[0]
	if name == "-h" || name == "--help" {
		return runHelp(nil, env)
	}
	for _, c := range commands() {
		if c.name == name {
			return c.run(args[1:], env)
		}
	}
	return usageError(env, msg.Text(msg.ErrUnknownCommand, name))
}

func runHelp(args []string, env Env) int {
	if len(args) > 0 {
		return noArgs("help", env)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n\n%s\n", msg.Text(msg.HelpIntro), msg.Text(msg.HelpUsage), msg.Text(msg.HelpCommands))
	cmds := commands()
	width := 0
	for _, c := range cmds {
		width = max(width, len(c.name))
	}
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.name, msg.Text(c.summary))
	}
	io.WriteString(env.Stdout, b.String())
	return ExitOK
}

func runVersion(args []string, env Env) int {
	if len(args) > 0 {
		return noArgs("version", env)
	}
	fmt.Fprintf(env.Stdout, "gentry %s\n", buildinfo.Version())
	return ExitOK
}

func noArgs(name string, env Env) int {
	return usageError(env, msg.Text(msg.ErrUnexpectedArgs, name))
}

func usageError(env Env, text string) int {
	fmt.Fprintf(env.Stderr, "gentry: %s\n", text)
	return ExitUsage
}
