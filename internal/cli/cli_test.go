package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(args, Env{Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		code, stdout, stderr := run(args...)
		if code != contract.ExitOK {
			t.Errorf("%v: exit code %d, want %d", args, code, contract.ExitOK)
		}
		if stderr != "" {
			t.Errorf("%v: unexpected stderr: %q", args, stderr)
		}
		for _, c := range commands() {
			if c.hidden {
				continue
			}
			if !strings.Contains(stdout, c.name) {
				t.Errorf("%v: help does not list command %s:\n%s", args, c.name, stdout)
			}
		}
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run("version")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, msg.Text(msg.VersionGentry, buildinfo.Version())+"\n") {
		t.Errorf("unexpected output: %q", stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	tests := [][]string{
		{"no-such-command"},
		{"version", "extra"},
		{"help", "extra"},
	}
	for _, args := range tests {
		code, stdout, stderr := run(args...)
		if code != contract.ExitUsage {
			t.Errorf("%v: exit code %d, want %d", args, code, contract.ExitUsage)
		}
		if stdout != "" {
			t.Errorf("%v: unexpected stdout: %q", args, stdout)
		}
		if stderr == "" || strings.HasPrefix(stderr, "gentry:") {
			t.Errorf("%v: unexpected stderr: %q", args, stderr)
		}
	}
}

func TestVersionText(t *testing.T) {
	_, stdout, _ := run("version")
	if want := msg.Text(msg.VersionGentry, buildinfo.Version()) + "\n"; stdout != want {
		t.Errorf("output %q, want %q", stdout, want)
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := run("version", "--json")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Errorf("want exactly one line of JSON, got %q", stdout)
	}
	validate(t, "schemas/version.json", stdout)

	var got contract.VersionOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	want := contract.VersionOutput{Gentry: buildinfo.Version(), Contract: contract.Version, StateSchema: state.SchemaVersion()}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// No fields beyond the contract type.
	var fields map[string]any
	json.Unmarshal([]byte(stdout), &fields)
	if len(fields) != 3 {
		t.Errorf("unexpected fields: %v", fields)
	}
}

func TestFlags(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{[]string{"version", "--foo"}, contract.ExitUsage},
		{[]string{"version", "-x"}, contract.ExitUsage},
		{[]string{"version", "--json=1"}, contract.ExitUsage},
		{[]string{"version", "--json", "extra"}, contract.ExitUsage},
		{[]string{"version", "--", "--json"}, contract.ExitUsage},
		{[]string{"version", "--help"}, contract.ExitOK},
	}
	for _, tt := range tests {
		if code, _, _ := run(tt.args...); code != tt.code {
			t.Errorf("%v: exit code %d, want %d", tt.args, code, tt.code)
		}
	}
	_, _, stderr := run("version", "--foo")
	if want := msg.Text(msg.ErrUnknownFlag, "version", "--foo") + "\n"; stderr != want {
		t.Errorf("stderr %q, want %q", stderr, want)
	}
}

// validate checks a JSON document against an embedded contract schema.
func validate(t *testing.T, schema, doc string) {
	t.Helper()
	f, err := contract.Schemas.Open(schema)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schema, s); err != nil {
		t.Fatal(err)
	}
	compiled, err := c.Compile(schema)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(v); err != nil {
		t.Errorf("%s does not match %s: %v", doc, schema, err)
	}
}

