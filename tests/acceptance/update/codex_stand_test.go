package update_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// The stand of the integration with Codex (GEN-70): CODEX_HOME of its own,
// the fake Codex of tests/testkit/fakecodex as GENTRY_CODEX, and its log.
// Until a case makes the fake Codex its program, the codex program of the
// stand does not exist.

// useCodex makes the fake Codex the codex program of the stand, with the
// folder of its settings made.
func (s *stand) useCodex() {
	s.t.Helper()
	s.codexProgram = testkit.FakeCodex(s.t)
	if err := os.MkdirAll(s.codexHome, 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// noCodex makes Codex unavailable: GENTRY_CODEX is not set and no codex is
// in PATH.
func (s *stand) noCodex() {
	s.codexProgram = ""
	s.set("PATH", testkit.PathWithoutCodex())
}

// connectCodex connects Codex by gentry setup codex of the installed program
// with the fake Codex: the shorthand «Codex подключён».
func (s *stand) connectCodex() {
	s.t.Helper()
	s.useCodex()
	s.mustIn(s.root, "setup", "codex")
	if !testkit.FileExists(s.codexIntegration()) {
		s.t.Fatalf("stand: setup codex made no folder %s", s.codexIntegration())
	}
}

// codexIntegration is the folder of the integration of Codex in the data
// root.
func (s *stand) codexIntegration() string { return filepath.Join(s.data, "integrations", "codex") }

// codexReviewer is the file of the subagent reviewer of Codex in the
// worktree.
func codexReviewer(worktree string) string {
	return filepath.Join(worktree, ".codex", "agents", "reviewer.toml")
}

// codexConfigFile and codexHooksFile are the settings of Codex and of its
// hooks of the stand.
func (s *stand) codexConfigFile() string { return testkit.CodexConfigFile(s.codexHome) }
func (s *stand) codexHooksFile() string  { return testkit.CodexHooksFile(s.codexHome) }

// codexConfig returns the settings of Codex of the stand.
func (s *stand) codexConfig() map[string]any {
	s.t.Helper()
	return testkit.ReadCodexConfig(s.t, s.codexHome)
}

// editCodexConfig changes the settings of Codex of the stand.
func (s *stand) editCodexConfig(edit func(cfg map[string]any)) {
	s.t.Helper()
	testkit.EditCodexConfig(s.t, s.codexHome, edit)
}

// writeCodexConfig writes the settings of Codex of the stand as text.
func (s *stand) writeCodexConfig(text string) {
	s.t.Helper()
	writeFile(s.t, s.codexConfigFile(), text)
}

// fileState is the content of a file, to tell whether it changed; "<нет>"
// for a file that does not exist.
func fileState(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "<нет>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// wantFileState checks that the file has the content of fileState.
func wantFileState(t *testing.T, p, want string) {
	t.Helper()
	if got := fileState(t, p); got != want {
		t.Errorf("%s changed:\n%s\nwas:\n%s", p, got, want)
	}
}

// codexCalls returns the commands of the fake Codex run after the first n,
// with the methods of its app-server as «app-server <method>».
func (s *stand) codexCalls(n int) []string {
	s.t.Helper()
	log := testkit.CodexLog(s.t, s.codexLog)
	if n > len(log) {
		return nil
	}
	return log[n:]
}

// codex runs the fake Codex of the stand with args and fails the test unless
// it succeeds.
func (s *stand) codex(args ...string) string {
	s.t.Helper()
	r := s.runIn(testkit.FakeCodex(s.t), s.root, args...)
	if r.code != 0 {
		s.t.Fatalf("stand: codex %s: %s", strings.Join(args, " "), r)
	}
	return r.stdout
}

// A codexPlugin is a plugin as codex plugin list --json gives it.
type codexPlugin struct {
	PluginID          string `json:"pluginId"`
	Version           string `json:"version"`
	Installed         bool   `json:"installed"`
	Enabled           bool   `json:"enabled"`
	MarketplaceSource struct {
		Source string `json:"source"`
	} `json:"marketplaceSource"`
}

// codexPlugin returns the plugin of Gentry installed in the fake Codex;
// false if there is none.
func (s *stand) codexPlugin() (codexPlugin, bool) {
	s.t.Helper()
	var list struct {
		Installed []codexPlugin `json:"installed"`
	}
	if out := s.codex("plugin", "list", "--json"); json.Unmarshal([]byte(out), &list) != nil {
		s.t.Fatalf("stand: codex plugin list --json: %s", out)
	}
	for _, p := range list.Installed {
		if p.PluginID == testkit.CodexPluginID {
			return p, true
		}
	}
	return codexPlugin{}, false
}

// codexMarketplace returns the root of the marketplace gentry of the fake
// Codex; "" if there is none.
func (s *stand) codexMarketplace() string {
	s.t.Helper()
	var list struct {
		Marketplaces []struct {
			Name string `json:"name"`
			Root string `json:"root"`
		} `json:"marketplaces"`
	}
	if out := s.codex("plugin", "marketplace", "list", "--json"); json.Unmarshal([]byte(out), &list) != nil {
		s.t.Fatalf("stand: codex plugin marketplace list --json: %s", out)
	}
	for _, m := range list.Marketplaces {
		if m.Name == "gentry" {
			return m.Root
		}
	}
	return ""
}

// staleCodexPlugin makes the plugin of Gentry installed in the fake Codex one
// of a version no program builds: the shorthand «плагин другой версии».
func (s *stand) staleCodexPlugin() {
	s.t.Helper()
	testkit.StaleCodexPlugin(s.t, s.codexHome)
}

// disableCodexPlugin disables the plugin of Gentry in the settings of Codex,
// as the operator does.
func (s *stand) disableCodexPlugin() {
	s.t.Helper()
	s.editCodexConfig(func(cfg map[string]any) { toml.Set(cfg, false, "plugins", testkit.CodexPluginID, "enabled") })
}

// codexPluginEnabled returns plugins."gentry@gentry".enabled of the settings
// of Codex; false if it is not there.
func (s *stand) codexPluginEnabled() (bool, bool) {
	s.t.Helper()
	v, ok := toml.At(s.codexConfig(), "plugins", testkit.CodexPluginID, "enabled")
	b, isBool := v.(bool)
	return b, ok && isBool
}

// requirements makes the fake Codex answer configRequirements/read with the
// requirements, as an administrator sets them.
func (s *stand) requirements(req string) {
	s.t.Helper()
	p := filepath.Join(s.root, "requirements.json")
	writeFile(s.t, p, `{"requirements": `+req+`}`)
	s.set(testkit.FakeCodexRequirementsEnv, p)
}

// gentryHandlers returns the handlers of Gentry in the settings of the hooks
// of Codex of the stand.
func (s *stand) gentryHandlers() []testkit.CodexHandler {
	s.t.Helper()
	return testkit.GentryHandlers(s.t, s.codexHome)
}

// hookTrust returns the trust of the handler of Gentry as the app-server of
// the fake Codex gives it: trusted, untrusted, modified; "" if hooks/list
// gives no handler of Gentry.
func (s *stand) hookTrust() string {
	s.t.Helper()
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"test","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"hooks/list","params":{"cwds":[` + testkit.JSONString(s.root) + `]}}` + "\n"
	r := s.runEnv(testkit.FakeCodex(s.t), s.root, strings.NewReader(in), nil, "app-server")
	for _, l := range lines(r.stdout) {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Data []struct {
					Hooks []struct {
						Command     string `json:"command"`
						TrustStatus string `json:"trustStatus"`
					} `json:"hooks"`
				} `json:"data"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(l), &msg) != nil || msg.ID != 2 {
			continue
		}
		for _, d := range msg.Result.Data {
			for _, h := range d.Hooks {
				if strings.HasSuffix(h.Command, testkit.CodexHookSuffix) {
					return h.TrustStatus
				}
			}
		}
	}
	return ""
}

// wantHookCommand checks that the command of the handler names the program
// p: «<путь программы> hook session-start --tool codex», the path in double
// quotes or, with no space in it, without them.
func wantHookCommand(t *testing.T, h testkit.CodexHandler, p string) {
	t.Helper()
	cmd := h.Command()
	path, ok := strings.CutSuffix(cmd, " "+testkit.CodexHookSuffix)
	if strings.HasPrefix(path, `"`) && strings.HasSuffix(path, `"`) && len(path) > 1 {
		path = path[1 : len(path)-1]
	} else if strings.Contains(path, " ") {
		ok = false
	}
	if !ok || !samePath(path, p) {
		t.Errorf("the command of the hook of Gentry is %q, want «%s %s»", cmd, p, testkit.CodexHookSuffix)
	}
}

// A setupCodexOutput is the output of gentry setup codex --json, as the
// specification names its fields.
type setupCodexOutput struct {
	Tool                string   `json:"tool"`
	Dir                 string   `json:"dir"`
	PluginVersion       string   `json:"plugin_version"`
	Action              string   `json:"action"`
	PreviousDir         *string  `json:"previous_dir"`
	Enabled             *bool    `json:"enabled"`
	HookTrust           string   `json:"hook_trust"`
	HookError           *string  `json:"hook_error"`
	HooksFile           string   `json:"hooks_file"`
	HooksFeature        string   `json:"hooks_feature"`
	Sandbox             string   `json:"sandbox"`
	WorktreesTrusted    []string `json:"worktrees_trusted"`
	WorktreesDistrusted []string `json:"worktrees_distrusted"`
	Settings            string   `json:"settings"`
}

// setupCodexJSON runs gentry setup codex --json of the installed program
// with the flags, checks that it succeeds and matches the schema of setup,
// and returns the output, its fields and the names of its fields.
func (s *stand) setupCodexJSON(flags ...string) (string, setupCodexOutput, map[string]json.RawMessage) {
	s.t.Helper()
	r := s.gentry(append([]string{"setup", "codex", "--json"}, flags...)...)
	if r.code != 0 {
		s.t.Fatalf("setup codex --json: %s", r)
	}
	testkit.Validate(s.t, "schemas/setup.json", r.stdout)
	var out setupCodexOutput
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(r.stdout), &out) != nil || json.Unmarshal([]byte(r.stdout), &fields) != nil {
		s.t.Fatalf("setup codex --json: %s", r.stdout)
	}
	return r.stdout, out, fields
}

// setupError runs gentry setup <args> --json of the installed program,
// checks that it fails with the exit code 1 and an output of the schema of
// errors, and returns the code and the details of the error.
func (s *stand) setupError(args ...string) (string, map[string]any) {
	s.t.Helper()
	r := s.gentry(append(append([]string{"setup"}, args...), "--json")...)
	if r.code != 1 {
		s.t.Errorf("setup %s --json: %s\nwant exit code 1", strings.Join(args, " "), r)
	}
	testkit.Validate(s.t, "schemas/error.json", r.stdout)
	var e struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	json.Unmarshal([]byte(r.stdout), &e)
	return e.Error.Code, e.Error.Details
}

// wantSamePaths checks that the paths are those wanted, in any order, by
// the rules of the paths of the platform.
func wantSamePaths(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	ok := len(got) == len(want)
	for _, w := range want {
		if !slices.ContainsFunc(got, func(g string) bool { return samePath(g, w) }) {
			ok = false
		}
	}
	if !ok || got == nil {
		t.Errorf("%s: %q, want %q", what, got, want)
	}
}

// clone makes a clone of the repository of the project name next to it,
// <root>/<name>-<c>, and returns it: a copy that is no worktree of the
// repository.
func (s *stand) clone(name, c string) string {
	s.t.Helper()
	dir := s.worktree(name, c)
	gittest.Run(s.t, s.root, "clone", "--quiet", filepath.Join(s.root, name), dir)
	return dir
}

// writableRoots returns sandbox_workspace_write.writable_roots of the
// settings of Codex.
func (s *stand) writableRoots() []string {
	s.t.Helper()
	v, _ := toml.At(s.codexConfig(), "sandbox_workspace_write", "writable_roots")
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if str, ok := x.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// countPath returns how many of the paths name p by the rules of Codex.
func countPath(paths []string, p string) int {
	n := 0
	for _, x := range paths {
		if testkit.CodexSamePath(x, p) {
			n++
		}
	}
	return n
}
