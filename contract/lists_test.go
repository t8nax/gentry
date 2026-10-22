package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSetupOutputLists checks that the lists of worktrees of Codex are
// written when they are set, empty ones too, and are absent when they are
// not, as for Claude Code.
func TestSetupOutputLists(t *testing.T) {
	claude := SetupOutput{Tool: "claude", Dir: "d", PluginVersion: "1", Action: "installed", Enabled: true}
	b, err := json.Marshal(claude)
	if err != nil || strings.Contains(string(b), "worktrees_") {
		t.Errorf("setup of claude: %s, %v; want no lists of worktrees", b, err)
	}
	codex := claude
	codex.Tool, codex.WorktreesTrusted, codex.WorktreesDistrusted = "codex", []string{}, []string{"/w"}
	b, err = json.Marshal(codex)
	if err != nil || !strings.Contains(string(b), `"worktrees_trusted":[]`) || !strings.Contains(string(b), `"worktrees_distrusted":["/w"]`) {
		t.Errorf("setup of codex: %s, %v; want worktrees_trusted [] and worktrees_distrusted [\"/w\"]", b, err)
	}
	// The signs of a path are written as they are, as the commands write
	// their output.
	codex.Dir = "/home/dev/R&D <shop>/integrations/codex"
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(codex); err != nil || !strings.Contains(buf.String(), `"dir":"/home/dev/R&D <shop>/integrations/codex"`) {
		t.Errorf("setup of codex with & and <: %s, %v", buf.String(), err)
	}
	var back SetupOutput
	if err := json.Unmarshal(b, &back); err != nil || back.Tool != "codex" || len(back.WorktreesDistrusted) != 1 {
		t.Errorf("setup of codex read back: %+v, %v", back, err)
	}
}
