package update_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// 1 and 8. The commands of installation and maintenance are commands of the
// command line only: the help names update, and gentry mcp serves neither
// update nor setup. These cases run the commands of testkit.

// TestHelpUpdate checks that the help names update in the section of
// installation and maintenance.
func TestHelpUpdate(t *testing.T) {
	code, stdout, _ := testkit.Run("--help")
	if code != 0 {
		t.Fatalf("--help: exit code %d", code)
	}
	ls := lines(stdout)
	at := slices.Index(ls, "Установка и обслуживание:")
	if at < 0 {
		t.Fatalf("the help has no section «Установка и обслуживание:»:\n%s", stdout)
	}
	line := regexp.MustCompile(`^  update +Обновить Gentry до последнего выпуска$`)
	for _, l := range ls[at+1:] {
		if l == "" {
			break
		}
		if line.MatchString(l) {
			return
		}
	}
	t.Errorf("the section «Установка и обслуживание:» has no line of update with «Обновить Gentry до последнего выпуска»:\n%s", stdout)
}

// An rpcResponse is an answer of gentry mcp.
type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// serveMCP runs gentry mcp with the requests after initialize, the request
// i with the id i+2, and returns the answers by id.
func serveMCP(t *testing.T, requests ...string) map[int]rpcResponse {
	t.Helper()
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	for i, r := range requests {
		in += fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,%s}`, i+2, r) + "\n"
	}
	code, stdout, stderr := testkit.RunWith(in, "mcp")
	if code != 0 {
		t.Fatalf("mcp: exit code %d, stderr:\n%s", code, stderr)
	}
	out := map[int]rpcResponse{}
	for _, l := range lines(stdout) {
		var r rpcResponse
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("mcp: %v: %s", err, l)
		}
		out[r.ID] = r
	}
	return out
}

// toolNames returns the names of the tools gentry mcp serves.
func toolNames(t *testing.T) []string {
	t.Helper()
	r, ok := serveMCP(t, `"method":"tools/list"`)[2]
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if !ok || r.Error != nil || json.Unmarshal(r.Result, &list) != nil {
		t.Fatalf("tools/list: %+v", r)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// TestUpdateNotTool checks that the command update is no tool of the agent.
func TestUpdateNotTool(t *testing.T) {
	// The command is there: it is no tool because it is a command of the
	// command line.
	if code, _, stderr := testkit.Run("update", "--help"); code != 0 {
		t.Fatalf("update --help: exit code %d, stderr:\n%s", code, stderr)
	}
	if names := toolNames(t); slices.Contains(names, "update") {
		t.Errorf("gentry mcp serves the tool update: %q", names)
	}
}

// TestToolList checks the names of the tools gentry mcp serves: setup is not
// one of them.
func TestToolList(t *testing.T) {
	want := []string{"agents_deploy", "flow_apply", "flow_diff", "flow_discard", "flow_show",
		"process_remote_set", "process_show", "process_sync", "project_add", "project_list",
		"task_artifact_save", "task_attempt_list", "task_attempt_show", "task_cancel", "task_close",
		"task_decision_add", "task_flow_show", "task_list", "task_note_add", "task_note_list",
		"task_show", "task_stage_exit", "task_stage_show", "task_stage_skip", "task_step_add",
		"task_step_done", "task_step_drop", "task_take", "worktree_add", "worktree_list"}
	if got := toolNames(t); !slices.Equal(got, want) {
		t.Errorf("the tools of gentry mcp:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestSetupToolUnknown checks that a call of the tool setup is the error of
// MCP of an unknown tool and builds no plugin: the fake Claude Code has no
// plugin of Gentry and the data root no integration of Claude Code.
func TestSetupToolUnknown(t *testing.T) {
	root := testkit.EmptyHome(t)
	state := filepath.Join(root, "claude-state.json")
	t.Setenv(testkit.ClaudeProgramEnv, claudeProgram(t))
	t.Setenv(fakeClaudeStateEnv, state)
	t.Setenv(testkit.ClaudeConfigEnv, filepath.Join(root, "claude-config"))
	r, ok := serveMCP(t, `"method":"tools/call","params":{"name":"setup","arguments":{"tool":"claude"}}`)[2]
	if !ok || r.Error == nil || r.Error.Code != -32602 || !strings.Contains(r.Error.Message, "unknown tool") {
		t.Errorf("tools/call setup: %+v (result %s), want the error -32602 of an unknown tool", r, r.Result)
	}
	if p := filepath.Join(root, "integrations", "claude"); testkit.FileExists(p) {
		t.Errorf("the call built the plugin in %s", p)
	}
	b, err := os.ReadFile(state)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var st claudeState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("the state of the fake Claude Code: %v\n%s", err, b)
	}
	if slices.ContainsFunc(st.Plugins, func(p claudePlugin) bool { return p.ID == pluginID }) {
		t.Errorf("the call registered the plugin %s in Claude Code: %s", pluginID, b)
	}
}

// TestUpdateHelp checks the help of update: its description and that of
// the flag --json.
func TestUpdateHelp(t *testing.T) {
	code, stdout, stderr := testkit.Run("update", "--help")
	if code != 0 {
		t.Fatalf("update --help: exit code %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	ls := lines(stdout)
	const first = "Обновить Gentry до последнего выпуска и пересоздать подключённые интеграции с ИИ-инструментами."
	if len(ls) == 0 || ls[0] != first {
		t.Errorf("update --help:\n%s\nwant the first line «%s»", stdout, first)
	}
	at := slices.Index(ls, "Флаги:")
	line := regexp.MustCompile(`^  --json +Вывести результат в формате JSON\.$`)
	if at >= 0 {
		for _, l := range ls[at+1:] {
			if l == "" {
				break
			}
			if line.MatchString(l) {
				return
			}
		}
	}
	t.Errorf("the section «Флаги:» of update --help has no line of --json with «Вывести результат в формате JSON.»:\n%s", stdout)
}

// TestSetupInSession checks that gentry setup runs in a session of an AI
// tool as outside one.
func TestSetupInSession(t *testing.T) {
	root := testkit.EmptyHome(t)
	t.Setenv(testkit.ClaudeProgramEnv, claudeProgram(t))
	t.Setenv(fakeClaudeStateEnv, filepath.Join(root, "claude-state.json"))
	t.Setenv(testkit.ClaudeConfigEnv, filepath.Join(root, "claude-config"))
	testkit.MustRun(t, "setup", "claude")
	code, stdout, stderr := testkit.Run("setup", "claude")
	if code != 0 {
		t.Fatalf("setup claude outside a session: exit code %d, stderr:\n%s", code, stderr)
	}
	t.Setenv(testkit.ClaudeCodeEnv, "1")
	testkit.WantRun(t, 0, stdout, stderr, "setup", "claude")
}

// TestSetupHelp checks that the help of setup says that gentry update builds
// the plugin anew itself.
func TestSetupHelp(t *testing.T) {
	code, stdout, _ := testkit.Run("setup", "--help")
	want := []string{
		"Подключить Gentry к ИИ-инструменту: сформировать и зарегистрировать плагин.",
		"После gentry update плагин пересоздаётся сам; после скрипта установки команду нужно повторить.",
	}
	if ls := lines(stdout); code != 0 || len(ls) < 2 || !slices.Equal(ls[:2], want) {
		t.Errorf("setup --help: exit code %d, stdout:\n%s\nwant the first lines:\n%s", code, stdout, testkit.Lines(want...))
	}
}
