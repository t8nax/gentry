package state

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/flow"
)

// TestMigrationFromSchema3 checks that the flow tables of schema 3 go away
// and the projects stay.
func TestMigrationFromSchema3(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:3]
	s := mustOpen(t, path)
	if err := s.Write(func(tx *Tx) error {
		if err := tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"}); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO flow_drafts (project, created) VALUES ('shop', '2026-10-05T10:00:00.000Z')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	migrations = orig

	s = mustOpen(t, path)
	if v, _ := schemaVersion(s.db); v != SchemaVersion() {
		t.Fatalf("schema %d, want %d", v, SchemaVersion())
	}
	if ps, _ := s.Projects(); len(ps) != 1 {
		t.Errorf("projects lost in migration: %+v", ps)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name IN ('flow_versions', 'flow_drafts')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("flow tables left: %d, %v", n, err)
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-3.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	c.Close()
}

// TestMigrationFromSchema4 checks that a project of schema 4 gets the series
// of numbers from 1 and the store can hold tasks.
func TestMigrationFromSchema4(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:4]
	s := mustOpen(t, path)
	if err := s.Write(func(tx *Tx) error {
		return tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"})
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	migrations = orig

	s = mustOpen(t, path)
	if ps, _ := s.Projects(); len(ps) != 1 {
		t.Errorf("projects lost in migration: %+v", ps)
	}
	var n int
	if err := s.Write(func(tx *Tx) error {
		var err error
		n, err = tx.TakeNumber("shop")
		return err
	}); err != nil || n != 1 {
		t.Errorf("first number %d, %v; want 1", n, err)
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-4.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	// A store of schema 4 opened for reading has no tasks.
	if ts, err := c.Tasks(); err != nil || ts != nil {
		t.Errorf("tasks of schema 4: %v, %v", ts, err)
	}
	c.Close()
}

// TestMigrationFromSchema5 checks that a task under way gets the pass of its
// current node, with the stage of the node from the snapshot of its flow.
func TestMigrationFromSchema5(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:5]
	s := mustOpen(t, path)
	res, err := flow.ReadDir(filepath.Join("..", "flow", "testdata", "process", "shop", "flow"),
		filepath.Join("..", "flow", "testdata", "process", "agents"))
	if err != nil || len(res.Problems) > 0 {
		t.Fatalf("flow of the testdata: %v, %v", err, res.Problems)
	}
	content, err := res.Snapshot.Encode()
	if err != nil {
		t.Fatal(err)
	}
	taken := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if err := s.Write(func(tx *Tx) error {
		if err := tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"}); err != nil {
			return err
		}
		if err := tx.AddWorktree(Worktree{Path: "/work/shop", Project: "shop", Main: true}); err != nil {
			return err
		}
		for _, snap := range []FlowSnapshot{{Commit: "c1", Content: string(content)}, {Commit: "c2", Content: `{"files":{}}`}} {
			snap.Project, snap.Applied = "shop", taken
			if err := tx.AddSnapshot(snap); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO tasks (project, number, title, statement, source, state, scenario, node, flow_commit, worktree, taken)
			VALUES ('shop', 1, 'Возврат', 'Текст', 'operator', 'active', 'feature', 'plan', 'c1', '/work/shop', ?),
			       ('shop', 2, 'Списание', 'Текст', 'operator', 'active', 'bug', 'branch', 'c2', NULL, ?)`,
			taken.Format(TimeFormat), taken.Format(TimeFormat))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Opened for reading, the store of schema 5 shows the first pass of the
	// current node.
	r, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := r.Tasks()
	if ps, err := r.TaskPath(ts[0]); err != nil || len(ps) != 1 || ps[0].Node != "plan" || ps[0].Round != 1 || !ps[0].Current() {
		t.Errorf("path of schema 5: %+v, %v", ps, err)
	}
	r.Close()
	s.Close()
	migrations = orig

	s = mustOpen(t, path)
	ts, err = s.Tasks()
	if err != nil || len(ts) != 2 {
		t.Fatalf("tasks %+v, %v", ts, err)
	}
	for i, want := range []string{"plan-feature", "branch"} {
		ps, err := s.TaskPath(ts[i])
		if err != nil || len(ps) != 1 {
			t.Fatalf("path of %s: %+v, %v", ts[i].Key(), ps, err)
		}
		if p := ps[0]; p.Stage != want || p.Round != 1 || !p.Entered.Equal(taken) || !p.Current() {
			t.Errorf("pass of %s: %+v, want stage %s", ts[i].Key(), p, want)
		}
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-5.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	c.Close()
}

// TestMigrationFromSchema6 checks that a task under way of schema 6 keeps
// its path, gets no decisions of the operator and can take them.
func TestMigrationFromSchema6(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:6]
	s := mustOpen(t, path)
	var task Task
	var pass Pass
	if err := s.Write(func(tx *Tx) error {
		if err := tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"}); err != nil {
			return err
		}
		if err := tx.AddSnapshot(FlowSnapshot{Project: "shop", Commit: "c1", Applied: time.Now(), Content: `{"files":{}}`}); err != nil {
			return err
		}
		var err error
		task, err = tx.AddTask(Task{Project: "shop", Number: 1, Title: "Возврат", Statement: "Текст", Source: SourceOperator,
			State: TaskActive, Scenario: "feature", Node: "review", FlowCommit: "c1"})
		if err != nil {
			return err
		}
		pass, err = tx.AddPass(Pass{Task: task.ID, Node: "review", Stage: "review", Round: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Opened for reading, the store of schema 6 has no decisions.
	r, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if ds, err := r.Decisions(task.ID); err != nil || ds != nil {
		t.Errorf("decisions of schema 6: %+v, %v", ds, err)
	}
	r.Close()
	migrations = orig

	s = mustOpen(t, path)
	if ds, err := s.Decisions(task.ID); err != nil || len(ds) != 0 {
		t.Errorf("decisions after the move: %+v, %v", ds, err)
	}
	if ps, err := s.TaskPath(task); err != nil || len(ps) != 1 || ps[0].Round != 2 {
		t.Errorf("path after the move: %+v, %v", ps, err)
	}
	if err := s.Write(func(tx *Tx) error {
		_, err := tx.AddDecision(task.ID, Decision{Question: "Вернуть?", Options: []Option{{Label: "Да", Recommended: true}, {Label: "Нет"}},
			Answer: "Да.", Pass: pass.ID, AllowReturn: "implementation", Source: SourceAgent})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ds, err := s.Decisions(task.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("decisions %+v, %v", ds, err)
	}
	if d := ds[0]; d.Number != 1 || d.Node != "review" || d.Round != 2 || d.AllowReturn != "implementation" ||
		len(d.Options) != 2 || !d.Options[0].Recommended || d.Options[1].Description != "" || d.Source != SourceAgent {
		t.Errorf("decision %+v", d)
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-6.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	c.Close()
}
