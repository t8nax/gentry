//go:build acceptance

package main

import (
	"io"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
)

// The build with the tag acceptance is the program the acceptance tests run
// on: tests/testkit runs every command on it when told so. Its two entries
// run a command as the agent does, which the command line cannot: the
// commands only the agent runs and the tools of gentry mcp. The tag is set
// by the tests and CI only; the release and go install do not set it, so
// their command line refuses the commands of the agent as before.
const (
	// agentEntry before a command runs it in the channel of the agent, as
	// cli.RunAgent does: gentry --agent task step add 'Шаг'.
	agentEntry = "--agent"
	// toolEntry before the name of a tool runs it as gentry mcp does, as
	// cli.CallTool does: the standard input holds its arguments, a JSON
	// object; the standard output gets its text, and the exit code is
	// ExitError if it failed. The standard error gets nothing but the trace
	// of a panic, such as for a tool that does not exist: tests/testkit
	// fails the test on it.
	toolEntry = "--tool"
)

func init() { run = runTest }

// runTest runs a command line of the tests: a command after an entry, or a
// command of the command line.
func runTest(args []string, env cli.Env) int {
	if len(args) == 0 {
		return cli.Run(args, env)
	}
	switch args[0] {
	case agentEntry:
		return cli.RunAgent(args[1:], env)
	case toolEntry:
		if len(args) != 2 {
			return cli.Run(args, env)
		}
		in, err := io.ReadAll(env.Stdin)
		if err != nil {
			panic(err)
		}
		text, failed := cli.CallTool(args[1], in)
		io.WriteString(env.Stdout, text)
		if failed {
			return contract.ExitError
		}
		return contract.ExitOK
	}
	return cli.Run(args, env)
}
