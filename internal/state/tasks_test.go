package state

import (
	"testing"
	"time"
)

// shopStore returns a store with the shop connected, two worktrees in its
// pool and a snapshot of its flow.
func shopStore(t *testing.T) *Store {
	t.Helper()
	s := mustOpen(t, tempPath(t))
	err := s.Write(func(tx *Tx) error {
		if err := tx.AddProject(Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"}); err != nil {
			return err
		}
		for _, w := range []Worktree{{Path: "/work/shop", Project: "shop", Main: true}, {Path: "/work/shop-2", Project: "shop"}} {
			if err := tx.AddWorktree(w); err != nil {
				return err
			}
		}
		applied := time.Date(2026, 10, 6, 11, 5, 0, 0, time.UTC)
		return tx.AddSnapshot(FlowSnapshot{Project: "shop", Commit: "c1", Applied: applied, Content: `{"files":{}}`})
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// take records a task of the shop in worktree and returns it.
func take(t *testing.T, s *Store, worktree string) (Task, error) {
	t.Helper()
	var task Task
	err := s.Write(func(tx *Tx) error {
		n, err := tx.TakeNumber("shop")
		if err != nil {
			return err
		}
		task, err = tx.AddTask(Task{
			Project: "shop", Number: n, Title: "Возврат", Statement: "Текст", Source: SourceOperator,
			State: TaskActive, Scenario: "feature", Node: "branch", FlowCommit: "c1", Worktree: worktree,
		})
		return err
	})
	return task, err
}

func TestTasks(t *testing.T) {
	s := shopStore(t)
	if _, err := take(t, s, "/work/shop-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := take(t, s, "/work/shop"); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Tasks()
	if err != nil || len(ts) != 2 {
		t.Fatalf("tasks %+v, %v", ts, err)
	}
	if k := ts[0].Key() + " " + ts[1].Key(); k != "SHOP-1 SHOP-2" {
		t.Errorf("keys %s, want SHOP-1 SHOP-2", k)
	}
	got := ts[0]
	if got.Worktree != "/work/shop-2" || got.State != TaskActive || got.Source != SourceOperator ||
		got.FlowApplied.Hour() != 11 || got.Taken.IsZero() {
		t.Errorf("task %+v", got)
	}
	var in Task
	var ok bool
	if err := s.Write(func(tx *Tx) error {
		var err error
		in, ok, err = tx.TaskIn("/work/shop")
		return err
	}); err != nil || !ok || in.Key() != "SHOP-2" {
		t.Errorf("task in /work/shop: %+v, %v, %v", in, ok, err)
	}
}

// TestWorktreeHoldsOneTask checks the index that protects a worktree from two
// tasks; the number of the refused task is not used up.
func TestWorktreeHoldsOneTask(t *testing.T) {
	s := shopStore(t)
	if _, err := take(t, s, "/work/shop-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := take(t, s, "/work/shop-2"); err == nil {
		t.Fatal("a second task in a worktree must be refused")
	}
	task, err := take(t, s, "/work/shop")
	if err != nil || task.Number != 2 {
		t.Errorf("next task %+v, %v; want number 2", task, err)
	}
}
