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

// sessionStart is replaced in tests to simulate failures.
var sessionStart = startSession

func runHook(args []string, env Env) int {
	f := newFlags("hook")
	tool := f.String("tool")
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
	}
	in := hook.ReadInput(hookStdin(env.Stdin))
	in.Tool = tool.Value
	switch f.args[0] {
	case hook.SessionStart:
		runSessionStart(env.Stdout, in)
	case hook.PreTool:
		quietly(func() error { return hook.RunPreTool(in) })
	case hook.PostTool:
		quietly(func() error { return hook.RunPostTool(in) })
	case hook.Stop:
		quietly(func() error { return hook.RunStop(in) })
	}
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

// quietly runs a hook that prints nothing and never fails: a failure to mark
// a call must not get in the way of the command of the agent.
func quietly(fn func() error) {
	defer func() { recover() }()
	fn()
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
