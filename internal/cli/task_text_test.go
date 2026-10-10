package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// The shop of the tests of the output: its feature scenario, task SHOP-1 at
// the first stage, and the worktrees of the pool.
const (
	shopMain = "/work/shop"
	shopFix  = "/work/shop-fix"
)

// shopTime is the time of the tests of the output: they run in UTC.
var shopTime = time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)

// shopNames are the names of the snapshot of the feature scenario of the shop.
func shopNames() names {
	n := names{read: true, stages: map[string]flow.Stage{}, nodes: map[string]string{
		"branch": "branch", "plan": "plan-feature", "implementation": "implementation", "review": "review", "merge": "merge",
	}}
	for _, st := range []flow.Stage{
		{ID: "branch", Title: "Ветка", Executor: "orchestrator"},
		{ID: "plan-feature", Title: "План фичи", Executor: "orchestrator"},
		{ID: "implementation", Title: "Реализация", Executor: "orchestrator"},
		{ID: "review", Title: "Ревью", Executor: "reviewer"},
		{ID: "merge", Title: "Слияние", Executor: flow.Operator},
	} {
		n.stages[st.ID] = st
	}
	return n
}

// shopTask is task SHOP-1 of the feature scenario at its first stage, taken
// in shop-fix.
func shopTask() contract.Task {
	fix := shopFix
	return contract.Task{
		Id: "SHOP-1", Attempt: 1, Project: "shop", Title: "Частичный возврат по карте", State: contract.TaskStateActive,
		Scenario:  contract.TaskScenario{Id: "feature", Title: "Фича"},
		Stage:     &contract.TaskStage{Node: "branch", Id: "branch", Title: "Ветка", Round: 1},
		Progress:  contract.TaskProgress{Passed: 0, TotalMin: 5, TotalMax: 5},
		Flow:      contract.Applied{Commit: "c0ffee", Time: shopTime},
		Statement: contract.TaskStatement{Text: "Клиент возвращает часть заказа.", Source: contract.TaskStatementSourceOperator},
		Taken:     shopTime,
		Worktree:  &fix,
	}
}

// currentPass is the pass of the current stage of shopTask.
func currentPass(steps ...contract.TaskStep) contract.TaskPass {
	if steps == nil {
		steps = []contract.TaskStep{}
	}
	return contract.TaskPass{Node: "branch", Stage: "branch", Round: 1, Entered: shopTime, Steps: steps}
}

func TestTaskTakeText(t *testing.T) {
	again := shopTask()
	again.Attempt = 2
	byAgent := shopTask()
	byAgent.Statement.Source = contract.TaskStatementSourceAgent
	tests := []struct {
		name       string
		out        contract.TaskTakeOutput
		here       bool
		cli, agent string
	}{
		{"taken", contract.TaskTakeOutput{Task: shopTask()}, false, lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show SHOP-1"),
		), lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "task_show (task: SHOP-1)"),
		)},
		{"in the worktree of the task", contract.TaskTakeOutput{Task: shopTask()}, true, lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show"),
		), lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "task_show"),
		)},
		{"taken anew", contract.TaskTakeOutput{Task: again}, true, lines(
			"Задача SHOP-1 взята заново.",
			"Попытка: 2",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show"),
		), lines(
			"Задача SHOP-1 взята заново.",
			"Попытка: 2",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: оператором",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "task_show"),
		)},
		{"by the agent", contract.TaskTakeOutput{Task: byAgent}, true, lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: агентом со слов оператора",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show"),
		), lines(
			"Задача SHOP-1 взята.",
			"Название: Частичный возврат по карте",
			"Сценарий: Фича",
			"Этап: Ветка",
			"Постановка записана: агентом со слов оператора",
			"Рабочая копия: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "task_show"),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { taskTakeText(p, tt.out, tt.here) }, tt.cli, tt.agent)
	}
}

