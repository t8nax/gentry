package task_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

func TestTaskTake(t *testing.T) {
	shop, fix := clitest.TaskShop(t)

	code, stdout, stderr := clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	want := strings.Join([]string{
		msg.Text(msg.TaskTaken, "SHOP-1"),
		msg.Text(msg.TaskTitle, "Частичный возврат по карте"),
		msg.Text(msg.TaskScenario, "Фича"),
		msg.Text(msg.TaskStage, "Ветка"),
		msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
		msg.Text(msg.TaskWorktree, fix),
		"",
		cli.HintFor(msg.HintTaskShow, "task", "SHOP-1"),
	}, "\n") + "\n"
	if code != contract.ExitOK || stderr != "" || stdout != want {
		t.Fatalf("exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	// From the worktree the hint needs no number.
	t.Chdir(fix)
	code, stdout, stderr = clitest.Run("task", "show")
	want = clitest.Lines(
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskState, msg.Text(msg.TaskStateActive)),
		msg.Text(msg.TaskScenario, msg.Text(msg.TaskNamed, "Фича", "feature")),
		msg.Text(msg.TaskStage, msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, "Ветка", "branch"), 1)),
		msg.Text(msg.ProgressLine, msg.Text(msg.ProgressValue, 0, 5)),
		msg.Text(msg.TaskTakenAt, "<время>"),
		msg.Text(msg.TaskFlowApplied, "<время>"),
		msg.Text(msg.TaskWorktree, fix),
		"",
	) + clitest.Table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", msg.Text(msg.OutcomeCurrent), msg.Text(msg.ValueNone)},
	) + clitest.Lines(
		"",
		msg.Text(msg.StepsNone),
		"",
		cli.HintText(msg.HintStatement),
	)
	if code != contract.ExitOK || stderr != "" || clitest.Masked(stdout) != want {
		t.Errorf("task show: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	// The repeat is refused: the worktree holds the task.
	code, _, stderr = clitest.Run(clitest.TakeArgs()...)
	want = msg.Text(msg.ErrWorktreeBusy, "SHOP-1", fix) + "\n\n" + cli.HintFor(msg.HintTaskShow, "task", "SHOP-1") + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("repeat: exit code %d, stderr %q, want %q", code, stderr, want)
	}

	// A statement of several lines from a file, in the main worktree.
	t.Chdir(shop)
	input := filepath.Join(t.TempDir(), "task.json")
	clitest.WriteFiles(t, filepath.Dir(input), map[string]string{"task.json": `{"scenario":"bug","title":"Двойное списание","statement":"Первая строка.\n\nТретья строка."}`})
	code, stdout, _ = clitest.Run("task", "take", "--input", input)
	if code != contract.ExitOK || !strings.HasPrefix(stdout, msg.Text(msg.TaskTaken, "SHOP-2")+"\n") ||
		!strings.HasSuffix(stdout, "\n\n"+cli.HintText(msg.HintTaskShow)+"\n") {
		t.Errorf("take from a file: exit code %d, output:\n%s", code, stdout)
	}
	t.Chdir(filepath.Dir(shop))
	clitest.WantRun(t, contract.ExitOK, clitest.Lines(
		msg.Text(msg.StatementHeading, "SHOP-2"),
		"  Первая строка.",
		"",
		"  Третья строка.",
		"",
		msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
	), "", "task", "show", "shop-2", "--statement")
	_, stdout, _ = clitest.Run("task", "show", "shop-2")
	stage := msg.Text(msg.TaskStage, msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, "Ветка", "branch"), 1))
	if !strings.Contains(stdout, stage+"\n") || strings.Contains(stdout, "Первая строка") ||
		!strings.HasSuffix(stdout, "\n\n"+cli.HintFor(msg.HintStatement, "task", "SHOP-2")+"\n") {
		t.Errorf("task show shop-2:\n%s", stdout)
	}
	clitest.WantRun(t, contract.ExitUsage, "", clitest.Lines(msg.Text(msg.ErrConflictingFlags, "--path", "--statement"), "", cli.HintText(msg.HintCommandHelp, "task show")),
		"task", "show", "shop-2", "--statement", "--path")

	// Outside a project the list has the project column.
	header := []string{msg.Text(msg.ColProject), msg.Text(msg.ColNumber), msg.Text(msg.ColTitle), msg.Text(msg.ColState),
		msg.Text(msg.ColScenario), msg.Text(msg.ColStage), msg.Text(msg.ColWorktree)}
	active := msg.Text(msg.TaskStateActive)
	if _, stdout, _ := clitest.Run("task", "list"); stdout != clitest.Table(header,
		[]string{"shop", "SHOP-1", "Частичный возврат по карте", active, "Фича", "Ветка", fix},
		[]string{"shop", "SHOP-2", "Двойное списание", active, "Баг", "Ветка", shop}) {
		t.Errorf("task list outside the project:\n%s", stdout)
	}
	t.Chdir(fix)
	if _, stdout, _ := clitest.Run("task", "list"); stdout != clitest.Table(header[1:],
		[]string{"SHOP-1", "Частичный возврат по карте", active, "Фича", "Ветка", fix},
		[]string{"SHOP-2", "Двойное списание", active, "Баг", "Ветка", shop}) {
		t.Errorf("task list in the project:\n%s", stdout)
	}
	if _, stdout, _ := clitest.Run("worktree", "list"); stdout != clitest.Table(
		[]string{msg.Text(msg.ColProject), msg.Text(msg.ColWorktree), msg.Text(msg.ColBranch), msg.Text(msg.ColState)},
		[]string{"shop", shop, "main", msg.Text(msg.WorktreeMain) + ", " + msg.Text(msg.WorktreeTask, "SHOP-2")},
		[]string{"shop", fix, "fix", msg.Text(msg.WorktreeTask, "SHOP-1")}) {
		t.Errorf("worktree list:\n%s", stdout)
	}
}

