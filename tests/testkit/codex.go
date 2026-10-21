package testkit

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// The environment variables of the integration with Codex, as the
// specification of the task names them: the code of the program does not
// name them yet when the tests are written, so they are given here.
const (
	CodexProgramEnv = "GENTRY_CODEX"    // the codex program Gentry runs
	CodexHomeEnv    = "CODEX_HOME"      // the folder of the settings of Codex
	CodexThreadEnv  = "CODEX_THREAD_ID" // set by Codex for the commands of its sessions
)

// The variables the fake Codex reads: the command it fails, the file of the
// requirements of the administrator and the file it logs its commands to.
const (
	FakeCodexFailEnv         = "FAKECODEX_FAIL"
	FakeCodexRequirementsEnv = "FAKECODEX_REQUIREMENTS"
	FakeCodexLogEnv          = "FAKECODEX_LOG"
)

// The marks of the integration with Codex the tests look for.
const (
	// CodexHookSuffix ends the command of the session start hook of Gentry
	// in the settings of the hooks of Codex.
	CodexHookSuffix = "hook session-start --tool codex"
	// CodexPluginID is the plugin of Gentry in Codex.
	CodexPluginID = "gentry@gentry"
)

// exe is the suffix of a program on the platform of the tests.
var exe = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// fakeCodex holds the fake Codex, built once for the process of the tests.
var fakeCodex struct {
	once    sync.Once
	dir     string
	program string
	err     error
}

// moduleDir is the root of the module under test, found by the file of
// testkit, not by the current directory: the tests run in the worktrees of
// the shop, outside the module.
var moduleDir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}()

// FakeCodex returns the fake Codex of tests/testkit/fakecodex, built once for
// the package, whatever the current directory. The packages that use it
// keep the settings of the go command by KeepGo in their TestMain.
func FakeCodex(t *testing.T) string {
	t.Helper()
	fakeCodex.once.Do(func() {
		if fakeCodex.dir, fakeCodex.err = os.MkdirTemp("", "gentry-codex-"); fakeCodex.err != nil {
			return
		}
		fakeCodex.program = filepath.Join(fakeCodex.dir, "codex"+exe)
		cmd := exec.Command("go", "build", "-o", fakeCodex.program, "./tests/testkit/fakecodex")
		cmd.Dir = moduleDir
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeCodex.err = errors.New("go build ./tests/testkit/fakecodex: " + err.Error() + "\n" + string(out))
		}
	})
	if fakeCodex.err != nil {
		t.Fatal(fakeCodex.err)
	}
	return fakeCodex.program
}

// RemoveFakeCodex removes the build of the fake Codex; Main calls it after
// the tests.
func RemoveFakeCodex() {
	if fakeCodex.dir != "" {
		os.RemoveAll(fakeCodex.dir)
	}
}

