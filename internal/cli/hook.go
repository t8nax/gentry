package cli

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/msg"
)

// sessionStart is replaced in tests to simulate failures. It is set in init:
// the introduction names tools, which are the commands, and the commands
// name the hook.
var sessionStart func(w io.Writer, in hook.Input) error

func init() { sessionStart = startSession }

func runHook(args []string, env Env) int {
	f := newFlags("hook")
	tool := f.String("tool")
	if code, done := f.parse(args, env); done {
		return code
	}
	events := strings.Join(hook.Events, ", ")
	switch {
	case len(f.args) == 0:
		return fail(env, missingArgument("hook", "event", msg.Text(msg.ErrHookEventMissing), hintOf(msg.HintHookEvents, events)))
	case slices.Contains(hook.Retired, f.args[0]):
		return contract.ExitOK
	case !slices.Contains(hook.Events, f.args[0]):
		e := f.args[0]
		return fail(env, invalidArgument("hook", "event", e, msg.Text(msg.ErrHookEventUnknown, e), hintOf(msg.HintHookEvents, events)))
	}
	in := hook.ReadInput(hookStdin(env.Stdin))
	in.Tool = tool.Value
	runSessionStart(env.Stdout, in)
	return contract.ExitOK
}

// hookStdin returns the standard input of a hook, or nil for a terminal: the
// agent tool always pipes the input, and a hook run by hand must not wait
// for it.
func hookStdin(r io.Reader) io.Reader {
	if f, ok := r.(*os.File); ok {
		if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeCharDevice != 0 {
			return nil
		}
	}
	return r
}

// runSessionStart never breaks the agent session: on any failure, including a
// panic, it prints nothing. Output is buffered so that a failure midway does
// not leave a partial introduction.
func runSessionStart(stdout io.Writer, in hook.Input) {
	var buf bytes.Buffer
	ok := func() (ok bool) {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		return sessionStart(&buf, in) == nil
	}()
	if ok {
		stdout.Write(buf.Bytes())
	}
}
