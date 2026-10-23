package update_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-72: each setup starts with its own data and Claude configuration.
// These cases use the same command entry in process and on an acceptance build.
type claudeRollbackStand struct {
	t          *testing.T
	dir, state string
}

func rollbackStand(t *testing.T) *claudeRollbackStand {
	t.Helper()
	root := t.TempDir()
	t.Setenv(testkit.HomeEnv, filepath.Join(root, "data"))
	t.Setenv(testkit.ClaudeConfigEnv, filepath.Join(root, "config"))
	t.Setenv(testkit.ClaudeProgramEnv, claudeProgram(t))
	t.Setenv(fakeClaudeFailEnv, "")
	state := filepath.Join(root, "claude.json")
	t.Setenv(fakeClaudeStateEnv, state)
	return &claudeRollbackStand{t, filepath.Join(root, "data", "integrations", "claude"), state}
}

func (s *claudeRollbackStand) read() claudeState {
	s.t.Helper()
	var st claudeState
	if err := json.Unmarshal([]byte(readFile(s.t, s.state)), &st); err != nil {
		s.t.Fatal(err)
	}
	return st
}

func (s *claudeRollbackStand) seed(st claudeState) {
	s.t.Helper()
	b, err := json.Marshal(st)
	if err != nil {
		s.t.Fatal(err)
	}
	writeFile(s.t, s.state, string(b))
}

func wantAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: want absent, stat error = %v", path, err)
	}
}

func failedClaudeSetup(t *testing.T, command string) {
	t.Helper()
	code, out, stderr := testkit.Run("setup", "claude", "--json")
	if code != 1 || stderr != "" {
		t.Fatalf("setup: exit %d, stdout %s, stderr %s; want 1 and JSON only", code, out, stderr)
	}
	var got struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "tool_failed" || got.Error.Details["tool"] != "claude" || got.Error.Details["command"] != command || got.Error.Details["output"] != "simulated failure" {
		t.Fatalf("setup did not retain the original Claude failure: %s", out)
	}
}

func TestSetupClaudeRollbackFirstFailure(t *testing.T) {
	for _, action := range []string{"marketplace-add", "install"} {
		t.Run(action, func(t *testing.T) {
			s := rollbackStand(t)
			t.Setenv(fakeClaudeFailEnv, action)
			command := "claude plugin install gentry@gentry --scope user --json"
			if action == "marketplace-add" {
				command = "claude plugin marketplace add " + s.dir + " --json"
			}
			failedClaudeSetup(t, command)
			wantAbsent(t, s.dir)
			st := s.read()
			if len(st.Marketplaces) != 0 || len(st.Plugins) != 0 {
				t.Errorf("failed first setup left registrations: %+v", st)
			}
		})
	}
}

func TestSetupClaudeRollbackKeepsExisting(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		directory, marketplace bool
	}{
		{"directory", true, false}, {"marketplace", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := rollbackStand(t)
			if tc.directory {
				if err := os.MkdirAll(s.dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			st := claudeState{Marketplaces: []claudeMarketplace{{Name: "other", Source: "directory", Path: filepath.Join(t.TempDir(), "other")}}}
			if tc.marketplace {
				st.Marketplaces = append(st.Marketplaces, claudeMarketplace{Name: "gentry", Source: "directory", Path: s.dir})
			}
			s.seed(st)
			t.Setenv(fakeClaudeFailEnv, "install")
			failedClaudeSetup(t, "claude plugin install gentry@gentry --scope user --json")
			if tc.directory {
				if info, err := os.Stat(s.dir); err != nil || !info.IsDir() {
					t.Errorf("preexisting directory was not retained: %v", err)
				}
			} else {
				wantAbsent(t, s.dir)
			}
			got := s.read()
			if !slices.Equal(got.Marketplaces, st.Marketplaces) {
				t.Errorf("marketplaces: %+v, want preexisting %+v", got.Marketplaces, st.Marketplaces)
			}
			if len(got.Plugins) != 0 {
				t.Errorf("failed install left plugins: %+v", got.Plugins)
			}
		})
	}
}

