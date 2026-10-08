package state

import (
	"errors"
	"time"
)

// States of a step.
const (
	StepPlanned = "planned" // declared, not done yet
	StepDone    = "done"    // done, with its check if any
	StepDropped = "dropped" // dropped with a reason
)

// Step is a step of a pass: declared ahead, then done or dropped.
type Step struct {
	Number       int // 1, 2, … within the pass
	Title        string
	State        string // one of Step*
	Check        string // how a done step was checked; may be empty
	Reason       string // why a step was dropped
	Source       string // who added it
	Added        time.Time
	ClosedSource string // who marked it done or dropped it
	Closed       time.Time
}

// AddSteps adds steps after the steps of the pass and returns their numbers.
func (t *Tx) AddSteps(pass int64, titles []string, source string) ([]int, error) {
	var last int
	if err := t.tx.QueryRow(`SELECT coalesce(max(number), 0) FROM steps WHERE pass = ?`, pass).Scan(&last); err != nil {
		return nil, err
	}
	added := now().UTC().Format(TimeFormat)
	numbers := make([]int, len(titles))
	for i, title := range titles {
		numbers[i] = last + 1 + i
		if _, err := t.tx.Exec(`INSERT INTO steps (pass, number, title, state, source, added) VALUES (?, ?, ?, ?, ?, ?)`,
			pass, numbers[i], title, StepPlanned, source, added); err != nil {
			return nil, err
		}
	}
	return numbers, nil
}

// CloseStep marks a planned step of the pass done, with its check, or
// dropped, with its reason.
func (t *Tx) CloseStep(pass int64, number int, state, text, source string) error {
	var check, reason any
	if state == StepDone {
		check = nullable(text)
	} else {
		reason = nullable(text)
	}
	res, err := t.tx.Exec(`UPDATE steps SET state = ?, check_text = ?, reason = ?, closed_source = ?, closed = ?
		WHERE pass = ? AND number = ? AND state = ?`,
		state, check, reason, source, now().UTC().Format(TimeFormat), pass, number, StepPlanned)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return errors.New("state: the step is not planned")
	}
	return nil
}

// stepsOf returns the steps of the passes of a task by pass, in order.
func stepsOf(q querier, task int64) (map[int64][]Step, error) {
	rows, err := q.Query(`SELECT s.pass, s.number, s.title, s.state, coalesce(s.check_text, ''), coalesce(s.reason, ''),
			s.source, s.added, coalesce(s.closed_source, ''), coalesce(s.closed, '')
		FROM steps s JOIN task_path p ON p.id = s.pass
		WHERE p.task = ? ORDER BY s.pass, s.number`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := map[int64][]Step{}
	for rows.Next() {
		var pass int64
		var s Step
		var added, closed string
		if err := rows.Scan(&pass, &s.Number, &s.Title, &s.State, &s.Check, &s.Reason, &s.Source, &added,
			&s.ClosedSource, &closed); err != nil {
			return nil, err
		}
		if s.Added, err = time.Parse(TimeFormat, added); err != nil {
			return nil, err
		}
		if s.Closed, err = parseOptional(closed); err != nil {
			return nil, err
		}
		steps[pass] = append(steps[pass], s)
	}
	return steps, rows.Err()
}
