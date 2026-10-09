package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/mcp"
	"github.com/t8nax/gentry/internal/msg"
)

// The commands of gentry are the tools of the agent too: gentry mcp serves
// each command as a tool over the Model Context Protocol. A tool runs its
// command in the channel of the agent, so what it records is the agent's and
// its hints name tools.

// serviceCommands are the commands that are no tools: they serve the tool of
// the agent, the panel and the help.
var serviceCommands = []string{"help", "hook", "events", "mcp", "version"}

// toolDescs are the descriptions of tools that differ from the help of their
// commands: the help names flags.
var toolDescs = map[string]msg.Key{
	"task take":       msg.ToolTaskTakeDesc,
	"task cancel":     msg.ToolTaskCancelDesc,
	"operator record": msg.ToolOperatorRecordDesc,
}

// toolCommands returns the commands served as tools, in the order of the
// help.
func toolCommands() []command {
	var cmds []command
	for _, c := range allCommands() {
		if !slices.Contains(serviceCommands, c.name) {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// toolName is the name of the tool of the command named cmd, such as
// stage_show for stage show.
func toolName(cmd string) string { return strings.ReplaceAll(cmd, " ", "_") }

// fieldName is the field of the flag named name, as --input names it.
func fieldName(flag string) string { return strings.ReplaceAll(flag, "-", "_") }

// toolFlags returns the flags of c that are fields of its tool: all but
// --json and --input, which the tool sets itself.
func toolFlags(c command) []flagSpec {
	var fs []flagSpec
	for _, f := range c.flags {
		if f.name != "json" && f.name != "input" {
			fs = append(fs, f)
		}
	}
	return fs
}

// inputSchema returns the contract schema of --input of c, with the schemas
// it refers to put in place; nil if c takes no --input.
func inputSchema(c command) (map[string]any, error) {
	return readSchema(strings.ReplaceAll(c.name, " ", "-") + "-input.json")
}

// readSchema reads the contract schema name without the keys that name the
// schema itself, with the schemas it refers to put in place: a client of MCP
// does not resolve references. Nil if there is no such schema.
func readSchema(name string) (map[string]any, error) {
	b, err := contract.Schemas.ReadFile("schemas/" + name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for _, k := range []string{"$schema", "$id", "title"} {
		delete(m, k)
	}
	return m, inlineRefs(m)
}

// inlineRefs replaces each {"$ref": "./name.json"} in v with that schema.
func inlineRefs(v any) error {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if m, ok := x.(map[string]any); ok {
				if ref, ok := m["$ref"].(string); ok {
					s, err := readSchema(strings.TrimPrefix(ref, "./"))
					if err != nil || s == nil {
						return fmt.Errorf("schema %s: %v", ref, err)
					}
					// The field that refers to it describes it in the words of
					// the help.
					dropDescriptions(s)
					v[k] = s
					continue
				}
			}
			if err := inlineRefs(x); err != nil {
				return err
			}
		}
	case []any:
		for _, x := range v {
			if err := inlineRefs(x); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropDescriptions removes the descriptions of schema s and of its fields.
func dropDescriptions(s map[string]any) {
	delete(s, "description")
	props, _ := s["properties"].(map[string]any)
	for _, p := range props {
		if m, ok := p.(map[string]any); ok {
			dropDescriptions(m)
		}
	}
}

// tool is a command served as a tool: the fields of its --input and its
// schema.
type tool struct {
	cmd    command
	input  []string // fields passed in --input; the rest are flags and arguments
	schema map[string]any
}

// newTool builds the tool of c. Its fields are the fields of --input of c, if
// it takes it, and its flags and arguments; a field is described by the help
// of its flag or argument.
func newTool(c command) (tool, error) {
	t := tool{cmd: c}
	in, err := inputSchema(c)
	if err != nil {
		return t, err
	}
	props := map[string]any{}
	var required []string
	if in != nil {
		props, _ = in["properties"].(map[string]any)
		for k := range props {
			t.input = append(t.input, k)
		}
		slices.Sort(t.input)
		req, _ := in["required"].([]any)
		for _, r := range req {
			required = append(required, r.(string))
		}
	}
	// A field is described by the lines of the help of its flag that do not
	// name the command line, such as how to pass a long text with --input.
	describe := func(name, desc string) {
		var lines []string
		for _, l := range strings.Split(desc, "\n") {
			if !strings.Contains(l, "--") && !strings.Contains(l, "gentry ") {
				lines = append(lines, l)
			}
		}
		desc = strings.Join(lines, "\n")
		p, ok := props[name].(map[string]any)
		if !ok {
			p = map[string]any{"type": "string"}
			props[name] = p
		}
		p["description"] = desc
	}
	for _, f := range toolFlags(c) {
		name := fieldName(f.name)
		if _, ok := props[name]; !ok && f.value == "" {
			props[name] = map[string]any{"type": "boolean"}
		}
		describe(name, f.desc())
		if f.required && !slices.Contains(required, name) {
			required = append(required, name)
		}
	}
	for _, a := range c.args {
		if a.field == "" {
			continue
		}
		if _, ok := props[a.field]; !ok && a.number {
			props[a.field] = map[string]any{"type": "integer"}
		}
		describe(a.field, a.desc())
		if !a.optional && !slices.Contains(required, a.field) {
			required = append(required, a.field)
		}
	}
	if c.name == "operator record" {
		describe("options", msg.Text(msg.FlagInputOptions))
	}
	t.schema = map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		t.schema["required"] = required
	}
	return t, nil
}

// description is what the tool does: the help of its command.
func (t tool) description() string {
	if k, ok := toolDescs[t.cmd.name]; ok {
		return msg.Text(k)
	}
	return msg.Text(t.cmd.desc)
}

// commandLine turns the arguments of the tool into the arguments of its
// command: the fields of --input on the standard input, a flag per other
// field, then the arguments in their order after "--", so that a value is
// never taken for a flag.
func (t tool) commandLine(raw json.RawMessage) ([]string, []byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, nil, fieldsError(msg.Text(msg.InputNotObject))
	}
	args := strings.Fields(t.cmd.name)
	input := map[string]json.RawMessage{}
	for _, name := range t.input {
		if v, ok := fields[name]; ok {
			input[name] = v
			delete(fields, name)
		}
	}
	for _, f := range toolFlags(t.cmd) {
		v, ok := fields[fieldName(f.name)]
		if !ok {
			continue
		}
		delete(fields, fieldName(f.name))
		if f.value == "" {
			var on bool
			if err := json.Unmarshal(v, &on); err != nil {
				return nil, nil, fieldsError(msg.Text(msg.InputBadValue, fieldName(f.name)))
			}
			if on {
				args = append(args, "--"+f.name)
			}
			continue
		}
		s, ok := scalar(v, false)
		if !ok {
			return nil, nil, fieldsError(msg.Text(msg.InputNotString, fieldName(f.name)))
		}
		args = append(args, "--"+f.name+"="+s)
	}
	var positional []string
	for _, a := range t.cmd.args {
		v, ok := fields[a.field]
		if a.field == "" || !ok {
			continue
		}
		delete(fields, a.field)
		s, ok := scalar(v, a.number)
		if !ok {
			k := msg.InputNotString
			if a.number {
				k = msg.InputNotInteger
			}
			return nil, nil, fieldsError(msg.Text(k, a.field))
		}
		positional = append(positional, s)
	}
	for name := range fields {
		return nil, nil, fieldsError(msg.Text(msg.InputUnknownField, name))
	}
	var stdin []byte
	if t.input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, nil, err
		}
		args, stdin = append(args, "--input", "-"), b
	}
	if len(positional) > 0 {
		args = append(append(args, "--"), positional...)
	}
	return args, stdin, nil
}

// fieldsError is a field of a tool that cannot be read: the cause in the
// words of the catalog.
type fieldsError string

func (e fieldsError) Error() string { return string(e) }

// scalar is the value of a field as a command line value: a string, or an
// integer if number is set, given as a number or as a string of one.
func scalar(v json.RawMessage, number bool) (string, bool) {
	var s string
	if json.Unmarshal(v, &s) == nil {
		if number {
			_, err := strconv.ParseInt(s, 10, 64)
			return s, err == nil
		}
		return s, true
	}
	var n json.Number
	if !number || json.Unmarshal(v, &n) != nil {
		return "", false
	}
	_, err := strconv.ParseInt(n.String(), 10, 64)
	return n.String(), err == nil
}

// runTool runs the command of a tool in place of Run; tests set it.
var runTool func(args []string, env Env) int

// call runs the command of the tool for the agent and returns its text. A
// panic of the command fails the call, not the server of the session.
func (t tool) call(raw json.RawMessage) (text string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			text, failed = msg.Text(msg.ErrInternal, r), true
		}
	}()
	args, stdin, err := t.commandLine(raw)
	if err != nil {
		return msg.Text(msg.ErrToolFields, err.Error()), true
	}
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: &out, agent: true}
	if stdin != nil {
		env.Stdin = bytes.NewReader(stdin)
	}
	run := runTool
	if run == nil {
		run = Run
	}
	code := run(args, env)
	return strings.TrimRight(out.String(), "\n"), code != contract.ExitOK
}