func TestTaskShowText(t *testing.T) {
	inUTC(t)
	byLines := shopTask()
	byLines.Statement.Text = "Первая строка.\n\nТретья строка."
	// The test of task take states the words of the scenario, the stage and
	// the worktree.
	current := lines(
		"Задача SHOP-1: Частичный возврат по карте",
		"",
		"Проект: shop",
		"Состояние: в работе",
		msg.Text(msg.TaskScenario, "Фича (feature)"),
		msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
		"Прогресс: 0 из 5",
		"Взята: 2026-10-09 12:30",
		"Флоу задачи применён: 2026-10-09 12:30",
		msg.Text(msg.TaskWorktree, shopFix),
		"",
	) + table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", "идёт", "—"},
	) + lines(
		"",
		"У этапа нет шагов.",
		"",
	)
	// The scenario passed: a pass of each outcome, closed by the agent but the
	// last, the stage of the operator closed by the operator; the third has
	// steps, the fourth a reason.
	pass := func(node, stage, to string, exit *contract.TaskPassExit, src contract.TaskPassSource, steps ...contract.TaskStep) contract.TaskPass {
		outcome := contract.TaskPassOutcome(state.OutcomeExit)
		if exit == nil {
			outcome = contract.TaskPassOutcome(state.OutcomeSkip)
		}
		if steps == nil {
			steps = []contract.TaskStep{}
		}
		return contract.TaskPass{Node: node, Stage: stage, Round: 1, Entered: shopTime, Steps: steps,
			Outcome: &outcome, Source: &src, To: ptr(to), Closed: &shopTime, Exit: exit}
	}
	result := func(text string) *contract.TaskPassExit {
		return &contract.TaskPassExit{Kind: contract.TaskPassExitKindResult, Text: text}
	}
	agent := contract.TaskPassSourceAgent
	resultWord := msg.Text(msg.ExitKindResult)
	review := pass("review", "review", "merge", result("Замечаний нет"), agent)
	review.Reason = ptr("Существенных замечаний нет")
	path := []contract.TaskPass{
		pass("branch", "branch", "plan", nil, agent),
		pass("plan", "plan-feature", "implementation", &contract.TaskPassExit{Kind: contract.TaskPassExitKindArtifact, Artifact: ptr("plan"), Text: "План согласован"}, agent),
		pass("implementation", "implementation", "review", result("Изменения сделаны, тесты проходят"), agent,
			contract.TaskStep{Number: 1, Title: "Добавить расчёт суммы", State: contract.TaskStepStateDone, Check: ptr("go test ./backend/payments/...")},
			contract.TaskStep{Number: 2, Title: "Покрыть расчёт тестами", State: contract.TaskStepStateDropped, Reason: ptr("Тесты уже есть в шаге 1")}),
		review,
		pass("merge", "merge", flow.Finish, result("Ветка влита в main"), contract.TaskPassSourceOperator),
	}
	path[4].Source = ptr(agent)
	passed := shopTask()
	passed.Stage, passed.Finished, passed.Progress.Passed = nil, true, 5
	artifacts := []contract.TaskArtifact{{Name: "plan", Kind: contract.TaskArtifactKindLink, Url: ptr("https://claude.ai/code/artifact/plan"),
		Source: contract.TaskArtifactSourceAgent, Saved: shopTime}}
	notes := []contract.TaskNote{{Number: 1, Node: "implementation", Stage: "implementation", Round: 1, Text: "Проверить СБП", Added: shopTime}}
	passedFields := lines(
		msg.Text(msg.TaskHeading, "SHOP-1", "Частичный возврат по карте"),
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskState, msg.Text(msg.TaskStateActive)),
		msg.Text(msg.TaskScenario, "Фича (feature)"),
		msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
		msg.Text(msg.ProgressLine, "5 из 5"),
		msg.Text(msg.TaskTakenAt, "2026-10-09 12:30"),
		msg.Text(msg.TaskFlowApplied, "2026-10-09 12:30"),
		msg.Text(msg.TaskWorktree, shopFix),
	)
	passedTable := passedFields + "\n" + table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)},
		[]string{"Ветка", "1", "пропуск", "plan"},
		[]string{"План фичи", "1", msg.Text(msg.ExitKindArtifact, "plan"), "implementation"},
		[]string{"Реализация", "1", resultWord, "review"},
		[]string{"Ревью", "1", resultWord, "merge"},
		[]string{"Слияние", "1", resultWord, "конец"},
	) + "\n" + table(
		[]string{msg.Text(msg.ColArtifact), msg.Text(msg.ColKind), msg.Text(msg.ColSaved), msg.Text(msg.ColPlace)},
		[]string{"plan", "ссылка", "2026-10-09 12:30", "https://claude.ai/code/artifact/plan"},
	) + "\n"
	steps := table(
		[]string{msg.Text(msg.ColStepNumber), msg.Text(msg.ColState), msg.Text(msg.ColStep), msg.Text(msg.ColComment)},
		[]string{"1", "выполнен", "Добавить расчёт суммы", "go test ./backend/payments/..."},
		[]string{"2", "снят", "Покрыть расчёт тестами", "Тесты уже есть в шаге 1"},
	)
	passedPasses := passedFields + lines(
		"",
		"Ветка (branch), круг 1",
		"Итог: пропуск",
		msg.Text(msg.TransitionLine, "plan"),
		"Записано: агентом",
		"Закрыт: 2026-10-09 12:30",
		"",
		"План фичи (plan-feature), круг 1",
		msg.Text(msg.OutcomeLine, msg.Text(msg.ExitKindArtifact, "plan")),
		msg.Text(msg.ExitTextLine, "План согласован"),
		msg.Text(msg.TransitionLine, "implementation"),
		"Записано: агентом",
		"Закрыт: 2026-10-09 12:30",
		"",
		"Реализация (implementation), круг 1",
		msg.Text(msg.OutcomeLine, resultWord),
		msg.Text(msg.ExitTextLine, "Изменения сделаны, тесты проходят"),
		msg.Text(msg.TransitionLine, "review"),
		"Записано: агентом",
		"Закрыт: 2026-10-09 12:30",
		"",
	) + steps + lines(
		"",
		"Ревью (review), круг 1",
		msg.Text(msg.OutcomeLine, resultWord),
		msg.Text(msg.ExitTextLine, "Замечаний нет"),
		msg.Text(msg.TransitionLine, "merge"),
		msg.Text(msg.ReasonLine, "Существенных замечаний нет"),
		"Записано: агентом",
		"Закрыт: 2026-10-09 12:30",
		"",
		"Слияние (merge), круг 1",
		msg.Text(msg.OutcomeLine, resultWord),
		msg.Text(msg.ExitTextLine, "Ветка влита в main"),
		msg.Text(msg.TransitionLine, "конец"),
		msg.Text(msg.RecordedLine, msg.Text(msg.TaskSourceAgent)),
		"Закрыт: 2026-10-09 12:30",
		"",
	)
	tests := []struct {
		name       string
		out        contract.TaskShowOutput
		x          taskShowExtra
		cli, agent string
	}{
		{"scenario passed", contract.TaskShowOutput{Task: passed, Path: path, Artifacts: artifacts, Notes: notes},
			taskShowExtra{names: shopNames(), here: true},
			passedTable + lines(
				hintLineOf(msg.HintStatement, "gentry task show --statement"),
				hintLineOf(msg.HintNotes, "gentry note list"),
			), passedTable + lines(
				hintLineOf(msg.HintStatement, "task_show (statement)"),
				hintLineOf(msg.HintNotes, "note_list"),
				hintLineOf(msg.HintTaskClose, "task_close"),
			)},
		{"path in full", contract.TaskShowOutput{Task: passed, Path: path},
			taskShowExtra{names: shopNames(), here: true, full: true},
			passedPasses + lines(hintLineOf(msg.HintStatement, "gentry task show --statement")),
			passedPasses + lines(
				hintLineOf(msg.HintStatement, "task_show (statement)"),
				hintLineOf(msg.HintTaskClose, "task_close"),
			)},
		{"current stage", contract.TaskShowOutput{Task: shopTask(), Path: []contract.TaskPass{currentPass()}},
			taskShowExtra{names: shopNames(), here: true},
			current + lines(hintLineOf(msg.HintStatement, "gentry task show --statement")),
			current + lines(hintLineOf(msg.HintStatement, "task_show (statement)"))},
		{"outside the worktree", contract.TaskShowOutput{Task: shopTask(), Path: []contract.TaskPass{currentPass()}},
			taskShowExtra{names: shopNames()},
			current + lines(hintLineOf(msg.HintStatement, "gentry task show SHOP-1 --statement")),
			current + lines(hintLineOf(msg.HintStatement, "task_show (task: SHOP-1, statement)"))},
		{"statement", contract.TaskShowOutput{Task: byLines}, taskShowExtra{names: shopNames(), statement: true}, lines(
			"Постановка задачи SHOP-1:",
			"  Первая строка.",
			"",
			"  Третья строка.",
			"",
			msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
		), ""},
		{"statement with decisions", contract.TaskShowOutput{Task: shopTask(), OperatorDecisions: decisions},
			taskShowExtra{names: shopNames(), statement: true}, lines(
				msg.Text(msg.StatementHeading, "SHOP-1"),
				"  Клиент возвращает часть заказа.",
				"",
				msg.Text(msg.TaskSource, msg.Text(msg.TaskSourceOperator)),
				"",
				"Решения оператора:",
				"  1. Ветка (branch), круг 1, записано оператором:",
				"     Вопрос: Делать частичный возврат и для СБП?",
				"     Варианты:",
				"       1. Только карта (рекомендован): СБП требует другого API банка.",
				"       2. Карта и СБП:",
				"            Задача вырастет примерно вдвое.",
				"            Сроки сдвинутся.",
				"       3. Отложить",
				"     Ответ: Давай первый.",
				"",
				"  2. План фичи (plan-feature), круг 1, записано "+msg.Text(msg.TaskSourceAgent)+":",
				"     Ответ:",
				"       Сумму возврата писать в лог.",
				"       Уровень — info.",
				"",
				"  3. Ревью (review), круг 2, записано оператором:",
				"     Ответ: Да.",
				"     "+msg.Text(msg.AllowedReturnLine, "Реализация (implementation)"),
			), ""},
		{"hint to the decisions", contract.TaskShowOutput{Task: shopTask(), Path: []contract.TaskPass{currentPass()}, OperatorDecisions: decisions},
			taskShowExtra{names: shopNames(), here: true},
			current + lines(hintLineOf(msg.HintStatementDecisions, "gentry task show --statement")),
			current + lines(hintLineOf(msg.HintStatementDecisions, "task_show (statement)"))},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { taskShowText(p, tt.out, tt.x) }, tt.cli, tt.agent)
	}
}

