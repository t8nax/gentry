package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// at is shopTask at the node of the stage of the feature scenario, with its
// progress.
func at(node, stage, title string, round, passed int) contract.Task {
	t := shopTask()
	t.Stage = &contract.TaskStage{Node: node, Id: stage, Title: title, Round: round}
	t.Progress.Passed = passed
	return t
}

func TestStageShowText(t *testing.T) {
	branch := contract.TaskStageShowOutput{
		Task: "SHOP-1", Node: "branch", Round: 1,
		Stage: contract.TaskStageShowStage{Id: "branch", Title: "Ветка", Executor: "orchestrator", Exit: "создана ветка задачи",
			Instruction: "Создать ветку задачи от main.", Parts: []contract.TaskStageShowPart{}},
		Transitions: []contract.TaskStageShowTransition{{To: "plan", Stage: ptr("plan-feature"), Title: ptr("План фичи")}},
	}
	review := contract.TaskStageShowOutput{
		Task: "SHOP-1", Node: "review", Round: 4,
		Stage: contract.TaskStageShowStage{Id: "review", Title: "Ревью", Executor: "reviewer", Exit: "замечания ревью записаны и разобраны",
			Instruction: "Провести ревью.", Parts: []contract.TaskStageShowPart{}},
		Transitions: []contract.TaskStageShowTransition{
			{To: "implementation", Stage: ptr("implementation"), Title: ptr("Реализация"), If: ptr("ревью выявило существенные замечания"),
				MaxReturns: ptr(3), Returns: ptr(3), AllowedReturns: ptr(0)},
			{To: "merge", Stage: ptr("merge"), Title: ptr("Слияние")},
		},
	}
	header := []string{msg.Text(msg.ColTransition), msg.Text(msg.ColStage), msg.Text(msg.ColCondition), msg.Text(msg.ColReturns)}
	checked := branch
	checked.Stage.Checks, checked.Stage.Timeout = []string{"go vet ./...", `go test -run "Total|Sum" ./...`}, ptr("10m")
	plan := branch
	plan.Node, plan.Stage = "plan", contract.TaskStageShowStage{Id: "plan-feature", Title: "План фичи", Executor: "orchestrator",
		Exit: "план согласован с оператором", Agents: []string{"scout", "reviewer"}, Instruction: "Написать план.", Parts: []contract.TaskStageShowPart{}}
	plan.Transitions = []contract.TaskStageShowTransition{{To: "implementation", Stage: ptr("implementation"), Title: ptr("Реализация")}}
	tests := []struct {
		name       string
		out        contract.TaskStageShowOutput
		cli, agent string
	}{
		{"first stage", branch, lines(
			msg.Text(msg.FlowStage, round("Ветка", "branch", 1)),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "orchestrator"),
			msg.Text(msg.FlowAgents, noValue),
			msg.Text(msg.FlowExit, "создана ветка задачи"),
			msg.Text(msg.FlowParts, noValue),
			"",
		) + table(header, []string{"plan", "План фичи", noValue, noValue}) + lines(
			"",
			msg.Text(msg.FlowInstruction),
			"  Создать ветку задачи от main.",
		), ""},
		// The subagents the stage runs follow the executor, in the order of
		// the flow.
		{"agents", plan, lines(
			msg.Text(msg.FlowStage, round("План фичи", "plan-feature", 1)),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "orchestrator"),
			"Субагенты: scout, reviewer",
			msg.Text(msg.FlowExit, "план согласован с оператором"),
			msg.Text(msg.FlowParts, noValue),
			"",
		) + table(header, []string{"implementation", "Реализация", noValue, noValue}) + lines(
			"",
			msg.Text(msg.FlowInstruction),
			"  Написать план.",
		), ""},
		// The commands of checks follow the parts, one a line, as written.
		{"checks", checked, lines(
			msg.Text(msg.FlowStage, round("Ветка", "branch", 1)),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "orchestrator"),
			msg.Text(msg.FlowAgents, noValue),
			msg.Text(msg.FlowExit, "создана ветка задачи"),
			msg.Text(msg.FlowParts, noValue),
			"Время проверок: 10m",
			"Проверки:",
			"  go vet ./...",
			`  go test -run "Total|Sum" ./...`,
			"",
		) + table(header, []string{"plan", "План фичи", noValue, noValue}) + lines(
			"",
			msg.Text(msg.FlowInstruction),
			"  Создать ветку задачи от main.",
		), ""},
		{"returns used up", review, lines(
			msg.Text(msg.FlowStage, round("Ревью", "review", 4)),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "reviewer"),
			msg.Text(msg.FlowAgents, noValue),
			msg.Text(msg.FlowExit, "замечания ревью записаны и разобраны"),
			msg.Text(msg.FlowParts, noValue),
			"",
		) + table(header,
			[]string{"implementation", "Реализация", "ревью выявило существенные замечания", "3 из 3"},
			[]string{"merge", "Слияние", noValue, noValue},
		) + lines(
			"",
			msg.Text(msg.FlowInstruction),
			"  Провести ревью.",
		), ""},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { stageShowText(p, tt.out, nil) }, tt.cli, tt.agent)
	}
}

