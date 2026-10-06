package state

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// States of a task.
const (
	TaskActive    = "active"    // in work
	TaskWaiting   = "waiting"   // waits for the operator
	TaskClosed    = "closed"    // done
	TaskCancelled = "cancelled" // given up
)

// Sources of the title and the statement of a task.
const (
	SourceOperator = "operator" // passed by the operator
	SourceAgent    = "agent"    // written by the agent from the operator's words
)

// Task is a task of a project.
type Task struct {
	ID          int64 // the row of the task in the store
	Project     string
	Prefix      string // prefix of the project: with Number it makes the key, SHOP-1
	Number      int
	Title       string
	Statement   string
	Source      string // SourceOperator or SourceAgent
	State       string // one of Task*
	Scenario    string
	Node        string    // current node of the scenario
	FlowCommit  string    // the snapshot of the flow the task is pinned to
	FlowApplied time.Time // when that flow was applied
	Worktree    string    // empty once the task released its worktree
	Taken       time.Time
}

// Key returns the identifier of the task, such as SHOP-1.
func (t Task) Key() string { return fmt.Sprintf("%s-%d", t.Prefix, t.Number) }

// FlowSnapshot is a flow a task is pinned to.
type FlowSnapshot struct {
	Project string
	Commit  string // commit of the process repository that applied the flow
	Applied time.Time
	Content string // flow.Snapshot as JSON
}

// tasksSchema is the first schema with tasks: an older store opened for
// reading has none.
const tasksSchema = 5

// Snapshot returns the snapshot of the flow of project applied by commit.
func (t *Tx) Snapshot(project, commit string) (FlowSnapshot, bool, error) {
	return snapshot(t.tx, project, commit)
}

// AddSnapshot records the snapshot of a flow.
func (t *Tx) AddSnapshot(s FlowSnapshot) error {
	_, err := t.tx.Exec(`INSERT INTO flow_snapshots (project, commit_hash, applied, content) VALUES (?, ?, ?, ?)`,
		s.Project, s.Commit, s.Applied.UTC().Format(TimeFormat), s.Content)
	return err
}

// TakeNumber returns the next number of the series of project and moves the
// series on: a number is never given twice.
func (t *Tx) TakeNumber(project string) (int, error) {
	var n int
	err := t.tx.QueryRow(`UPDATE projects SET next_number = next_number + 1 WHERE id = ? RETURNING next_number - 1`, project).Scan(&n)
	return n, err
}

// AddTask records a task taken now and returns it with its time of taking.
func (t *Tx) AddTask(task Task) (Task, error) {
	task.Taken = now()
	res, err := t.tx.Exec(`INSERT INTO tasks (project, number, title, statement, source, state, scenario, node, flow_commit, worktree, taken)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.Project, task.Number, task.Title, task.Statement, task.Source, task.State, task.Scenario, task.Node,
		task.FlowCommit, nullable(task.Worktree), task.Taken.UTC().Format(TimeFormat))
	if err != nil {
		return Task{}, err
	}
	task.ID, err = res.LastInsertId()
	return task, err
}

// Task returns the task of row id as the transaction sees it.
func (t *Tx) Task(id int64) (Task, error) {
	ts, err := tasks(t.tx, `WHERE t.id = ?`, id)
	if err != nil {
		return Task{}, err
	}
	if len(ts) == 0 {
		return Task{}, fmt.Errorf("task %d: %w", id, sql.ErrNoRows)
	}
	return ts[0], nil
}

// SetNode moves the task to node: the next node of its scenario, or Finish.
func (t *Tx) SetNode(task int64, node string) error {
	_, err := t.tx.Exec(`UPDATE tasks SET node = ? WHERE id = ?`, node, task)
	return err
}

// TaskIn returns the task that holds the worktree at path.
func (t *Tx) TaskIn(path string) (Task, bool, error) {
	ts, err := tasks(t.tx, `WHERE t.worktree = ?`, path)
	if err != nil || len(ts) == 0 {
		return Task{}, false, err
	}
	return ts[0], true, nil
}

// Tasks returns the tasks of all projects by project and number; tasks of one
// number, possible from stage 8, in the order they were taken.
func (s *Store) Tasks() ([]Task, error) {
	if s.schema < tasksSchema {
		return nil, nil
	}
	var ts []Task
	err := s.retry(func() error {
		var err error
		ts, err = tasks(s.db, "")
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ts, nil
}

// Snapshot returns the snapshot of the flow of project applied by commit.
func (s *Store) Snapshot(project, commit string) (FlowSnapshot, bool, error) {
	if s.schema < tasksSchema {
		return FlowSnapshot{}, false, nil
	}
	var fs FlowSnapshot
	var ok bool
	err := s.retry(func() error {
		var err error
		fs, ok, err = snapshot(s.db, project, commit)
		return err
	})
	if err != nil {
		return FlowSnapshot{}, false, s.unavailable(err)
	}
	return fs, ok, nil
}

type rowQuerier interface {
	querier
	QueryRow(query string, args ...any) *sql.Row
}

func snapshot(q rowQuerier, project, commit string) (FlowSnapshot, bool, error) {
	s := FlowSnapshot{Project: project, Commit: commit}
	var applied string
	err := q.QueryRow(`SELECT applied, content FROM flow_snapshots WHERE project = ? AND commit_hash = ?`, project, commit).
		Scan(&applied, &s.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowSnapshot{}, false, nil
	}
	if err != nil {
		return FlowSnapshot{}, false, err
	}
	if s.Applied, err = time.Parse(TimeFormat, applied); err != nil {
		return FlowSnapshot{}, false, err
	}
	return s, true, nil
}

// tasks returns the tasks that match where, a clause on the tasks t.
func tasks(q querier, where string, args ...any) ([]Task, error) {
	rows, err := q.Query(`SELECT t.id, t.project, p.prefix, t.number, t.title, t.statement, t.source, t.state, t.scenario,
			t.node, t.flow_commit, f.applied, coalesce(t.worktree, ''), t.taken
		FROM tasks t
		JOIN projects p ON p.id = t.project
		JOIN flow_snapshots f ON f.project = t.project AND f.commit_hash = t.flow_commit
		`+where+`
		ORDER BY t.project, t.number, t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ts []Task
	for rows.Next() {
		var t Task
		var applied, taken string
		if err := rows.Scan(&t.ID, &t.Project, &t.Prefix, &t.Number, &t.Title, &t.Statement, &t.Source, &t.State, &t.Scenario,
			&t.Node, &t.FlowCommit, &applied, &t.Worktree, &taken); err != nil {
			return nil, err
		}
		if t.FlowApplied, err = time.Parse(TimeFormat, applied); err != nil {
			return nil, err
		}
		if t.Taken, err = time.Parse(TimeFormat, taken); err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}
