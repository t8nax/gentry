package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// passedShop returns the main worktree of a shop with a task taken in it
// that has passed its scenario by skips.
func passedShop(t *testing.T, bin string) string {
	t.Helper()
	shop := takenShop(t, bin)
	for range 3 {
		gentry(t, bin, shop, "stage", "skip", "--reason", "Не нужен")
	}
	gentry(t, bin, shop, "stage", "skip", "--reason", "Не нужен", "--to", "merge")
	gentry(t, bin, shop, "stage", "exit", "--kind", "result", "--text", "Ветка влита в main")
	return shop
}

// TestEndTaskAtOnce checks that of several processes closing and cancelling
// one task at once exactly one does: the others find it ended.
func TestEndTaskAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop := passedShop(t, bin)
	commands := make([][]string, writers)
	for i := range commands {
		action := "close"
		if i%2 == 1 {
			action = "cancel"
		}
		commands[i] = []string{"task", action, "SHOP-1", "--json"}
	}
	ended := 0
	for _, r := range atOnce(t, bin, shop, commands) {
		switch {
		case r.code == 0:
			ended++
		case r.code != 1 || !strings.Contains(r.stdout, `"code":"task_ended"`):
			t.Errorf("exit code %d, output %s; want task_ended", r.code, r.stdout)
		}
	}
	if ended != 1 {
		t.Errorf("%d processes ended the task, want 1", ended)
	}
	events := gentry(t, bin, shop, "events")
	if n := strings.Count(events, `"type":"task.closed"`) + strings.Count(events, `"type":"task.cancelled"`); n != 1 {
		t.Errorf("%d events of the end, want 1:\n%s", n, events)
	}
}

// TestTakeAgainAtOnce checks that of several processes taking one cancelled
// task anew in different worktrees at once exactly one does: the others find
// it in work, and the task has two attempts.
func TestTakeAgainAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	worktrees := shopWithFlow(t, bin, writers+1)
	gentry(t, bin, worktrees[0], "task", "take", "--scenario", "feature", "--title", "Возврат", "--statement", "Постановка.")
	gentry(t, bin, worktrees[0], "task", "cancel")
	commands := make([][]string, writers)
	for i := range commands {
		commands[i] = []string{"task", "take", "--task", "SHOP-1", "--scenario", "feature", "--worktree", worktrees[i+1], "--json"}
	}
	taken := 0
	for _, r := range atOnce(t, bin, worktrees[0], commands) {
		switch {
		case r.code == 0:
			taken++
		case r.code != 1 || !strings.Contains(r.stdout, `"code":"task_in_work"`):
			t.Errorf("exit code %d, output %s; want task_in_work", r.code, r.stdout)
		}
	}
	if taken != 1 {
		t.Errorf("%d processes took the task anew, want 1", taken)
	}
	var out struct {
		Attempts []struct {
			Attempt int    `json:"attempt"`
			State   string `json:"state"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(gentry(t, bin, worktrees[0], "task", "attempts", "SHOP-1", "--json")), &out); err != nil ||
		len(out.Attempts) != 2 || out.Attempts[1].Attempt != 2 || out.Attempts[1].State != "active" {
		t.Errorf("attempts %+v, %v", out.Attempts, err)
	}
}
