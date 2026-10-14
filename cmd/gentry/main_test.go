//go:build !acceptance

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
)

// TestNoTestEntries checks that the build without the tag acceptance, the
// release, runs the command line alone: the entries of the build for the
// tests are unknown commands, and so are the commands only the agent runs.
func TestNoTestEntries(t *testing.T) {
	for _, args := range [][]string{
		{"--agent", "task", "step", "add", "Шаг", "--json"},
		{"--tool", "task_take", "--json"},
		{"task", "step", "add", "Шаг", "--json"},
	} {
		var out bytes.Buffer
		code := run(args, cli.Env{Stdout: &out, Stderr: &out})
		var e contract.ErrorOutput
		json.Unmarshal(out.Bytes(), &e)
		want := contract.CodeUnknownCommand
		if args[0] == "task" {
			want = contract.CodeInvalidArgument
		}
		if code != contract.ExitUsage || e.Error.Code != want {
			t.Errorf("%v: exit code %d, output %s; want %d and %s", args, code, out.String(), contract.ExitUsage, want)
		}
	}
}