func TestTaskJSON(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	t.Chdir(fix)
	code, stdout, _ := clitest.Run(clitest.TakeArgs("--json")...)
	if code != contract.ExitOK {
		t.Fatalf("exit code %d: %s", code, stdout)
	}
	clitest.Validate(t, "schemas/task-take.json", stdout)
	var out contract.TaskTakeOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	tk := out.Task
	if tk.Id != "SHOP-1" || tk.Project != "shop" || tk.State != "active" || tk.Scenario.Title != "Фича" ||
		tk.Stage.Node != "branch" || tk.Stage.Id != "branch" || tk.Stage.Title != "Ветка" || tk.Worktree == nil ||
		*tk.Worktree != fix || tk.Statement.Source != "operator" || tk.Statement.Text != clitest.Statement || tk.Flow.Commit == "" {
		t.Errorf("task %+v", tk)
	}
	if !regexp.MustCompile(`"taken":"[^"]+\.\d{3}Z"`).MatchString(stdout) {
		t.Errorf("the time of taking is not in UTC with milliseconds: %s", stdout)
	}

	code, show, _ := clitest.Run("task", "show", "--json")
	clitest.Validate(t, "schemas/task-show.json", show)
	var shown contract.TaskShowOutput
	json.Unmarshal([]byte(show), &shown)
	if code != contract.ExitOK || !clitest.JSONEqual(shown.Task, out.Task) {
		t.Errorf("task show --json:\n%s\nwant the task of:\n%s", show, stdout)
	}
	_, list, _ := clitest.Run("task", "list", "--json")
	clitest.Validate(t, "schemas/task-list.json", list)
	if !strings.Contains(list, `"id":"SHOP-1"`) || strings.Contains(list, `"statement"`) {
		t.Errorf("task list --json: %s", list)
	}
	_, wl, _ := clitest.Run("worktree", "list", "--json")
	clitest.Validate(t, "schemas/worktree-list.json", wl)
	if !strings.Contains(wl, `"path":`+clitest.JSONString(fix)+`,"project":"shop","task":"SHOP-1"`) {
		t.Errorf("worktree list --json: %s", wl)
	}

	_, events, _ := clitest.Run("events")
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
	clitest.Validate(t, "schemas/events/task.taken.json", string(e.Data))
	want := `{"attempt":1,"flow_commit":"` + tk.Flow.Commit + `","node":"branch","scenario":"feature","source":"operator","title":"Частичный возврат по карте","worktree":` + clitest.JSONString(fix) + `}`
	if string(e.Data) != want {
		t.Errorf("event data %s, want %s", e.Data, want)
	}
}

