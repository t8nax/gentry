// Package clitest runs gentry commands in tests and checks their output. The
// tests of the commands live in packages under it, one per group of commands,
// so that go test runs the groups at once: each test changes the environment
// and the current directory of its process, so the tests of one package
// cannot run in parallel.
//
// Smaller packages do not make the run shorter: it is bound by the total
// work, the runs of git and their checks by the antivirus, and each package
// builds the shop anew. Split into 28 packages, the tests did a third more
// work and the run took longer (GNT-4).
package clitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/paths"
)

// Main points the data root and the user's home at a temporary directory, so
// no test can touch the operator's data, and runs the tests. Claude Code is a
// program that does not exist: the commands under test must not call it.
// Call it from TestMain.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv(home.EnvVar, dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	// The settings of Claude Code are in the home, and the tests run outside a
	// session of an AI tool even when they are run from one.
	os.Unsetenv(claude.ConfigDirEnv)
	os.Unsetenv(caller.ClaudeCodeEnv)
	os.Unsetenv(caller.SessionEnv)
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	os.Setenv(claude.ProgramEnv, filepath.Join(dir, "no-claude"))
	canonical, err := paths.Canonical(dir)
	if err != nil {
		panic(err)
	}
	work = filepath.Join(canonical, "work")
	layers.dir = filepath.Join(canonical, "layers")
	return m.Run()
}

// agentOnly are the commands only the agent runs, as its tools: Run runs them
// as a tool does.
var agentOnly = []string{"step add", "step done", "step drop", "stage exit", "stage skip", "artifact save", "task close"}

// byAgent reports whether the command of args is one of agentOnly.
func byAgent(args []string) bool {
	return len(args) > 1 && slices.Contains(agentOnly, args[0]+" "+args[1])
}

// InChannel states text as the command of args prints it in its channel: the
// hints of the help name tools for a command only the agent runs.
func InChannel(text string, args []string) string {
	return cli.Hints(text, byAgent(args))
}

// run runs a command of the operator, or of the agent for a command of
// agentOnly.
func run(args []string, env cli.Env) int {
	if byAgent(args) {
		return cli.RunAgent(args, env)
	}
	return cli.Run(args, env)
}

// Run runs a command and returns its exit code and output. A command only the
// agent runs runs as its tool does: hints name tools.
func Run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, cli.Env{Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

// RunWith runs a command with stdin as its standard input.
func RunWith(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, cli.Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

// MustRun runs a command that must succeed and returns its output.
func MustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := Run(args...)
	if code != contract.ExitOK {
		t.Fatalf("%v: exit code %d, stderr:\n%s", args, code, stderr)
	}
	return stdout
}

// WantRun runs a command and checks its exit code, stdout and stderr. The
// hints of stdout and stderr are given as the help names them; they are
// checked in the channel of the command.
func WantRun(t *testing.T, exit int, stdout, stderr string, args ...string) {
	t.Helper()
	if !slices.Contains(args, "--json") {
		stdout, stderr = InChannel(stdout, args), InChannel(stderr, args)
	}
	code, out, errOut := Run(args...)
	if code != exit || out != stdout || errOut != stderr {
		t.Errorf("%v: exit code %d, stdout:\n%s\nstderr:\n%s\nwant %d, stdout:\n%s\nstderr:\n%s", args, code, out, errOut, exit, stdout, stderr)
	}
}

// WantJSON runs a command with --json, checks its exit code and the output
// against a schema, and returns the output.
func WantJSON(t *testing.T, exit int, schema string, args ...string) string {
	t.Helper()
	code, out, _ := Run(append(args, "--json")...)
	if code != exit {
		t.Fatalf("%v --json: exit code %d, output %s", args, code, out)
	}
	Validate(t, schema, out)
	return out
}

// ErrorCode returns the exit code and the code of the failure of a --json
// command.
func ErrorCode(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, out, _ := Run(append(args, "--json")...)
	Validate(t, "schemas/error.json", out)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(out), &e)
	return code, e.Error.Code
}

var schemas struct {
	once     sync.Once
	compiler *jsonschema.Compiler
	err      error
	mu       sync.Mutex
	compiled map[string]*jsonschema.Schema
}

// Validate checks doc against a schema of the contract, named by its path in
// the contract module.
func Validate(t *testing.T, schema, doc string) {
	t.Helper()
	schemas.once.Do(func() { schemas.compiler, schemas.err = compiler() })
	if schemas.err != nil {
		t.Fatal(schemas.err)
	}
	schemas.mu.Lock()
	compiled, ok := schemas.compiled[schema]
	if !ok {
		var err error
		compiled, err = schemas.compiler.Compile("https://github.com/t8nax/gentry/contract/" + schema)
		if err != nil {
			schemas.mu.Unlock()
			t.Fatal(err)
		}
		if schemas.compiled == nil {
			schemas.compiled = map[string]*jsonschema.Schema{}
		}
		schemas.compiled[schema] = compiled
	}
	schemas.mu.Unlock()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(v); err != nil {
		t.Errorf("%s does not match %s: %v", doc, schema, err)
	}
}

// compiler returns a compiler with every schema of the contract, as one may
// refer to another.
func compiler() (*jsonschema.Compiler, error) {
	names, err := fs.Glob(contract.Schemas, "schemas/*.json")
	if err != nil {
		return nil, err
	}
	events, err := fs.Glob(contract.Schemas, "schemas/events/*.json")
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	for _, name := range append(names, events...) {
		f, err := contract.Schemas.Open(name)
		if err != nil {
			return nil, err
		}
		s, err := jsonschema.UnmarshalJSON(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := c.AddResource("https://github.com/t8nax/gentry/contract/"+name, s); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Lines joins lines of an output, each ended by a newline.
func Lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// timePattern is a time as the output names it, to the minute.
var timePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`)

// Masked replaces the times in an output by <время>: they are those of the
// commits.
func Masked(s string) string { return timePattern.ReplaceAllString(s, "<время>") }

// Table returns rows printed as a table, the header row first: columns two
// spaces apart, the last one not padded.
func Table(rows ...[]string) string {
	var widths []int
	for _, r := range rows {
		for i, cell := range r {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}
	var b strings.Builder
	for _, r := range rows {
		for i, cell := range r {
			if i == len(r)-1 {
				b.WriteString(cell)
				break
			}
			fmt.Fprintf(&b, "%-*s", widths[i]+2, cell)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// JSONString returns s as a JSON string.
func JSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// JSONEqual reports whether a and b are the same in JSON.
func JSONEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// FileExists reports whether a file or directory exists at p.
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// MustWd returns the current directory.
func MustWd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// EmptyHome points the data root at a new empty directory and returns it.
func EmptyHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	return dir
}
