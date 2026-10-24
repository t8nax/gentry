package update_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-74-spec-1: the commands are expectations from the specification, not
// calls to the formatter of the program under test.
func psHintPath(p string) string {
	return "& '" + strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’").Replace(p) + "'"
}

func oldHintPath(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

func windowsHints(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("GEN-74: PowerShell hints of Codex on Windows")
	}
}

// hintStand uses the existing update stand, with a non-release build so
// executing the update hint cannot request a release from the network.
func hintStand(t *testing.T, folder string) (*stand, string) {
	t.Helper()
	s := newStand(t)
	s.dataIn(folder)
	s.installedFile(builds.dev)
	s.useCodex()
	s.set(testkit.CodexThreadEnv, "gen-74-thread")
	other := filepath.Join(s.root, "other", "gentry"+exe)
	copyFile(t, builds.dev, other)
	return s, other
}

func hintHook(s *stand, dir, tool string) result {
	s.t.Helper()
	in, err := json.Marshal(map[string]string{
		"session_id": "gen-74", "cwd": dir,
		"source": "startup", "hook_event_name": "SessionStart",
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return s.runEnv(s.program(), dir, bytes.NewReader(in), nil,
		"hook", "session-start", "--tool", tool)
}

// hintCommand returns the command exactly as received, after the first
// colon and space. It does not trim or repair the command for the shell.
func hintCommand(t *testing.T, hint string) string {
	t.Helper()
	_, command, ok := strings.Cut(hint, ": ")
	if !ok || command == "" || strings.ContainsAny(command, "\r\n") {
		t.Fatalf("not a single hint line: %q", hint)
	}
	return command
}

func hookHintCommand(t *testing.T, r result) string {
	t.Helper()
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("SessionStart: %s", r)
	}
	ls := strings.Split(r.stdout, "\n")
	if len(ls) < 3 {
		t.Fatalf("SessionStart has no warning and hint: %s", r)
	}
	return hintCommand(t, ls[1])
}

func updateHintCommand(t *testing.T, r result) string {
	t.Helper()
	if r.code != 1 || r.stdout != "" {
		t.Fatalf("update: %s", r)
	}
	_, hint, ok := strings.Cut(r.stderr, "\n\n")
	if !ok || !strings.HasSuffix(hint, "\n") {
		t.Fatalf("update has no separate hint block: %s", r)
	}
	return hintCommand(t, strings.TrimSuffix(hint, "\n"))
}

type hintErrorOutput struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
		Hint    string            `json:"hint"`
	} `json:"error"`
}

func readHintError(t *testing.T, r result, running, installed string) hintErrorOutput {
	t.Helper()
	if r.code != 1 || r.stderr != "" {
		t.Fatalf("update --json: %s", r)
	}
	testkit.Validate(t, "schemas/error.json", r.stdout)
	var e hintErrorOutput
	if err := json.Unmarshal([]byte(r.stdout), &e); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"program": running}
	if installed != "" {
		want["installed"] = installed
	}
	if e.Error.Code != "program_elsewhere" || !reflect.DeepEqual(e.Error.Details, want) {
		t.Errorf("update --json: code %q, details %#v; want program_elsewhere, %#v",
			e.Error.Code, e.Error.Details, want)
	}
	return e
}

func wantHintCommand(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("command from hint:\n%s\nwant:\n%s", got, want)
	}
}

// runHintPowerShell passes the received command unchanged to a separate
// PowerShell process. EncodedCommand transports Unicode and quotes without
// another shell parsing them. Only the output encoding and propagation of
// the native program's exit code surround the command.
func runHintPowerShell(t *testing.T, s *stand, command string) result {
	t.Helper()
	ps, err := exec.LookPath("pwsh")
	if err != nil {
		ps, err = exec.LookPath("powershell")
	}
	if err != nil {
		t.Fatal("Windows acceptance stand requires PowerShell: ", err)
	}
	script := "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)\n" +
		"$OutputEncoding = [Console]::OutputEncoding\n" + command + "\nexit $LASTEXITCODE\n"
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], u)
	}
	return s.runIn(ps, s.root, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
}

func wantHintSetup(t *testing.T, s *stand, command string) {
	t.Helper()
	r := runHintPowerShell(t, s, command)
	if r.code != 0 || r.stderr != "" {
		t.Errorf("PowerShell executing received setup command: %s", r)
	}
	plugin, ok := s.codexPlugin()
	if !ok || !plugin.Installed || !plugin.Enabled || s.codexMarketplace() != s.codexIntegration() {
		t.Errorf("received setup command did not connect the stand's plugin: %+v, exists %v", plugin, ok)
	}
	after := hintHook(s, s.root, "codex")
	if after.code != 0 || after.stdout != "" || after.stderr != "" {
		t.Errorf("SessionStart after the received setup command: %s; want no warning", after)
	}
}

func wantHintUpdate(t *testing.T, s *stand, command string) result {
	t.Helper()
	got := runHintPowerShell(t, s, command)
	want := s.update()
	if got != want || got.code != 1 || got.stdout != "" {
		t.Errorf("PowerShell executing received update command: %s\nwant the installed program's refusal: %s", got, want)
	}
	// A different program, parse error or command-not-found cannot satisfy
	// the installed program's not_release refusal and its version.
	prefix := testkit.Lines("Установлена сборка не из выпуска.", "Версия: 0.0.0-dev")
	if !strings.HasPrefix(got.stderr, prefix) {
		t.Errorf("PowerShell did not reach the installed non-release build:\n%s", got.stderr)
	}
	s.wantNoRequests()
	return got
}

// hintData records the integration, settings, and state as bytes, including
// absence of files. It deliberately excludes the fake tool's invocation log.
func hintData(t *testing.T, s *stand) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{filepath.Join(s.data, "integrations"), filepath.Join(s.data, "state"), s.codexHome} {
		if !testkit.FileExists(root) {
			out[root] = "<absent>"
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = fmt.Sprintf("%x", sha256.Sum256(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func wantHintData(t *testing.T, s *stand, before map[string]string) {
	t.Helper()
	if after := hintData(t, s); !reflect.DeepEqual(after, before) {
		t.Errorf("integration, Codex settings or task state changed:\nafter: %#v\nbefore: %#v", after, before)
	}
}