// TestTaskTakeByAgent checks the source of the statement: the tool of the
// agent and a command in a session of Claude Code are the agent's, a command
// outside a session is the operator's.
func TestTaskTakeByAgent(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	input := `{"scenario":"feature","title":"Частичный возврат по карте","statement":"` + clitest.Statement + `","worktree":` + clitest.JSONString(fix) + `}`
	text, failed := cli.CallTool("task_take", []byte(input))
	if failed || !strings.Contains(text, msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceAgent))+"\n") {
		t.Fatalf("by the tool of the agent: failed %v, output:\n%s", failed, text)
	}
	t.Chdir(shop)
	if _, stdout, _ := clitest.Run(clitest.TakeArgs()...); !strings.Contains(stdout, msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator))+"\n") {
		t.Errorf("by the operator:\n%s", stdout)
	}
	_, stdout, _ := clitest.Run("task", "show", "SHOP-1", "--json")
	if !strings.Contains(stdout, `"source":"agent"`) {
		t.Errorf("task show --json: %s", stdout)
	}

	// A command in a session of Claude Code is the agent's, whoever typed it.
	clitest.MustRun(t, "task", "cancel", "SHOP-2")
	t.Setenv(caller.ClaudeCodeEnv, "1")
	if _, stdout, _ := clitest.Run(clitest.TakeArgs()...); !strings.Contains(stdout, msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceAgent))+"\n") {
		t.Errorf("in a session of Claude Code:\n%s", stdout)
	}

	// The hooks of an earlier plugin before and after a tool call and at the
	// end of a turn do nothing and never fail; the session start hook
	// introduces the current directory.
	_, intro, _ := clitest.RunWith("{}", "hook", "session-start")
	for e, want := range map[string]string{"pre-tool": "", "post-tool": "", "stop": "", "session-start": intro} {
		if code, stdout, stderr := clitest.RunWith("not json", "hook", e); code != contract.ExitOK || stdout != want || stderr != "" {
			t.Errorf("hook %s with bad input: exit code %d, stdout %q, stderr %q", e, code, stdout, stderr)
		}
	}
}
func TestTaskTakeRefusals(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	root := filepath.Dir(shop)
	input := func(text string) string {
		p := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	long := strings.Repeat("я", 81)
	help := cli.HintText(msg.HintCommandHelp, "task take")
	tests := []struct {
		name   string
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{"no scenario", []string{"task", "take", "--title", "Т", "--statement", "С"}, contract.ExitUsage, contract.CodeMissingField,
			msg.Text(msg.ErrScenarioMissing) + "\n\n" + cli.HintText(msg.HintFlowScenarios)},
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
			msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotFound)) + "\n\n" + help},
		{"input not object", []string{"task", "take", "--input", input(`["feature"]`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotObject)) + "\n\n" + help},
		{"input with more", []string{"task", "take", "--input", input(`{} {}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotObject)) + "\n\n" + help},
		{"unknown field", []string{"task", "take", "--input", input(`{"scenario":"feature","source":"agent"}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputUnknownField, "source")) + "\n\n" + help},
		{"not a string", []string{"task", "take", "--input", input(`{"scenario":"feature","title":5}`)}, contract.ExitUsage, contract.CodeInputInvalid,
			msg.Text(msg.ErrInputInvalid, msg.Text(msg.InputNotString, "title")) + "\n\n" + help},
		{"unknown scenario", []string{"task", "take", "--scenario", "epic", "--title", "Т", "--statement", "С"}, contract.ExitError, contract.CodeFlowObjectNotFound,
			msg.Text(msg.ErrFlowObjectNotFound, "shop", msg.Text(msg.FlowKindScenario), "epic") + "\n\n" + cli.HintText(msg.HintFlowObjects)},
		{"not pooled", clitest.TakeArgs("--worktree", root), contract.ExitError, contract.CodeWorktreeNotPooled,
			msg.Text(msg.ErrWorktreeNotPooled, root) + "\n\n" + cli.HintText(msg.HintWorktreeAdd)},
	}
	for _, tt := range tests {
		code, stdout, stderr := clitest.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want %d, %q", tt.name, code, stdout, stderr, tt.exit, tt.stderr)
		}
		code, stdout, _ = clitest.Run(append(tt.args, "--json")...)
		clitest.Validate(t, "schemas/error.json", stdout)
		var e contract.ErrorOutput
		json.Unmarshal([]byte(stdout), &e)
		if code != tt.exit || e.Error.Code != tt.code {
			t.Errorf("%s in JSON: exit code %d, code %q; want %d, %q", tt.name, code, e.Error.Code, tt.exit, tt.code)
		}
	}

	// A worktree with uncommitted changes; ignored files do not count.
	clitest.WriteFiles(t, fix, map[string]string{".gitignore": "*.log\n"})
	gittest.Run(t, fix, "add", ".gitignore")
	gittest.Run(t, fix, "commit", "--quiet", "-m", "ignore logs")
	clitest.WriteFiles(t, fix, map[string]string{"build.log": "x", "backend/payments/refund.go": "package payments\n"})
	code, _, stderr := clitest.Run(clitest.TakeArgs("--worktree", fix)...)
	want := msg.Text(msg.ErrWorktreeDirty, fix) + "\n\n" + msg.Text(msg.WorktreeChangedFiles) + "\n  backend/payments/refund.go\n\n" + msg.Text(msg.HintWorktreeDirty) + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("dirty: exit code %d, stderr %q, want %q", code, stderr, want)
	}
	if _, stdout, _ := clitest.Run(clitest.TakeArgs("--worktree", fix, "--json")...); !strings.Contains(stdout, `"files":["backend/payments/refund.go"]`) {
		t.Errorf("dirty in JSON: %s", stdout)
	}
	// No number is used up by refusals.
	if _, stdout, _ := clitest.Run(clitest.TakeArgs()...); !strings.HasPrefix(stdout, msg.Text(msg.TaskTaken, "SHOP-1")) {
		t.Errorf("after refusals:\n%s", stdout)
	}
}

