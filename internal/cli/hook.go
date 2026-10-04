package cli

import (
	"bytes"
	"io"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/msg"
)

// sessionStart is replaced in tests to simulate failures.
var sessionStart = hook.RunSessionStart

func runHook(args []string, env Env) int {
	f := newFlags("hook")
	if code, done := f.parse(args, env); done {
		return code
	}
	events := strings.Join(hook.Events, ", ")
	switch {
	case len(f.args) == 0:
		return fail(env, missingArgument("hook", "event", msg.Text(msg.ErrHookEventMissing), msg.Text(msg.HintHookEvents, events)))
	case !slices.Contains(hook.Events, f.args[0]):
		e := f.args[0]
		return fail(env, invalidArgument("hook", "event", e, msg.Text(msg.ErrHookEventUnknown, e), msg.Text(msg.HintHookEvents, events)))
	case len(f.args) > 1:
		return fail(env, extraArgs("hook", f.args[1:]))
	}
	runSessionStart(env.Stdout)
	return contract.ExitOK
}

// runSessionStart never breaks the agent session: on any failure, including a
// panic, it prints nothing. Output is buffered so that a failure midway does
// not leave a partial introduction.
func runSessionStart(stdout io.Writer) {
	var buf bytes.Buffer
	ok := func() (ok bool) {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		return sessionStart(&buf) == nil
	}()
	if ok {
		stdout.Write(buf.Bytes())
	}
}
