package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/t8nax/gentry/internal/hook"
)

// hookBudget is the time limit of the session start hook (technical
// solution, section 16); the median of the runs is checked, so that a single
// run slowed down by the OS does not fail the check.
const hookBudget = 100 * time.Millisecond

// TestHookBudget checks the session start hook against its time and output
// limits. The hook runs in an empty directory, outside any project.
func TestHookBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("hook budget check is skipped in short mode")
	}
	bin := buildGentry(t)
	dir := t.TempDir()

	cmd := exec.Command(bin, "hook", hook.SessionStart)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if n := utf8.RuneCount(out); n > hook.MaxOutput {
		t.Errorf("hook output is %d characters, limit %d", n, hook.MaxOutput)
	}

	const warmup, runs = 3, 30
	d := measure(t, bin, dir, warmup, runs, "hook", hook.SessionStart)
	platform := runtime.GOOS + "/" + runtime.GOARCH
	t.Logf("gentry hook %s on %s, %d runs: median %s, p95 %s, output %d characters",
		hook.SessionStart, platform, runs, ms(median(d)), ms(p95(d)), utf8.RuneCount(out))
	writeSummary(t, fmt.Sprintf(
		"### Session start hook, %s\n\n| Runs | Median | p95 | Budget | Output, characters |\n| --- | --- | --- | --- | --- |\n| %d | %s | %s | %s | %d |\n\n",
		platform, runs, ms(median(d)), ms(p95(d)), ms(hookBudget), utf8.RuneCount(out)))
	if median(d) > hookBudget {
		t.Errorf("hook median %s exceeds the budget %s", ms(median(d)), ms(hookBudget))
	}
}
