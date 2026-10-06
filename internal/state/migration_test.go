package state

import (
	"fmt"
	"testing"
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
