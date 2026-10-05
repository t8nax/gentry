package state

import (
	"database/sql"
	"time"
)

// Project is a connected project.
type Project struct {
	ID           string // identifier shared by the team, such as shop
	Prefix       string // prefix of task numbers, personal to the operator
	Knowledge    string // absolute path of the knowledge repository
	Added        time.Time
	MainWorktree string // absolute path of the main worktree; empty if none
}

// Worktree is a worktree in the pool of a project.
type Worktree struct {
	Path    string // absolute path of the worktree root
	Project string
	Main    bool
	Added   time.Time
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// Projects returns the connected projects by identifier.
func (t *Tx) Projects() ([]Project, error) { return projects(t.tx) }

// Worktrees returns the worktrees of all projects by path.
func (t *Tx) Worktrees() ([]Worktree, error) { return worktrees(t.tx) }

// AddProject records a project. Its time of connection is now.
func (t *Tx) AddProject(p Project) error {
	_, err := t.tx.Exec(`INSERT INTO projects (id, prefix, knowledge, added) VALUES (?, ?, ?, ?)`,
		p.ID, p.Prefix, p.Knowledge, now().UTC().Format(TimeFormat))
	return err
}

// AddWorktree adds a worktree to the pool of its project, now.
func (t *Tx) AddWorktree(w Worktree) error {
	_, err := t.tx.Exec(`INSERT INTO worktrees (path, project, main, added) VALUES (?, ?, ?, ?)`,
		w.Path, w.Project, w.Main, now().UTC().Format(TimeFormat))
	return err
}

// Projects returns the connected projects by identifier.
func (s *Store) Projects() ([]Project, error) {
	var ps []Project
	err := s.retry(func() error {
		var err error
		ps, err = projects(s.db)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ps, nil
}

// Worktrees returns the worktrees of all projects by path.
func (s *Store) Worktrees() ([]Worktree, error) {
	var ws []Worktree
	err := s.retry(func() error {
		var err error
		ws, err = worktrees(s.db)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return ws, nil
}

func projects(q querier) ([]Project, error) {
	rows, err := q.Query(`SELECT p.id, p.prefix, p.knowledge, p.added, coalesce(w.path, '')
		FROM projects p LEFT JOIN worktrees w ON w.project = p.id AND w.main = 1
		ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ps []Project
	for rows.Next() {
		var p Project
		var added string
		if err := rows.Scan(&p.ID, &p.Prefix, &p.Knowledge, &added, &p.MainWorktree); err != nil {
			return nil, err
		}
		if p.Added, err = time.Parse(TimeFormat, added); err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

func worktrees(q querier) ([]Worktree, error) {
	rows, err := q.Query(`SELECT path, project, main, added FROM worktrees ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ws []Worktree
	for rows.Next() {
		var w Worktree
		var added string
		if err := rows.Scan(&w.Path, &w.Project, &w.Main, &added); err != nil {
			return nil, err
		}
		if w.Added, err = time.Parse(TimeFormat, added); err != nil {
			return nil, err
		}
		ws = append(ws, w)
	}
	return ws, rows.Err()
}
