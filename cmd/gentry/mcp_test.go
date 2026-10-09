package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
)

// server is a gentry mcp process, as a session of the agent starts it.
type server struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Scanner
	id  int
}

// startServer starts gentry mcp in the worktree dir.
func startServer(t *testing.T, bin, dir string) *server {
	t.Helper()
	cmd := exec.Command(bin, "mcp")
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &server{cmd: cmd, in: in, out: bufio.NewScanner(out)}
	s.out.Buffer(make([]byte, 64*1024), 16<<20)
	t.Cleanup(func() {
		in.Close()
		cmd.Wait()
	})
	return s
}

// request sends a request and returns its result.
func (s *server) request(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	s.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.id, "method": method, "params": params})
	if _, err := fmt.Fprintf(s.in, "%s\n", b); err != nil {
		t.Fatal(err)
	}
	if !s.out.Scan() {
		t.Fatalf("%s: no response: %v", method, s.out.Err())
	}
	var r struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(s.out.Bytes(), &r); err != nil || r.Error != nil {
		t.Fatalf("%s: %s", method, s.out.Bytes())
	}
	return r.Result
}

// call calls a tool and returns its text and whether it failed.
func (s *server) call(t *testing.T, tool string, args any) (string, bool) {
	t.Helper()
	r := s.request(t, "tools/call", map[string]any{"name": tool, "arguments": args})
	return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool)
}

// tool calls a tool in the worktree dir through a server of its own; the
// call must succeed.
func tool(t *testing.T, bin, dir, name string, args any) string {
	t.Helper()
	s := startServer(t, bin, dir)
	text, failed := s.call(t, name, args)
	if failed {
		t.Fatalf("%s %v: %s", name, args, text)
	}
	return text
}

// toolResult is the outcome of one call of toolsAtOnce.
type toolResult struct {
	text   string
	failed bool
}

// toolsAtOnce calls the tool name in the worktree dir with each of args at
// once, each through a server of its own, as sessions do.
func toolsAtOnce(t *testing.T, bin, dir, name string, args []any) []toolResult {
	t.Helper()
	servers := make([]*server, len(args))
	for i := range args {
		servers[i] = startServer(t, bin, dir)
		servers[i].request(t, "ping", nil)
	}
	results := make([]toolResult, len(args))
	var wg sync.WaitGroup
	for i, a := range args {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, failed := servers[i].call(t, name, a)
			results[i] = toolResult{text, failed}
		}()
	}
	wg.Wait()
	return results
}

// TestMCPServer serves the tools of the agent from the binary: the task is
// the task of the worktree the server runs in, and the records are the
// agent's.
func TestMCPServer(t *testing.T) {
	bin := buildGentry(t)
	shop := shopWithFlow(t, bin, 1)[0]
	s := startServer(t, bin, shop)
	if r := s.request(t, "initialize", map[string]any{"protocolVersion": "2025-11-25"}); r["protocolVersion"] != "2025-11-25" {
		t.Errorf("initialize: %v", r)
	}
	tools := s.request(t, "tools/list", nil)["tools"].([]any)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	for _, want := range []string{"task_take", "stage_show", "step_add", "stage_exit", "note_add", "operator_record", "flow_apply", "setup"} {
		if !slices.Contains(names, want) {
			t.Errorf("no tool %s: %v", want, names)
		}
	}
	if text, failed := s.call(t, "task_take", map[string]any{"scenario": "feature", "title": "Корзина", "statement": "Скидка по промокоду."}); failed ||
		!strings.Contains(text, "Постановка записана: агентом со слов оператора") {
		t.Fatalf("task_take: failed %v:\n%s", failed, text)
	}
	if text, failed := s.call(t, "stage_exit", map[string]any{"kind": "result", "text": "Готово"}); !failed || !strings.Contains(text, "step_add (steps)") {
		t.Errorf("stage_exit without steps: failed %v:\n%s", failed, text)
	}
	note := "Промокод \"SALE-10\" — с кавычками,\nпереносом и $0 `корзиной`"
	if _, failed := s.call(t, "note_add", map[string]any{"text": note}); failed {
		t.Fatal("note_add failed")
	}
	var notes struct {
		Notes []struct{ Text, Source string } `json:"notes"`
	}
	json.Unmarshal([]byte(gentry(t, bin, shop, "note", "list", "--json")), &notes)
	if len(notes.Notes) != 1 || notes.Notes[0].Text != note || notes.Notes[0].Source != "agent" {
		t.Errorf("notes: %+v", notes.Notes)
	}
}

// TestMCPServersAtOnce checks that several servers, long-lived as sessions
// are, and the command line write to the state store at once.
func TestMCPServersAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	worktrees := shopWithFlow(t, bin, 2)
	for i, w := range worktrees {
		gentry(t, bin, w, "task", "take", "--scenario", "feature", "--title", fmt.Sprintf("Задача %d", i), "--statement", "Постановка.")
	}
	const calls = 20
	// Two servers on one task, as a session and its subagent, one on the
	// other task, and the command line.
	dirs := []string{worktrees[0], worktrees[0], worktrees[1]}
	var wg sync.WaitGroup
	failures := make(chan string, 4*calls)
	for i, dir := range dirs {
		s := startServer(t, bin, dir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range calls {
				if text, failed := s.call(t, "note_add", map[string]any{"text": fmt.Sprintf("s%d-%d", i, j)}); failed {
					failures <- text
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := range calls {
			cmd := exec.Command(bin, "note", "add", fmt.Sprintf("cli-%d", j))
			cmd.Dir = worktrees[0]
			if out, err := cmd.CombinedOutput(); err != nil {
				failures <- string(out)
			}
		}
	}()
	wg.Wait()
	close(failures)
	for f := range failures {
		t.Errorf("a write failed: %s", f)
	}
	for i, want := range []int{3 * calls, calls} {
		var notes struct {
			Notes []struct {
				Number int    `json:"number"`
				Source string `json:"source"`
			} `json:"notes"`
		}
		json.Unmarshal([]byte(gentry(t, bin, worktrees[i], "note", "list", "--json")), &notes)
		numbers := map[int]bool{}
		agent := 0
		for _, n := range notes.Notes {
			numbers[n.Number] = true
			if n.Source == "agent" {
				agent++
			}
		}
		if len(notes.Notes) != want || len(numbers) != want {
			t.Errorf("task %d: %d notes, %d numbers, want %d", i+1, len(notes.Notes), len(numbers), want)
		}
		if wantAgent := want - calls*(1-i); agent != wantAgent {
			t.Errorf("task %d: %d notes of the agent, want %d", i+1, agent, wantAgent)
		}
	}
}
