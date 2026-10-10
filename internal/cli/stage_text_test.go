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
	branch := contract.StageShowOutput{
		Task: "SHOP-1", Node: "branch", Round: 1,
		Stage: contract.StageShowStage{Id: "branch", Title: "Ветка", Executor: "orchestrator", Exit: "создана ветка задачи",
			Instruction: "Создать ветку задачи от main.", Parts: []contract.StageShowPart{}},
		Transitions: []contract.StageShowTransition{{To: "plan", Stage: ptr("plan-feature"), Title: ptr("План фичи")}},
	}
	review := contract.StageShowOutput{
		Task: "SHOP-1", Node: "review", Round: 4,
		Stage: contract.StageShowStage{Id: "review", Title: "Ревью", Executor: "reviewer", Exit: "замечания ревью записаны и разобраны",
			Instruction: "Провести ревью.", Parts: []contract.StageShowPart{}},
		Transitions: []contract.StageShowTransition{
			{To: "implementation", Stage: ptr("implementation"), Title: ptr("Реализация"), If: ptr("ревью выявило существенные замечания"),
				MaxReturns: ptr(3), Returns: ptr(3), AllowedReturns: ptr(0)},
			{To: "merge", Stage: ptr("merge"), Title: ptr("Слияние")},
		},
	}
	header := []string{msg.Text(msg.ColTransition), msg.Text(msg.ColStage), msg.Text(msg.ColCondition), msg.Text(msg.ColReturns)}
	tests := []struct {
		name       string
		out        contract.StageShowOutput
		cli, agent string
	}{
		{"first stage", branch, lines(
			msg.Text(msg.FlowStage, "Ветка (branch), круг 1"),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "orchestrator"),
			msg.Text(msg.FlowExit, "создана ветка задачи"),
			msg.Text(msg.FlowParts, "—"),
			"",
		) + table(header, []string{"plan", "План фичи", "—", "—"}) + lines(
			"",
			msg.Text(msg.FlowInstruction),
			"  Создать ветку задачи от main.",
		), ""},
		{"returns used up", review, lines(
			msg.Text(msg.FlowStage, "Ревью (review), круг 4"),
			"Задача: SHOP-1",
			msg.Text(msg.FlowExecutor, "reviewer"),
			msg.Text(msg.FlowExit, "замечания ревью записаны и разобраны"),
			msg.Text(msg.FlowParts, "—"),
			"",
		) + table(header,
			[]string{"implementation", "Реализация", "ревью выявило существенные замечания", "3 из 3"},
			[]string{"merge", "Слияние", "—", "—"},
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
	finished := shopTask()
	finished.Stage, finished.Finished, finished.Progress.Passed = nil, true, 5
	show := []hint{hintOf(msg.HintStageShow)}
	tests := []struct {
		name       string
		out        contract.StageCloseOutput
		x          stageCloseExtra
		cli, agent string
	}{
		{"exited", contract.StageCloseOutput{Task: at("plan", "plan-feature", "План фичи", 1, 1),
			Closed: closed("branch", "branch", 1, "plan", result("Создана ветка feature/shop-1"), "")},
			stageCloseExtra{title: "Ветка", hints: show}, lines(
				"Этап «Ветка» закрыт.",
				"Вид выхода: результат",
				"Выход: Создана ветка feature/shop-1",
				"Переход: plan",
				msg.Text(msg.TaskStage, "План фичи (plan-feature), круг 1"),
				msg.Text(msg.ProgressLine, "1 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "gentry stage show"),
			), lines(
				"Этап «Ветка» закрыт.",
				"Вид выхода: результат",
				"Выход: Создана ветка feature/shop-1",
				"Переход: plan",
				msg.Text(msg.TaskStage, "План фичи (plan-feature), круг 1"),
				msg.Text(msg.ProgressLine, "1 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "stage_show"),
			)},
		{"exited by an artifact", contract.StageCloseOutput{Task: at("implementation", "implementation", "Реализация", 1, 2),
			Closed: closed("plan", "plan-feature", 1, "implementation",
				&contract.TaskPassExit{Kind: contract.TaskPassExitKindArtifact, Artifact: ptr("plan"), Text: "План согласован с оператором"}, "")},
			stageCloseExtra{title: "План фичи", hints: show}, lines(
				"Этап «План фичи» закрыт.",
				"Вид выхода: артефакт plan",
				"Выход: План согласован с оператором",
				"Переход: implementation",
				msg.Text(msg.TaskStage, "Реализация (implementation), круг 1"),
				msg.Text(msg.ProgressLine, "2 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "gentry stage show"),
			), lines(
				"Этап «План фичи» закрыт.",
				"Вид выхода: артефакт plan",
				"Выход: План согласован с оператором",
				"Переход: implementation",
				msg.Text(msg.TaskStage, "Реализация (implementation), круг 1"),
				msg.Text(msg.ProgressLine, "2 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "stage_show"),
			)},
		{"return", contract.StageCloseOutput{Task: at("implementation", "implementation", "Реализация", 2, 4),
			Closed: closed("review", "review", 1, "implementation", result("Замечания записаны"), "Две ошибки в расчёте суммы")},
			stageCloseExtra{title: "Ревью", hints: taskHints("SHOP-1", false, show...)}, lines(
				"Этап «Ревью» закрыт.",
				"Вид выхода: результат",
				"Выход: Замечания записаны",
				"Переход: implementation",
				"Обоснование: Две ошибки в расчёте суммы",
				msg.Text(msg.TaskStage, "Реализация (implementation), круг 2"),
				msg.Text(msg.ProgressLine, "4 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "gentry stage show --task SHOP-1"),
			), lines(
				"Этап «Ревью» закрыт.",
				"Вид выхода: результат",
				"Выход: Замечания записаны",
				"Переход: implementation",
				"Обоснование: Две ошибки в расчёте суммы",
				msg.Text(msg.TaskStage, "Реализация (implementation), круг 2"),
				msg.Text(msg.ProgressLine, "4 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "stage_show (task: SHOP-1)"),
			)},
		{"end of the scenario", contract.StageCloseOutput{Task: finished,
			Closed: closed("merge", "merge", 1, flow.Finish, result("Ветка влита в main"), "")},
			stageCloseExtra{title: "Слияние", hints: []hint{hintOf(msg.HintTaskClose)}}, lines(
				"Этап «Слияние» закрыт.",
				"Вид выхода: результат",
				"Выход: Ветка влита в main",
				"Переход: конец",
				msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
				msg.Text(msg.ProgressLine, "5 из 5"),
			), lines(
				"Этап «Слияние» закрыт.",
				"Вид выхода: результат",
				"Выход: Ветка влита в main",
				"Переход: конец",
				msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
				msg.Text(msg.ProgressLine, "5 из 5"),
				"",
				hintLineOf(msg.HintTaskClose, "task_close"),
			)},
		{"skipped", contract.StageCloseOutput{Task: at("plan", "plan-feature", "План фичи", 1, 1),
			Closed: closed("branch", "branch", 1, "plan", nil, "Ветка уже создана оператором")},
			stageCloseExtra{title: "Ветка", hints: show}, lines(
				"Этап «Ветка» пропущен.",
				"Обоснование: Ветка уже создана оператором",
				"Переход: plan",
				msg.Text(msg.TaskStage, "План фичи (plan-feature), круг 1"),
				msg.Text(msg.ProgressLine, "1 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "gentry stage show"),
			), lines(
				"Этап «Ветка» пропущен.",
				"Обоснование: Ветка уже создана оператором",
				"Переход: plan",
				msg.Text(msg.TaskStage, "План фичи (plan-feature), круг 1"),
				msg.Text(msg.ProgressLine, "1 из 5"),
				"",
				hintLineOf(msg.HintStageShow, "stage_show"),
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
		{"no reason of a skip", "stage skip", task.Close{Skip: true}, false, lines(
			"Не указано обоснование пропуска.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage skip --help"),
		), "Не указано обоснование пропуска.\n"},
		{"no kind", "stage exit", task.Close{Text: "Т"}, false, lines(
			"Не указан вид выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), "Не указан вид выхода.\n"},
		{"bad kind", "stage exit", task.Close{Kind: "fact", Text: "Т"}, false, lines(
			msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact"),
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), msg.Text(msg.ErrFlagValueInvalid, "--kind", "fact") + "\n"},
		{"bad kind in input", "stage exit", task.Close{Kind: "fact", Text: "Т"}, true, lines(
			msg.Text(msg.ErrInputInvalid, "недопустимое значение поля «kind»"),
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), msg.Text(msg.ErrToolFields, "недопустимое значение поля «kind»") + "\n"},
		{"no text", "stage exit", task.Close{Kind: "result"}, false, lines(
			"Не указан текст выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), "Не указан текст выхода.\n"},
		{"no artifact", "stage exit", task.Close{Kind: "artifact", Text: "Т"}, false, lines(
			"Не указан артефакт выхода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), "Не указан артефакт выхода.\n"},
		{"artifact of a result", "stage exit", task.Close{Kind: "result", Artifact: "plan", Text: "Т"}, false, lines(
			"Артефакт указывается только у выхода вида artifact.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
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
		out        contract.StepListOutput
		change     stepChange
		hints      []hint
		cli, agent string
	}{
		{"one added", contract.StepListOutput{Added: []int{1}, Steps: []contract.TaskStep{step(1, "Создать ветку", contract.TaskStepStatePlanned)}},
			stepChange{}, exit, lines(
				"К этапу добавлено шагов: 1.",
				"Номер шага: 1",
			), ""},
		{"two added", contract.StepListOutput{Added: []int{1, 2}, Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStatePlanned), step(2, "Покрыть расчёт тестами", contract.TaskStepStatePlanned)}},
			stepChange{}, exit, lines(
				"К этапу добавлено шагов: 2.",
				"Номера шагов: 1–2",
			), ""},
		{"done, one left", contract.StepListOutput{Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStateDone), step(2, "Покрыть расчёт тестами", contract.TaskStepStatePlanned)}},
			stepChange{step: 1, done: true}, exit, lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 1",
			), ""},
		{"done, none left", contract.StepListOutput{Steps: []contract.TaskStep{step(1, "Создать ветку", contract.TaskStepStateDone)}},
			stepChange{step: 1, done: true}, exit, lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 0",
			), lines(
				"Шаг 1 выполнен.",
				"Осталось шагов: 0",
				"",
				hintLineOf(msg.HintStageExit, "stage_exit (kind, text)"),
			)},
		{"dropped, outside the worktree", contract.StepListOutput{Steps: []contract.TaskStep{
			step(1, "Добавить расчёт суммы", contract.TaskStepStateDone), step(2, "Покрыть расчёт тестами", contract.TaskStepStateDropped)}},
			stepChange{step: 2}, taskHints("SHOP-1", false, exit...), lines(
				"Шаг 2 снят.",
				"Осталось шагов: 0",
			), lines(
				"Шаг 2 снят.",
				"Осталось шагов: 0",
				"",
				hintLineOf(msg.HintStageExit, "stage_exit (kind, text, task: SHOP-1)"),
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
	wantText(t, "note add", func(p *page) {
		noteAddText(p, contract.NoteAddOutput{Task: "SHOP-1", Note: note(1, "implementation", "implementation", "Проверить СБП")})
	}, "Заметка 1 добавлена.\n", "")
	wantText(t, "note list, no notes", func(p *page) {
		noteListText(p, contract.NoteListOutput{Task: "SHOP-1", Notes: []contract.TaskNote{}}, names{})
	}, "У задачи нет заметок.\n", "")
	wantText(t, "note list", func(p *page) {
		noteListText(p, contract.NoteListOutput{Task: "SHOP-1", Notes: []contract.TaskNote{
			note(1, "implementation", "implementation", "На ревью проверить, что возврат по СБП не задет"),
			note(2, "merge", "merge", "Первая строка.\nВторая строка."),
		}}, shopNames())
	}, lines(
		"1. Реализация (implementation), круг 1:",
		"   На ревью проверить, что возврат по СБП не задет",
		"",
		"2. Слияние (merge), круг 1:",
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
		out  contract.ArtifactSaveOutput
		cli  string
	}{
		{"link", contract.ArtifactSaveOutput{Task: "SHOP-1", Artifact: link}, lines(
			"Артефакт plan сохранён.",
			"Адрес: https://claude.ai/code/artifact/plan",
		)},
		{"file", contract.ArtifactSaveOutput{Task: "SHOP-1", Artifact: file}, lines(
			"Артефакт plan.md сохранён.",
			"Файл: /home/dev/.gentry/state/shop/tasks/SHOP-1/artifacts/plan.md",
		)},
		{"replaced", contract.ArtifactSaveOutput{Task: "SHOP-1", Artifact: link, Replaced: true}, lines(
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
		{"scenario passed", "stage show", &task.FinishedError{Task: "SHOP-1"}, true, lines(
			"Сценарий задачи SHOP-1 пройден.",
		), lines(
			"Сценарий задачи SHOP-1 пройден.",
			"",
			hintLineOf(msg.HintTaskClose, "task_close"),
		)},
		{"no transition at a fork", "stage exit", &task.FieldError{Field: "to", Fork: true}, true, lines(
			"На развилке не указан переход.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry stage show"),
		), lines(
			"На развилке не указан переход.",
			"",
			hintLineOf(msg.HintStageTransitions, "stage_show"),
		)},
		{"no reason at a fork", "stage exit", &task.FieldError{Field: "reason", Fork: true}, true, lines(
			"На развилке не указано обоснование перехода.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry stage exit --help"),
		), "На развилке не указано обоснование перехода.\n"},
		{"no transition", "stage exit", &task.TransitionError{Node: "review", To: "deploy", Stage: review, Transitions: []string{"implementation", "merge"}}, true, lines(
			"У этапа «Ревью» нет перехода к узлу deploy.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry stage show"),
		), lines(
			"У этапа «Ревью» нет перехода к узлу deploy.",
			"",
			hintLineOf(msg.HintStageTransitions, "stage_show"),
		)},
		{"returns used up", "stage exit", &task.ReturnLimitError{Node: "review", To: "implementation", Limit: 3}, true, lines(
			"Возвраты к узлу implementation исчерпаны.",
			"Возвратов: 3 из 3",
			"",
			hintLineOf(msg.HintOtherTransitions, "gentry stage show"),
			hintLineOf(msg.HintAllowReturn, "gentry operator record --answer "+msg.Text(msg.ArgAnswer)+" --allow-return implementation"),
		), lines(
			"Возвраты к узлу implementation исчерпаны.",
			"Возвратов: 3 из 3",
			"",
			hintLineOf(msg.HintOtherTransitions, "stage_show"),
			hintLineOf(msg.HintAllowReturn, "operator_record (answer, allow_return: implementation)"),
		)},
		{"no return", "operator record", &task.ReturnNotFoundError{Node: "branch", To: "plan", Stage: flow.Stage{ID: "branch", Title: "Ветка"}, Returns: []string{}}, true, lines(
			"У этапа «Ветка» нет возврата к узлу plan.",
			"",
			hintLineOf(msg.HintStageTransitions, "gentry stage show"),
		), lines(
			"У этапа «Ветка» нет возврата к узлу plan.",
			"",
			hintLineOf(msg.HintStageTransitions, "stage_show"),
		)},
		{"no steps", "stage exit", &task.StepsEmptyError{Node: "branch", Stage: flow.Stage{ID: "branch", Title: "Ветка"}}, true, lines(
			"У этапа «Ветка» нет шагов.",
		), lines(
			"У этапа «Ветка» нет шагов.",
			"",
			hintLineOf(msg.HintStepAdd, "step_add (steps)"),
		)},
		{"no steps, outside the worktree", "stage exit", &task.StepsEmptyError{Node: "branch", Stage: flow.Stage{ID: "branch", Title: "Ветка"}}, false, lines(
			msg.Text(msg.ErrStepsEmpty, "Ветка"),
		), lines(
			msg.Text(msg.ErrStepsEmpty, "Ветка"),
			"",
			hintLineOf(msg.HintStepAdd, "step_add (steps, task: SHOP-1)"),
		)},
		{"steps not done", "stage exit", &task.StepsOpenError{Node: "implementation", Steps: steps}, true, lines(
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
			hintLineOf(msg.HintStepDone, "step_done (step)"),
			hintLineOf(msg.HintStepDrop, "step_drop (step, reason)"),
		)},
		{"no step", "step done", &task.StepNotFoundError{Step: 7}, true, lines(
			"У этапа нет шага 7.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"У этапа нет шага 7.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"step done already", "step done", &task.StepClosedError{Step: 1, State: state.StepDone}, true, lines(
			"Шаг 1 уже выполнен.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Шаг 1 уже выполнен.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"step dropped already", "step drop", &task.StepClosedError{Step: 2, State: state.StepDropped}, true, lines(
			"Шаг 2 уже снят.",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Шаг 2 уже снят.",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"no artifact", "stage exit", &task.ArtifactNotFoundError{Name: "plan"}, true, lines(
			"Артефакт plan не сохранён.",
		), lines(
			"Артефакт plan не сохранён.",
			"",
			hintLineOf(msg.HintArtifactSave, "artifact_save (name: plan, file)"),
		)},
		{"file too large", "artifact save", &task.FileError{Path: "big.bin", Reason: task.FileTooLarge}, false, lines(
			"Файл артефакта больше 10 МБ: big.bin",
		), lines(
			"Файл артефакта больше 10 МБ: big.bin",
			"",
			hintLineOf(msg.HintArtifactLink, "artifact_save (name, url, task: SHOP-1)"),
		)},
		{"no file", "artifact save", &task.FileError{Path: "plan.md", Reason: task.FileNotFound}, true, lines(
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
		{"no step", "step add", nil, lines(
			"Не указан ни один шаг.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step add --help"),
		), "Не указан ни один шаг.\n"},
		{"long step", "step add", []string{long}, lines(
			"Шаг длиннее 120 знаков.",
			"",
			msg.Text(msg.HintStep),
		), ""},
		{"step of two lines", "step add", []string{"А\nБ"}, lines(
			"Шаг состоит из нескольких строк.",
			"",
			msg.Text(msg.HintStep),
		), ""},
		{"step number", "step done", []string{"первый"}, lines(
			"Недопустимый номер шага: «первый».",
			"",
			hintLineOf(msg.HintSteps, "gentry task show"),
		), lines(
			"Недопустимый номер шага: «первый».",
			"",
			hintLineOf(msg.HintSteps, "task_show"),
		)},
		{"no step number", "step done", nil, lines(
			"Не указан номер шага.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step done --help"),
		), "Не указан номер шага.\n"},
		{"no reason of a drop", "step drop", []string{"2"}, lines(
			"Не указано обоснование снятия шага.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry step drop --help"),
		), "Не указано обоснование снятия шага.\n"},
		{"no note", "note add", nil, lines(
			"Не указан текст заметки.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry note add --help"),
		), "Не указан текст заметки.\n"},
		{"no artifact name", "artifact save", []string{"--url", "https://example.com"}, lines(
			"Не указано имя артефакта.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry artifact save --help"),
		), "Не указано имя артефакта.\n"},
		{"bad artifact name", "artifact save", []string{"план", "--url", "https://example.com"}, lines(
			"Недопустимое имя артефакта: «план».",
			"",
			msg.Text(msg.HintArtifactName),
		), ""},
		{"no file or address", "artifact save", []string{"plan"}, lines(
			"Не указан файл или адрес артефакта.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry artifact save --help"),
		), "Не указан файл или адрес артефакта.\n"},
		{"bad address", "artifact save", []string{"plan", "--url", "claude.ai/code/artifact/1"}, lines(
			"Недопустимый адрес артефакта: «claude.ai/code/artifact/1».",
			"",
			msg.Text(msg.HintArtifactURL),
		), ""},
	}
	for _, tt := range tests {
		wantRefusal(t, tt.name, tt.cmd, tt.args, tt.cli, tt.agent)
	}
}
