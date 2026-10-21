package stage_test

import (
	"encoding/json"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-70, 7. Команды в сессии Codex — записи агента: a command of the command
// line run in the shell of a session of Codex, which sets CODEX_THREAD_ID,
// makes its records as the agent.

// decisionSource records a decision of the operator by the command line in
// the worktree of a task, with CODEX_THREAD_ID set to thread unless it is
// "", and returns the source of the decision task show gives.
func decisionSource(t *testing.T, thread string) string {
	t.Helper()
	_, fix := testkit.TaskShop(t)
	testkit.MustRun(t, testkit.TakeArgs("--worktree", fix)...)
	t.Chdir(fix)
	t.Setenv(testkit.CodexThreadEnv, thread)
	testkit.MustRun(t, "task", "decision", "add", "--answer", "Да")
	t.Setenv(testkit.CodexThreadEnv, "")
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-show.json", "task", "show", "--statement")
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(out), &shown)
	if len(shown.OperatorDecisions) != 1 {
		t.Fatalf("task show --statement --json: %s\nwant one decision", out)
	}
	return string(shown.OperatorDecisions[0].Source)
}

// TestCodexSessionSource checks the source of a record made by the command
// line in a session of Codex and outside one.
func TestCodexSessionSource(t *testing.T) {
	t.Run("codex_session", func(t *testing.T) {
		if got := decisionSource(t, "t1"); got != "agent" {
			t.Errorf("the source of the decision with CODEX_THREAD_ID: %s, want agent", got)
		}
	})
	t.Run("outside_session", func(t *testing.T) {
		if got := decisionSource(t, ""); got != "operator" {
			t.Errorf("the source of the decision without CODEX_THREAD_ID, CLAUDECODE and GENTRY_SESSION: %s, want operator", got)
		}
	})
}
