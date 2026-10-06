package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// statement is the statement of the example of the plan.
const statement = "Клиент возвращает часть заказа. Деньги должны вернуться на карту, которой он платил."

// runWith runs a command with stdin as its standard input.
func runWith(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

// taskShop applies the flow of the shop and adds shop-fix to its pool. The
// test runs in the main worktree; the calls are the operator's. It returns
// the main worktree and shop-fix.
func taskShop(t *testing.T) (shop, fix string) {
	t.Helper()
	t.Setenv(caller.SessionEnv, "")
	t.Setenv(caller.ClaudeSessionEnv, "")
	shopFlow(t)
	root := filepath.Dir(mustWd(t))
	shop, fix = filepath.Join(root, "shop"), filepath.Join(root, "shop-fix")
	mustRun(t, "worktree", "add", fix)
	return shop, fix
}

func mustWd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// takeArgs are the flags of a task of the feature scenario.
func takeArgs(extra ...string) []string {
	return append([]string{"task", "take", "--scenario", "feature", "--title", "Частичный возврат по карте", "--statement", statement}, extra...)
}

func TestTaskTakeText(t *testing.T) {
	shop, fix := taskShop(t)

	code, stdout, stderr := run(takeArgs("--worktree", fix)...)
	want := strings.Join([]string{
		msg.Text(msg.TaskTaken, "SHOP-1"),
		msg.Text(msg.TaskTitle, "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, "Фича"),
		msg.Text(msg.TaskStage, "Ветка"),
		msg.Text(msg.TaskSource, "оператором"),
		msg.Text(msg.TaskWorktree, fix),
		"",
		msg.Text(msg.HintTaskShowKey, "SHOP-1"),
	}, "\n") + "\n"
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Fatalf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	// From the worktree the hint needs no number.
	t.Chdir(fix)
	code, stdout, stderr = run("task", "show")
	want = strings.Join([]string{
		"Задача SHOP-1: Частичный возврат по карте",
		"",
		"Проект: shop",
		"Состояние: в работе",
		"Сценарий: Фича (feature)",
		"Этап: Ветка (branch), круг 1",
		"Прогресс: 0 из 5",
		"Взята: <время>",
		"Флоу задачи применён: <время>",
		"Постановка записана: оператором",
		"Рабочая копия: " + fix,
		"",
		"Постановка:",
		"  " + statement,
		"",
		"ЭТАП   КРУГ  ИТОГ  ПЕРЕХОД",
		"Ветка  1     идёт  —",
		"",
		"У этапа нет шагов.",
	}, "\n") + "\n"
	if code != contract.ExitOK || stderr != "" || masked(stdout) != want {
		t.Errorf("task show: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	// The repeat is refused: the worktree holds the task.
	code, _, stderr = run(takeArgs()...)
	want = msg.Text(msg.ErrWorktreeBusy, "SHOP-1", fix) + "\n\n" + msg.Text(msg.HintTaskShowKey, "SHOP-1") + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("repeat: exit code %d, stderr %q, want %q", code, stderr, want)
	}

	// A statement of several lines from a file, in the main worktree.
	t.Chdir(shop)
	input := filepath.Join(t.TempDir(), "task.json")
	writeFiles(t, filepath.Dir(input), map[string]string{"task.json": `{"scenario":"bug","title":"Двойное списание","statement":"Первая строка.\n\nТретья строка."}`})
	code, stdout, _ = run("task", "take", "--input", input)
	if code != contract.ExitOK || !strings.HasPrefix(stdout, msg.Text(msg.TaskTaken, "SHOP-2")+"\n") ||
		!strings.HasSuffix(stdout, "\n\n"+msg.Text(msg.HintTaskShow)+"\n") {
		t.Errorf("take from a file: exit code %d, output:\n%s", code, stdout)
	}
	t.Chdir(filepath.Dir(shop))
	_, stdout, _ = run("task", "show", "shop-2")
	if !strings.Contains(stdout, "Постановка:\n  Первая строка.\n\n  Третья строка.\n\n") || !strings.Contains(stdout, "Этап: Ветка (branch), круг 1\n") {
		t.Errorf("task show shop-2:\n%s", stdout)
	}

	// Outside a project the list has the project column.
	header := []string{"ПРОЕКТ", "НОМЕР", "НАЗВАНИЕ", "СОСТОЯНИЕ", "СЦЕНАРИЙ", "ЭТАП", "РАБОЧАЯ КОПИЯ"}
	if _, stdout, _ := run("task", "list"); stdout != table(header,
		[]string{"shop", "SHOP-1", "Частичный возврат по карте", "в работе", "Фича", "Ветка", fix},
		[]string{"shop", "SHOP-2", "Двойное списание", "в работе", "Баг", "Ветка", shop}) {
		t.Errorf("task list outside the project:\n%s", stdout)
	}
	t.Chdir(fix)
	if _, stdout, _ := run("task", "list"); stdout != table(header[1:],
		[]string{"SHOP-1", "Частичный возврат по карте", "в работе", "Фича", "Ветка", fix},
		[]string{"SHOP-2", "Двойное списание", "в работе", "Баг", "Ветка", shop}) {
		t.Errorf("task list in the project:\n%s", stdout)
	}
	if _, stdout, _ := run("worktree", "list"); stdout != table([]string{"ПРОЕКТ", "РАБОЧАЯ КОПИЯ", "ВЕТКА", "СОСТОЯНИЕ"},
		[]string{"shop", shop, "main", "основная, задача SHOP-2"},
		[]string{"shop", fix, "fix", "задача SHOP-1"}) {
		t.Errorf("worktree list:\n%s", stdout)
	}
}

func TestTaskJSON(t *testing.T) {
	_, fix := taskShop(t)
	t.Chdir(fix)
	code, stdout, _ := run(takeArgs("--json")...)
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	validate(t, "schemas/task-take.json", stdout)
	var out contract.TaskTakeOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	tk := out.Task
	if tk.Id != "SHOP-1" || tk.Project != "shop" || tk.State != "active" || tk.Scenario.Title != "Фича" ||
		tk.Stage.Node != "branch" || tk.Stage.Id != "branch" || tk.Stage.Title != "Ветка" || tk.Worktree == nil ||
		*tk.Worktree != fix || tk.Statement.Source != "operator" || tk.Statement.Text != statement || tk.Flow.Commit == "" {
		t.Errorf("task %+v", tk)
	}
	if !regexp.MustCompile(`"taken":"[^"]+\.\d{3}Z"`).MatchString(stdout) {
		t.Errorf("the time of taking is not in UTC with milliseconds: %s", stdout)
	}

	code, show, _ := run("task", "show", "--json")
	validate(t, "schemas/task-show.json", show)
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(show), &shown)
	if code != contract.ExitOK || !jsonEqual(shown.Task, out.Task) {
		t.Errorf("task show --json:\n%s\nwant the task of:\n%s", show, stdout)
	}
	_, list, _ := run("task", "list", "--json")
	validate(t, "schemas/task-list.json", list)
	if !strings.Contains(list, `"id":"SHOP-1"`) || strings.Contains(list, `"statement"`) {
		t.Errorf("task list --json: %s", list)
	}
	_, wl, _ := run("worktree", "list", "--json")
	validate(t, "schemas/worktree-list.json", wl)
	if !strings.Contains(wl, `"path":`+jsonString(fix)+`,"project":"shop","task":"SHOP-1"`) {
		t.Errorf("worktree list --json: %s", wl)
	}

	_, events, _ := run("events")
	lines := strings.Split(strings.TrimSpace(events), "\n")
	last := lines[len(lines)-1]
	var e struct {
		Type    string          `json:"type"`
		Project string          `json:"project"`
		Task    string          `json:"task"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(last), &e)
	if e.Type != "task.taken" || e.Project != "shop" || e.Task != "SHOP-1" {
		t.Fatalf("last event: %s", last)
	}
	validate(t, "schemas/events/task.taken.json", string(e.Data))
	want := `{"flow_commit":"` + tk.Flow.Commit + `","node":"branch","scenario":"feature","source":"operator","title":"Частичный возврат по карте","worktree":` + jsonString(fix) + `}`
	if string(e.Data) != want {
		t.Errorf("event data %s, want %s", e.Data, want)
	}
}

// TestTaskTakeByAgent checks the source of the statement: a call of the agent
// is marked by the hooks; a command typed by the operator with ! in the same
// session is not.
func TestTaskTakeByAgent(t *testing.T) {
	shop, fix := taskShop(t)
	t.Setenv(caller.ClaudeSessionEnv, "sess-1")
	hookInput := `{"session_id":"sess-1","tool_use_id":"toolu_1","tool_name":"Bash","tool_input":{"command":"gentry task take"}}`
	if code, stdout, stderr := runWith(hookInput, "hook", "pre-tool"); code != contract.ExitOK || stdout != "" || stderr != "" {
		t.Fatalf("hook pre-tool: exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	input := `{"scenario":"feature","title":"Частичный возврат по карте","statement":"` + statement + `","worktree":` + jsonString(fix) + `}`
	code, stdout, stderr := runWith(input, "task", "take", "--input", "-")
	if code != contract.ExitOK || !strings.Contains(stdout, msg.Text(msg.TaskSource, "агентом со слов оператора")+"\n") {
		t.Fatalf("by the agent: exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	if code, _, _ := runWith(hookInput, "hook", "post-tool"); code != contract.ExitOK {
		t.Fatalf("hook post-tool: exit code %d", code)
	}
	t.Chdir(shop)
	if _, stdout, _ := run(takeArgs()...); !strings.Contains(stdout, msg.Text(msg.TaskSource, "оператором")+"\n") {
		t.Errorf("after the call of the agent:\n%s", stdout)
	}
	_, stdout, _ = run("task", "show", "SHOP-1", "--json")
	if !strings.Contains(stdout, `"source":"agent"`) {
		t.Errorf("task show --json: %s", stdout)
	}

	// Stop removes a mark left by an interrupted call.
	runWith(`{"session_id":"sess-1","tool_use_id":"toolu_2"}`, "hook", "pre-tool")
	if !caller.ByAgent() {
		t.Fatal("the call is not marked")
	}
	runWith(`{"session_id":"sess-1"}`, "hook", "stop")
	if caller.ByAgent() {
		t.Error("stop left the mark")
	}
	runWith(`{"session_id":"sess-1","tool_use_id":"toolu_3"}`, "hook", "pre-tool")
	runWith(`{"session_id":"sess-1","source":"resume"}`, "hook", "session-start")
	if caller.ByAgent() {
		t.Error("session-start left the mark")
	}
	// A hook with input it cannot read does nothing and does not fail.
	for _, e := range []string{"pre-tool", "post-tool", "stop", "session-start"} {
		if code, stdout, stderr := runWith("not json", "hook", e); code != contract.ExitOK || stdout != "" || stderr != "" {
			t.Errorf("hook %s with bad input: exit code %d, stdout %q, stderr %q", e, code, stdout, stderr)
		}
	}
	// A session of the driver is the agent's.
	t.Setenv(caller.ClaudeSessionEnv, "")
	t.Setenv(caller.SessionEnv, "drv-1")
	if !caller.ByAgent() {
		t.Error("a session of the driver is not the agent's")
	}
}

func TestTaskTakeRefusals(t *testing.T) {
	shop, fix := taskShop(t)
	root := filepath.Dir(shop)
	input := func(text string) string {
		p := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	long := strings.Repeat("я", 81)
	help := msg.Text(msg.HintCommandHelp, "task take")
	tests := []struct {
		name   string
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{"no scenario", []string{"task", "take", "--title", "Т", "--statement", "С"}, contract.ExitUsage, contract.CodeMissingField,
			msg.Text(msg.ErrScenarioMissing) + "\n\n" + msg.Text(msg.HintFlowScenarios)},
		{"no title", []string{"task", "take", "--scenario", "feature", "--statement", "С"}, contract.ExitUsage, contract.CodeMissingField,
			msg.Text(msg.ErrTitleMissing) + "\n\n" + help},
		{"no statement", []string{"task", "take", "--scenario", "feature", "--title", "Т"}, contract.ExitUsage, contract.CodeMissingField,
			msg.Text(msg.ErrStatementMissing) + "\n\n" + help},
		{"long title", []string{"task", "take", "--scenario", "feature", "--title", long, "--statement", "С"}, contract.ExitUsage, contract.CodeFieldInvalid,
			msg.Text(msg.ErrTitleTooLong) + "\n\n" + msg.Text(msg.HintTitle)},
		{"two lines", []string{"task", "take", "--input", input(`{"scenario":"feature","title":"А\nБ","statement":"С"}`)}, contract.ExitUsage, contract.CodeFieldInvalid,
			msg.Text(msg.ErrTitleMultiline) + "\n\n" + msg.Text(msg.HintTitle)},
		{"flag with input", []string{"task", "take", "--title", "Т", "--input", "x.json"}, contract.ExitUsage, contract.CodeConflictingFlags,
			msg.Text(msg.ErrConflictingFlags, "--title", "--input") + "\n\n" + help},
		{"input not found", []string{"task", "take", "--input", filepath.Join(root, "none.json")}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, "файл не найден") + "\n\n" + help},
		{"input not object", []string{"task", "take", "--input", input(`["feature"]`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, "текст не является объектом JSON") + "\n\n" + help},
		{"input with more", []string{"task", "take", "--input", input(`{} {}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, "текст не является объектом JSON") + "\n\n" + help},
		{"unknown field", []string{"task", "take", "--input", input(`{"scenario":"feature","source":"agent"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, "неизвестное поле «source»") + "\n\n" + help},
		{"not a string", []string{"task", "take", "--input", input(`{"scenario":"feature","title":5}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, "поле «title» должно быть строкой") + "\n\n" + help},
		{"unknown scenario", []string{"task", "take", "--scenario", "epic", "--title", "Т", "--statement", "С"}, contract.ExitError, contract.CodeFlowObjectNotFound,
			msg.Text(msg.ErrFlowObjectNotFound, "shop", "сценария", "epic") + "\n\n" + msg.Text(msg.HintFlowObjects)},
		{"not pooled", takeArgs("--worktree", root), contract.ExitError, contract.CodeWorktreeNotPooled,
			msg.Text(msg.ErrWorktreeNotPooled, root) + "\n\n" + msg.Text(msg.HintWorktreeAdd)},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want %d, %q", tt.name, code, stdout, stderr, tt.exit, tt.stderr)
		}
		code, stdout, _ = run(append(tt.args, "--json")...)
		validate(t, "schemas/error.json", stdout)
		var e contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &e)
		if code != tt.exit || e.Error.Code != tt.code {
			t.Errorf("%s in JSON: exit code %d, code %q; want %d, %q", tt.name, code, e.Error.Code, tt.exit, tt.code)
		}
	}

	// A worktree with uncommitted changes; ignored files do not count.
	writeFiles(t, fix, map[string]string{".gitignore": "*.log\n"})
	gittest.Run(t, fix, "add", ".gitignore")
	gittest.Run(t, fix, "commit", "--quiet", "-m", "ignore logs")
	writeFiles(t, fix, map[string]string{"build.log": "x", "backend/payments/refund.go": "package payments\n"})
	code, _, stderr := run(takeArgs("--worktree", fix)...)
	want := msg.Text(msg.ErrWorktreeDirty, fix) + "\n\n" + msg.Text(msg.WorktreeChangedFiles) + "\n  backend/payments/refund.go\n\n" + msg.Text(msg.HintWorktreeDirty) + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("dirty: exit code %d, stderr %q, want %q", code, stderr, want)
	}
	if _, stdout, _ := run(takeArgs("--worktree", fix, "--json")...); !strings.Contains(stdout, `"files":["backend/payments/refund.go"]`) {
		t.Errorf("dirty in JSON: %s", stdout)
	}
	// No number is used up by refusals.
	if _, stdout, _ := run(takeArgs()...); !strings.HasPrefix(stdout, msg.Text(msg.TaskTaken, "SHOP-1")) {
		t.Errorf("after refusals:\n%s", stdout)
	}
}

