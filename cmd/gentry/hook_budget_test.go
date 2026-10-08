package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/integration"
)

// hookBudget is the time limit of the session start hook (technical
// solution, section 16); the median of the runs is checked, so that a single
// run slowed down by the OS does not fail the check.
const hookBudget = 100 * time.Millisecond

// readBudget is the limit of the texts of Gentry the agent reads before it
// starts to work, in characters (technical solution, section 16).
const readBudget = 10000

// flowNames is the room left for the titles of a scenario and a stage longer
// than those of the example flow; the introduction names both.
const flowNames = 2 * 80

// stalePlugin makes the hooks of the test run as hooks of a plugin of
// another version, so that the introduction starts with the remark: the
// longest introduction and the slowest hook.
func stalePlugin(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"version":"0.0.0+00000000"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(claude.PluginRootEnv, root)
}

// TestHookBudget checks the session start hook against its time and output
// limits outside any project and in a worktree with a task.
func TestHookBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("hook budget check is skipped in short mode")
	}
	bin := buildGentry(t)
	shop := takenShop(t, bin)
	gentry(t, bin, shop, "step", "add", "Создать ветку", "Проверить имя ветки")
	stalePlugin(t)

	platform := runtime.GOOS + "/" + runtime.GOARCH
	summary := fmt.Sprintf("### Session start hook, %s\n\n| Where | Runs | Median | p95 | Budget | Output, characters |\n| --- | --- | --- | --- | --- | --- |\n", platform)
	for _, place := range []struct{ name, dir string }{
		{"outside a project", t.TempDir()},
		{"worktree with a task", shop},
	} {
		cmd := exec.Command(bin, "hook", hook.SessionStart, "--tool", claude.Tool)
		cmd.Dir = place.dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook %s: %v", place.name, err)
		}
		n := utf8.RuneCount(out)
		if n > hook.MaxOutput {
			t.Errorf("hook output %s is %d characters, limit %d", place.name, n, hook.MaxOutput)
		}
		if place.name == "worktree with a task" && !strings.Contains(string(out), "gentry stage show") {
			t.Fatalf("no introduction to the task:\n%s", out)
		}

		const warmup, runs = 3, 30
		d := measure(t, bin, place.dir, warmup, runs, "hook", hook.SessionStart, "--tool", claude.Tool)
		t.Logf("gentry hook %s %s on %s, %d runs: median %s, p95 %s, output %d characters",
			hook.SessionStart, place.name, platform, runs, ms(median(d)), ms(p95(d)), n)
		summary += fmt.Sprintf("| %s | %d | %s | %s | %s | %d |\n", place.name, runs, ms(median(d)), ms(p95(d)), ms(hookBudget), n)
		if median(d) > hookBudget {
			t.Errorf("hook median %s %s exceeds the budget %s", ms(median(d)), place.name, ms(hookBudget))
		}
	}
	writeSummary(t, summary+"\n")
}

// TestReadBudget checks that what the agent reads before it starts to work
// fits the budget: the introduction to a task at its longest, the
// descriptions of all the skills, which the tool shows at session start, and
// the text of the skill of working on a task. The text of the skill of
// canceling a task is read only to cancel one.
func TestReadBudget(t *testing.T) {
	bin := buildGentry(t)
	shop := shopWithFlow(t, bin, 1)[0]
	gentry(t, bin, shop, "task", "take", "--scenario", "feature",
		"--title", string([]rune(strings.Repeat("Частичный возврат по карте. ", 3))[:80]), "--statement", "Постановка.")
	gentry(t, bin, shop, "step", "add", "Создать ветку")
	stalePlugin(t)

	cmd := exec.Command(bin, "hook", hook.SessionStart, "--tool", claude.Tool)
	cmd.Dir = shop
	intro, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	total := utf8.RuneCount(intro) + flowNames
	parts := fmt.Sprintf("introduction %d (with %d for flow names)", total, flowNames)
	d := integration.Gentry(bin)
	for _, s := range d.Skills {
		n := utf8.RuneCountInString(s.Description)
		total += n
		parts += fmt.Sprintf(", description of %s %d", s.Name, n)
		if s.Name == agenttext.WorkingOnTask {
			n := utf8.RuneCountInString(s.Text)
			total += n
			parts += fmt.Sprintf(", text of %s %d", s.Name, n)
		}
	}
	t.Logf("read before work: %d characters: %s", total, parts)
	if total > readBudget {
		t.Errorf("the agent reads %d characters before work, budget %d: %s", total, readBudget, parts)
	}
}
