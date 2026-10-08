package state

import (
	"encoding/json"
	"time"
)

// TimeFormat is the format of event times: UTC, RFC 3339 with milliseconds.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Event is an entry of the event journal.
type Event struct {
	Seq     int64
	Time    time.Time
	Type    string // object.action in the past tense, such as task.taken
	Project string // empty for an event without a project
	Task    string // empty for an event without a task
	Data    json.RawMessage
}

// now is replaced in tests.
var now = time.Now

// AddEvent writes an event in the transaction and returns its number. data is
// marshalled to a JSON object; nil gives an empty object.
func (t *Tx) AddEvent(typ, project, task string, data any) (int64, error) {
	raw := []byte("{}")
	if data != nil {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return 0, err
		}
	}
	res, err := t.tx.Exec(`INSERT INTO events (time, project, task, type, data) VALUES (?, ?, ?, ?, ?)`,
		now().UTC().Format(TimeFormat), nullable(project), nullable(task), typ, string(raw))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Events returns the events with numbers greater than after, in order.
func (s *Store) Events(after int64) ([]Event, error) {
	var events []Event
	err := s.retry(func() error {
		events = nil
		rows, err := s.db.Query(`SELECT seq, time, coalesce(project, ''), coalesce(task, ''), type, data
			FROM events WHERE seq > ? ORDER BY seq`, after)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			var tm, data string
			if err := rows.Scan(&e.Seq, &tm, &e.Project, &e.Task, &e.Type, &data); err != nil {
				return err
			}
			if e.Time, err = time.Parse(TimeFormat, tm); err != nil {
				return err
			}
			e.Data = json.RawMessage(data)
			events = append(events, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return events, nil
}
