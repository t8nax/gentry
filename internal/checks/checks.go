// Package checks runs the commands of checks of a stage: programs with their
// arguments, without a shell, in a worktree. It knows nothing of tasks: what
// to run, where and for how long is given by the caller.
package checks

import (
	"regexp"
	"strings"
	"time"
)

// DefaultTimeout is the time of the commands of a stage without the field
// timeout, as the flow writes it.
const DefaultTimeout = "10m"

// QuoteError means a quote of a command is not closed.
type QuoteError struct{ Command string }

func (e *QuoteError) Error() string { return e.Command + ": unclosed quote" }

// Split divides a command into the program and its arguments by spaces. A
// text in single or double quotes is a part of one argument without the
// quotes, and a quote of the other kind inside it is kept; parts in quotes
// and out of them that touch make one argument, so -run="A B" is -run=A B.
// There is no escape: a backslash is an ordinary character, as are the
// characters of a shell.
func Split(command string) ([]string, error) {
	var args []string
	var arg strings.Builder
	inArg := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch c {
		case ' ', '\t', '\r', '\n':
			if inArg {
				args = append(args, arg.String())
				arg.Reset()
				inArg = false
			}
		case '"', '\'':
			end := strings.IndexByte(command[i+1:], c)
			if end < 0 {
				return nil, &QuoteError{Command: command}
			}
			arg.WriteString(command[i+1 : i+1+end])
			inArg = true
			i += end + 1
		default:
			arg.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, arg.String())
	}
	return args, nil
}

// timeoutPattern is a time as the flow writes it: whole numbers with the
// units h, m and s in this order.
var timeoutPattern = regexp.MustCompile(`^(?:[0-9]+h)?(?:[0-9]+m)?(?:[0-9]+s)?$`)

// ParseTimeout returns the time s names; ok is false if s is not a time of
// the flow or is not above zero.
func ParseTimeout(s string) (d time.Duration, ok bool) {
	if s == "" || !timeoutPattern.MatchString(s) {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
