// Package mcp serves tools to an AI tool over the Model Context Protocol: one
// JSON-RPC 2.0 message per line on the standard input and output. It knows
// nothing of Gentry: the tools and what they run are given by the caller.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// Tool is a tool of the server.
type Tool struct {
	Name        string
	Description string
	// InputSchema is the JSON schema of the arguments, an object.
	InputSchema json.RawMessage
	// Call runs the tool with its arguments, a JSON object, and returns the
	// text for the model and whether the call failed.
	Call func(args json.RawMessage) (text string, failed bool)
}

// Server is a server with its name, version and tools.
type Server struct {
	Name    string
	Version string
	Tools   []Tool
	// OnInitialized runs once the client is initialized, with a function
	// that sends a message to it.
	OnInitialized func(send func(any) error)
}

// latestProtocol is the protocol version offered to a client that asks for
// none the server knows.
const latestProtocol = "2025-11-25"

// protocols are the protocol versions the server speaks: the methods it
// serves are the same in all of them.
var protocols = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true, "2025-11-25": true}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads requests from in and writes responses to out until in ends.
// Calls run one at a time, in the order they come.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	send := func(r response) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(r)
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := send(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParse, err.Error()}}); err != nil {
				return err
			}
			continue
		}
		// A response to a request of the server has no method.
		if req.Method == "" {
			continue
		}
		if req.Method == "notifications/initialized" && s.OnInitialized != nil {
			s.OnInitialized(func(v any) error {
				mu.Lock()
				defer mu.Unlock()
				return enc.Encode(v)
			})
		}
		// A notification has no id and gets no response.
		if len(req.ID) == 0 {
			continue
		}
		result, rerr := s.handle(req)
		if err := send(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rerr}); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
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
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		for _, t := range s.Tools {
			if t.Name == p.Name {
				args := p.Arguments
				if len(args) == 0 || string(args) == "null" {
					args = json.RawMessage("{}")
				}
				text, failed := t.Call(args)
				return map[string]any{
					"content": []any{map[string]any{"type": "text", "text": text}},
					"isError": failed,
				}, nil
			}
		}
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + req.Method}
}
