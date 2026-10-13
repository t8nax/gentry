package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// session runs a server whose tool cart_wait reports its progress every 10
// ms until its call is cancelled or a second passes, and returns the input
// of the server, its messages as they come and the number of calls run.
func session(t *testing.T) (io.WriteCloser, <-chan map[string]any, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	s := Server{Name: "shop", Version: "1.0", Tools: []Tool{{
		Name:        "cart_wait",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Call: func(ctx context.Context, args json.RawMessage, progress func()) (string, bool) {
			calls.Add(1)
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			deadline := time.After(time.Second)
			for {
				select {
				case <-ctx.Done():
					return "cancelled", true
				case <-deadline:
					return "done " + string(args), false
				case <-tick.C:
					progress()
				}
			}
		},
	}}}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(context.Background(), inR, outW)
		outW.Close()
	}()
	msgs := make(chan map[string]any, 1000)
	go func() {
		defer close(msgs)
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("not JSON: %s", sc.Text())
				return
			}
			msgs <- m
		}
	}()
	t.Cleanup(func() {
		inW.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return inW, msgs, calls
}

func send(t *testing.T, w io.Writer, line string) {
	t.Helper()
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

// next returns the next message of the server that is not a notification
// of progress, and the notifications of progress before it.
func next(t *testing.T, msgs <-chan map[string]any) (map[string]any, []map[string]any) {
	t.Helper()
	var progress []map[string]any
	for {
		select {
		case m, ok := <-msgs:
			if !ok {
				t.Fatal("the server ended")
			}
			if m["method"] == "notifications/progress" {
				progress = append(progress, m["params"].(map[string]any))
				continue
			}
			return m, progress
		case <-time.After(5 * time.Second):
			t.Fatal("no message of the server")
		}
	}
}

// TestCancel checks that a call that runs ends when the client cancels it,
// without an answer, and that a call cancelled while it waits for its turn
// does not run; the calls after them are served.
func TestCancel(t *testing.T) {
	in, msgs, calls := session(t)
	start := time.Now()
	send(t, in, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"cart_wait"}}`)
	send(t, in, `{"jsonrpc":"2.0","id":"8","method":"tools/call","params":{"name":"cart_wait"}}`)
	send(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"8","reason":"не нужно"}}`)
	send(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`)
	send(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`)
	send(t, in, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	m, progress := next(t, msgs)
	if m["id"] != 9.0 || len(progress) > 0 {
		t.Errorf("the message after the cancelled calls: %v, progress %v", m, progress)
	}
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("the cancelled calls ended after %v", d)
	}
	// The call 7 may be cancelled before its turn too.
	if n := calls.Load(); n > 1 {
		t.Errorf("%d calls ran: the call cancelled while it waited ran", n)
	}
}

// TestCancelBehindQueue checks that the client cancels a call however many
// requests wait behind it: the input is read on.
func TestCancelBehindQueue(t *testing.T) {
	in, msgs, _ := session(t)
	start := time.Now()
	send(t, in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cart_wait"}}`)
	for i := range 100 {
		send(t, in, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"ping"}`, 100+i))
	}
	send(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	if m, _ := next(t, msgs); m["id"] != 100.0 {
		t.Errorf("the message after the cancelled call: %v", m)
	}
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("the cancelled call ended after %v", d)
	}
}

// TestProgress checks that a call with a progress token tells the client
// that it goes on, with a progress that grows, before its answer, and that a
// call without one does not.
func TestProgress(t *testing.T) {
	in, msgs, _ := session(t)
	send(t, in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cart_wait","arguments":{"a":1},"_meta":{"progressToken":"p1"}}}`)
	m, progress := next(t, msgs)
	if m["id"] != 1.0 || len(progress) < 2 {
		t.Fatalf("answer %v after %d notifications", m, len(progress))
	}
	for i, p := range progress {
		if p["progressToken"] != "p1" || p["progress"] != float64(i+1) {
			t.Errorf("notification %d: %v", i, p)
		}
	}
	send(t, in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"cart_wait"}}`)
	if m, progress := next(t, msgs); m["id"] != 2.0 || len(progress) > 0 {
		t.Errorf("a call without a token: %v, progress %v", m, progress)
	}
}

// TestEndOfInput checks that the end of the input cancels the call that
// runs, which is answered as it ends, and that Serve then returns.
func TestEndOfInput(t *testing.T) {
	in, msgs, _ := session(t)
	start := time.Now()
	send(t, in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cart_wait"}}`)
	time.Sleep(50 * time.Millisecond)
	in.Close()
	m, _ := next(t, msgs)
	if text := m["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; m["id"] != 1.0 || text != "cancelled" {
		t.Errorf("the answer after the end of the input: %v", m)
	}
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("the call ended after %v", d)
	}
}

// TestServeDone checks that Serve returns nil once its context is done,
// cancelling the call that runs, and does not run the calls that wait.
func TestServeDone(t *testing.T) {
	var calls atomic.Int32
	s := Server{Name: "shop", Version: "1.0", Tools: []Tool{{
		Name:        "cart_wait",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Call: func(ctx context.Context, _ json.RawMessage, _ func()) (string, bool) {
			calls.Add(1)
			select {
			case <-ctx.Done():
				return "cancelled", true
			case <-time.After(time.Second):
				return "done", false
			}
		},
	}}}
	inR, inW := io.Pipe()
	defer inW.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, inR, io.Discard) }()
	go func() {
		io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cart_wait"}}`+"\n")
		io.WriteString(inW, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"cart_wait"}}`+"\n")
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(900 * time.Millisecond):
		t.Fatal("Serve did not return")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("Serve returned after %v", d)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("%d calls ran, want 1", n)
	}
}
