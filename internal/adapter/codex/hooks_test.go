package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	for _, bad := range []string{"{ no", `{"hooks": []}`, `{"hooks": {"SessionStart": {"hooks": []}}}`} {
		if _, _, err := hookEdit([]byte(bad), want); !errors.Is(err, errHooks) {
			t.Errorf("hookEdit of %s: %v, want the settings not read", bad, err)
		}
	}
}

// TestHookEditKeepsSigns checks that the hooks of the operator keep the
// signs &, < and > as they are written.
func TestHookEditKeepsSigns(t *testing.T) {
	operator := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "a && b < c > d"}]}]}}`
	out, changed, err := hookEdit([]byte(operator), handler(testExe))
	if err != nil || !changed || !strings.Contains(string(out), `"command": "a && b < c > d"`) || strings.Contains(string(out), `\u0026`) {
		t.Errorf("hookEdit = %v, %v:\n%s", changed, err, out)
	}
}

// TestHookCommand checks the command of the hook: the path always in double
// quotes, with forward slashes.
func TestHookCommand(t *testing.T) {
	if got := HookCommand("/usr/bin/gentry"); got != `"/usr/bin/gentry" hook session-start --tool codex` {
		t.Errorf("HookCommand = %q", got)
	}
	in, slashed := osPath(`C:\R&D\gentry.exe`, "/opt/R&D/gentry.exe")
	if got := HookCommand(in); got != `"`+slashed+`" hook session-start --tool codex` {
		t.Errorf("HookCommand of %s = %q", in, got)
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

// TestHookCommandWindows checks the handler of Gentry: the command and the
// command of Windows, which runs the program by the call operator of
// PowerShell.
func TestHookCommandWindows(t *testing.T) {
	if got := HookCommandWindows(testExe); got != `& '/opt/Gentry Tools/gentry' hook session-start --tool codex` {
		t.Errorf("HookCommandWindows = %q", got)
	}
	// $ and ` are not expanded in single quotes; a single quote is doubled.
	in, slashed := osPath(`C:\gen70 a$b\it's`+"`"+`\gentry.exe`, "/opt/gen70 a$b/it's`/gentry.exe")
	if got := HookCommandWindows(in); got != `& '`+strings.ReplaceAll(slashed, "'", "''")+`' hook session-start --tool codex` {
		t.Errorf("HookCommandWindows of %s = %q", in, got)
	}
	out, _, err := hookEdit(nil, handler(testExe))
	var file struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err != nil || json.Unmarshal(out, &file) != nil {
		t.Fatalf("hookEdit = %v\n%s", err, out)
	}
	h := file.Hooks[sessionStart][0].Hooks[0]
	if h["command"] != HookCommand(testExe) || h["commandWindows"] != HookCommandWindows(testExe) || h["additionalContextLimit"] != float64(0) {
		t.Errorf("the handler of Gentry: %v", h)
	}
}

// TestHookEditWithoutWindows checks that a handler of Gentry without the
// command of Windows, as an earlier Gentry wrote it, is replaced, and the
// handler is there once.
func TestHookEditWithoutWindows(t *testing.T) {
	old := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "` + strings.ReplaceAll(HookCommand(testExe), `"`, `\"`) + `", "timeout": 10, "additionalContextLimit": 0}]}]}}`
	out, changed, err := hookEdit([]byte(old), handler(testExe))
	if err != nil || !changed {
		t.Fatalf("hookEdit = %v, %v", changed, err)
	}
	if got := handlersOf(t, out)[sessionStart]; len(got) != 1 || !strings.Contains(string(out), `"commandWindows"`) {
		t.Errorf("handlers: %v\n%s", got, out)
	}
	p := filepath.Join(t.TempDir(), "hooks.json")
	os.WriteFile(p, []byte(old), 0o644)
	if hookCurrent(p, testExe) {
		t.Error("a handler without the command of Windows is current")
	}
	other := strings.Replace(string(out), "& ", "", 1)
	os.WriteFile(p, []byte(other), 0o644)
	if hookCurrent(p, testExe) {
		t.Error("a handler with another command of Windows is current")
	}
	// The command of Windows of the path in double quotes, as Gentry wrote
	// it before, is replaced.
	stale := strings.Replace(string(out), string(raw(HookCommandWindows(testExe))), string(raw("& "+HookCommand(testExe))), 1)
	if stale == string(out) {
		t.Fatal("stand: no command of Windows to make stale")
	}
	os.WriteFile(p, []byte(stale), 0o644)
	if hookCurrent(p, testExe) {
		t.Error("a handler with the command of Windows in double quotes is current")
	}
	if again, changed, err := hookEdit([]byte(stale), handler(testExe)); err != nil || !changed || len(handlersOf(t, again)[sessionStart]) != 1 || !strings.Contains(string(again), string(raw(HookCommandWindows(testExe)))) {
		t.Errorf("hookEdit of the stale handler: %v, %v\n%s", changed, err, again)
	}
	os.WriteFile(p, out, 0o644)
	if !hookCurrent(p, testExe) {
		t.Error("the handler of Gentry is not current")
	}
}

// TestFindHook checks that the hook of Gentry is found in the list of Codex
// by its command and by its command of Windows, which Codex lists on
// Windows, in its settings of the hooks only.
func TestFindHook(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	for _, command := range []string{HookCommand(testExe), HookCommandWindows(testExe)} {
		listed := []listedHook{{Key: "other", Command: "echo start"}, {Key: "gentry", Command: command, SourcePath: p}}
		if h, ok := findHook(listed, p, hookCommands(testExe)); !ok || h.Key != "gentry" {
			t.Errorf("findHook by %q = %+v, %v", command, h, ok)
		}
	}
	listed := []listedHook{{Key: "elsewhere", Command: HookCommand(testExe), SourcePath: filepath.Join(t.TempDir(), "hooks.json")}}
	if _, ok := findHook(listed, p, hookCommands(testExe)); ok {
		t.Error("the hook of other settings of the hooks is found")
	}
	if _, ok := findHook([]listedHook{{Command: "echo start"}}, p, hookCommands(testExe)); ok {
		t.Error("a hook of the operator is found")
	}
}

// osPath returns a path of the system of the test, the one of Windows or the
// other: a backslash separates a path only on Windows. It returns the path
// with forward slashes too, as the commands of the hook write it.
func osPath(windows, other string) (path, slashed string) {
	if runtime.GOOS == "windows" {
		return windows, strings.ReplaceAll(windows, `\`, "/")
	}
	return other, other
}
