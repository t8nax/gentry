package state

import (
	"database/sql"
	"errors"
	"time"

	"github.com/t8nax/gentry/internal/flow"
)

// Finish is the node of a task that has passed its scenario.
const Finish = "finish"

// Outcomes of a closed pass.
const (
	OutcomeExit = "exit" // closed by an exit
	OutcomeSkip = "skip" // skipped with a reason
)

// Kinds of an exit of a stage.
const (
	ExitArtifact = "artifact" // an artifact of the task
	ExitResult   = "result"   // the result of an event
	ExitNegative = "negative" // a negative result with where it was looked for
)

// pathSchema is the first schema with the way of a task: passes, steps,
// notes and artifacts.
const pathSchema = 6

// Pass is a pass of a task through a node of its scenario: one per entry
// into the node, closed by an exit or a skip.
type Pass struct {
	ID       int64
	Task     int64
	Node     string
	Stage    string // the stage of the node; empty for a store read at schema 5
	Round    int    // the number of the pass of the node in the task, from 1
	Entered  time.Time
	Outcome  string // empty while current, OutcomeExit or OutcomeSkip
	ExitKind string // one of Exit* for an exit
	ExitText string
	Artifact string // the artifact of an exit of kind ExitArtifact
	Next     string // the node taken, or Finish
	Reason   string // why that transition; why the skip
	Source   string // who closed the pass: SourceOperator or SourceAgent
	Closed   time.Time
	Steps    []Step // in the order added
}

// Current reports whether the pass is not closed yet.
func (p Pass) Current() bool { return p.Outcome == "" }

// AddPass records the entry of a task into a node and returns the pass. A
// pass without a time of entry is entered now.
func (t *Tx) AddPass(p Pass) (Pass, error) {
	if p.Entered.IsZero() {
		p.Entered = now()
	}
	res, err := t.tx.Exec(`INSERT INTO task_path (task, node, stage, round, entered) VALUES (?, ?, ?, ?, ?)`,
		p.Task, p.Node, p.Stage, p.Round, p.Entered.UTC().Format(TimeFormat))
	if err != nil {
		return Pass{}, err
	}
	p.ID, err = res.LastInsertId()
	return p, err
}

// ClosePass closes the current pass p with its outcome, now.
func (t *Tx) ClosePass(p Pass) (Pass, error) {
	p.Closed = now()
	res, err := t.tx.Exec(`UPDATE task_path SET outcome = ?, exit_kind = ?, exit_text = ?, artifact = ?, next = ?,
			reason = ?, source = ?, closed = ?
		WHERE id = ? AND outcome IS NULL`,
		p.Outcome, nullable(p.ExitKind), nullable(p.ExitText), nullable(p.Artifact), p.Next,
		nullable(p.Reason), p.Source, p.Closed.UTC().Format(TimeFormat), p.ID)
	if err != nil {
		return Pass{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Pass{}, err
	} else if n != 1 {
		return Pass{}, errors.New("state: the pass is closed already")
	}
	return p, nil
}

// TaskPath returns the passes of a task in the order entered, with their steps.
func (t *Tx) TaskPath(task int64) ([]Pass, error) { return path(t.tx, task) }

// CurrentPass returns the current pass of a task; false once the task has
// passed its scenario.
func (t *Tx) CurrentPass(task int64) (Pass, bool, error) {
	ps, err := path(t.tx, task)
	if err != nil {
		return Pass{}, false, err
	}
	if n := len(ps); n > 0 && ps[n-1].Current() {
		return ps[n-1], true, nil
	}
	return Pass{}, false, nil
}

// TaskPath returns the passes of task in the order entered, with their steps.
// A store of schema 5 opened for reading has no passes: the task is in its
// first pass of its current node, entered when it was taken.
func (s *Store) TaskPath(task Task) ([]Pass, error) {
	if s.schema < tasksSchema {
		return nil, nil
	}
	if s.schema < pathSchema {
		return []Pass{{Task: task.ID, Node: task.Node, Round: 1, Entered: task.Taken}}, nil
	}
	var ps []Pass
	err := s.retry(func() error {
		var err error
		ps, err = path(s.db, task.ID)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ps, nil
}

func path(q querier, task int64) ([]Pass, error) {
	rows, err := q.Query(`SELECT id, node, stage, round, entered, coalesce(outcome, ''), coalesce(exit_kind, ''),
			coalesce(exit_text, ''), coalesce(artifact, ''), coalesce(next, ''), coalesce(reason, ''),
			coalesce(source, ''), coalesce(closed, '')
		FROM task_path WHERE task = ? ORDER BY id`, task)
	if err != nil {
		return nil, err
	}
	var ps []Pass
	for rows.Next() {
		p := Pass{Task: task}
		var entered, closed string
		if err := rows.Scan(&p.ID, &p.Node, &p.Stage, &p.Round, &entered, &p.Outcome, &p.ExitKind, &p.ExitText,
			&p.Artifact, &p.Next, &p.Reason, &p.Source, &closed); err != nil {
			rows.Close()
			return nil, err
		}
		if p.Entered, err = time.Parse(TimeFormat, entered); err != nil {
			rows.Close()
			return nil, err
		}
		if p.Closed, err = parseOptional(closed); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	steps, err := stepsOf(q, task)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].Steps = steps[ps[i].ID]
	}
	return ps, nil
}

// parseOptional parses a time that may be absent: zero for "".
func parseOptional(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(TimeFormat, s)
}

// fillPath gives each task under way the pass of its current node when the
// store moves to schema 6: round 1, entered when the task was taken. The
// stage of the node is read from the snapshot of the flow of the task by
// stageOf; the node names it if the snapshot cannot be read.
func fillPath(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT t.id, t.scenario, t.node, t.taken, f.content
		FROM tasks t JOIN flow_snapshots f ON f.project = t.project AND f.commit_hash = t.flow_commit
		WHERE t.node != ? ORDER BY t.id`, Finish)
	if err != nil {
		return err
	}
	type entry struct {
		task        int64
		node, stage string
		taken       string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		var scenario, content string
		if err := rows.Scan(&e.task, &scenario, &e.node, &e.taken, &content); err != nil {
			rows.Close()
			return err
		}
		e.stage = e.node
		if st, ok := stageOf(content, scenario, e.node); ok {
			e.stage = st
		}
		entries = append(entries, e)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(`INSERT INTO task_path (task, node, stage, round, entered) VALUES (?, ?, ?, 1, ?)`,
			e.task, e.node, e.stage, e.taken); err != nil {
			return err
		}
	}
	return nil
}

// stageOf returns the stage of a node of a scenario in a stored snapshot of
// a flow; false if the snapshot cannot be read, as a later Gentry may check
// more than the one that took the task.
func stageOf(content, scenario, node string) (string, bool) {
	snap, err := flow.DecodeSnapshot([]byte(content))
	if err != nil {
		return "", false
	}
	res, err := flow.ReadSnapshot(snap)
	if err != nil || res.Flow == nil {
		return "", false
	}
	sc, ok := res.Flow.Scenario(scenario)
	if !ok {
		return "", false
	}
	for _, n := range sc.Nodes {
		if n.ID == node {
			return n.Stage, true
		}
	}
	return "", false
}
