package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// The test binary is the command of checks of the tests: helperEnv names
// what it does, spansEnv and markerEnv name its files. The command inherits
// the environment of Gentry, which inherits that of the test.
const (
	helperEnv = "GENTRY_MAIN_HELPER"
	spansEnv  = "GENTRY_MAIN_SPANS"
	markerEnv = "GENTRY_MAIN_MARKER"
)

// checkHelper does what mode names and returns the exit code:
//   - span: writes "start" to the file of spansEnv, sleeps 2 seconds and
//     writes "end";
//   - tree: writes "start" to the file of spansEnv, starts a child that
//     writes the file of markerEnv after 3 seconds, then sleeps 10 seconds;
//   - marker: sleeps 3 seconds and writes the file of markerEnv.
func checkHelper(mode string) int {
	switch mode {
	case "span":
		appendLine(os.Getenv(spansEnv), "start")
		time.Sleep(2 * time.Second)
		appendLine(os.Getenv(spansEnv), "end")
		return 0
	case "tree":
		appendLine(os.Getenv(spansEnv), "start")
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=marker")
		if err := child.Start(); err != nil {
			return 9
		}
		time.Sleep(10 * time.Second)
		return 0
	case "marker":
		time.Sleep(3 * time.Second)
		os.WriteFile(os.Getenv(markerEnv), nil, 0o644)
		return 0
	}
	return 99
}

// appendLine appends a line to the file at path.
func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// checksShop returns the main worktree of a shop whose stage branch has the
// test binary as its command of checks, with a task taken in it and the step
// of the stage done. The command does what mode names; its files are in a
// temporary directory, returned too.
func checksShop(t *testing.T, bin, mode string) (shop, files string) {
	t.Helper()
	shop = shopWithFlow(t, bin, 1)[0]
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(os.Getenv(home.EnvVar), "process", "shop", "flow", "stages", "branch.yaml")
	f, err := os.OpenFile(stage, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, "checks:\n  - '\"%s\"'\n", exe)
	f.Close()
	gentry(t, bin, shop, "flow", "apply")
	gentry(t, bin, shop, "task", "take", "--scenario", "feature", "--title", "Проверки", "--statement", "Постановка.")
	tool(t, bin, shop, "step_add", map[string]any{"steps": []string{"Создать ветку"}})
	tool(t, bin, shop, "step_done", map[string]any{"step": 1})
	files = t.TempDir()
	t.Setenv(helperEnv, mode)
	t.Setenv(spansEnv, filepath.Join(files, "spans"))
	t.Setenv(markerEnv, filepath.Join(files, "marker"))
	return shop, files
}

// send sends a call of a tool, with meta if it is given, and returns the id
// of its request.
func (s *server) send(t *testing.T, name string, args any, meta map[string]any) int {
	t.Helper()
	s.id++
	params := map[string]any{"name": name, "arguments": args}
	if meta != nil {
		params["_meta"] = meta
	}
	s.write(t, map[string]any{"jsonrpc": "2.0", "id": s.id, "method": "tools/call", "params": params})
	return s.id
}

// write writes a message to the server.
func (s *server) write(t *testing.T, m any) {
	t.Helper()
	b, _ := json.Marshal(m)
	if _, err := fmt.Fprintf(s.in, "%s\n", b); err != nil {
		t.Fatal(err)
	}
}

// answer reads the messages of the server up to the answer of request id
// and returns its text, whether it failed and the notifications before it.
func (s *server) answer(t *testing.T, id int) (text string, failed bool, notes []map[string]any) {
	t.Helper()
	for s.out.Scan() {
		var m map[string]any
		if err := json.Unmarshal(s.out.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %s", s.out.Bytes())
		}
		if m["id"] != float64(id) {
			notes = append(notes, m)
			continue
		}
		r, ok := m["result"].(map[string]any)
		if !ok {
			t.Fatalf("request %d: %s", id, s.out.Bytes())
		}
		return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool), notes
	}
	t.Fatalf("request %d: no answer: %v", id, s.out.Err())
	return "", false, nil
}

// waitFile waits up to 10 seconds for the file at path to be made.
func waitFile(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("no file %s", path)
}

// read returns the text of the file at path with line ends of \n, or "" if
// there is none.
func read(path string) string {
	b, _ := os.ReadFile(path)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// pathNodes returns the passes of the task in the worktree shop as
// node:outcome.
func pathNodes(t *testing.T, bin, shop string) string {
	t.Helper()
	var shown struct {
		Path []struct {
			Node    string `json:"node"`
			Outcome string `json:"outcome"`
		} `json:"path"`
	}
	out := gentry(t, bin, shop, "task", "show", "--json")
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("task show: %s", out)
	}
	var nodes []string
	for _, p := range shown.Path {
		nodes = append(nodes, p.Node+":"+p.Outcome)
	}
	return strings.Join(nodes, " ")
}

