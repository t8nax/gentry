package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
)

// TestHookCodexBudget checks the built program's Codex session start hook
// with a task, a stale plugin and an untrusted worktree. Keep it sequential:
// other tests must not compete with the measurements of its process budget.
func TestHookCodexBudget(t *testing.T) {
	bin := buildGentry(t)
	t.Setenv(testkit.CodexThreadEnv, "")
	shop := takenShop(t, bin)
	codexHome := t.TempDir()
	t.Setenv(testkit.CodexHomeEnv, codexHome)
	t.Setenv(testkit.CodexProgramEnv, testkit.FakeCodex(t))
	t.Setenv(testkit.FakeCodexLogEnv, filepath.Join(codexHome, "codex.log"))
	t.Setenv(testkit.FakeCodexFailEnv, "")
	t.Setenv(testkit.FakeCodexRequirementsEnv, "")
	gentry(t, bin, shop, "setup", "codex")
	testkit.SetCodexTrust(t, codexHome, shop, "untrusted")
	testkit.StaleCodexPlugin(t, codexHome)

	// Acceptance tests in task own the words of both warnings and the
	// introduction. Here the complete output must still contain them in order.
	program, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatalf("resolve gentry program %s: %v", bin, err)
	}
	if strings.ContainsAny(program, " \t") {
		program = `"` + program + `"`
	}
	want := testkit.Lines(
		msg.Text(msg.IntroPluginStale),
		msg.Text(msg.HintIntroPluginStale)+": "+program+" setup codex",
		msg.Text(msg.IntroCodexUntrusted),
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskWorktree, shop),
		msg.Text(msg.TaskHeading, "SHOP-1", "Возврат"),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
		msg.Text(msg.TaskStage, testkit.Round("Ветка", "branch", 1)),
		msg.Text(msg.IntroNoSteps),
		msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)),
		"",
		msg.Text(msg.HintIntroContinue, "gentry:working-on-task"),
	)
	input, err := json.Marshal(map[string]string{
		"cwd": shop, "session_id": "s1", "source": "startup", "hook_event_name": "SessionStart",
	})
	if err != nil {
		t.Fatal(err)
	}
	run := func() time.Duration {
		t.Helper()
		start := time.Now()
		cmd := exec.Command(bin, "hook", "session-start", "--tool", "codex")
		cmd.Dir = shop
		cmd.Stdin = bytes.NewReader(input)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		elapsed := time.Since(start)
		if err != nil || stderr.Len() != 0 {
			t.Fatalf("hook --tool codex: %v, stderr:\n%s", err, &stderr)
		}
		if string(out) != want {
			t.Fatalf("the hook of Codex:\n%s\nwant both warnings and the introduction:\n%s", out, want)
		}
		if n := utf8.RuneCount(out); n > readBudget {
			t.Errorf("the output of the hook of Codex is %d characters, the budget %d", n, readBudget)
		}
		return elapsed
	}
	const warmup, runs = 3, 30
	for range warmup {
		run()
	}
	times := make([]time.Duration, runs)
	for i := range times {
		times[i] = run()
	}
	slices.Sort(times)
	// The Codex case uses the upper middle duration, including for an even
	// number of runs; median in startup_test.go averages the middle pair.
	upperMedian := times[len(times)/2]
	t.Logf("the hook of Codex in a worktree with a task, both warnings, %d runs: median %s, output %d characters",
		runs, upperMedian, utf8.RuneCountInString(want))
	if upperMedian > hookBudget {
		t.Errorf("the median of the hook of Codex is %s, the budget %s", upperMedian, hookBudget)
	}
}
