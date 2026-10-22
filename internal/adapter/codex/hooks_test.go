package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testExe = "/opt/Gentry Tools/gentry"

// handlersOf returns the commands of the handlers of the settings of the
// hooks by event.
func handlersOf(t *testing.T, data []byte) map[string][]string {
	t.Helper()
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	out := map[string][]string{}
	for e, groups := range file.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[e] = append(out[e], h.Command)
			}
		}
	}
	return out
}

// TestHookEdit checks that the handler of Gentry is written once, in place
// of those of Gentry of any program, and the hooks of the operator stay in
// their order.
func TestHookEdit(t *testing.T) {
	operator := `{
  "note": "mine",
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre"}]}],
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo start"}, {"type": "command", "command": "/old/gentry hook session-start --tool codex"}]},
      {"hooks": [{"type": "command", "command": "\"/other one/gentry\" hook session-start --tool codex"}]}
    ]
  }
}
`
	want := handler(testExe)
	out, changed, err := hookEdit([]byte(operator), want)
	if err != nil || !changed {
		t.Fatalf("hookEdit = %v, %v", changed, err)
	}
	got := handlersOf(t, out)
	if strings.Join(got["PreToolUse"], "|") != "echo pre" || strings.Join(got["SessionStart"], "|") != "echo start|"+HookCommand(testExe) {
		t.Errorf("handlers: %v\n%s", got, out)
	}
	if !strings.HasPrefix(string(out), "{\n  \"note\": \"mine\",\n  \"hooks\": {\n    \"PreToolUse\"") {
		t.Errorf("the keys of the operator lost their order:\n%s", out)
	}
	again, changed, err := hookEdit(out, want)
	if err != nil || changed || string(again) != string(out) {
		t.Errorf("hookEdit again: changed %v, %v", changed, err)
	}

	out, _, err = hookEdit(nil, want)
	if err != nil || strings.Join(handlersOf(t, out)[sessionStart], "|") != HookCommand(testExe) {
		t.Errorf("hookEdit of no settings: %s, %v", out, err)
	}
	if !strings.Contains(string(out), `"additionalContextLimit": 0`) {
		t.Errorf("the handler has no additionalContextLimit 0:\n%s", out)
	}
	if _, _, err := hookEdit([]byte("{ no"), want); !errors.Is(err, errHooks) {
		t.Errorf("hookEdit of no JSON: %v", err)
	}
}

// TestHookCommand checks the command of the hook: the path in double quotes
// only with a space.
func TestHookCommand(t *testing.T) {
	if got := HookCommand("/usr/bin/gentry"); got != "/usr/bin/gentry hook session-start --tool codex" {
		t.Errorf("HookCommand = %q", got)
	}
	if got := HookCommand(testExe); got != `"/opt/Gentry Tools/gentry" hook session-start --tool codex` {
		t.Errorf("HookCommand with a space = %q", got)
	}
}

// TestWriteHook checks the settings of the hooks it cannot read or write:
// they are left as they are.
func TestWriteHook(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	if got := writeHook(p, testExe); got != "" || !hookCurrent(p, testExe) {
		t.Errorf("writeHook of no settings = %q", got)
	}
	os.WriteFile(p, []byte("{ no"), 0o644)
	if got := writeHook(p, testExe); got != HookFailed {
		t.Errorf("writeHook of no JSON = %q, want failed", got)
	}
	if b, _ := os.ReadFile(p); string(b) != "{ no" {
		t.Errorf("the settings changed: %q", b)
	}
	if os.Geteuid() == 0 {
		return
	}
	os.WriteFile(p, []byte(`{"hooks": {}}`), 0o644)
	os.Chmod(p, 0o444)
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	if got := writeHook(p, testExe); got != HookUnwritten {
		t.Errorf("writeHook of read-only settings = %q, want unwritten", got)
	}
	if b, _ := os.ReadFile(p); string(b) != `{"hooks": {}}` {
		t.Errorf("the settings changed: %q", b)
	}
}