func TestSetupClaudeRollbackRetry(t *testing.T) {
	s := rollbackStand(t)
	t.Setenv(fakeClaudeFailEnv, "install")
	failedClaudeSetup(t, "claude plugin install gentry@gentry --scope user --json")
	t.Setenv(fakeClaudeFailEnv, "")
	out := testkit.MustRun(t, "setup", "claude", "--json")
	var got struct {
		Action  string `json:"action"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "installed" || !got.Enabled {
		t.Errorf("retry: %s", out)
	}
	st := s.read()
	if len(st.Marketplaces) != 1 || st.Marketplaces[0].Name != "gentry" || !samePath(st.Marketplaces[0].Path, s.dir) || len(st.Plugins) != 1 || st.Plugins[0].ID != "gentry@gentry" || !st.Plugins[0].Enabled {
		t.Errorf("retry registrations: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "gentry", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSetupClaudeRollbackDeploy(t *testing.T) {
	s := rollbackStand(t)
	testkit.ToolsShopFlow(t, false, false)
	data := os.Getenv(testkit.HomeEnv)
	shop := testkit.MustWd(t)
	s.dir = filepath.Join(data, "integrations", "claude")
	t.Setenv(fakeClaudeFailEnv, "install")
	failedClaudeSetup(t, "claude plugin install gentry@gentry --scope user --json")
	out := testkit.WantJSON(t, 0, "schemas/agents-deploy.json", "agents", "deploy", "--project", "shop")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"project": `"shop"`, "changed": "false", "tools": "[]", "worktrees": "[]"} {
		if string(fields[key]) != want {
			t.Errorf("deploy %s = %s, want %s", key, fields[key], want)
		}
	}
	wantAbsent(t, filepath.Join(shop, ".claude", "agents"))
}

func TestSetupClaudeRollbackCleanupFailure(t *testing.T) {
	s := rollbackStand(t)
	proxy := filepath.Join(t.TempDir(), "claude-cleanup-failure"+exe)
	if err := goBuild(testkit.ModuleRoot(t), "./tests/testkit/failclaudecleanup", proxy); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GENTRY_TEST_CLAUDE_DELEGATE", claudeProgram(t))
	t.Setenv(testkit.ClaudeProgramEnv, proxy)
	t.Setenv(fakeClaudeFailEnv, "install")
	failedClaudeSetup(t, "claude plugin install gentry@gentry --scope user --json")
	wantAbsent(t, s.dir)
	st := s.read()
	if !slices.Contains(st.Log, "plugin marketplace remove gentry --json") {
		t.Errorf("cleanup was not attempted: %q", st.Log)
	}
	if len(st.Marketplaces) != 1 || st.Marketplaces[0].Name != "gentry" || len(st.Plugins) != 0 {
		t.Errorf("stand: failed cleanup must leave only the marketplace: %+v", st)
	}
}

// Update needs a release installed in its data root, so this case uses the
// same release stand as the other update cases (skipped on GENTRY_TEST_BINARY).
func TestSetupClaudeRollbackUpdate(t *testing.T) {
	parallel(t)
	s := installedStand(t, v020, false)
	shop := s.project("shop")
	s.set(fakeClaudeFailEnv, "install")
	r := s.gentry("setup", "claude", "--json")
	wantCode(t, r, 1)
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Error.Code != "tool_failed" {
		t.Fatalf("setup: %s", r)
	}
	s.set(fakeClaudeFailEnv, "")
	n := len(s.claude().Log)
	out, res := s.updateJSON()
	if res.Action != "updated" || res.Version != latest || len(res.Integrations) != 0 {
		t.Errorf("update after failed setup: %s; want updated release without integrations", out)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["agents"]; ok {
		t.Errorf("update unexpectedly laid out agents: %s", out)
	}
	wantAbsent(t, filepath.Join(shop, ".claude", "agents"))
	wantAbsent(t, s.integration())
	for _, call := range s.claudeCalls(n) {
		if !strings.HasSuffix(call, "list --json") {
			t.Errorf("update changed unconnected Claude: %s", call)
		}
	}
}