// PathWithoutCodex returns the list of PATH without the folders that hold a
// program codex: Codex is not found there.
func PathWithoutCodex() string {
	var kept []string
	exts := []string{"", ".exe", ".cmd", ".bat", ".com", ".ps1"}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		// Lstat finds an alias of an application of Windows too.
		if slices.ContainsFunc(exts, func(e string) bool { _, err := os.Lstat(filepath.Join(dir, "codex"+e)); return err == nil }) {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// CodexHome returns the folder of the settings of Codex of the test:
// CODEX_HOME, inside the shop of the test, so that a layer keeps it.
func CodexHome() string { return os.Getenv(CodexHomeEnv) }

// UseCodex makes the fake Codex the codex program of the commands, with the
// folder of its settings made.
func UseCodex(t *testing.T) {
	t.Helper()
	t.Setenv(CodexProgramEnv, FakeCodex(t))
	if err := os.MkdirAll(CodexHome(), 0o755); err != nil {
		t.Fatal(err)
	}
}

// NoCodex makes Codex unavailable: no GENTRY_CODEX and no codex in PATH.
func NoCodex(t *testing.T) {
	t.Helper()
	t.Setenv(CodexProgramEnv, "")
	t.Setenv("PATH", PathWithoutCodex())
}

// ConnectCodex connects Codex: gentry setup codex with the fake Codex must
// succeed.
func ConnectCodex(t *testing.T) {
	t.Helper()
	UseCodex(t)
	if code, stdout, stderr := Run("setup", "codex"); code != contract.ExitOK {
		t.Fatalf("setup codex: exit code %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// ConnectClaude connects Claude Code as the layout of subagents sees it: the
// folder of its integration in the data root. The cases that need its plugin
// registered run setup claude with the fake Claude Code.
func ConnectClaude(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(os.Getenv(home.EnvVar), "integrations", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// DisconnectClaude removes the folder of the integration with Claude Code
// from the data root.
func DisconnectClaude(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(os.Getenv(home.EnvVar), "integrations", "claude")); err != nil {
		t.Fatal(err)
	}
}

// CodexConfigFile and CodexHooksFile are the settings of Codex and the
// settings of its hooks in the folder of its settings dir.
func CodexConfigFile(dir string) string { return filepath.Join(dir, "config.toml") }
func CodexHooksFile(dir string) string  { return filepath.Join(dir, "hooks.json") }

// ReadCodexConfig returns the settings of Codex in dir; empty if there are
// none.
func ReadCodexConfig(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(CodexConfigFile(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}
	}
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := toml.Parse(string(b))
	if err != nil {
		t.Fatalf("%s: %v\n%s", CodexConfigFile(dir), err, b)
	}
	return cfg
}

// WriteCodexConfig writes the settings of Codex in dir.
func WriteCodexConfig(t *testing.T, dir string, cfg map[string]any) {
	t.Helper()
	WriteFiles(t, dir, map[string]string{"config.toml": toml.Marshal(cfg) + "\n"})
}

// EditCodexConfig changes the settings of Codex in dir.
func EditCodexConfig(t *testing.T, dir string, edit func(cfg map[string]any)) {
	t.Helper()
	cfg := ReadCodexConfig(t, dir)
	edit(cfg)
	WriteCodexConfig(t, dir, cfg)
}

// A CodexHandler is a handler of an event in the settings of the hooks of
// Codex.
type CodexHandler struct {
	Event   string
	Handler map[string]any
}

// Command is the command of the handler.
func (h CodexHandler) Command() string {
	s, _ := h.Handler["command"].(string)
	return s
}

// ReadCodexHooks returns the settings of the hooks of Codex in dir as they
// are in the file; nil if there is none.
func ReadCodexHooks(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(CodexHooksFile(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v\n%s", CodexHooksFile(dir), err, b)
	}
	return v
}

// CodexHandlers returns the handlers of all events in the settings of the
// hooks of Codex in dir.
func CodexHandlers(t *testing.T, dir string) []CodexHandler {
	t.Helper()
	var out []CodexHandler
	events, _ := ReadCodexHooks(t, dir)["hooks"].(map[string]any)
	names := make([]string, 0, len(events))
	for e := range events {
		names = append(names, e)
	}
	slices.Sort(names)
	for _, e := range names {
		groups, _ := events[e].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				out = append(out, CodexHandler{Event: e, Handler: hm})
			}
		}
	}
	return out
}

// GentryHandlers returns the handlers of Gentry in the settings of the hooks
// of Codex in dir: those whose command ends with CodexHookSuffix.
func GentryHandlers(t *testing.T, dir string) []CodexHandler {
	t.Helper()
	var out []CodexHandler
	for _, h := range CodexHandlers(t, dir) {
		if strings.HasSuffix(h.Command(), CodexHookSuffix) {
			out = append(out, h)
		}
	}
	return out
}

// CodexSamePath reports whether a key of the settings of Codex names the
// path p by the rules of Codex: the path cleaned, without the prefix \\?\,
// and on Windows without regard to the case.
func CodexSamePath(key, p string) bool {
	a, b := filepath.Clean(strings.TrimPrefix(key, `\\?\`)), filepath.Clean(strings.TrimPrefix(p, `\\?\`))
	return a == b || runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

// CodexProjects returns the records of trust of the settings of Codex: the
// key of each record and its trust_level.
func CodexProjects(cfg map[string]any) map[string]string {
	out := map[string]string{}
	projects, _ := cfg["projects"].(map[string]any)
	for k, v := range projects {
		m, _ := v.(map[string]any)
		level, _ := m["trust_level"].(string)
		out[k] = level
	}
	return out
}

// CodexTrust returns the record of trust of the settings of Codex for the
// folder dir, compared by the rules of Codex: its key and trust_level; false
// if there is none.
func CodexTrust(cfg map[string]any, dir string) (key, level string, ok bool) {
	for k, l := range CodexProjects(cfg) {
		if CodexSamePath(k, dir) {
			return k, l, true
		}
	}
	return "", "", false
}

// SetCodexTrust writes a record of trust for dir in the settings of Codex in
// codexHome, under the key dir; "" removes the records of dir.
func SetCodexTrust(t *testing.T, codexHome, dir, level string) {
	t.Helper()
	EditCodexConfig(t, codexHome, func(cfg map[string]any) {
		for k := range CodexProjects(cfg) {
			if CodexSamePath(k, dir) {
				toml.Delete(cfg, "projects", k)
			}
		}
		if level != "" {
			toml.Set(cfg, map[string]any{"trust_level": level}, "projects", dir)
		}
	})
}

// CodexLog returns the lines the fake Codex logged to the file p: the
// commands and the methods of its app-server.
func CodexLog(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
}

// StaleCodexPlugin makes the plugin of Gentry installed in the fake Codex of
// codexHome one of the version 0.0.0+00000000, which no program builds: the
// folder of its version in the cache and the version in its manifest.
func StaleCodexPlugin(t *testing.T, codexHome string) {
	t.Helper()
	cache := filepath.Join(codexHome, "plugins", "cache", "gentry", "gentry")
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 {
		t.Fatalf("stand: the cache of the fake Codex has no plugin of Gentry: %v %v", entries, err)
	}
	const stale = "0.0.0+00000000"
	to := filepath.Join(cache, stale)
	if err := os.Rename(filepath.Join(cache, entries[0].Name()), to); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(to, ".codex-plugin", "plugin.json")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = stale
	b, _ = json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(manifest, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
