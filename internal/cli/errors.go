package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// failure is a refusal or failure of a command. It is printed as text on
// stderr, without a program prefix, or as contract.ErrorOutput on stdout when
// --json is given.
type failure struct {
	exit    int
	code    string
	message string
	more    string // lines under the message in text only, such as a list of problems
	hints   []hint
	details map[string]any
	// agentMessage is the message the agent gets instead, if it has words of
	// its own.
	agentMessage string
}

// fail prints f and returns its exit code.
func fail(env Env, f failure) int {
	if env.json {
		out := contract.ErrorOutput{Error: contract.Error{Code: f.code, Message: f.message, Details: f.details}}
		if lines := renderHints(f.hints, jsonChannel); len(lines) > 0 {
			hint := strings.Join(lines, "\n")
			out.Error.Hint = &hint
		}
		if err := writeJSON(env, out); err == nil {
			return f.exit
		}
		// Fall back to text: the failure must reach the operator anyway.
	}
	// The hint is a block of its own, the last one, after an empty line
	// (principle 12).
	p := &page{ch: channelOf(env)}
	if env.agent && f.agentMessage != "" {
		fmt.Fprintln(p, f.agentMessage)
	} else {
		fmt.Fprintln(p, f.message)
	}
	p.WriteString(f.more)
	p.hints(f.hints...)
	fmt.Fprint(env.Stderr, p.String())
	return f.exit
}

func unknownCommand(name string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeUnknownCommand,
		message: msg.Text(msg.ErrUnknownCommand, name),
		hints:   []hint{helpHint(msg.HintUnknownCommand, "")},
		details: map[string]any{"command": name},
	}
}

func unexpectedArgs(cmd string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeUnexpectedArgs,
		message: msg.Text(msg.ErrUnexpectedArgs, cmd),
		details: map[string]any{"command": cmd},
	}
}

// extraArgs is for a command that takes some arguments but got more.
func extraArgs(cmd string, extra []string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeUnexpectedArgs,
		message: msg.Text(msg.ErrExtraArgs, cmd, strings.Join(extra, " ")),
		details: map[string]any{"command": cmd, "args": extra},
	}
}

func unknownFlag(cmd, flag string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeUnknownFlag,
		message: msg.Text(msg.ErrUnknownFlag, cmd, flag),
		details: map[string]any{"command": cmd, "flag": flag},
	}
}

// flagValueMissing is for a flag that takes a value but got none.
func flagValueMissing(flag string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFlagValue,
		message: msg.Text(msg.ErrFlagValueMissing, flag),
		details: map[string]any{"flag": flag},
	}
}

// flagValueInvalid is for a flag value the command cannot accept.
func flagValueInvalid(flag, value, message string, hints ...hint) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFlagValue,
		message: message,
		hints:   hints,
		details: map[string]any{"flag": flag, "value": value},
	}
}

// stateFailure turns an error of the state store into a failure.
func stateFailure(err error) failure {
	var ne *state.NewerError
	var ue *state.UnavailableError
	switch {
	case errors.As(err, &ne):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStateNewer,
			message: msg.Text(msg.ErrStateNewer, ne.Schema, ne.Supported),
			hints:   []hint{hintOf(msg.HintStateNewer)},
			details: map[string]any{"path": ne.Path, "schema": ne.Schema, "supported": ne.Supported},
		}
	case errors.As(err, &ue):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStateUnavailable,
			message: msg.Text(msg.ErrStateUnavailable, ue.Err),
			hints:   []hint{hintOf(msg.HintStateUnavail, ue.Path)},
			details: map[string]any{"path": ue.Path},
		}
	}
	return internal(err)
}

// conflictingFlags is for flags that cannot be given together, named in the
// order of the command spec.
func conflictingFlags(cmd string, flags []string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeConflictingFlags,
		message: msg.Text(msg.ErrConflictingFlags, flags[0], flags[1]),
		hints:   []hint{helpHint(msg.HintCommandHelp, cmd)},
		details: map[string]any{"command": cmd, "flags": flags},
	}
}

func flagValue(flag string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFlagValue,
		message: msg.Text(msg.ErrFlagValue, flag),
		details: map[string]any{"flag": flag},
	}
}

func internal(err error) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeInternal,
		message: msg.Text(msg.ErrInternal, err),
	}
}

func missingArgument(cmd, arg, message string, hints ...hint) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeMissingArgument,
		message: message,
		hints:   hints,
		details: map[string]any{"command": cmd, "argument": arg},
	}
}

func invalidArgument(cmd, arg, value, message string, hints ...hint) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeInvalidArgument,
		message: message,
		hints:   hints,
		details: map[string]any{"command": cmd, "argument": arg, "value": value},
	}
}

func homeUnknown() failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeHomeUnknown,
		message: msg.Text(msg.ErrHomeUnknown),
		hints:   []hint{hintOf(msg.HintHomeUnknown)},
	}
}

func ioError(path string, err error) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeIOError,
		message: msg.Text(msg.ErrIO, path, err),
		details: map[string]any{"path": path},
	}
}

func toolNotFound(tool, program, title string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeToolNotFound,
		message: msg.Text(msg.ErrToolNotFound, program),
		hints:   []hint{hintOf(msg.HintToolNotFound, title).set("tool", tool)},
		details: map[string]any{"tool": tool, "program": program},
	}
}

func toolFailed(tool, command, output string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeToolFailed,
		message: msg.Text(msg.ErrToolFailed, command, output),
		details: map[string]any{"tool": tool, "command": command, "output": output},
	}
}

func pluginElsewhere(tool, registered, dir string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodePluginElsewhere,
		message: msg.Text(msg.ErrPluginElsewhere, registered, dir),
		hints:   []hint{hintOf(msg.HintPluginElsewhere).set("tool", tool)},
		details: map[string]any{"tool": tool, "registered": registered, "dir": dir},
	}
}

func missingAction(g command) failure {
	return missingArgument(g.name, "action", msg.Text(msg.ErrActionMissing, g.name),
		helpHint(msg.HintActions, g.name))
}

func unknownAction(g command, action string) failure {
	return invalidArgument(g.name, "action", action, msg.Text(msg.ErrActionUnknown, action, g.name),
		helpHint(msg.HintActions, g.name))
}
