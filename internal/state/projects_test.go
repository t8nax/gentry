package state

import (
	"fmt"
	"reflect"
	"testing"
)

func TestMigrationFromSchema1(t *testing.T) {
	path := tempPath(t)
	orig := migrations
	migrations = orig[:1]
	s := mustOpen(t, path)
	s.Write(func(tx *Tx) error {
		_, err := tx.AddEvent("task.taken", "shop", "", nil)
		return err
	})
	s.Close()
	migrations = orig

	s = mustOpen(t, path)
	if v, _ := schemaVersion(s.db); v != 2 {
		t.Fatalf("schema %d, want 2", v)
	}
	if ps, err := s.Projects(); err != nil || len(ps) != 0 {
		t.Errorf("projects after migration: %+v, %v", ps, err)
	}
	if events, _ := s.Events(0); len(events) != 1 {
		t.Errorf("events lost in migration: %+v", events)
	}
	c, err := OpenRead(fmt.Sprintf("%s.schema-1.bak", path))
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	c.Close()
}

func TestProjects(t *testing.T) {
	s := mustOpen(t, tempPath(t))
	err := s.Write(func(tx *Tx) error {
		for _, p := range []Project{
			{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"},
			{ID: "cart", Prefix: "CART", Knowledge: "/k/cart"},
		} {
			if err := tx.AddProject(p); err != nil {
				return err
			}
		}
		for _, w := range []Worktree{
			{Path: "/dev/shop", Project: "shop", Main: true},
			{Path: "/dev/shop-fix", Project: "shop"},
		} {
			if err := tx.AddWorktree(w); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]string
	for _, p := range ps {
		got = append(got, [2]string{p.ID, p.MainWorktree})
	}
	if want := [][2]string{{"cart", ""}, {"shop", "/dev/shop"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("projects %v, want %v", got, want)
	}

	// The schema guards what the code checks too.
	dup := []func(tx *Tx) error{
		func(tx *Tx) error { return tx.AddProject(Project{ID: "store", Prefix: "SHOP", Knowledge: "/k/store"}) },
		func(tx *Tx) error { return tx.AddProject(Project{ID: "store", Prefix: "STORE", Knowledge: "/k/shop"}) },
		func(tx *Tx) error { return tx.AddWorktree(Worktree{Path: "/dev/shop-2", Project: "shop", Main: true}) },
		func(tx *Tx) error { return tx.AddWorktree(Worktree{Path: "/dev/x", Project: "nope"}) },
	}
	for i, fn := range dup {
		if err := s.Write(fn); err == nil {
			t.Errorf("write %d: want a constraint error", i)
		}
	}
	var ws []Worktree
	s.Write(func(tx *Tx) error {
		ws, err = tx.Worktrees()
		return err
	})
	if len(ws) != 2 || !ws[0].Main || ws[0].Added.IsZero() {
		t.Errorf("worktrees %+v", ws)
	}
}