// decisions are decisions of the operator: with a question and options, of
// several lines recorded by the agent, and one that allows a return.
var decisions = []contract.OperatorDecision{
	{Number: 1, Node: "branch", Stage: "branch", Round: 1, Source: contract.OperatorDecisionSourceOperator, Recorded: shopTime,
		Question: ptr("Делать частичный возврат и для СБП?"), Answer: "Давай первый.", Options: []contract.QuestionOption{
			{Label: "Только карта", Description: ptr("СБП требует другого API банка."), Recommended: ptr(true)},
			{Label: "Карта и СБП", Description: ptr("Задача вырастет примерно вдвое.\nСроки сдвинутся.")},
			{Label: "Отложить"},
		}},
	{Number: 2, Node: "plan", Stage: "plan-feature", Round: 1, Source: contract.OperatorDecisionSourceAgent, Recorded: shopTime,
		Answer: "Сумму возврата писать в лог.\nУровень — info."},
	{Number: 3, Node: "review", Stage: "review", Round: 2, Source: contract.OperatorDecisionSourceOperator, Recorded: shopTime,
		Answer: "Да.", AllowReturn: ptr("implementation")},
}

func TestTaskListText(t *testing.T) {
	main, fix := shopMain, shopFix
	tasks := []contract.TaskListItem{
		{Id: "SHOP-1", Project: "shop", Title: "Частичный возврат по карте", State: contract.TaskListOutputTasksElemStateActive,
			Scenario: contract.TaskScenario{Id: "feature", Title: "Фича"}, Stage: &contract.TaskStage{Node: "branch", Id: "branch", Title: "Ветка", Round: 1},
			Worktree: &fix},
		{Id: "SHOP-2", Project: "shop", Title: "Двойное списание", State: contract.TaskListOutputTasksElemStateClosed,
			Scenario: contract.TaskScenario{Id: "bug", Title: "Баг"}, Finished: true, Worktree: &main},
	}
	header := []string{msg.Text(msg.ColNumber), msg.Text(msg.ColTitle), msg.Text(msg.ColState), msg.Text(msg.ColScenario),
		msg.Text(msg.ColStage), msg.Text(msg.ColWorktree)}
	active := msg.Text(msg.TaskStateActive)
	tests := []struct {
		name       string
		out        contract.TaskListOutput
		x          taskListExtra
		cli, agent string
	}{
		{"no open tasks", contract.TaskListOutput{}, taskListExtra{}, "Незавершённых задач нет.\n", ""},
		{"no tasks of the filter", contract.TaskListOutput{}, taskListExtra{filtered: true}, "Задач нет.\n", ""},
		{"all projects", contract.TaskListOutput{Tasks: tasks}, taskListExtra{filtered: true}, table(
			append([]string{msg.Text(msg.ColProject)}, header...),
			[]string{"shop", "SHOP-1", "Частичный возврат по карте", active, "Фича", "Ветка", shopFix},
			[]string{"shop", "SHOP-2", "Двойное списание", "закрыта", "Баг", "сценарий пройден", shopMain},
		), ""},
		{"one project", contract.TaskListOutput{Tasks: tasks[:1]}, taskListExtra{oneProject: true}, table(
			header,
			[]string{"SHOP-1", "Частичный возврат по карте", active, "Фича", "Ветка", shopFix},
		), ""},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { taskListText(p, tt.out, tt.x) }, tt.cli, tt.agent)
	}
}

