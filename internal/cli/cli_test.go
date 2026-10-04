package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(args, Env{Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		code, stdout, stderr := run(args...)
		if code != ExitOK {
			t.Errorf("%v: exit code %d, want %d", args, code, ExitOK)
		}
		if stderr != "" {
			t.Errorf("%v: unexpected stderr: %q", args, stderr)
		}
		for _, c := range commands() {
			if !strings.Contains(stdout, c.name) {
				t.Errorf("%v: help does not list command %s:\n%s", args, c.name, stdout)
			}
		}
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run("version")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "gentry ") || !strings.HasSuffix(stdout, "\n") {
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
		if code != ExitUsage {
			t.Errorf("%v: exit code %d, want %d", args, code, ExitUsage)
		}
		if stdout != "" {
			t.Errorf("%v: unexpected stdout: %q", args, stdout)
		}
		if !strings.HasPrefix(stderr, "gentry: ") {
			t.Errorf("%v: unexpected stderr: %q", args, stderr)
		}
	}
}
