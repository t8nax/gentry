package state

import (
	"database/sql"
	"errors"
	"time"
)

// flowSchema is the first schema with flow versions and drafts. A store opened
// for reading may be older: it has neither.
const flowSchema = 3

// FlowVersion is a version of the flow of a project.
type FlowVersion struct {
	Project string
	Version int    // 1, 2, 3 within the project
	Content []byte // the snapshot of the flow, a JSON object
	Applied time.Time
}

// FlowDraft is the open flow draft of a project. Its files are in the process
// directory; the store holds only that it exists and what it was made from.
type FlowDraft struct {
	Project     string
	BaseVersion int // the version it was made from; 0 if the project had no flow
	Created     time.Time
}

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// LatestFlow returns the latest flow version of project, the active flow; ok
// is false if the project has none.
func (t *Tx) LatestFlow(project string) (v FlowVersion, ok bool, err error) {
	return latestFlow(t.tx, project)
}

// FlowVersion returns version of the flow of project; ok is false if there
// is no such version.
func (t *Tx) FlowVersion(project string, version int) (v FlowVersion, ok bool, err error) {
	v = FlowVersion{Project: project, Version: version}
	var content, applied string
	err = t.tx.QueryRow(`SELECT content, applied FROM flow_versions WHERE project = ? AND version = ?`,
		project, version).Scan(&content, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowVersion{}, false, nil
	}
	if err != nil {
		return FlowVersion{}, false, err
	}
	if v.Applied, err = time.Parse(TimeFormat, applied); err != nil {
		return FlowVersion{}, false, err
	}
	v.Content = []byte(content)
	return v, true, nil
}

// FlowDraft returns the open flow draft of project; ok is false if there is
// none.
func (t *Tx) FlowDraft(project string) (d FlowDraft, ok bool, err error) {
	return flowDraft(t.tx, project)
}

// AddFlowDraft records that project has a draft made from version base, now;
// base is 0 if the project has no flow.
func (t *Tx) AddFlowDraft(project string, base int) error {
	var b any
	if base > 0 {
		b = base
	}
	_, err := t.tx.Exec(`INSERT INTO flow_drafts (project, base_version, created) VALUES (?, ?, ?)`,
		project, b, now().UTC().Format(TimeFormat))
	return err
}

// DeleteFlowDraft removes the record of the draft of project.
func (t *Tx) DeleteFlowDraft(project string) error {
	_, err := t.tx.Exec(`DELETE FROM flow_drafts WHERE project = ?`, project)
	return err
}

// AddFlowVersion records version of the flow of project with the snapshot
// content, applied now.
func (t *Tx) AddFlowVersion(project string, version int, content []byte) error {
	_, err := t.tx.Exec(`INSERT INTO flow_versions (project, version, content, applied) VALUES (?, ?, ?, ?)`,
		project, version, string(content), now().UTC().Format(TimeFormat))
	return err
}

// LatestFlow returns the latest flow version of project; ok is false if the
// project has none.
func (s *Store) LatestFlow(project string) (v FlowVersion, ok bool, err error) {
	if s.schema < flowSchema {
		return FlowVersion{}, false, nil
	}
	err = s.retry(func() error {
		var err error
		v, ok, err = latestFlow(s.db, project)
		return err
	})
	if err != nil {
		return FlowVersion{}, false, s.unavailable(err)
	}
	return v, ok, nil
}

// FlowDraft returns the open flow draft of project; ok is false if there is
// none.
func (s *Store) FlowDraft(project string) (d FlowDraft, ok bool, err error) {
	if s.schema < flowSchema {
		return FlowDraft{}, false, nil
	}
	err = s.retry(func() error {
		var err error
		d, ok, err = flowDraft(s.db, project)
		return err
	})
	if err != nil {
		return FlowDraft{}, false, s.unavailable(err)
	}
	return d, ok, nil
}

func latestFlow(q rowQuerier, project string) (FlowVersion, bool, error) {
	v := FlowVersion{Project: project}
	var content, applied string
	err := q.QueryRow(`SELECT version, content, applied FROM flow_versions
		WHERE project = ? ORDER BY version DESC LIMIT 1`, project).Scan(&v.Version, &content, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowVersion{}, false, nil
	}
	if err != nil {
		return FlowVersion{}, false, err
	}
	if v.Applied, err = time.Parse(TimeFormat, applied); err != nil {
		return FlowVersion{}, false, err
	}
	v.Content = []byte(content)
	return v, true, nil
}

func flowDraft(q rowQuerier, project string) (FlowDraft, bool, error) {
	d := FlowDraft{Project: project}
	var base sql.NullInt64
	var created string
	err := q.QueryRow(`SELECT base_version, created FROM flow_drafts WHERE project = ?`, project).Scan(&base, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowDraft{}, false, nil
	}
	if err != nil {
		return FlowDraft{}, false, err
	}
	if d.Created, err = time.Parse(TimeFormat, created); err != nil {
		return FlowDraft{}, false, err
	}
	d.BaseVersion = int(base.Int64)
	return d, true, nil
}
