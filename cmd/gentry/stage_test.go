package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// atOnce starts a process of the binary in dir for each command line at once
// and waits for all of them.
func atOnce(t *testing.T, bin, dir string, commands [][]string) []result {
	t.Helper()
	results := make([]result, len(commands))
	cmds := make([]*exec.Cmd, len(commands))
	outs := make([]strings.Builder, len(commands))
	for i, args := range commands {
		cmds[i] = exec.Command(bin, args...)
		cmds[i].Dir = dir
		cmds[i].Stdout = &outs[i]
	}
	var wg sync.WaitGroup
	for i, c := range cmds {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Wait()
			results[i] = result{code: c.ProcessState.ExitCode(), stdout: outs[i].String()}
		}()
	}
	wg.Wait()
	return results
}

// takenShop returns the main worktree of a shop with the flow of the flow
// testdata and a task taken in it, at its first stage.
func takenShop(t *testing.T, bin string) string {
	t.Helper()
	shop := shopWithFlow(t, bin, 1)[0]
	gentry(t, bin, shop, "task", "take", "--scenario", "feature", "--title", "Возврат", "--statement", "Постановка.")
	return shop
}

// TestCloseStageAtOnce checks that of several processes closing one stage
// at once exactly one does: the others find the next stage, without steps.
func TestCloseStageAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop := takenShop(t, bin)
	tool(t, bin, shop, "step_add", map[string]any{"steps": []string{"Создать ветку"}})
	tool(t, bin, shop, "step_done", map[string]any{"step": 1})
	args := make([]any, writers)
	for i := range args {
		args[i] = map[string]any{"kind": "result", "text": fmt.Sprintf("Ветка %d", i)}
	}
	closed := 0
	for _, r := range toolsAtOnce(t, bin, shop, "stage_exit", args) {
		switch {
		case !r.failed:
			closed++
		case !strings.Contains(r.text, msg.Text(msg.ErrStepsEmpty, "План фичи")):
			t.Errorf("%s; want no steps", r.text)
		}
	}
	if closed != 1 {
		t.Errorf("%d processes closed the stage, want 1", closed)
	}
	var shown struct {
		Path []struct {
			Node string `json:"node"`
		} `json:"path"`
	}
	out := gentry(t, bin, shop, "task", "show", "--json")
	if err := json.Unmarshal([]byte(out), &shown); err != nil || len(shown.Path) != 2 || shown.Path[1].Node != "plan" {
		t.Errorf("path after the closing: %s", out)
	}
}

// TestAddStepsAtOnce checks that processes adding steps at once all do,
// with the numbers 1 to n without repeats.
func TestAddStepsAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop := takenShop(t, bin)
	args := make([]any, writers)
	for i := range args {
		args[i] = map[string]any{"steps": []string{fmt.Sprintf("Шаг %d", i)}}
	}
	for _, r := range toolsAtOnce(t, bin, shop, "step_add", args) {
		if r.failed {
			t.Errorf("step_add: %s", r.text)
		}
	}
	var shown struct {
		Path []struct {
			Steps []struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
			} `json:"steps"`
		} `json:"path"`
	}
	out := gentry(t, bin, shop, "task", "show", "--json")
	if err := json.Unmarshal([]byte(out), &shown); err != nil || len(shown.Path) != 1 {
		t.Fatalf("task show: %s", out)
	}
	var numbers, titles []int
	for _, s := range shown.Path[0].Steps {
		numbers = append(numbers, s.Number)
		var n int
		fmt.Sscanf(s.Title, "Шаг %d", &n)
		titles = append(titles, n)
	}
	slices.Sort(titles)
	var want []int
	for i := range writers {
		want = append(want, i)
	}
	if !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) || !slices.Equal(titles, want) {
		t.Errorf("numbers %v, steps %v", numbers, titles)
	}
}

// TestRecordDecisionsAtOnce checks that processes recording decisions of the
// operator at once all do, with the numbers 1 to n without repeats.
func TestRecordDecisionsAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop := takenShop(t, bin)
	commands := make([][]string, writers)
	for i := range commands {
		commands[i] = []string{"operator", "record", "--answer", fmt.Sprintf("Решение %d", i), "--json"}
	}
	for _, r := range atOnce(t, bin, shop, commands) {
		if r.code != 0 {
			t.Errorf("exit code %d, output %s", r.code, r.stdout)
		}
	}
	var shown struct {
		Decisions []struct {
			Number int    `json:"number"`
			Answer string `json:"answer"`
		} `json:"operator_decisions"`
	}
	out := gentry(t, bin, shop, "task", "show", "--json")
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("task show: %s", out)
	}
	var numbers, answers []int
	for _, d := range shown.Decisions {
		numbers = append(numbers, d.Number)
		var n int
		fmt.Sscanf(d.Answer, "Решение %d", &n)
		answers = append(answers, n)
	}
	slices.Sort(answers)
	var want []int
	for i := range writers {
		want = append(want, i)
	}
	if !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) || !slices.Equal(answers, want) {
		t.Errorf("numbers %v, decisions %v", numbers, answers)
	}
}
