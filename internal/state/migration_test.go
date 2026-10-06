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
	if v, _ := schemaVersion(s.db); v != 4 {
		t.Fatalf("schema %d, want 4", v)
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