// TestStageExitChecksAtOnce checks that of two processes closing a stage
// with checks at once one runs the commands and closes the stage, and the
// other waits for it and then finds the next stage, without steps: the
// commands of a task run one at a time.
func TestStageExitChecksAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop, files := checksShop(t, bin, "span")
	exit := map[string]any{"kind": "result", "text": "Ветка"}
	closed := 0
	for _, r := range toolsAtOnce(t, bin, shop, "stage_exit", []any{exit, exit}) {
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
	if got := read(filepath.Join(files, "spans")); got != "start\nend\n" {
		t.Errorf("the commands ran: %q, want once", got)
	}
}

// TestStageSkipDuringChecks checks that a stage skipped by another process
// while its commands of checks run is not closed by the exit again: the exit
// starts anew at the next stage, which has no steps.
func TestStageSkipDuringChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop, files := checksShop(t, bin, "span")
	s := startServer(t, bin, shop)
	id := s.send(t, "stage_exit", map[string]any{"kind": "result", "text": "Ветка"}, nil)
	waitFile(t, filepath.Join(files, "spans"))
	tool(t, bin, shop, "stage_skip", map[string]any{"reason": "Ветка есть"})
	text, failed, _ := s.answer(t, id)
	if !failed || !strings.Contains(text, msg.Text(msg.ErrStepsEmpty, "План фичи")) {
		t.Errorf("the exit: failed %v:\n%s", failed, text)
	}
	if got := pathNodes(t, bin, shop); got != "branch:skip plan:" {
		t.Errorf("path: %s", got)
	}
}

// TestTaskCancelDuringChecks checks that the cancel of a task ends its
// command of checks that runs, with the process it started, which leaves no
// marker, and that the exit is refused as the task is cancelled.
func TestTaskCancelDuringChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent processes are skipped in short mode")
	}
	bin := buildGentry(t)
	shop, files := checksShop(t, bin, "tree")
	s := startServer(t, bin, shop)
	id := s.send(t, "stage_exit", map[string]any{"kind": "result", "text": "Ветка"}, nil)
	waitFile(t, filepath.Join(files, "spans"))
	start := time.Now()
	gentry(t, bin, shop, "task", "cancel", "--reason", "Отмена")
	text, failed, _ := s.answer(t, id)
	if first, _, _ := strings.Cut(text, "\n"); !failed || first != msg.Text(msg.TaskCancelled, "SHOP-1") {
		t.Errorf("the exit: failed %v:\n%s", failed, text)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the exit was answered %v after the cancel", d)
	}
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(filepath.Join(files, "marker")); err == nil {
		t.Error("the child of the command was not ended")
	}
}

// TestStageExitProgress checks that a call of stage_exit with a progress
// token is told of the progress of its commands of checks before its answer.
func TestStageExitProgress(t *testing.T) {
	bin := buildGentry(t)
	shop, _ := checksShop(t, bin, "span")
	s := startServer(t, bin, shop)
	id := s.send(t, "stage_exit", map[string]any{"kind": "result", "text": "Ветка"}, map[string]any{"progressToken": "p1"})
	text, failed, notes := s.answer(t, id)
	if failed || !strings.Contains(text, msg.Text(msg.ChecksPassedLine, 1)) {
		t.Errorf("the exit: failed %v:\n%s", failed, text)
	}
	progress := 0
	for _, n := range notes {
		if p, ok := n["params"].(map[string]any); ok && n["method"] == "notifications/progress" && p["progressToken"] == "p1" {
			progress++
		}
	}
	if progress == 0 {
		t.Errorf("no notification of progress: %v", notes)
	}
}

// TestStageExitCancelled checks that a call of stage_exit cancelled by the
// client while its command of checks runs gets no answer, ends the command
// with the process it started, which leaves no marker, and does not close
// the stage.
func TestStageExitCancelled(t *testing.T) {
	bin := buildGentry(t)
	shop, files := checksShop(t, bin, "tree")
	s := startServer(t, bin, shop)
	id := s.send(t, "stage_exit", map[string]any{"kind": "result", "text": "Ветка"}, nil)
	waitFile(t, filepath.Join(files, "spans"))
	s.write(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
	start := time.Now()
	shown := s.send(t, "stage_show", map[string]any{}, nil)
	if _, failed, notes := s.answer(t, shown); failed || len(notes) > 0 {
		t.Errorf("stage_show after the cancel: failed %v, messages before it %v", failed, notes)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("stage_show was answered %v after the cancel", d)
	}
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(filepath.Join(files, "marker")); err == nil {
		t.Error("the child of the command was not ended")
	}
	if got := pathNodes(t, bin, shop); got != "branch:" {
		t.Errorf("path: %s", got)
	}
}
