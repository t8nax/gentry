package events_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// writeEvents creates the state store in the data root with the given events,
// each as type, project, task.
func writeEvents(t *testing.T, events ...[3]string) {
	t.Helper()
	path, err := state.Path()
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(func(tx *state.Tx) error {
		for _, e := range events {
			if _, err := tx.AddEvent(e[0], e[1], e[2], map[string]string{"key": "value"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventsWithoutStore(t *testing.T) {
	dir := clitest.EmptyHome(t)
	// An empty journal is an empty stream.
	for _, args := range [][]string{{"events"}, {"events", "--after", "5"}, {"events", "--after=5"}, {"events", "--json"}} {
		code, stdout, stderr := clitest.Run(args...)
		if code != contract.ExitOK || stdout != "" || stderr != "" {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want nothing", args, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Errorf("reading the journal must not create the store, stat: %v", err)
	}
}

func TestEvents(t *testing.T) {
	clitest.EmptyHome(t)
	writeEvents(t, [3]string{"task.taken", "shop", "SHOP-7"}, [3]string{"settings.changed", "", ""})

	code, stdout, stderr := clitest.Run("events", "--json")
	if code != contract.ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	// Without --json the output is the same: there is no text form.
	if _, plain, _ := clitest.Run("events"); plain != stdout {
		t.Errorf("without --json:\n%s\nwant:\n%s", plain, stdout)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSON lines, got %q", stdout)
	}
	for _, line := range lines {
		clitest.Validate(t, "schemas/event.json", line)
		var e contract.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		var raw map[string]any
		json.Unmarshal([]byte(line), &raw)
		if tm := raw["time"].(string); len(tm) != len("2026-10-05T11:03:11.482Z") || !strings.HasSuffix(tm, "Z") {
			t.Errorf("time %q is not UTC with milliseconds", tm)
		}
	}
	var second map[string]any
	json.Unmarshal([]byte(lines[1]), &second)
	if _, ok := second["project"]; ok {
		t.Errorf("event without a project has the field: %s", lines[1])
	}
	if _, ok := second["task"]; ok {
		t.Errorf("event without a task has the field: %s", lines[1])
	}
	if !reflect.DeepEqual(second["data"], map[string]any{"key": "value"}) {
		t.Errorf("data %v", second["data"])
	}

	_, stdout, _ = clitest.Run("events", "--after", "1", "--json")
	if strings.Count(stdout, "\n") != 1 || !strings.Contains(stdout, `"seq":2`) {
		t.Errorf("events after 1: %q", stdout)
	}
	_, stdout, _ = clitest.Run("events", "--after", "2")
	if stdout != "" {
		t.Errorf("events after 2: %q", stdout)
	}
}

func TestEventsUsageErrors(t *testing.T) {
	clitest.EmptyHome(t)
	tests := []struct {
		args    []string
		stderr  string
		code    string
		details map[string]any
	}{
		{[]string{"events", "--after", "abc"}, msg.Text(msg.ErrEventsAfter, "abc") + "\n\n" + msg.Text(msg.HintEventsAfter), contract.CodeFlagValue, map[string]any{"flag": "--after", "value": "abc"}},
		{[]string{"events", "--after", "-1"}, msg.Text(msg.ErrEventsAfter, "-1") + "\n\n" + msg.Text(msg.HintEventsAfter), contract.CodeFlagValue, map[string]any{"flag": "--after", "value": "-1"}},
		{[]string{"events", "--after"}, msg.Text(msg.ErrFlagValueMissing, "--after"), contract.CodeFlagValue, map[string]any{"flag": "--after"}},
		{[]string{"events", "--after", "--json"}, msg.Text(msg.ErrFlagValueMissing, "--after"), contract.CodeFlagValue, map[string]any{"flag": "--after"}},
		{[]string{"events", "extra"}, msg.Text(msg.ErrUnexpectedArgs, "events"), contract.CodeUnexpectedArgs, map[string]any{"command": "events"}},
	}
	for _, tt := range tests {
		code, stdout, stderr := clitest.Run(tt.args...)
		if code != contract.ExitUsage {
			t.Errorf("%v: exit code %d, want %d", tt.args, code, contract.ExitUsage)
		}
		if !strings.Contains(strings.Join(tt.args, " "), "--json") && (stdout != "" || stderr != tt.stderr+"\n") {
			t.Errorf("%v: stdout %q, stderr %q, want %q", tt.args, stdout, stderr, tt.stderr)
		}

		args := append(tt.args, "--json")
		code, stdout, stderr = clitest.Run(args...)
		if code != contract.ExitUsage || stderr != "" {
			t.Errorf("%v: exit code %d, stderr %q", args, code, stderr)
		}
		clitest.Validate(t, "schemas/error.json", stdout)
		var out contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &out)
		if out.Error.Code != tt.code || !reflect.DeepEqual(map[string]any(out.Error.Details), tt.details) {
			t.Errorf("%v: code %q, details %v; want %q, %v", args, out.Error.Code, out.Error.Details, tt.code, tt.details)
		}
	}
}

func TestEventsNewerStore(t *testing.T) {
	clitest.EmptyHome(t)
	writeEvents(t)
	path, _ := state.Path()
	// Simulate a store of a newer Gentry by bumping its schema version.
	s, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Write(func(tx *state.Tx) error {
		_, err := tx.Exec(`UPDATE meta SET value = '7' WHERE key = 'schema_version'`)
		return err
	})
	s.Close()
	if err != nil {
		t.Fatal(err)
	}

	code, _, stderr := clitest.Run("events")
	want := msg.Text(msg.ErrStateNewer, 7, state.SchemaVersion()) + "\n\n" + msg.Text(msg.HintStateNewer) + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("exit code %d, stderr %q, want %q", code, stderr, want)
	}
	_, stdout, _ := clitest.Run("events", "--json")
	clitest.Validate(t, "schemas/error.json", stdout)
	var out contract.ErrorOutput
	json.Unmarshal([]byte(stdout), &out)
	wantDetails := map[string]any{"path": path, "schema": float64(7), "supported": float64(state.SchemaVersion())}
	if out.Error.Code != contract.CodeStateNewer || !reflect.DeepEqual(map[string]any(out.Error.Details), wantDetails) {
		t.Errorf("code %q, details %v", out.Error.Code, out.Error.Details)
	}
}

func TestEventsHidden(t *testing.T) {
	_, help, _ := clitest.Run("help")
	if strings.Contains(help, "events") {
		t.Errorf("events must not be listed in the help:\n%s", help)
	}
}
