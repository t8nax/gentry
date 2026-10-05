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
	hint    string
	details map[string]any
}

// fail prints f and returns its exit code.
func fail(env Env, f failure) int {
	if env.json {
		out := contract.ErrorOutput{Error: contract.Error{Code: f.code, Message: f.message, Details: f.details}}
		if f.hint != "" {
			out.Error.Hint = &f.hint
		}
		if err := writeJSON(env, out); err == nil {
			return f.exit
		}
		// Fall back to text: the failure must reach the operator anyway.
	}
	// The hint is a line of its own, right under the message (principle 12).
	fmt.Fprintln(env.Stderr, f.message)
	if f.more != "" {
		fmt.Fprint(env.Stderr, f.more)
	}
	if f.hint != "" {
		fmt.Fprintln(env.Stderr, f.hint)
	}
	return f.exit
}

func unknownCommand(name string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeUnknownCommand,
		message: msg.Text(msg.ErrUnknownCommand, name),
		hint:    msg.Text(msg.HintUnknownCommand),
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
func flagValueInvalid(flag, value, message, hint string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeFlagValue,
		message: message,
		hint:    hint,
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
			hint:    msg.Text(msg.HintStateNewer),
			details: map[string]any{"path": ne.Path, "schema": ne.Schema, "supported": ne.Supported},
		}
	case errors.As(err, &ue):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeStateUnavailable,
			message: msg.Text(msg.ErrStateUnavailable, ue.Err),
			hint:    msg.Text(msg.HintStateUnavail, ue.Path),
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
		hint:    msg.Text(msg.HintCommandHelp, cmd),
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

func missingArgument(cmd, arg, message, hint string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeMissingArgument,
		message: message,
		hint:    hint,
		details: map[string]any{"command": cmd, "argument": arg},
	}
}

func invalidArgument(cmd, arg, value, message, hint string) failure {
	return failure{
		exit:    contract.ExitUsage,
		code:    contract.CodeInvalidArgument,
		message: message,
		hint:    hint,
		details: map[string]any{"command": cmd, "argument": arg, "value": value},
	}
}

func homeUnknown() failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeHomeUnknown,
		message: msg.Text(msg.ErrHomeUnknown),
		hint:    msg.Text(msg.HintHomeUnknown),
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
		hint:    msg.Text(msg.HintToolNotFound, title, tool),
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

func missingAction(g command) failure {
	return missingArgument(g.name, "action", msg.Text(msg.ErrActionMissing, g.name),
		msg.Text(msg.HintActions, g.name))
}

func unknownAction(g command, action string) failure {
	return invalidArgument(g.name, "action", action, msg.Text(msg.ErrActionUnknown, action, g.name),
		msg.Text(msg.HintActions, g.name))
}
