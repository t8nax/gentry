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
	"github.com/t8nax/gentry/internal/state"
)

// Env is the environment a command runs in.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer

	json bool // --json is among the arguments: failures are printed as JSON
}

// command is a gentry command. Its arguments and flags are declared here and
// nowhere else: the help is built from them, and newFlags accepts only them.
type command struct {
	name    string
	section msg.Key // heading of the help section; empty for a service command
	summary msg.Key // one line in the command list
	desc    msg.Key // first paragraph of the command help
	args    []argSpec
	flags   []flagSpec
	run     func(args []string, env Env) int
}

// hidden reports whether c is a service command: not shown in the help.
func (c command) hidden() bool { return c.section == "" }

// argSpec is a positional argument. The command checks required arguments
// itself, to name them in its own words; flags.parse refuses extra ones.
type argSpec struct {
	name     msg.Key // placeholder, e.g. <ИИ-инструмент>
	desc     func() string
	optional bool
}

// flagSpec is a flag. A flag with a value placeholder takes a value.
type flagSpec struct {
	name  string  // without dashes
	value msg.Key // placeholder of the value; empty for a boolean flag
	desc  func() string
}

// descText returns a description that is the text of k.
func descText(k msg.Key) func() string {
	return func() string { return msg.Text(k) }
}

var jsonFlag = flagSpec{name: "json", desc: descText(msg.FlagJSON)}

func commands() []command {
	return []command{
		{
			name: "setup", section: msg.HelpSectionMaint, summary: msg.CmdSetupSummary, desc: msg.CmdSetupDesc,
			args: []argSpec{{name: msg.ArgTool, desc: func() string {
				return msg.Text(msg.ArgToolDesc, strings.Join(tools, ", "))
			}}},
			flags: []flagSpec{jsonFlag},
			run:   runSetup,
		},
		{
			name: "version", section: msg.HelpSectionMaint, summary: msg.CmdVersionSummary, desc: msg.CmdVersionDesc,
			flags: []flagSpec{{name: "json", desc: descText(msg.FlagVersionJSON)}},
			run:   runVersion,
		},
		{
			name: "help", section: msg.HelpSectionMaint, summary: msg.CmdHelpSummary, desc: msg.CmdHelpDesc,
			args: []argSpec{{name: msg.ArgCommand, desc: descText(msg.ArgCommandDesc), optional: true}},
			run:  runHelp,
		},
		{name: "hook", args: []argSpec{{}}, run: runHook},
		{name: "events", flags: []flagSpec{{name: "json"}, {name: "after", value: msg.ArgNumber}}, run: runEvents},
	}
}

// lookup returns the command named name.
func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
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
	if c, ok := lookup(name); ok {
		// A command without --json refuses it in text: JSON output would
		// suggest the flag is supported.
		env.json = env.json && c.acceptsJSON()
		return c.run(args[1:], env)
	}
	// An unknown command fails in JSON when asked: a client newer than this
	// gentry learns from the code that the command does not exist yet.
	return fail(env, unknownCommand(name))
}

// acceptsJSON reports whether c declares the --json flag.
func (c command) acceptsJSON() bool {
	for _, f := range c.flags {
		if f.name == "json" {
			return true
		}
	}
	return false
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

func runVersion(args []string, env Env) int {
	f := newFlags("version")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	out := contract.VersionOutput{Gentry: buildinfo.Version(), Contract: contract.Version, StateSchema: state.SchemaVersion()}
	if *asJSON {
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	// The contract and schema versions are for programs: only --json has them.
	fmt.Fprintln(env.Stdout, msg.Text(msg.VersionGentry, out.Gentry))
	return contract.ExitOK
}

// writeJSON prints v as the single JSON object of a --json command.
func writeJSON(env Env, v any) error {
	enc := json.NewEncoder(env.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