func TestTaskTakeWithoutFlow(t *testing.T) {
	t.Setenv(caller.SessionEnv, "")
	t.Setenv(caller.ClaudeCodeEnv, "")
	p := clitest.EmptyShopFlow(t)
	code, _, stderr := clitest.Run(clitest.TakeArgs()...)
	want := msg.Text(msg.ErrFlowNotFound, "shop") + "\n" + msg.Text(msg.FlowDir, p.Dir) + "\n"
	if code != contract.ExitError || stderr != want {
		t.Errorf("exit code %d, stderr %q, want %q", code, stderr, want)
	}
}

func TestTaskShowRefusals(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	root := filepath.Dir(shop)
	t.Chdir(root)
	tests := []struct {
		args   []string
		exit   int
		code   string
		stderr string
	}{
		{[]string{"task", "show"}, contract.ExitError, contract.CodeTaskUndetermined,
			msg.Text(msg.ErrTaskUndetermined, root) + "\n\n" + cli.HintText(msg.HintTaskList)},
		{[]string{"task", "show", "SHOP-9"}, contract.ExitError, contract.CodeTaskNotFound,
			msg.Text(msg.ErrTaskNotFound, "SHOP-9") + "\n\n" + cli.HintText(msg.HintTaskListAll)},
		{[]string{"task", "show", "shop"}, contract.ExitUsage, contract.CodeInvalidArgument,
			msg.Text(msg.ErrTaskKeyInvalid, "shop") + "\n\n" + msg.Text(msg.HintTaskKey)},
	}
	for _, tt := range tests {
		code, _, stderr := clitest.Run(tt.args...)
		if code != tt.exit || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, code, stderr, tt.stderr)
		}
		_, stdout, _ := clitest.Run(append(tt.args, "--json")...)
		clitest.Validate(t, "schemas/error.json", stdout)
		if !strings.Contains(stdout, `"code":"`+tt.code+`"`) {
			t.Errorf("%v in JSON: %s", tt.args, stdout)
		}
	}
	// A worktree of the pool without a task.
	t.Chdir(fix)
	if code, _, stderr := clitest.Run("task", "show"); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrTaskUndetermined, fix)) {
		t.Errorf("free worktree: exit code %d, stderr %q", code, stderr)
	}
}