func TestTaskTakeWithoutFlow(t *testing.T) {
	t.Setenv(caller.SessionEnv, "")
	t.Setenv(caller.ClaudeSessionEnv, "")
	p := emptyShopFlow(t)
	code, _, stderr := run(takeArgs()...)
	want := msg.Text(msg.ErrFlowNotFound, "shop") + "\n" + msg.Text(msg.FlowDir, p.Dir) + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("exit code %d, stderr %q, want %q", code, stderr, want)
	}
}

func TestTaskShowRefusals(t *testing.T) {
	shop, fix := taskShop(t)
	root := filepath.Dir(shop)
	t.Chdir(root)
	tests := []struct {
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{[]string{"task", "show"}, contract.ExitError, contract.CodeTaskUndetermined,
			msg.Text(msg.ErrTaskUndetermined, root) + "\n\n" + msg.Text(msg.HintTaskList)},
		{[]string{"task", "show", "SHOP-9"}, contract.ExitError, contract.CodeTaskNotFound,
			msg.Text(msg.ErrTaskNotFound, "SHOP-9") + "\n\n" + msg.Text(msg.HintTaskListAll)},
		{[]string{"task", "show", "shop"}, contract.ExitUsage, contract.CodeInvalidArgument,
			msg.Text(msg.ErrTaskKeyInvalid, "shop") + "\n\n" + msg.Text(msg.HintTaskKey)},
	}
	for _, tt := range tests {
		code, _, stderr := run(tt.args...)
		if code != tt.exit || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, code, stderr, tt.stderr)
		}
		_, stdout, _ := run(append(tt.args, "--json")...)
		validate(t, "schemas/error.json", stdout)
		if !strings.Contains(stdout, `"code":"`+tt.code+`"`) {
			t.Errorf("%v in JSON: %s", tt.args, stdout)
		}
	}
	// A worktree of the pool without a task.
	t.Chdir(fix)
	if code, _, stderr := run("task", "show"); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrTaskUndetermined, fix)) {
		t.Errorf("free worktree: exit code %d, stderr %q", code, stderr)
	}
}

