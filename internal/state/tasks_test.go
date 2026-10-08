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

// TestEndTask checks that a task closed or cancelled releases its worktree
// and keeps who ended it, when and why.
func TestEndTask(t *testing.T) {
	s := shopStore(t)
	task, err := take(t, s, "/work/shop-2")
	if err != nil {
		t.Fatal(err)
	}
	var ended Task
	if err := s.Write(func(tx *Tx) error {
		var err error
		ended, err = tx.EndTask(task.ID, TaskCancelled, SourceAgent, "Отложено.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ended.State != TaskCancelled || ended.Worktree != "" || ended.Ended.IsZero() || ended.EndedSource != SourceAgent ||
		ended.Reason != "Отложено." || !ended.IsEnded() || ended.Attempt != 1 {
		t.Errorf("cancelled task %+v", ended)
	}
	// The worktree is free for the next task.
	if _, err := take(t, s, "/work/shop-2"); err != nil {
		t.Errorf("take in the released worktree: %v", err)
	}
}

// TestAttempts checks that the attempts of a number are kept apart and in
// order, and that an attempt is not recorded twice.
func TestAttempts(t *testing.T) {
	s := shopStore(t)
	first, err := take(t, s, "/work/shop-2")
	if err != nil {
		t.Fatal(err)
	}
	again := func() error {
		return s.Write(func(tx *Tx) error {
			_, err := tx.AddTask(Task{Project: "shop", Number: first.Number, Attempt: 2, Title: first.Title, Statement: first.Statement,
				Source: first.Source, State: TaskActive, Scenario: "bug", Node: "branch", FlowCommit: "c1", Worktree: "/work/shop"})
			return err
		})
	}
	if err := s.Write(func(tx *Tx) error {
		_, err := tx.EndTask(first.ID, TaskCancelled, SourceOperator, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := again(); err != nil {
		t.Fatal(err)
	}
	if err := again(); err == nil {
		t.Error("an attempt recorded twice")
	}
	var attempts []Task
	if err := s.Write(func(tx *Tx) error {
		var err error
		attempts, err = tx.Attempts("shop", first.Number)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Attempt != 1 || attempts[0].State != TaskCancelled || attempts[0].Reason != "" ||
		attempts[1].Attempt != 2 || attempts[1].Key() != "SHOP-1" || !attempts[1].Ended.IsZero() {
		t.Errorf("attempts %+v", attempts)
	}
	ts, err := s.Tasks()
	if err != nil || len(ts) != 2 || ts[1].Attempt != 2 {
		t.Errorf("tasks %+v, %v", ts, err)
	}
}

// TestTaskLookup checks that a task is found by its number and by its
// worktree among thousands, without reading them all.
func TestTaskLookup(t *testing.T) {
	s := shopStore(t)
	if err := s.Write(func(tx *Tx) error {
		for range 3000 {
			n, err := tx.TakeNumber("shop")
			if err != nil {
				return err
			}
			if _, err := tx.AddTask(Task{Project: "shop", Number: n, Title: "Возврат", Statement: "Текст", Source: SourceOperator,
				State: TaskClosed, Scenario: "feature", Node: "finish", FlowCommit: "c1"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	held, err := take(t, s, "/work/shop-2")
	if err != nil {
		t.Fatal(err)
	}
	if ts, err := s.TaskAttempts("SHOP", 1500); err != nil || len(ts) != 1 || ts[0].Key() != "SHOP-1500" {
		t.Errorf("SHOP-1500: %+v, %v", ts, err)
	}
	if ts, err := s.TaskAttempts("SHOP", 9999); err != nil || len(ts) != 0 {
		t.Errorf("SHOP-9999: %+v, %v", ts, err)
	}
	if in, ok, err := s.TaskIn("/work/shop-2"); err != nil || !ok || in.ID != held.ID {
		t.Errorf("task in /work/shop-2: %+v, %v, %v", in, ok, err)
	}
	if _, ok, err := s.TaskIn("/work/shop"); err != nil || ok {
		t.Errorf("task in the free /work/shop: %v, %v", ok, err)
	}
	if got, ok, err := s.Task(held.ID); err != nil || !ok || got.Key() != "SHOP-3001" {
		t.Errorf("task of row %d: %+v, %v, %v", held.ID, got, ok, err)
	}
}
