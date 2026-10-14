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
	for _, c := range []struct {
		args []string
		want string // the code of the failure
	}{
		{[]string{"--agent", "task", "step", "add", "Шаг", "--json"}, contract.CodeUnknownCommand},
		{[]string{"--tool", "task_take", "--json"}, contract.CodeUnknownCommand},
		{[]string{"task", "step", "add", "Шаг", "--json"}, contract.CodeInvalidArgument},
	} {
		var out bytes.Buffer
		code := run(c.args, cli.Env{Stdout: &out, Stderr: &out})
		var e contract.ErrorOutput
		if err := json.Unmarshal(out.Bytes(), &e); err != nil {
			t.Errorf("%v: exit code %d, output %s; not an error in JSON: %v", c.args, code, out.String(), err)
			continue
		}
		if code != contract.ExitUsage || e.Error.Code != c.want {
			t.Errorf("%v: exit code %d, output %s; want %d and %s", c.args, code, out.String(), contract.ExitUsage, c.want)
		}
	}
}
