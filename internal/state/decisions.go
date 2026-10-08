package state

import (
	"database/sql"
	"encoding/json"
	"time"
)

// decisionSchema is the first schema with the decisions of the operator.
const decisionSchema = 7

// Decision is a decision of the operator: the words of the operator given in
// conversation, with the question they answer if there was one. Decisions
// are only added.
type Decision struct {
	Number      int    // 1, 2, … within the task
	Question    string // empty for words given without a question
	Options     []Option
	Answer      string
	Pass        int64 // the pass it was recorded at
	Node        string
	Stage       string
	Round       int
	AllowReturn string // the node of one more return from Node; empty if none
	Source      string
	Recorded    time.Time
}

// Option is an option of an answer offered with a question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

// AddDecision adds a decision of the operator to a task at its pass and
// returns it with its number.
func (t *Tx) AddDecision(task int64, d Decision) (Decision, error) {
	if err := t.tx.QueryRow(`SELECT coalesce(max(number), 0) + 1 FROM operator_decisions WHERE task = ?`, task).Scan(&d.Number); err != nil {
		return Decision{}, err
	}
	var options any
	if len(d.Options) > 0 {
		b, err := json.Marshal(d.Options)
		if err != nil {
			return Decision{}, err
		}
		options = string(b)
	}
	d.Recorded = now()
	_, err := t.tx.Exec(`INSERT INTO operator_decisions (task, number, question, options, answer, pass, allow_return, source, recorded)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task, d.Number, nullable(d.Question), options, d.Answer, d.Pass, nullable(d.AllowReturn), d.Source,
		d.Recorded.UTC().Format(TimeFormat))
	return d, err
}

// Decisions returns the decisions of the operator of a task by number.
func (t *Tx) Decisions(task int64) ([]Decision, error) { return decisions(t.tx, task) }

// Decisions returns the decisions of the operator of a task by number; none
// for an older store.
func (s *Store) Decisions(task int64) ([]Decision, error) {
	if s.schema < decisionSchema {
		return nil, nil
	}
	var ds []Decision
	err := s.retry(func() error {
		var err error
		ds, err = decisions(s.db, task)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ds, nil
}

func decisions(q querier, task int64) ([]Decision, error) {
	rows, err := q.Query(`SELECT d.number, d.question, d.options, d.answer, d.pass, p.node, p.stage, p.round,
			d.allow_return, d.source, d.recorded
		FROM operator_decisions d JOIN task_path p ON p.id = d.pass
		WHERE d.task = ? ORDER BY d.number`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ds []Decision
	for rows.Next() {
		var d Decision
		var question, options, allow sql.NullString
		var recorded string
		if err := rows.Scan(&d.Number, &question, &options, &d.Answer, &d.Pass, &d.Node, &d.Stage, &d.Round,
			&allow, &d.Source, &recorded); err != nil {
			return nil, err
		}
		d.Question, d.AllowReturn = question.String, allow.String
		if options.Valid {
			if err := json.Unmarshal([]byte(options.String), &d.Options); err != nil {
				return nil, err
			}
		}
		if d.Recorded, err = time.Parse(TimeFormat, recorded); err != nil {
			return nil, err
		}
		ds = append(ds, d)
	}
	return ds, rows.Err()
}
