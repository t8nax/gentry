package state

import "time"

// Kinds of an artifact.
const (
	ArtifactFile = "file" // a copy of a file in the state directory
	ArtifactLink = "link" // an address Gentry does not visit
)

// Artifact is an artifact of a task. The copy of a file lies in the state
// directory by the name of the artifact; the store does not keep its path.
type Artifact struct {
	Name   string
	Kind   string // ArtifactFile or ArtifactLink
	URL    string // the address of a link
	Source string
	Saved  time.Time
}

// SaveArtifact records an artifact of a task, now, in place of one of the
// same name; replaced tells whether there was one.
func (t *Tx) SaveArtifact(task int64, a Artifact) (saved Artifact, replaced bool, err error) {
	if _, replaced, err = artifact(t.tx, task, a.Name); err != nil {
		return Artifact{}, false, err
	}
	a.Saved = now()
	_, err = t.tx.Exec(`INSERT INTO artifacts (task, name, kind, url, source, saved) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (task, name) DO UPDATE SET kind = excluded.kind, url = excluded.url, source = excluded.source,
			saved = excluded.saved`,
		task, a.Name, a.Kind, nullable(a.URL), a.Source, a.Saved.UTC().Format(TimeFormat))
	return a, replaced, err
}

// Artifact returns the artifact of a task by name.
func (t *Tx) Artifact(task int64, name string) (Artifact, bool, error) {
	return artifact(t.tx, task, name)
}

// Artifacts returns the artifacts of a task by name; none for an older store.
func (s *Store) Artifacts(task int64) ([]Artifact, error) {
	if s.schema < pathSchema {
		return nil, nil
	}
	var as []Artifact
	err := s.retry(func() error {
		var err error
		as, err = artifacts(s.db, `WHERE task = ?`, task)
		return err
	})
	if err != nil {
		return nil, s.unavailable(err)
	}
	return as, nil
}

func artifact(q querier, task int64, name string) (Artifact, bool, error) {
	as, err := artifacts(q, `WHERE task = ? AND name = ?`, task, name)
	if err != nil || len(as) == 0 {
		return Artifact{}, false, err
	}
	return as[0], true, nil
}

func artifacts(q querier, where string, args ...any) ([]Artifact, error) {
	rows, err := q.Query(`SELECT name, kind, coalesce(url, ''), source, saved FROM artifacts `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var as []Artifact
	for rows.Next() {
		var a Artifact
		var saved string
		if err := rows.Scan(&a.Name, &a.Kind, &a.URL, &a.Source, &saved); err != nil {
			return nil, err
		}
		if a.Saved, err = time.Parse(TimeFormat, saved); err != nil {
			return nil, err
		}
		as = append(as, a)
	}
	return as, rows.Err()
}
