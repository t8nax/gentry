package state

import "time"

// Note is a note of a task: what the agent wants to keep in mind. Notes are
// only added.
type Note struct {
	Number int // 1, 2, … within the task
	Text   string
	Pass   int64 // the pass it was added at
	Node   string
	Stage  string
	Round  int
	Source string
	Added  time.Time
}

// AddNote adds a note to a task at its pass and returns it with its number.
func (t *Tx) AddNote(task int64, n Note) (Note, error) {
	if err := t.tx.QueryRow(`SELECT coalesce(max(number), 0) + 1 FROM notes WHERE task = ?`, task).Scan(&n.Number); err != nil {
		return Note{}, err
	}
	n.Added = now()
	_, err := t.tx.Exec(`INSERT INTO notes (task, number, text, pass, source, added) VALUES (?, ?, ?, ?, ?, ?)`,
		task, n.Number, n.Text, n.Pass, n.Source, n.Added.UTC().Format(TimeFormat))
	return n, err
}

// Notes returns the notes of a task by number; none for an older store.
func (s *Store) Notes(task int64) ([]Note, error) {
	if s.schema < pathSchema {
		return nil, nil
	}
	var ns []Note
	err := s.retry(func() error {
		var err error
		ns, err = notes(s.db, task)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ns, nil
}

func notes(q querier, task int64) ([]Note, error) {
	rows, err := q.Query(`SELECT n.number, n.text, n.pass, p.node, p.stage, p.round, n.source, n.added
		FROM notes n JOIN task_path p ON p.id = n.pass
		WHERE n.task = ? ORDER BY n.number`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ns []Note
	for rows.Next() {
		var n Note
		var added string
		if err := rows.Scan(&n.Number, &n.Text, &n.Pass, &n.Node, &n.Stage, &n.Round, &n.Source, &added); err != nil {
			return nil, err
		}
		if n.Added, err = time.Parse(TimeFormat, added); err != nil {
			return nil, err
		}
		ns = append(ns, n)
	}
	return ns, rows.Err()
}
