package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/mcp"
	"github.com/t8nax/gentry/internal/msg"
)

// TestTools checks that every command but the service ones is a tool, and
// that the fields of each tool are the flags and arguments of its command,
// each described.
func TestTools(t *testing.T) {
	s, err := mcpServer()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]mcp.Tool{}
	for _, tool := range s.Tools {
		byName[tool.Name] = tool
	}
	for _, c := range allCommands() {
		tool, ok := byName[toolName(c.name)]
		if slices.Contains(serviceCommands, c.name) {
			if ok {
				t.Errorf("service command %s is a tool", c.name)
			}
			continue
		}
		if !ok {
			t.Errorf("%s is not a tool", c.name)
			continue
		}
		if tool.Description == "" || strings.Contains(tool.Description, "--") || strings.Contains(tool.Description, "gentry ") {
			t.Errorf("%s: description names the command line: %q", tool.Name, tool.Description)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
				Type        string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		var fields []string
		for _, f := range toolFlags(c) {
			fields = append(fields, fieldName(f.name))
		}
		for _, a := range c.args {
			if a.field == "" {
				t.Errorf("%s: argument %s has no field", c.name, msg.Text(a.name))
			}
			fields = append(fields, a.field)
		}
		for name, p := range schema.Properties {
			if !slices.Contains(fields, name) && name != "options" && name != "worktree" {
				t.Errorf("%s: field %s is neither a flag nor an argument", tool.Name, name)
			}
			if p.Description == "" || strings.Contains(p.Description, "Input of") || strings.Contains(p.Description, "--") || strings.Contains(p.Description, "gentry ") {
				t.Errorf("%s: field %s has no description of the help: %q", tool.Name, name, p.Description)
			}
		}
		for _, f := range fields {
			if _, ok := schema.Properties[f]; !ok {
				t.Errorf("%s: no field %s", tool.Name, f)
			}
		}
	}
	if !strings.Contains(string(byName["operator_record"].InputSchema), `"label"`) || strings.Contains(string(byName["operator_record"].InputSchema), `$ref`) {
		t.Errorf("operator_record: options are not in place: %s", byName["operator_record"].InputSchema)
	}
}

func TestToolCommandLine(t *testing.T) {
	tool := func(name string) tool {
		c, _ := lookup(name)
		tl, err := newTool(c)
		if err != nil {
			t.Fatal(err)
		}
		return tl
	}
	for _, tt := range []struct {
		cmd, args string
		want      []string
		stdin     string
	}{
		{"stage show", `{}`, []string{"stage", "show"}, ""},
		{"task show", `{"task":"SHOP-1","statement":true,"path":false}`, []string{"task", "show", "--statement", "--", "SHOP-1"}, ""},
		{"task attempts", `{"task":"SHOP-1","attempt":2}`, []string{"task", "attempts", "--", "SHOP-1", "2"}, ""},
		{"flow show", `{"stage":"--draft","project":"shop"}`, []string{"flow", "show", "--stage=--draft", "--project=shop"}, ""},
		{"step done", `{"step":1,"check":"go test","task":"SHOP-1"}`, []string{"step", "done", "--task=SHOP-1", "--input", "-"}, `{"check":"go test","step":1}`},
		{"task cancel", `{"task":"SHOP-1","reason":"Не нужна"}`, []string{"task", "cancel", "--input", "-", "--", "SHOP-1"}, `{"reason":"Не нужна"}`},
		{"worktree add", `{"path":"-x"}`, []string{"worktree", "add", "--", "-x"}, ""},
	} {
		args, stdin, err := tool(tt.cmd).commandLine(json.RawMessage(tt.args))
		if err != nil || !slices.Equal(args, tt.want) || string(stdin) != tt.stdin {
			t.Errorf("%s %s: %q, stdin %s, %v; want %q, stdin %s", tt.cmd, tt.args, args, stdin, err, tt.want, tt.stdin)
		}
	}
	for _, bad := range []struct{ cmd, args string }{
		{"stage show", `{"kind":"result"}`},
		{"task show", `{"statement":"да"}`},
		{"task attempts", `{"attempt":1.5}`},
		{"stage show", `[]`},
	} {
		if _, _, err := tool(bad.cmd).commandLine(json.RawMessage(bad.args)); err == nil {
			t.Errorf("%s %s: no error", bad.cmd, bad.args)
		}
	}
}

// TestToolCall runs a tool: a refusal is a failed call with its text, and
// hints name tools.
func TestToolCall(t *testing.T) {
	t.Chdir(t.TempDir())
	text, failed := CallTool("stage_exit", []byte(`{"kind":"result","text":"Готово"}`))
	if !failed || text == "" || strings.Contains(text, "gentry ") {
		t.Errorf("stage_exit outside a task: failed %v, %q", failed, text)
	}
	text, failed = CallTool("step_add", []byte(`{"steps":["Шаг"],"kind":"x"}`))
	if want := msg.Text(msg.ErrToolFields, msg.Text(msg.InputUnknownField, "kind")); !failed || text != want {
		t.Errorf("unknown field: failed %v, %q, want %q", failed, text, want)
	}
	text, failed = CallTool("task_attempts", []byte(`{"attempt":"первая"}`))
	if want := msg.Text(msg.ErrToolFields, msg.Text(msg.InputNotInteger, "attempt")); !failed || text != want {
		t.Errorf("not an integer: failed %v, %q, want %q", failed, text, want)
	}
	// The fields of --input of a command are fields of the tool to the agent.
	text, failed = CallTool("step_add", []byte(`{"steps":"Шаг"}`))
	if !failed || strings.Contains(text, "--input") || !strings.Contains(text, msg.Text(msg.InputNotList, "steps")) {
		t.Errorf("steps not a list: failed %v, %q", failed, text)
	}
	// A panic of a command fails the call, not the server.
	runTool = func([]string, Env) int { panic("корзина") }
	t.Cleanup(func() { runTool = nil })
	text, failed = CallTool("stage_show", []byte(`{}`))
	if !failed || text != msg.Text(msg.ErrInternal, "корзина") {
		t.Errorf("panic: failed %v, %q", failed, text)
	}
}

// TestMCPProtocolUnfiltered checks that the answers of the server pass as
// they are, even one that looks like a hint.
func TestMCPProtocolUnfiltered(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stage_show","arguments":{"x: gentry task close":1}}}` + "\n"
	var out bytes.Buffer
	if code := Run([]string{"mcp"}, Env{Stdin: strings.NewReader(in), Stdout: &out, Stderr: &out}); code != 0 {
		t.Fatalf("exit code %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `gentry task close`) || !strings.Contains(out.String(), `"isError":true`) {
		t.Errorf("answer: %s", out.String())
	}
}
