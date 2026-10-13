// Package mcp serves tools to an AI tool over the Model Context Protocol: one
// JSON-RPC 2.0 message per line on the standard input and output. It knows
// nothing of Gentry: the tools and what they run are given by the caller.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
)

// Tool is a tool of the server.
type Tool struct {
	Name        string
	Description string
	// InputSchema is the JSON schema of the arguments, an object.
	InputSchema json.RawMessage
	// Call runs the tool with its arguments, a JSON object, and returns the
	// text for the model and whether the call failed. ctx is done when the
	// client cancels the call. progress tells the client that the call goes
	// on, if the client asked to be told; otherwise it does nothing.
	Call func(ctx context.Context, args json.RawMessage, progress func()) (text string, failed bool)
}

// Server is a server with its name, version and tools.
type Server struct {
	Name    string
	Version string
	Tools   []Tool
}

// latestProtocol is the protocol version offered to a client that asks for
// one the server does not know.
const latestProtocol = "2025-11-25"

// protocols are the protocol versions the server speaks: the methods it
// serves are the same in all of them. A client of a newer version first asks
// server/discover; the server does not know it, and the client falls back to
// initialize.
var protocols = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true, "2025-11-25": true}

// maxMessage limits one message: the arguments of a call carry the texts of
// the agent, such as a plan.
const maxMessage = 64 << 20

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// request is a request of the client waiting for its turn or served.
type request struct {
	m      message
	err    *rpcError // the message could not be read: the answer is this error
	ctx    context.Context
	cancel context.CancelFunc
	// cancelled is set when the client cancels the request: it gets no
	// answer then.
	cancelled atomic.Bool
}

// output writes the messages of the server one at a time: the answers and
// the notifications of the progress of a call.
type output struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (o *output) write(v any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.enc.Encode(v)
}

// Serve reads messages from in and answers the requests on out until in
// ends or ctx is done. Calls run one at a time, in the order they come. The
// input is read while a call runs, so that the client can cancel a call that
// runs or waits by notifications/cancelled: its context is done, and it gets
// no answer; a call cancelled while it waits does not run.
//
// When the input ends, the client is going away (MCP closes the input of
// the server to end it): the contexts of the calls that run or wait are done
// then, and the calls are answered as they end. When ctx is done, as on a
// signal to end the server, the context of the call that runs is done too,
// and Serve returns nil once the call ends, without the calls that wait.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	o := &output{enc: json.NewEncoder(out)}
	o.enc.SetEscapeHTML(false)
	q := &requests{pending: map[string]*request{}, wake: make(chan struct{}, 1)}
	q.base, q.endBase = context.WithCancel(ctx)
	defer q.endBase()
	go q.read(in)
	for {
		r, ok := q.next(ctx)
		if !ok {
			if ctx.Err() != nil {
				return nil
			}
			return q.err
		}
		var res any
		rerr := r.err
		if rerr == nil && !r.cancelled.Load() {
			res, rerr = s.handle(r.ctx, r.m, o)
		}
		q.done(r)
		if r.cancelled.Load() {
			continue
		}
		if err := o.write(response{JSONRPC: "2.0", ID: r.m.ID, Result: res, Error: rerr}); err != nil {
			return err
		}
	}
}

// requests are the requests of the client that wait for their turn, in the
// order they came. The queue has no limit, so that the input is read, and a
// call cancelled, however many requests wait.
type requests struct {
	mu      sync.Mutex
	queue   []*request
	pending map[string]*request // the requests that wait or run, by id
	ended   bool                // the input ended
	err     error               // why it ended, if not at its end
	wake    chan struct{}       // tells next that the queue changed
	// base is the context the contexts of the requests come from: endBase
	// ends it when the input ends.
	base    context.Context
	endBase context.CancelFunc
}

// read reads the messages of in into the queue until in ends.
func (q *requests) read(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), maxMessage)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		r := &request{}
		switch err := json.Unmarshal(line, &r.m); {
		case err != nil:
			r.m, r.err = message{ID: json.RawMessage("null")}, &rpcError{codeParse, err.Error()}
		case r.m.Method == "notifications/cancelled":
			q.cancel(r.m.Params)
			continue
		case len(r.m.ID) == 0 || r.m.Method == "":
			// A notification has no id, and a response of the client has
			// no method: neither gets an answer.
			continue
		}
		r.ctx, r.cancel = context.WithCancel(q.base)
		q.mu.Lock()
		q.queue = append(q.queue, r)
		if r.err == nil {
			q.pending[idKey(r.m.ID)] = r
		}
		q.mu.Unlock()
		q.signal()
	}
	q.endBase()
	q.mu.Lock()
	q.ended, q.err = true, sc.Err()
	q.mu.Unlock()
	q.signal()
}

// signal tells next that the queue changed, if it does not know yet.
func (q *requests) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next returns the first request of the queue, waiting for one; ok is false
// once the input ended and no request waits, or once ctx is done.
func (q *requests) next(ctx context.Context) (r *request, ok bool) {
	for {
		if ctx.Err() != nil {
			return nil, false
		}
		q.mu.Lock()
		if len(q.queue) > 0 {
			r = q.queue[0]
			q.queue = q.queue[1:]
			q.mu.Unlock()
			return r, true
		}
		ended := q.ended
		q.mu.Unlock()
		if ended {
			return nil, false
		}
		select {
		case <-q.wake:
		case <-ctx.Done():
		}
	}
}

// done forgets the request r once it is served.
func (q *requests) done(r *request) {
	r.cancel()
	q.mu.Lock()
	if key := idKey(r.m.ID); q.pending[key] == r {
		delete(q.pending, key)
	}
	q.mu.Unlock()
}

// idKey is the id of a request as a key: the JSON of a number or a string.
func idKey(id json.RawMessage) string { return string(bytes.TrimSpace(id)) }

// cancel cancels the request that params of notifications/cancelled name,
// if it waits or runs.
func (q *requests) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	q.mu.Lock()
	r, ok := q.pending[idKey(p.RequestID)]
	q.mu.Unlock()
	if ok {
		r.cancelled.Store(true)
		r.cancel()
	}
}

func (s *Server) handle(ctx context.Context, m message, o *output) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(m.Params, &p)
		version := latestProtocol
		if protocols[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := make([]map[string]any, len(s.Tools))
		for i, t := range s.Tools {
			tools[i] = map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      struct {
				ProgressToken json.RawMessage `json:"progressToken"`
			} `json:"_meta"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			args := p.Arguments
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			text, failed := t.Call(ctx, args, progress(o, p.Meta.ProgressToken))
			return map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}},
				"isError": failed,
			}, nil
		}
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + m.Method}
}

// progress returns the function that tells the client that a call with the
// progress token goes on: notifications/progress with a progress that grows
// by one each time. Without a token it does nothing. A notification that
// cannot be written is lost: the answer tells the rest.
func progress(o *output, token json.RawMessage) func() {
	if len(token) == 0 || string(token) == "null" {
		return func() {}
	}
	var n atomic.Int64
	return func() {
		o.write(notification{JSONRPC: "2.0", Method: "notifications/progress",
			Params: map[string]any{"progressToken": token, "progress": n.Add(1)}})
	}
}