func TestTaskFailureText(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		cli, agent string
	}{
		{"not pooled", &task.NotPooledError{Path: "/work"}, lines(
			"Папка не внесена в пул рабочих копий: /work",
			"",
			hintLineOf(msg.HintWorktreeAdd, "gentry worktree add"),
		), lines(
			"Папка не внесена в пул рабочих копий: /work",
			"",
			hintLineOf(msg.HintWorktreeAdd, "worktree_add"),
		)},
		{"busy", &task.BusyError{Path: shopFix, Task: "SHOP-1"}, lines(
			"В рабочей копии уже идёт задача SHOP-1: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show SHOP-1"),
		), lines(
			"В рабочей копии уже идёт задача SHOP-1: /work/shop-fix",
			"",
			hintLineOf(msg.HintTaskShow, "task_show (task: SHOP-1)"),
		)},
		{"dirty", &task.DirtyError{Path: shopFix, Files: []string{"backend/payments/refund.go"}}, lines(
			"В рабочей копии есть незакоммиченные изменения: /work/shop-fix",
			"",
			"Изменённые файлы:",
			"  backend/payments/refund.go",
			"",
			msg.Text(msg.HintWorktreeDirty),
		), ""},
		{"not found", &task.NotFoundError{Task: "SHOP-9"}, lines(
			"Задача SHOP-9 не найдена.",
			"",
			hintLineOf(msg.HintTaskListAll, "gentry task list --all"),
		), lines(
			"Задача SHOP-9 не найдена.",
			"",
			hintLineOf(msg.HintTaskListAll, "task_list (all)"),
		)},
		{"undetermined", &task.UndeterminedError{Dir: "/work"}, lines(
			"В текущей папке нет задачи: /work",
			"",
			hintLineOf(msg.HintTaskList, "gentry task list"),
		), lines(
			"В текущей папке нет задачи: /work",
			"",
			hintLineOf(msg.HintTaskList, "task_list"),
		)},
		{"invalid key", &task.InvalidKeyError{Value: "shop"}, lines(
			"Недопустимый номер задачи: «shop».",
			"",
			msg.Text(msg.HintTaskKey),
		), ""},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, taskFailure(tt.err), tt.cli, tt.agent)
	}
	if f := taskFailure(errors.New("boom")); f.code != contract.CodeInternal {
		t.Errorf("an unknown error: code %q, want %q", f.code, contract.CodeInternal)
	}
}

