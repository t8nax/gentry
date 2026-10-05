package state

import (
	"fmt"
	"testing"
)

func addShop(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Write(func(tx *Tx) error {
		return tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationFromSchema2(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:2]
	s := mustOpen(t, path)
	addShop(t, s)
	s.Close()
	migrations = orig

	// A reader does not migrate: an older store has no flow yet.
	r, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := r.LatestFlow("shop"); ok || err != nil {
		t.Errorf("flow in schema 2: %v, %v", ok, err)
	}
	if _, ok, err := r.FlowDraft("shop"); ok || err != nil {
		t.Errorf("draft in schema 2: %v, %v", ok, err)
	}
	r.Close()

	s = mustOpen(t, path)
	if v, _ := schemaVersion(s.db); v != 3 {
		t.Fatalf("schema %d, want 3", v)
	}
	if ps, _ := s.Projects(); len(ps) != 1 {
		t.Errorf("projects lost in migration: %+v", ps)
	}
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowDraft("shop", 0) }); err != nil {
		t.Errorf("draft after migration: %v", err)
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-2.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	c.Close()
}

func TestFlowVersionsAndDrafts(t *testing.T) {
	s := mustOpen(t, tempPath(t))
	addShop(t, s)
	if _, ok, err := s.LatestFlow("shop"); ok || err != nil {
		t.Fatalf("flow of a new project: %v, %v", ok, err)
	}

	// A draft of a project without a flow has no base.
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowDraft("shop", 0) }); err != nil {
		t.Fatal(err)
	}
	d, ok, err := s.FlowDraft("shop")
	if !ok || err != nil || d.BaseVersion != 0 || d.Created.IsZero() {
		t.Fatalf("draft: %+v, %v, %v", d, ok, err)
	}
	// One draft per project.
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowDraft("shop", 0) }); err == nil {
		t.Error("second draft accepted")
	}

	err = s.Write(func(tx *Tx) error {
		if err := tx.AddFlowVersion("shop", 1, []byte(`{"files":{}}`)); err != nil {
			return err
		}
		if err := tx.DeleteFlowDraft("shop"); err != nil {
			return err
		}
		return tx.AddFlowDraft("shop", 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowVersion("shop", 2, []byte(`{"files":{"a":"b"}}`)) }); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.LatestFlow("shop")
	if !ok || err != nil || v.Version != 2 || string(v.Content) != `{"files":{"a":"b"}}` || v.Applied.IsZero() {
		t.Errorf("latest: %+v, %v, %v", v, ok, err)
	}
	if d, _, _ := s.FlowDraft("shop"); d.BaseVersion != 1 {
		t.Errorf("draft base %d, want 1", d.BaseVersion)
	}
	// Versions are numbered once.
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowVersion("shop", 2, []byte(`{}`)) }); err == nil {
		t.Error("version 2 recorded twice")
	}
	// Only connected projects have flows.
	if err := s.Write(func(tx *Tx) error { return tx.AddFlowDraft("cart", 0) }); err == nil {
		t.Error("draft of a project that is not connected")
	}

	err = s.Write(func(tx *Tx) error {
		if _, ok, err := tx.FlowDraft("shop"); !ok || err != nil {
			return fmt.Errorf("draft in a transaction: %v, %v", ok, err)
		}
		if v, ok, err := tx.LatestFlow("shop"); !ok || err != nil || v.Version != 2 {
			return fmt.Errorf("latest in a transaction: %+v, %v, %v", v, ok, err)
		}
		return tx.DeleteFlowDraft("shop")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FlowDraft("shop"); ok {
		t.Error("draft not deleted")
	}
}
