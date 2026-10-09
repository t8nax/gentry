// Package mcp serves tools to an AI tool over the Model Context Protocol: one
// JSON-RPC 2.0 message per line on the standard input and output. It knows
// nothing of Gentry: the tools and what they run are given by the caller.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"
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

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads messages from in and answers the requests on out until in
// ends. Calls run one at a time, in the order they come.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), maxMessage)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			if err := enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParse, err.Error()}}); err != nil {
				return err
			}
			continue
		}
		// A notification has no id, and a response of the client has no
		// method: neither gets an answer.
		if len(m.ID) == 0 || m.Method == "" {
			continue
		}
		result, rerr := s.handle(m)
		if err := enc.Encode(response{JSONRPC: "2.0", ID: m.ID, Result: result, Error: rerr}); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(m message) (any, *rpcError) {
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
			text, failed := t.Call(args)
			return map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}},
				"isError": failed,
			}, nil
		}
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + m.Method}
}
