package contract

import (
	"encoding/json"
	"time"
)

// TimeFormat is the format of times in the output of commands and in events:
// UTC, RFC 3339 with milliseconds.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// MarshalJSON writes the time of the commit in TimeFormat.
func (a Applied) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"commit": a.Commit, "time": a.Time.UTC().Format(TimeFormat)})
}

// MarshalJSON writes the time of the synchronization in TimeFormat. Fields
// keep the order of the other outputs: by name.
func (s ProcessStatusOutput) MarshalJSON() ([]byte, error) {
	type plain ProcessStatusOutput
	b, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if s.Synced != nil {
		if fields["synced"], err = json.Marshal(s.Synced.UTC().Format(TimeFormat)); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

// MarshalJSON writes the time of taking in TimeFormat.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task
	return withTime(plain(t), "taken", t.Taken)
}

// MarshalJSON writes the time of taking in TimeFormat.
func (t TaskListItem) MarshalJSON() ([]byte, error) {
	type plain TaskListItem
	return withTime(plain(t), "taken", t.Taken)
}

// withTime marshals v with its field name holding t in TimeFormat. Fields
// keep the order of the other outputs: by name.
func withTime(v any, name string, t time.Time) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if fields[name], err = json.Marshal(t.UTC().Format(TimeFormat)); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