func TestCheckTakeFieldsText(t *testing.T) {
	long := ""
	for range 81 {
		long += "я"
	}
	tests := []struct {
		name       string
		fields     map[string]string
		cli, agent string
	}{
		{"no scenario", map[string]string{"title": "Т", "statement": "С"}, lines(
			"Не указан сценарий задачи.",
			"",
			hintLineOf(msg.HintFlowScenarios, "gentry flow show"),
		), lines(
			"Не указан сценарий задачи.",
			"",
			hintLineOf(msg.HintFlowScenarios, "flow_show"),
		)},
		{"no title", map[string]string{"scenario": "feature", "statement": "С"}, lines(
			"Не указано название задачи.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task take --help"),
		), "Не указано название задачи.\n"},
		{"no statement", map[string]string{"scenario": "feature", "title": "Т"}, lines(
			"Не указана постановка задачи.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task take --help"),
		), "Не указана постановка задачи.\n"},
		{"long title", map[string]string{"scenario": "feature", "title": long, "statement": "С"}, lines(
			"Название задачи длиннее 80 знаков.",
			"",
			msg.Text(msg.HintTitle),
		), ""},
		{"title of two lines", map[string]string{"scenario": "feature", "title": "А\nБ", "statement": "С"}, lines(
			"Название задачи состоит из нескольких строк.",
			"",
			msg.Text(msg.HintTitle),
		), ""},
	}
	for _, tt := range tests {
		f := checkTakeFields(tt.fields)
		if f == nil {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, *f, tt.cli, tt.agent)
	}
}
