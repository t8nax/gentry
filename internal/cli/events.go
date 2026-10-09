package cli

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// eventLine is one line of `gentry events --json`, as contract.Event. It is a
// type of its own to print the time with exactly three fractional digits.
type eventLine struct {
	Seq     int64           `json:"seq"`
	Time    string          `json:"time"`
	Type    string          `json:"type"`
	Project string          `json:"project,omitempty"`
	Task    string          `json:"task,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// runEvents prints the event journal as JSON lines, one per event: its
// callers are programs, such as the panel, so it has no text output. --json is
// accepted, as by every command, and changes nothing but the form of a
// failure. It only reads: without a state store the journal is empty, and no
// store is created.
func runEvents(args []string, env Env) int {
	f := newFlags("events")
	after := f.String("after")
	if code, done := f.parse(args, env); done {
		return code
	}
	var from int64
	if after.Set {
		n, err := strconv.ParseInt(after.Value, 10, 64)
		if err != nil || n < 0 {
			return fail(env, flagValueInvalid("--after", after.Value, msg.Text(msg.ErrEventsAfter, after.Value), hintOf(msg.HintEventsAfter)))
		}
		from = n
	}

	events, bad := readEvents(from)
	if bad != nil {
		return fail(env, *bad)
	}

	enc := json.NewEncoder(env.Stdout)
	enc.SetEscapeHTML(false)
	for _, e := range events {
		line := eventLine{Seq: e.Seq, Time: e.Time.UTC().Format(state.TimeFormat), Type: e.Type, Project: e.Project, Task: e.Task, Data: e.Data}
		if err := enc.Encode(line); err != nil {
			return fail(env, internal(err))
		}
	}
	return contract.ExitOK
}

// readEvents reads the events after from.
func readEvents(from int64) ([]state.Event, *failure) {
	path, err := state.Path()
	if err != nil {
		f := homeUnknown()
		return nil, &f
	}
	s, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	defer s.Close()
	events, err := s.Events(from)
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	return events, nil
}