func TestTaskList(t *testing.T) {
	_, fix := clitest.TaskShop(t)
	if _, stdout, _ := clitest.Run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("no tasks: %q", stdout)
	}
	if _, stdout, _ := clitest.Run("task", "list", "--all"); stdout != msg.Text(msg.TasksNone)+"\n" {
		t.Errorf("no tasks, --all: %q", stdout)
	}
	if _, stdout, _ := clitest.Run("task", "list", "--json"); stdout != `{"tasks":[]}`+"\n" {
		t.Errorf("no tasks in JSON: %q", stdout)
	}
	clitest.MustRun(t, clitest.TakeArgs("--worktree", fix)...)
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
		_, stdout, _ := clitest.Run(append(tt.args, "--json")...)
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
	if _, stdout, _ := clitest.Run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("closed only: %q", stdout)
	}
	if _, stdout, _ := clitest.Run("task", "list", "--all"); !strings.Contains(stdout, "SHOP-1") || !strings.Contains(stdout, msg.Text(msg.TaskStateClosed)) {
		t.Errorf("--all:\n%s", stdout)
	}

	help := cli.HintText(msg.HintCommandHelp, "task list")
	refusals := []struct {
		args   []string
		code   string
		stderr string
	}{
		{[]string{"task", "list", "--all", "--state", "active"}, contract.CodeConflictingFlags, msg.Text(msg.ErrConflictingFlags, "--all", "--state") + "\n\n" + help},
		{[]string{"task", "list", "--state", "done"}, contract.CodeFlagValue, msg.Text(msg.ErrFlagValueInvalid, "--state", "done") + "\n\n" + help},
	}
	for _, tt := range refusals {
		code, _, stderr := clitest.Run(tt.args...)
		if code != contract.ExitUsage || stderr != tt.stderr+"\n" {
			t.Errorf("%v: exit code %d, stderr %q, want %q", tt.args, code, stderr, tt.stderr)
		}
		_, stdout, _ := clitest.Run(append(tt.args, "--json")...)
		if !strings.Contains(stdout, `"code":"`+tt.code+`"`) {
			t.Errorf("%v in JSON: %s", tt.args, stdout)
		}
	}
	if code, _, stderr := clitest.Run("task", "list", "--project", "cart"); code != contract.ExitError || !strings.HasPrefix(stderr, msg.Text(msg.ErrProjectNotFound, "cart")) {
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
	clitest.EmptyHome(t)
	t.Chdir(t.TempDir())
	if _, stdout, _ := clitest.Run("task", "list"); stdout != msg.Text(msg.TasksNoneOpen)+"\n" {
		t.Errorf("task list: %q", stdout)
	}
	if code, _, _ := clitest.Run("task", "show", "SHOP-1"); code != contract.ExitError {
		t.Errorf("task show: exit code %d", code)
	}
	if p, _ := state.Path(); clitest.FileExists(p) {
		t.Errorf("reading tasks must not create the state store %s", p)
	}
}

// TestTaskSnapshot checks that tasks of one flow share its snapshot, and that
// a change of a library subagent the flow names pins later tasks to the
// commit of the library.
func TestTaskSnapshot(t *testing.T) {
	shop, fix := clitest.TaskShop(t)
	commitOf := func(args ...string) string {
		t.Helper()
		_, stdout, _ := clitest.Run(append(args, "--json")...)
		var out contract.TaskTakeOutput
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("%v: %s", args, stdout)
		}
		return out.Task.Flow.Commit
	}
	first := commitOf(clitest.TakeArgs("--worktree", fix)...)
	third := gittest.Worktree(t, shop, filepath.Join(filepath.Dir(shop), "shop-3"), "third")
	clitest.MustRun(t, "worktree", "add", third)
	if second := commitOf(clitest.TakeArgs()...); second != first {
		t.Errorf("one flow, two commits: %s, %s", first, second)
	}

	// The reviewer of the library, named by the review stage, changes.
	lib := clitest.ShopPlaces().Library
	text, err := os.ReadFile(filepath.Join(lib, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteFiles(t, lib, map[string]string{"reviewer.md": string(text) + "\nПроверить логи.\n"})
	_, stdout, _ := clitest.Run("library", "apply", "--json")
	var applied contract.LibraryApplyOutput
	json.Unmarshal([]byte(stdout), &applied)
	if got := commitOf(clitest.TakeArgs("--worktree", third)...); got != applied.Applied.Commit || got == first {
		t.Errorf("after the library change: %s, want %s", got, applied.Applied.Commit)
	}
	_, stdout, _ = clitest.Run("task", "show", "SHOP-1", "--json")
	if !strings.Contains(stdout, `"commit":"`+first+`"`) {
		t.Errorf("the first task changed its flow: %s", stdout)
	}
}
