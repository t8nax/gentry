package caller

import "testing"

func TestByAgent(t *testing.T) {
	t.Setenv(SessionEnv, "")
	t.Setenv(ClaudeCodeEnv, "")
	if ByAgent() {
		t.Error("outside a session the operator calls")
	}
	t.Setenv(ClaudeCodeEnv, "1")
	if !ByAgent() {
		t.Error("a command in a session of Claude Code is not the agent's")
	}
	t.Setenv(ClaudeCodeEnv, "")
	t.Setenv(SessionEnv, "drv-1")
	if !ByAgent() {
		t.Error("a session of the driver is not the agent's")
	}
}
