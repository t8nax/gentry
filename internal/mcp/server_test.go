package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// serve runs a server with one echo tool on the lines and returns the
// responses, one decoded object each.
func serve(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	s := Server{Name: "shop", Version: "1.0", Tools: []Tool{{
		Name:        "cart_show",
		Description: "Показать корзину.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Call: func(args json.RawMessage) (string, bool) {
			var a struct{ Fail bool }
			json.Unmarshal(args, &a)
			return "args " + string(args), a.Fail
		},
	}}}
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		res = append(res, m)
	}
	return res
}

func TestInitialize(t *testing.T) {
	for _, c := range []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-11-25", "2025-11-25"},
		{"2099-01-01", latestProtocol},
		{"", latestProtocol},
	} {
		res := serve(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+c.asked+`"}}`)
		r := res[0]["result"].(map[string]any)
		if r["protocolVersion"] != c.want {
			t.Errorf("asked %q: protocol %v, want %q", c.asked, r["protocolVersion"], c.want)
		}
		if info := r["serverInfo"].(map[string]any); info["name"] != "shop" || info["version"] != "1.0" {
			t.Errorf("server info %v", info)
		}
		if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok {
			t.Errorf("no tools capability: %v", r)
		}
	}
}

func TestToolsListAndCall(t *testing.T) {
	res := serve(t,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"cart_show","arguments":{"item":"чайник \"Витязь\"\nдва"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cart_show"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"cart_show","arguments":{"fail":true}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"cart_clear"}}`,
	)
	if len(res) != 5 {
		t.Fatalf("%d responses, want 5 (no answer to a notification): %v", len(res), res)
	}
	if res[0]["id"] != "a" {
		t.Errorf("id %v", res[0]["id"])
	}
	tools := res[0]["result"].(map[string]any)["tools"].([]any)
	if tool := tools[0].(map[string]any); len(tools) != 1 || tool["name"] != "cart_show" || tool["description"] != "Показать корзину." || tool["inputSchema"] == nil {
		t.Errorf("tools %v", tools)
	}
	text := func(r map[string]any) (string, bool) {
		result := r["result"].(map[string]any)
		return result["content"].([]any)[0].(map[string]any)["text"].(string), result["isError"].(bool)
	}
	if got, failed := text(res[1]); got != `args {"item":"чайник \"Витязь\"\nдва"}` || failed {
		t.Errorf("call: %q, failed %v", got, failed)
	}
	if got, _ := text(res[2]); got != "args {}" {
		t.Errorf("call without arguments: %q", got)
	}
	if _, failed := text(res[3]); !failed {
		t.Error("a failed call is not an error")
	}
	if e := res[4]["error"].(map[string]any); e["code"].(float64) != codeInvalidParams {
		t.Errorf("unknown tool: %v", e)
	}
}

func TestOtherMessages(t *testing.T) {
	res := serve(t,
		`{"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover"}`,
		`{"jsonrpc":"2.0","id":7,"result":{"roots":[]}}`,
		`not json`,
		`{"jsonrpc":"2.0","id":8,"method":"ping"}`,
	)
	if len(res) != 3 {
		t.Fatalf("%d responses, want 3 (no answer to a response of the client): %v", len(res), res)
	}
	if e := res[0]["error"].(map[string]any); e["code"].(float64) != codeMethodNotFound {
		t.Errorf("unknown method: %v", e)
	}
	if e := res[1]["error"].(map[string]any); e["code"].(float64) != codeParse || res[1]["id"] != nil {
		t.Errorf("parse error: %v", res[1])
	}
	if res[2]["id"].(float64) != 8 || res[2]["result"] == nil {
		t.Errorf("ping: %v", res[2])
	}
}
