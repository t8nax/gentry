package cli

import (
	"fmt"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// failure is a refusal or failure of a command. It is printed as text on
// stderr, or as contract.ErrorOutput on stdout when --json is given.
type failure struct {
	exit    int
	code    string
	message string
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
	text := f.message
	if f.hint != "" {
		text += " " + f.hint
	}
	fmt.Fprintf(env.Stderr, "gentry: %s\n", text)
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
