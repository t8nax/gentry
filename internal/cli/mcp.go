package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/mcp"
)

// mcpLogEnv names a file the server appends its requests to: for the checks
// of P24 only.
const mcpLogEnv = "GENTRY_MCP_LOG"

// mcpTool is a tool of gentry mcp: the command it runs and the contract
// schema of its input; a tool without a schema takes no arguments.
type mcpTool struct {
	name    string
	desc    string
	command []string
	schema  string
}

var mcpTools = []mcpTool{
	{
		name:    "stage_show",
		desc:    "Показать текущий этап задачи рабочей копии: исполнитель, выход, переходы, инструкция и фрагменты.",
		command: []string{"stage", "show"},
	},
	{
		name:    "step_add",
		desc:    "Добавить шаги текущего этапа после имеющихся. Шаг — одна строка до 120 знаков.",
		command: []string{"step", "add"},
		schema:  "step-add-input.json",
	},
	{
		name:    "stage_exit",
		desc:    "Закрыть текущий этап выходом и перейти к следующему. Этап закрывается, когда все его шаги выполнены или сняты; на развилке укажите переход и обоснование.",
		command: []string{"stage", "exit"},
		schema:  "stage-exit-input.json",
	},
	{
		name:    "note_add",
		desc:    "Добавить заметку задачи: то, что нужно учесть на следующих этапах.",
		command: []string{"note", "add"},
		schema:  "note-add-input.json",
	},
}

// runMCP serves the commands of the agent as MCP tools on the standard input
// and output. Every command it runs is the agent's.
func runMCP(args []string, env Env) int {
	f := newFlags("mcp")
	if code, done := f.parse(args, env); done {
		return code
	}
	caller.ServeAgent()
	logf := mcpLog()
	if logf != nil {
		defer logf.Close()
		wd, _ := os.Getwd()
		fmt.Fprintf(logf, "%s start pid=%d wd=%s args=%q\n", time.Now().Format(time.RFC3339Nano), os.Getpid(), wd, os.Args)
	}
	s := mcp.Server{Name: "gentry", Version: buildinfo.Version()}
	for _, t := range mcpTools {
		schema, err := mcpSchema(t.schema)
		if err != nil {
			return fail(env, internal(err))
		}
		s.Tools = append(s.Tools, mcp.Tool{
			Name: t.name, Description: t.desc, InputSchema: schema,
			Call: func(in json.RawMessage) (string, bool) { return callTool(t, in) },
		})
	}
	in := env.Stdin
	if logf != nil {
		in = teeLog{in: env.Stdin, log: logf}
		s.OnInitialized = func(send func(any) error) {
			send(map[string]any{"jsonrpc": "2.0", "id": "p24-roots", "method": "roots/list"})
		}
	}
	if err := s.Serve(in, env.Stdout); err != nil {
		return fail(env, internal(err))
	}
	return contract.ExitOK
}

// callTool runs the command of t with the arguments as its --input and
// returns its text output.
func callTool(t mcpTool, in json.RawMessage) (string, bool) {
	var out bytes.Buffer
	args := slices.Clone(t.command)
	sub := Env{Stdout: &out, Stderr: &out}
	if t.schema != "" {
		args = append(args, "--input", "-")
		sub.Stdin = bytes.NewReader(in)
	}
	code := Run(args, sub)
	return strings.TrimRight(out.String(), "\n"), code != contract.ExitOK
}

// mcpSchema returns the contract schema name as the input schema of a tool,
// without the keys that name the schema itself; an object without fields for
// an empty name.
func mcpSchema(name string) (json.RawMessage, error) {
	if name == "" {
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), nil
	}
	b, err := contract.Schemas.ReadFile("schemas/" + name)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for _, k := range []string{"$schema", "$id", "title", "description"} {
		delete(m, k)
	}
	return json.Marshal(m)
}

func mcpLog() *os.File {
	path := os.Getenv(mcpLogEnv)
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return f
}

// teeLog copies what is read to the log.
type teeLog struct {
	in  interface{ Read([]byte) (int, error) }
	log *os.File
}

func (t teeLog) Read(p []byte) (int, error) {
	n, err := t.in.Read(p)
	if n > 0 {
		fmt.Fprintf(t.log, "%s pid=%d <- %s", time.Now().Format(time.RFC3339Nano), os.Getpid(), p[:n])
	}
	return n, err
}
