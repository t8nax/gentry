// Package cli parses the gentry command line and dispatches commands.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/msg"
)

// Env is the environment a command runs in.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer

	json bool // --json is among the arguments: failures are printed as JSON
}

type command struct {
	name    string
	summary msg.Key
	hidden  bool // a service command, not shown in the help
	run     func(args []string, env Env) int
}

func commands() []command {
	return []command{
		{name: "version", summary: msg.CmdVersionSummary, run: runVersion},
		{name: "setup", summary: msg.CmdSetupSummary, run: runSetup},
		{name: "help", summary: msg.CmdHelpSummary, run: runHelp},
		{name: "hook", hidden: true, run: runHook},
	}
}

// Run executes the command given by args (without the program name)
// and returns the exit code.
func Run(args []string, env Env) int {
	env.json = hasJSONFlag(args)
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
	return fail(env, unknownCommand(name))
}

// hasJSONFlag reports whether --json is among the flags, so that even a
// command line that cannot be parsed fails in JSON.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--json" {
			return true
		}
	}
	return false
}

func runHelp(args []string, env Env) int {
	if len(args) > 0 {
		return fail(env, unexpectedArgs("help"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n\n%s\n", msg.Text(msg.HelpIntro), msg.Text(msg.HelpUsage), msg.Text(msg.HelpCommands))
	var cmds []command
	for _, c := range commands() {
		if !c.hidden {
			cmds = append(cmds, c)
		}
	}
	width := 0
	for _, c := range cmds {
		width = max(width, len(c.name))
	}
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.name, msg.Text(c.summary))
	}
	io.WriteString(env.Stdout, b.String())
	return contract.ExitOK
}

func runVersion(args []string, env Env) int {
	f := newFlags("version")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if len(f.args) > 0 {
		return fail(env, unexpectedArgs("version"))
	}
	out := contract.VersionOutput{Gentry: buildinfo.Version(), Contract: contract.Version}
	if *asJSON {
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintf(env.Stdout, "gentry %s\n%s\n", out.Gentry, msg.Text(msg.VersionContract, out.Contract))
	return contract.ExitOK
}

// writeJSON prints v as the single JSON object of a --json command.
func writeJSON(env Env, v any) error {
	enc := json.NewEncoder(env.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