func TestStageCloseText(t *testing.T) {
	closed := func(node, stage string, round int, to string, exit *contract.TaskPassExit, reason string) contract.TaskPass {
		outcome, src := contract.TaskPassOutcome(state.OutcomeExit), contract.TaskPassSourceAgent
		p := contract.TaskPass{Node: node, Stage: stage, Round: round, Entered: shopTime, Steps: []contract.TaskStep{},
			Outcome: &outcome, Source: &src, To: &to, Closed: &shopTime, Exit: exit}
		if exit == nil {
			skip := contract.TaskPassOutcome(state.OutcomeSkip)
			p.Outcome = &skip
		}
		if reason != "" {
			p.Reason = &reason
		}
		return p
	}
	result := func(text string) *contract.TaskPassExit {
		return &contract.TaskPassExit{Kind: contract.TaskPassExitKindResult, Text: text}
	}
	withLogs := func(p contract.TaskPass, logs ...string) contract.TaskPass {
		p.ChecksLogs = logs
		return p
	}
	finished := shopTask()
	finished.Stage, finished.Finished, finished.Progress.Passed = nil, true, 5
	show := []hint{hintOf(msg.HintStageShow)}
	tests := []struct {
		name       string
		out        contract.TaskStageCloseOutput
		x          stageCloseExtra
		cli, agent string
	}{
		{"exited", contract.TaskStageCloseOutput{Task: at("plan", "plan-feature", "План фичи", 1, 1),
			Closed: closed("branch", "branch", 1, "plan", result("Создана ветка feature/shop-1"), "")},
			stageCloseExtra{title: "Ветка", hints: show}, lines(
				"Этап «Ветка» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Создана ветка feature/shop-1",
				"Переход: plan",
				msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
				msg.Text(msg.ProgressLine, progress(1, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show"),
			), lines(
				"Этап «Ветка» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Создана ветка feature/shop-1",
				"Переход: plan",
				msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
				msg.Text(msg.ProgressLine, progress(1, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show"),
			)},
		{"exited by an artifact", contract.TaskStageCloseOutput{Task: at("implementation", "implementation", "Реализация", 1, 2),
			Closed: closed("plan", "plan-feature", 1, "implementation",
				&contract.TaskPassExit{Kind: contract.TaskPassExitKindArtifact, Artifact: ptr("plan"), Text: "План согласован с оператором"}, "")},
			stageCloseExtra{title: "План фичи", hints: show}, lines(
				"Этап «План фичи» закрыт.",
				"Вид выхода: артефакт plan",
				"Выход: План согласован с оператором",
				"Переход: implementation",
				msg.Text(msg.TaskStage, round("Реализация", "implementation", 1)),
				msg.Text(msg.ProgressLine, progress(2, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show"),
			), lines(
				"Этап «План фичи» закрыт.",
				"Вид выхода: артефакт plan",
				"Выход: План согласован с оператором",
				"Переход: implementation",
				msg.Text(msg.TaskStage, round("Реализация", "implementation", 1)),
				msg.Text(msg.ProgressLine, progress(2, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show"),
			)},
		{"exited after checks", contract.TaskStageCloseOutput{Task: at("review", "review", "Ревью", 1, 3),
			Closed: withLogs(closed("implementation", "implementation", 1, "review", result("Тесты проходят"), ""),
				"/work/checks/implementation-1-1.log", "/work/checks/implementation-1-2.log")},
			stageCloseExtra{title: "Реализация", hints: show}, lines(
				"Этап «Реализация» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Тесты проходят",
				"Пройдено проверок: 2",
				"Переход: review",
				msg.Text(msg.TaskStage, round("Ревью", "review", 1)),
				msg.Text(msg.ProgressLine, progress(3, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show"),
			), lines(
				"Этап «Реализация» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Тесты проходят",
				"Пройдено проверок: 2",
				"Переход: review",
				msg.Text(msg.TaskStage, round("Ревью", "review", 1)),
				msg.Text(msg.ProgressLine, progress(3, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show"),
			)},
		// A skip runs no command: the files of a refused exit are not checks
		// passed.
		{"skipped after checks", contract.TaskStageCloseOutput{Task: at("review", "review", "Ревью", 1, 3),
			Closed: withLogs(closed("implementation", "implementation", 1, "review", nil, "Изменений нет"), "/work/checks/implementation-1-1.log")},
			stageCloseExtra{title: "Реализация", hints: show}, lines(
				msg.Text(msg.StageSkipped, "Реализация"),
				msg.Text(msg.ReasonLine, "Изменений нет"),
				msg.Text(msg.TransitionLine, "review"),
				msg.Text(msg.TaskStage, round("Ревью", "review", 1)),
				msg.Text(msg.ProgressLine, progress(3, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show"),
			), lines(
				msg.Text(msg.StageSkipped, "Реализация"),
				msg.Text(msg.ReasonLine, "Изменений нет"),
				msg.Text(msg.TransitionLine, "review"),
				msg.Text(msg.TaskStage, round("Ревью", "review", 1)),
				msg.Text(msg.ProgressLine, progress(3, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show"),
			)},
		{"return", contract.TaskStageCloseOutput{Task: at("implementation", "implementation", "Реализация", 2, 4),
			Closed: closed("review", "review", 1, "implementation", result("Замечания записаны"), "Две ошибки в расчёте суммы")},
			stageCloseExtra{title: "Ревью", hints: taskHints("SHOP-1", false, show...)}, lines(
				"Этап «Ревью» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Замечания записаны",
				"Переход: implementation",
				"Обоснование: Две ошибки в расчёте суммы",
				msg.Text(msg.TaskStage, round("Реализация", "implementation", 2)),
				msg.Text(msg.ProgressLine, progress(4, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show --task SHOP-1"),
			), lines(
				"Этап «Ревью» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Замечания записаны",
				"Переход: implementation",
				"Обоснование: Две ошибки в расчёте суммы",
				msg.Text(msg.TaskStage, round("Реализация", "implementation", 2)),
				msg.Text(msg.ProgressLine, progress(4, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show (task: SHOP-1)"),
			)},
		{"end of the scenario", contract.TaskStageCloseOutput{Task: finished,
			Closed: closed("merge", "merge", 1, flow.Finish, result("Ветка влита в main"), "")},
			stageCloseExtra{title: "Слияние", hints: []hint{hintOf(msg.HintTaskClose)}}, lines(
				"Этап «Слияние» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Ветка влита в main",
				"Переход: "+msg.Text(msg.FlowEnd),
				msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
				msg.Text(msg.ProgressLine, progress(5, 5)),
			), lines(
				"Этап «Слияние» закрыт.",
				"Вид выхода: "+msg.Text(msg.ExitKindResult),
				"Выход: Ветка влита в main",
				"Переход: "+msg.Text(msg.FlowEnd),
				msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
				msg.Text(msg.ProgressLine, progress(5, 5)),
				"",
				hintLineOf(msg.HintTaskClose, "task_close"),
			)},
		{"skipped", contract.TaskStageCloseOutput{Task: at("plan", "plan-feature", "План фичи", 1, 1),
			Closed: closed("branch", "branch", 1, "plan", nil, "Ветка уже создана оператором")},
			stageCloseExtra{title: "Ветка", hints: show}, lines(
				"Этап «Ветка» пропущен.",
				"Обоснование: Ветка уже создана оператором",
				"Переход: plan",
				msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
				msg.Text(msg.ProgressLine, progress(1, 5)),
				"",
				hintLineOf(msg.HintStageShow, "gentry task stage show"),
			), lines(
				"Этап «Ветка» пропущен.",
				"Обоснование: Ветка уже создана оператором",
				"Переход: plan",
				msg.Text(msg.TaskStage, round("План фичи", "plan-feature", 1)),
				msg.Text(msg.ProgressLine, progress(1, 5)),
				"",
				hintLineOf(msg.HintStageShow, "task_stage_show"),
			)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { stageCloseText(p, tt.out, tt.x) }, tt.cli, tt.agent)
	}
}

func TestCheckCloseText(t *testing.T) {
	tests := []struct {
		name       string
		cmd        string
		req        task.Close
		fromInput  bool
		cli, agent string
	}{
		{"no reason of a skip", "task stage skip", task.Close{Skip: true}, false, lines(
			"Не указано обоснование пропуска.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage skip --help"),
		), "Не указано обоснование пропуска.\n"},
		{"no kind", "task stage exit", task.Close{Text: "Т"}, false, lines(
			"Не указан вид выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), "Не указан вид выхода.\n"},
		{"bad kind", "task stage exit", task.Close{Kind: "fact", Text: "Т"}, false, lines(
			msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact"),
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact") + "\n"},
		{"bad kind in input", "task stage exit", task.Close{Kind: "fact", Text: "Т"}, true, lines(
			msg.Text(msg.ErrInputInvalid, "недопустимое значение поля «kind»"),
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), msg.Text(msg.ErrToolFields, "недопустимое значение поля «kind»") + "\n"},
		{"no text", "task stage exit", task.Close{Kind: "result"}, false, lines(
			"Не указан текст выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), "Не указан текст выхода.\n"},
		{"no artifact", "task stage exit", task.Close{Kind: "artifact", Text: "Т"}, false, lines(
			"Не указан артефакт выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), "Не указан артефакт выхода.\n"},
		{"artifact of a result", "task stage exit", task.Close{Kind: "result", Artifact: "plan", Text: "Т"}, false, lines(
			"Артефакт указывается только у выхода вида artifact.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), "Артефакт указывается только у выхода вида artifact.\n"},
	}
	for _, tt := range tests {
		f := checkClose(tt.cmd, tt.req, tt.fromInput)
		if f == nil {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, *f, tt.cli, tt.agent)
	}
}

func TestStepsText(t *testing.T) {
	step := func(n int, title string, s contract.TaskStepState) contract.TaskStep {
		return contract.TaskStep{Number: n, Title: title, State: s, Source: contract.TaskStepSourceAgent, Added: shopTime}
	}
	exit := []hint{hintOf(msg.HintStageExit)}
	tests := []struct {
		name       string
		out        contract.TaskStepListOutput
		change     stepChange
		hints      []hint
		cli, agent string
	}{
		{"one added", contract.TaskStepListOutput{Added: []int{1}, Steps: []contract.TaskStep{step(1, "Создать ветку", contract.TaskStepStatePlanned)}},
			stepChange{}, exit, lines(
				"К этапу добавлено шагов: 1.",
				"Номер шага: 1",
			), ""},
		{"two added", contract.TaskStepListOutput{Added: []int{1, 2}, Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStatePlanned), step(2, "Покрыть расчёт тестами", contract.TaskStepStatePlanned)}},
			stepChange{}, exit, lines(
				"К этапу добавлено шагов: 2.",
				"Номера шагов: 1–2",
			), ""},
		{"done, one left", contract.TaskStepListOutput{Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStateDone), step(2, "Покрыть расчёт тестами", contract.TaskStepStatePlanned)}},
			stepChange{step: 1, done: true}, exit, lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 1",
			), ""},
		{"done, none left", contract.TaskStepListOutput{Steps: []contract.TaskStep{step(1, "Создать ветку", contract.TaskStepStateDone)}},
			stepChange{step: 1, done: true}, exit, lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 0",
			), lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 0",
				"",
				hintLineOf(msg.HintStageExit, "task_stage_exit (kind, text)"),
			)},
		{"dropped, outside the worktree", contract.TaskStepListOutput{Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStateDone), step(2, "Покрыть расчёт тестами", contract.TaskStepStateDropped)}},
			stepChange{step: 2}, taskHints("SHOP-1", false, exit...), lines(
				"Шаг 2 снят.",
				"Осталось шагов: 0",
			), lines(
				"Шаг 2 снят.",
				"Осталось шагов: 0",
				"",
				hintLineOf(msg.HintStageExit, "task_stage_exit (kind, text, task: SHOP-1)"),
			)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { stepsText(p, tt.out, tt.change, tt.hints) }, tt.cli, tt.agent)
	}
}

func TestNoteText(t *testing.T) {
	note := func(n int, node, stage, text string) contract.TaskNote {
		return contract.TaskNote{Number: n, Node: node, Stage: stage, Round: 1, Text: text, Source: contract.TaskNoteSourceAgent, Added: shopTime}
	}
	wantText(t, "task note add", func(p *page) {
		noteAddText(p, contract.TaskNoteAddOutput{Task: "SHOP-1", Note: note(1, "implementation", "implementation", "Проверить СБП")})
	}, "Заметка 1 добавлена.\n", "")
	wantText(t, "task note list, no notes", func(p *page) {
		noteListText(p, contract.TaskNoteListOutput{Task: "SHOP-1", Notes: []contract.TaskNote{}}, names{})
	}, "У задачи нет заметок.\n", "")
	wantText(t, "task note list", func(p *page) {
		noteListText(p, contract.TaskNoteListOutput{Task: "SHOP-1", Notes: []contract.TaskNote{
			note(1, "implementation", "implementation", "На ревью проверить, что возврат по СБП не задет"),
			note(2, "merge", "merge", "Первая строка.\nВторая строка."),
		}}, shopNames())
	}, lines(
		msg.Text(msg.NoteHeading, 1, round("Реализация", "implementation", 1)),
		"   На ревью проверить, что возврат по СБП не задет",
		"",
		msg.Text(msg.NoteHeading, 2, round("Слияние", "merge", 1)),
		"   Первая строка.",
		"   Вторая строка.",
	), "")
}

func TestArtifactSaveText(t *testing.T) {
	link := contract.TaskArtifact{Name: "plan", Kind: contract.TaskArtifactKindLink, Url: ptr("https://claude.ai/code/artifact/plan"),
		Source: contract.TaskArtifactSourceAgent, Saved: shopTime}
	file := contract.TaskArtifact{Name: "plan.md", Kind: contract.TaskArtifactKindFile, Path: ptr("/home/dev/.gentry/state/shop/tasks/SHOP-1/artifacts/plan.md"),
		Source: contract.TaskArtifactSourceAgent, Saved: shopTime}
	tests := []struct {
		name string
		out  contract.TaskArtifactSaveOutput
		cli  string
	}{
		{"link", contract.TaskArtifactSaveOutput{Task: "SHOP-1", Artifact: link}, lines(
			"Артефакт plan сохранён.",
			"Адрес: https://claude.ai/code/artifact/plan",
		)},
		{"file", contract.TaskArtifactSaveOutput{Task: "SHOP-1", Artifact: file}, lines(
			"Артефакт plan.md сохранён.",
			"Файл: /home/dev/.gentry/state/shop/tasks/SHOP-1/artifacts/plan.md",
		)},
		{"replaced", contract.TaskArtifactSaveOutput{Task: "SHOP-1", Artifact: link, Replaced: true}, lines(
			"Артефакт plan заменён.",
			"Адрес: https://claude.ai/code/artifact/plan",
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { artifactSaveText(p, tt.out) }, tt.cli, "")
	}
}

func TestWayFailureText(t *testing.T) {
	review := flow.Stage{ID: "review", Title: "Ревью"}
	steps := []state.Step{{Number: 2, Title: "Покрыть расчёт тестами", State: state.StepPlanned}}
	tests := []struct {
		name       string
		cmd        string
		err        error
		here       bool
		cli, agent string
	}{
		{"scenario passed", "task stage show", &task.FinishedError{Task: "SHOP-1"}, true, lines(
			"Сценарий задачи SHOP-1 пройден.",
		), lines(
			"Сценарий задачи SHOP-1 пройден.",
			"",
			hintLineOf(msg.HintTaskClose, "task_close"),
		)},
		{"no transition at a fork", "task stage exit", &task.FieldError{Field: "to", Fork: true}, true, lines(
			"На развилке не указан переход.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry task stage show"),
		), lines(
			"На развилке не указан переход.",
			"",
			hintLineOf(msg.HintStageTransitions, "task_stage_show"),
		)},
		{"no reason at a fork", "task stage exit", &task.FieldError{Field: "reason", Fork: true}, true, lines(
			"На развилке не указано обоснование перехода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task stage exit --help"),
		), "На развилке не указано обоснование перехода.\n"},
		{"no transition", "task stage exit", &task.TransitionError{Node: "review", To: "deploy", Stage: review, Transitions: []string{"implementation", "merge"}}, true, lines(
			"У этапа «Ревью» нет перехода к узлу deploy.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry task stage show"),
		), lines(
			"У этапа «Ревью» нет перехода к узлу deploy.",
			"",
			hintLineOf(msg.HintStageTransitions, "task_stage_show"),
		)},
		{"returns used up", "task stage exit", &task.ReturnLimitError{Node: "review", To: "implementation", Limit: 3}, true, lines(
			"Возвраты к узлу implementation исчерпаны.",
			"Возвратов: 3 из 3",
			"",
			hintLineOf(msg.HintOtherTransitions, "gentry task stage show"),
			hintLineOf(msg.HintAllowReturn, "gentry task decision add --answer "+msg.Text(msg.ArgAnswer)+" --allow-return implementation"),
		), lines(
			"Возвраты к узлу implementation исчерпаны.",
			"Возвратов: 3 из 3",
			"",
			hintLineOf(msg.HintOtherTransitions, "task_stage_show"),
			hintLineOf(msg.HintAllowReturn, "task_decision_add (answer, allow_return: implementation)"),
		)},
		{"no return", "task decision add", &task.ReturnNotFoundError{Node: "branch", To: "plan", Stage: flow.Stage{ID: "branch", Title: "Ветка"}, Returns: []string{}}, true, lines(
			"У этапа «Ветка» нет возврата к узлу plan.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry task stage show"),
		), lines(
			"У этапа «Ветка» нет возврата к узлу plan.",
			"",
			hintLineOf(msg.HintStageTransitions, "task_stage_show"),
		)},
		{"no steps", "task stage exit", &task.StepsEmptyError{Node: "branch", Stage: flow.Stage{ID: "branch", Title: "Ветка"}}, true, lines(
			"У этапа «Ветка» нет шагов.",
		), lines(
			"У этапа «Ветка» нет шагов.",
			"",
			hintLineOf(msg.HintStepAdd, "task_step_add (steps)"),
		)},
		{"no steps, outside the worktree", "task stage exit", &task.StepsEmptyError{Node: "branch", Stage: flow.Stage{ID: "branch", Title: "Ветка"}}, false, lines(
			msg.Text(msg.ErrStepsEmpty, "Ветка"),
		), lines(
			msg.Text(msg.ErrStepsEmpty, "Ветка"),
			"",
			hintLineOf(msg.HintStepAdd, "task_step_add (steps, task: SHOP-1)"),
		)},
		{"steps not done", "task stage exit", &task.StepsOpenError{Node: "implementation", Steps: steps}, true, lines(
			"У этапа есть невыполненные шаги.",
			"",
			"Невыполненные шаги:",
			"  2. Покрыть расчёт тестами",
		), lines(
			"У этапа есть невыполненные шаги.",
			"",
			"Невыполненные шаги:",
			"  2. Покрыть расчёт тестами",
			"",
			hintLineOf(msg.HintStepDone, "task_step_done (step)"),
			hintLineOf(msg.HintStepDrop, "task_step_drop (step, reason)"),
		)},
		{"no step", "task step done", &task.StepNotFoundError{Step: 7}, true, lines(
			"У этапа нет шага 7.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"У этапа нет шага 7.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"task step done already", "task step done", &task.StepClosedError{Step: 1, State: state.StepDone}, true, lines(
			"Шаг 1 уже выполнен.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Шаг 1 уже выполнен.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"step dropped already", "task step drop", &task.StepClosedError{Step: 2, State: state.StepDropped}, true, lines(
			"Шаг 2 уже снят.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Шаг 2 уже снят.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"no artifact", "task stage exit", &task.ArtifactNotFoundError{Name: "plan"}, true, lines(
			"Артефакт plan не сохранён.",
		), lines(
			"Артефакт plan не сохранён.",
			"",
			hintLineOf(msg.HintArtifactSave, "task_artifact_save (name: plan, file)"),
		)},
		// The output of a command of checks follows the lines of Gentry as
		// it is; the refusals of checks have no hint.
		{"check failed", "task stage exit", &task.ChecksFailedError{Node: "implementation", Command: "go test ./...", Code: 1,
			Log: "/work/checks/implementation-1-1.log", Output: "--- FAIL: TestTotal (0.00s)\n    cart_test.go:7: Total = 5, want 6\nFAIL"}, true, lines(
			"Команда проверки завершилась с ошибкой: go test ./...",
			"Код выхода: 1",
			"Полный вывод: /work/checks/implementation-1-1.log",
			"",
			"--- FAIL: TestTotal (0.00s)",
			"    cart_test.go:7: Total = 5, want 6",
			"FAIL",
		), ""},
		{"check failed without output", "task stage exit", &task.ChecksFailedError{Node: "implementation", Command: "go vet ./...", Code: 2,
			Log: "/work/checks/implementation-1-1.log"}, true, lines(
			msg.Text(msg.ErrCheckFailed, "go vet ./..."),
			msg.Text(msg.CheckExitCodeLine, 2),
			msg.Text(msg.CheckLogLine, "/work/checks/implementation-1-1.log"),
		), ""},
		{"checks timed out", "task stage exit", &task.ChecksTimeoutError{Node: "implementation", Timeout: "20m", Command: "go test ./...",
			Log: "/work/checks/implementation-1-2.log", Output: "=== RUN   TestTotal"}, true, lines(
			"Проверки этапа не завершились за 20m.",
			"Команда: go test ./...",
			"Полный вывод: /work/checks/implementation-1-2.log",
			"",
			"=== RUN   TestTotal",
		), ""},
		{"check program not found", "task stage exit", &task.CheckProgramError{Node: "implementation", Program: "golangci-lint",
			Command: "golangci-lint run"}, true, lines(
			"Программа команды проверки не найдена: golangci-lint",
			msg.Text(msg.CheckCommandLine, "golangci-lint run"),
		), ""},
		// The reason is what the system says, as it says it.
		{"check program not started", "task stage exit", &task.CheckStartError{Node: "implementation", Program: "./gradlew",
			Command: "./gradlew test", Reason: "permission denied"}, true, lines(
			"Программа команды проверки не запустилась: ./gradlew",
			msg.Text(msg.CheckCommandLine, "./gradlew test"),
			"Причина: permission denied",
		), ""},
		{"file too large", "task artifact save", &task.FileError{Path: "big.bin", Reason: task.FileTooLarge}, false, lines(
			"Файл артефакта больше 10 МБ: big.bin",
		), lines(
			"Файл артефакта больше 10 МБ: big.bin",
			"",
			hintLineOf(msg.HintArtifactLink, "task_artifact_save (name, url, task: SHOP-1)"),
		)},
		{"no file", "task artifact save", &task.FileError{Path: "plan.md", Reason: task.FileNotFound}, true, lines(
			"Файл артефакта не найден: plan.md",
			"",
			msg.Text(msg.HintArtifactFile),
		), ""},
	}
	for _, tt := range tests {
		f := wayFailure(tt.cmd, tt.err)
		f.hints = taskHints("SHOP-1", tt.here, f.hints...)
		wantFail(t, tt.name, f, tt.cli, tt.agent)
	}
}

// TestWayRefusalText checks the refusals of the arguments and fields of the
// commands of the way of a task, given before the task is looked at.
func TestWayRefusalText(t *testing.T) {
	long := strings.Repeat("ш", 121)
	tests := []struct {
		name       string
		cmd        string
		args       []string
		cli, agent string
	}{
		{"no step", "task step add", nil, lines(
			"Не указан ни один шаг.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task step add --help"),
		), "Не указан ни один шаг.\n"},
		{"long step", "task step add", []string{long}, lines(
			"Шаг длиннее 120 знаков.",
			"",
			msg.Text(msg.HintStep),
		), ""},
		{"step of two lines", "task step add", []string{"А\nБ"}, lines(
			"Шаг состоит из нескольких строк.",
			"",
			msg.Text(msg.HintStep),
		), ""},
		{"step number", "task step done", []string{"первый"}, lines(
			"Недопустимый номер шага: «первый».",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Недопустимый номер шага: «первый».",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"no step number", "task step done", nil, lines(
			"Не указан номер шага.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task step done --help"),
		), "Не указан номер шага.\n"},
		{"no reason of a drop", "task step drop", []string{"2"}, lines(
			"Не указано обоснование снятия шага.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task step drop --help"),
		), "Не указано обоснование снятия шага.\n"},
		{"no note", "task note add", nil, lines(
			"Не указан текст заметки.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task note add --help"),
		), "Не указан текст заметки.\n"},
		{"no artifact name", "task artifact save", []string{"--url", "https://example.com"}, lines(
			"Не указано имя артефакта.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task artifact save --help"),
		), "Не указано имя артефакта.\n"},
		{"bad artifact name", "task artifact save", []string{"план", "--url", "https://example.com"}, lines(
			"Недопустимое имя артефакта: «план».",
			"",
			msg.Text(msg.HintArtifactName),
		), ""},
		{"no file or address", "task artifact save", []string{"plan"}, lines(
			"Не указан файл или адрес артефакта.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task artifact save --help"),
		), "Не указан файл или адрес артефакта.\n"},
		{"bad address", "task artifact save", []string{"plan", "--url", "claude.ai/code/artifact/1"}, lines(
			"Недопустимый адрес артефакта: «claude.ai/code/artifact/1».",
			"",
			msg.Text(msg.HintArtifactURL),
		), ""},
	}
	for _, tt := range tests {
		wantRefusal(t, tt.name, tt.cmd, tt.args, tt.cli, tt.agent)
	}
}