func TestErrorsJSON(t *testing.T) {
	tests := []struct {
		args    []string
		code    string
		details map[string]any
	}{
		{[]string{"foo", "--json"}, contract.CodeUnknownCommand, map[string]any{"command": "foo"}},
		{[]string{"version", "--foo", "--json"}, contract.CodeUnknownFlag, map[string]any{"command": "version", "flag": "--foo"}},
		{[]string{"version", "extra", "--json"}, contract.CodeUnexpectedArgs, map[string]any{"command": "version"}},
		{[]string{"version", "--json", "--json=1"}, contract.CodeFlagValue, map[string]any{"flag": "--json"}},
	}
	for _, tt := range tests {
		exit, stdout, stderr := run(tt.args...)
		if exit != contract.ExitUsage {
			t.Errorf("%v: exit code %d, want %d", tt.args, exit, contract.ExitUsage)
		}
		if stderr != "" {
			t.Errorf("%v: stderr must be empty with --json, got %q", tt.args, stderr)
		}
		validate(t, "schemas/error.json", stdout)
		var out contract.ErrorOutput
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if out.Error.Code != tt.code {
			t.Errorf("%v: code %q, want %q", tt.args, out.Error.Code, tt.code)
		}
		if !reflect.DeepEqual(map[string]any(out.Error.Details), tt.details) {
			t.Errorf("%v: details %v, want %v", tt.args, out.Error.Details, tt.details)
		}
	}
}

func TestErrorText(t *testing.T) {
	_, stdout, stderr := run("foo")
	want := msg.Text(msg.ErrUnknownCommand, "foo") + " " + msg.Text(msg.HintUnknownCommand) + "\n"
	if stdout != "" || stderr != want {
		t.Errorf("stdout %q, stderr %q, want stderr %q", stdout, stderr, want)
	}
	// After "--", --json is an argument, not a flag: the failure stays text.
	if _, stdout, _ := run("foo", "--", "--json"); stdout != "" {
		t.Errorf("--json after -- must not switch to JSON, stdout %q", stdout)
	}
}

func TestHook(t *testing.T) {
	code, stdout, stderr := run("hook", "session-start")
	if code != contract.ExitOK || stdout != "" || stderr != "" {
		t.Errorf("session-start outside a project: exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	tests := []struct {
		args   []string
		code   string
		stderr string
	}{
		{[]string{"hook"}, contract.CodeMissingArgument, msg.Text(msg.ErrHookEventMissing) + " " + msg.Text(msg.HintHookEvents, "session-start")},
		{[]string{"hook", "foo"}, contract.CodeInvalidArgument, msg.Text(msg.ErrHookEventUnknown, "foo") + " " + msg.Text(msg.HintHookEvents, "session-start")},
		{[]string{"hook", "session-start", "extra"}, contract.CodeUnexpectedArgs, msg.Text(msg.ErrExtraArgs, "hook", "extra")},
	}
	for _, tt := range tests {
		exit, _, stderr := run(tt.args...)
		if exit != contract.ExitUsage || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, exit, stderr, tt.stderr)
		}
		// hook takes no --json flag, so check the JSON failure through the env.
		var out bytes.Buffer
		runHook(tt.args[1:], Env{Stdout: &out, Stderr: io.Discard, json: true})
		stdout := out.String()
		validate(t, "schemas/error.json", stdout)
		var e contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &e)
		if e.Error.Code != tt.code {
			t.Errorf("%v in JSON: code %q, want %q", tt.args, e.Error.Code, tt.code)
		}
	}

	_, help, _ := run("help")
	if strings.Contains(help, "hook") {
		t.Errorf("hook must not be listed in the help:\n%s", help)
	}
}

func TestSessionStartNeverBreaksSession(t *testing.T) {
	orig := sessionStart
	t.Cleanup(func() { sessionStart = orig })

	failures := map[string]func(io.Writer) error{
		"panic": func(w io.Writer) error {
			io.WriteString(w, "partial")
			panic("boom")
		},
		"error": func(w io.Writer) error {
			io.WriteString(w, "partial")
			return errors.New("boom")
		},
	}
	for name, f := range failures {
		sessionStart = f
		code, stdout, stderr := run("hook", "session-start")
		if code != contract.ExitOK || stdout != "" || stderr != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want 0 and no output", name, code, stdout, stderr)
		}
	}

	sessionStart = func(w io.Writer) error {
		_, err := io.WriteString(w, "introduction")
		return err
	}
	if _, stdout, _ := run("hook", "session-start"); stdout != "introduction" {
		t.Errorf("successful output must pass through, got %q", stdout)
	}
}