// mcpServer is the server of the tools.
func mcpServer() (mcp.Server, error) {
	s := mcp.Server{Name: "gentry", Version: buildinfo.Version()}
	for _, c := range toolCommands() {
		t, err := newTool(c)
		if err != nil {
			return s, err
		}
		schema, err := json.Marshal(t.schema)
		if err != nil {
			return s, err
		}
		s.Tools = append(s.Tools, mcp.Tool{Name: toolName(c.name), Description: t.description(), InputSchema: schema, Call: t.call})
	}
	return s, nil
}

// CallTool runs the tool name with the arguments args, a JSON object, as
// gentry mcp does, and returns its text and whether it failed.
func CallTool(name string, args []byte) (string, bool) {
	for _, c := range toolCommands() {
		if toolName(c.name) == name {
			t, err := newTool(c)
			if err != nil {
				panic(err)
			}
			return t.call(args)
		}
	}
	panic("cli: no tool " + name)
}

// runMCP serves the commands as tools of the agent on the standard input and
// output. Every command it runs is the agent's.
func runMCP(args []string, env Env) int {
	f := newFlags("mcp")
	if code, done := f.parse(args, env); done {
		return code
	}
	s, err := mcpServer()
	if err != nil {
		return fail(env, internal(err))
	}
	if err := s.Serve(env.Stdin, env.Stdout); err != nil {
		return fail(env, internal(err))
	}
	return contract.ExitOK
}