func TestTaskList(t *testing.T) {
	_, fix := taskShop(t)
	if _, stdout, _ := run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("no tasks: %q", stdout)
	}
	if _, stdout, _ := run("task", "list", "--all"); stdout != msg.Text(msg.TasksNone)+"\n" {
		t.Errorf("no tasks, --all: %q", stdout)
	}
	if _, stdout, _ := run("task", "list", "--json"); stdout != `{"tasks":[]}`+"\n" {
		t.Errorf("no tasks in JSON: %q", stdout)
	}
	mustRun(t, takeArgs("--worktree", fix)...)
	// A task of another state, until parts 11 and 12 can make one.
	setTaskState(t, "waiting")
	tests := []struct {
		args []string
		ids  string
	}{
		{[]string{"task", "list"}, "SHOP-1"},
		{[]string{"task", "list", "--state", "waiting"}, "SHOP-1"},
		{[]string{"task", "list", "--state", "active"}, ""},
		{[]string{"task", "list", "--project", "shop"}, "SHOP-1"},
	}
	for _, tt := range tests {
		_, stdout, _ := run(append(tt.args, "--json")...)
		var out contract.TaskListOutput
		json.Unmarshal([]byte(stdout), &out)
		var ids []string
		for _, it := range out.Tasks {
			ids = append(ids, it.Id)
		}
		if strings.Join(ids, " ") != tt.ids {
			t.Errorf("%v: %v, want %s", tt.args, ids, tt.ids)
		}
	}
	setTaskState(t, "closed")
	if _, stdout, _ := run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("closed only: %q", stdout)
	}
	if _, stdout, _ := run("task", "list", "--all"); !strings.Contains(stdout, "SHOP-1") || !strings.Contains(stdout, "закрыта") {
		t.Errorf("--all:\n%s", stdout)
	}

	help := msg.Text(msg.HintCommandHelp, "task list")
	refusals := []struct {
		args   []string
		code   string
		stderr string
	}{
		{[]string{"task", "list", "--all", "--state", "active"}, contract.CodeConflictingFlags, msg.Text(msg.ErrConflictingFlags, "--all", "--state") + "\n\n" + help},
		{[]string{"task", "list", "--state", "done"}, contract.CodeFlagValue, msg.Text(msg.ErrFlagValueInvalid, "--state", "done") + "\n\n" + help},
	}
	for _, tt := range refusals {
		code, _, stderr := run(tt.args...)
		if code != contract.ExitUsage || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, code, stderr, tt.stderr)
		}
		_, stdout, _ := run(append(tt.args, "--json")...)
		if !strings.Contains(stdout, `"code":"`+tt.code+`"`) {
			t.Errorf("%v in JSON: %s", tt.args, stdout)
		}
	}
	if code, _, stderr := run("task", "list", "--project", "cart"); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectNotFound, "cart")) {
		t.Errorf("unknown project: exit code %d, stderr %q", code, stderr)
	}
}

// setTaskState sets the state of every task.
func setTaskState(t *testing.T, s string) {
	t.Helper()
	path, _ := state.Path()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Write(func(tx *state.Tx) error {
		_, err := tx.Exec(`UPDATE tasks SET state = ?`, s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskWithoutStore(t *testing.T) {
	emptyHome(t)
	t.Chdir(t.TempDir())
	if _, stdout, _ := run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("task list: %q", stdout)
	}
	if code, _, _ := run("task", "show", "SHOP-1"); code != contract.ExitError {
		t.Errorf("task show: exit code %d", code)
	}
	if p, _ := state.Path(); fileExists(p) {
		t.Errorf("reading tasks must not create the state store %s", p)
	}
}

// TestTaskSnapshot checks that tasks of one flow share its snapshot, and that
// a change of a library subagent the flow names pins later tasks to the
// commit of the library.
func TestTaskSnapshot(t *testing.T) {
	shop, fix := taskShop(t)
	commitOf := func(args ...string) string {
		t.Helper()
		_, stdout, _ := run(append(args, "--json")...)
		var out contract.TaskTakeOutput
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("%v: %s", args, stdout)
		}
		return out.Task.Flow.Commit
	}
	first := commitOf(takeArgs("--worktree", fix)...)
	third := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-3"), "third")
	mustRun(t, "worktree", "add", third)
	if second := commitOf(takeArgs()...); second != first {
		t.Errorf("one flow, two commits: %s, %s", first, second)
	}

	// The reviewer of the library, named by the review stage, changes.
	lib := shopPlaces().Library
	text, err := os.ReadFile(filepath.Join(lib, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, lib, map[string]string{"reviewer.md": string(text) + "\nПроверить логи.\n"})
	_, stdout, _ := run("library", "apply", "--json")
	var applied contract.LibraryApplyOutput
	json.Unmarshal([]byte(stdout), &applied)
	if got := commitOf(takeArgs("--worktree", third)...); got != applied.Applied.Commit || got == first {
		t.Errorf("after the library change: %s, want %s", got, applied.Applied.Commit)
	}
	_, stdout, _ = run("task", "show", "SHOP-1", "--json")
	if !strings.Contains(stdout, `"commit":"`+first+`"`) {
		t.Errorf("the first task changed its flow: %s", stdout)
	}
}
