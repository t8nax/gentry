package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// The words «Задача SHOP-1 закрыта.» and «Задача SHOP-1 отменена.» are those
// of the refusal of an ended task, which TestEndFailureText states; task
// close and task cancel print them too.

func TestTaskCloseText(t *testing.T) {
	closed := shopTask()
	closed.State, closed.Stage, closed.Finished = contract.TaskStateClosed, nil, true
	wantText(t, "closed", func(p *page) {
		taskCloseText(p, contract.TaskCloseOutput{Task: closed, Worktree: shopFix})
	}, lines(
		msg.Text(msg.TaskClosed, "SHOP-1"),
		"Рабочая копия освобождена: /work/shop-fix",
	), "")
}

func TestTaskCancelText(t *testing.T) {
	cancelled := shopTask()
	cancelled.State = contract.TaskStateCancelled
	cancelled.Ended = &contract.TaskEnded{Time: shopTime, Source: contract.EndedSourceOperator, Reason: ptr("Отложено до релиза каталога.")}
	passed := shopTask()
	passed.State, passed.Stage, passed.Finished = contract.TaskStateCancelled, nil, true
	passed.Ended = &contract.TaskEnded{Time: shopTime, Source: contract.EndedSourceAgent}
	tests := []struct {
		name       string
		out        contract.TaskCancelOutput
		cli, agent string
	}{
		{"with a reason", contract.TaskCancelOutput{Task: cancelled, Worktree: shopFix}, lines(
			msg.Text(msg.TaskCancelled, "SHOP-1"),
			msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
			msg.Text(msg.ReasonLine, "Отложено до релиза каталога."),
			msg.Text(msg.WorktreeReleased, shopFix),
			"",
			hintLineOf(msg.HintTaskAgain, "gentry task take --task SHOP-1 --scenario "+msg.Text(msg.ArgScenario)),
		), lines(
			msg.Text(msg.TaskCancelled, "SHOP-1"),
			msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
			msg.Text(msg.ReasonLine, "Отложено до релиза каталога."),
			msg.Text(msg.WorktreeReleased, shopFix),
			"",
			hintLineOf(msg.HintTaskAgain, "task_take (task: SHOP-1, scenario)"),
		)},
		{"scenario passed", contract.TaskCancelOutput{Task: passed, Worktree: shopFix}, lines(
			msg.Text(msg.TaskCancelled, "SHOP-1"),
			msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
			msg.Text(msg.WorktreeReleased, shopFix),
			"",
			hintLineOf(msg.HintTaskAgain, "gentry task take --task SHOP-1 --scenario "+msg.Text(msg.ArgScenario)),
		), lines(
			msg.Text(msg.TaskCancelled, "SHOP-1"),
			msg.Text(msg.TaskStage, msg.Text(msg.StageFinished)),
			msg.Text(msg.WorktreeReleased, shopFix),
			"",
			hintLineOf(msg.HintTaskAgain, "task_take (task: SHOP-1, scenario)"),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { taskCancelText(p, tt.out) }, tt.cli, tt.agent)
	}
}

func TestNotFinishedText(t *testing.T) {
	v := task.View{Task: state.Task{Prefix: "SHOP", Number: 1, Node: "branch"}, Stage: "branch", StageTitle: "Ветка", Round: 1}
	f := notFinished(v)
	wantFail(t, "in the worktree", f, lines(
		"Сценарий задачи SHOP-1 ещё не пройден.",
		msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
		"",
		hintLineOf(msg.HintStageShow, "gentry stage show"),
	), lines(
		"Сценарий задачи SHOP-1 ещё не пройден.",
		msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
		"",
		hintLineOf(msg.HintStageShow, "stage_show"),
	))
	f.hints = taskHints("SHOP-1", false, f.hints...)
	wantFail(t, "outside the worktree", f, lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
		"",
		hintLineOf(msg.HintStageShow, "gentry stage show --task SHOP-1"),
	), lines(
		msg.Text(msg.ErrScenarioNotFinished, "SHOP-1"),
		msg.Text(msg.TaskStage, "Ветка (branch), круг 1"),
		"",
		hintLineOf(msg.HintStageShow, "stage_show (task: SHOP-1)"),
	))
}

func TestEndFailureText(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		cli, agent string
	}{
		{"task ended", &task.EndedError{Task: "SHOP-1", State: state.TaskClosed}, lines(
			"Задача SHOP-1 закрыта.",
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show SHOP-1"),
		), lines(
			"Задача SHOP-1 закрыта.",
			"",
			hintLineOf(msg.HintTaskShow, "task_show (task: SHOP-1)"),
		)},
		{"task cancelled", &task.EndedError{Task: "SHOP-2", State: state.TaskCancelled}, lines(
			"Задача SHOP-2 отменена.",
			"",
			hintLineOf(msg.HintTaskAgain, "gentry task take --task SHOP-2 --scenario "+msg.Text(msg.ArgScenario)),
		), lines(
			"Задача SHOP-2 отменена.",
			"",
			hintLineOf(msg.HintTaskAgain, "task_take (task: SHOP-2, scenario)"),
		)},
		{"task in work", &task.InWorkError{Task: "SHOP-3", Worktree: shopFix}, lines(
			"Задача SHOP-3 уже в работе.",
			msg.Text(msg.TaskWorktree, shopFix),
			"",
			hintLineOf(msg.HintTaskShow, "gentry task show SHOP-3"),
		), lines(
			"Задача SHOP-3 уже в работе.",
			msg.Text(msg.TaskWorktree, shopFix),
			"",
			hintLineOf(msg.HintTaskShow, "task_show (task: SHOP-3)"),
		)},
		{"task of another project", &task.ProjectMismatchError{Task: "SHOP-2", Project: "shop", Worktree: "/work/blog", WorktreeProject: "blog"}, lines(
			"Задача SHOP-2 относится к другому проекту.",
			"Проект задачи: shop",
			"Проект рабочей копии: blog",
			"",
			hintLineOf(msg.HintWorktreeListProject, "gentry worktree list --project shop"),
		), lines(
			"Задача SHOP-2 относится к другому проекту.",
			"Проект задачи: shop",
			"Проект рабочей копии: blog",
			"",
			hintLineOf(msg.HintWorktreeListProject, "worktree_list (project: shop)"),
		)},
	}
	for _, tt := range tests {
		f, ok := endFailure(tt.err)
		if !ok {
			t.Errorf("%s: no refusal", tt.name)
			continue
		}
		wantFail(t, tt.name, f, tt.cli, tt.agent)
	}
}

func TestAttemptsText(t *testing.T) {
	inUTC(t)
	out := contract.TaskAttemptsOutput{Task: "SHOP-1", Attempts: []contract.TaskAttempt{
		{Attempt: 1, State: contract.TaskAttemptsOutputAttemptsElemStateCancelled, Scenario: contract.TaskScenario{Id: "feature", Title: "Фича"},
			Taken: shopTime, Ended: &contract.TaskEnded{Time: shopTime, Source: contract.EndedSourceOperator, Reason: ptr("Отложено.")}},
		{Attempt: 2, State: contract.TaskAttemptsOutputAttemptsElemStateActive, Scenario: contract.TaskScenario{Id: "bug", Title: "Баг"}, Taken: shopTime},
	}}
	attempts := table(
		[]string{msg.Text(msg.ColAttempt), msg.Text(msg.ColState), msg.Text(msg.ColScenario), msg.Text(msg.ColTaken),
			msg.Text(msg.ColEnded), msg.Text(msg.ColCancelReason)},
		[]string{"1", msg.Text(msg.TaskStateCancelled), "Фича", "2026-10-09 12:30", "2026-10-09 12:30", "Отложено."},
		[]string{"2", msg.Text(msg.TaskStateActive), "Баг", "2026-10-09 12:30", noValue, noValue},
	) + "\n"
	wantText(t, "attempts", func(p *page) {
		attemptsText(p, out, taskHints("SHOP-1", false, hintOf(msg.HintAttempt)))
	}, attempts+lines(hintLineOf(msg.HintAttempt, "gentry task attempts SHOP-1 "+msg.Text(msg.ArgAttempt))),
		attempts+lines(hintLineOf(msg.HintAttempt, "task_attempts (task: SHOP-1, attempt)")))
}

func TestAttemptText(t *testing.T) {
	inUTC(t)
	first := shopTask()
	first.State, first.Worktree = contract.TaskStateCancelled, nil
	first.Stage = &contract.TaskStage{Node: "plan", Id: "plan-feature", Title: "План фичи", Round: 1}
	first.Progress.Passed = 1
	first.Ended = &contract.TaskEnded{Time: shopTime, Source: contract.EndedSourceOperator, Reason: ptr("Отложено.")}
	src := contract.TaskPassSourceAgent
	outcome := contract.TaskPassOutcome(state.OutcomeExit)
	path := []contract.TaskPass{
		{Node: "branch", Stage: "branch", Round: 1, Entered: shopTime, Steps: []contract.TaskStep{}, Outcome: &outcome, Source: &src,
			To: ptr("plan"), Closed: &shopTime, Exit: &contract.TaskPassExit{Kind: contract.TaskPassExitKindResult, Text: "Ветка создана"}},
		{Node: "plan", Stage: "plan-feature", Round: 1, Entered: shopTime, Steps: []contract.TaskStep{}},
	}
	out := contract.TaskShowOutput{Task: first, Path: path,
		Artifacts: []contract.TaskArtifact{{Name: "plan.md", Kind: contract.TaskArtifactKindFile, Path: ptr("/work/plan.md"), Saved: shopTime}},
		Notes:     []contract.TaskNote{{Number: 1, Node: "branch", Stage: "branch", Round: 1, Text: "Промокоды в таблице promo.", Added: shopTime}},
		OperatorDecisions: []contract.OperatorDecision{{Number: 1, Node: "branch", Stage: "branch", Round: 1, Answer: "Только для карт.",
			Source: contract.OperatorDecisionSourceOperator, Recorded: shopTime}},
	}
	text := lines(
		"Задача SHOP-1, попытка 1: Частичный возврат по карте",
		"",
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.TaskState, msg.Text(msg.TaskStateCancelled)),
		msg.Text(msg.TaskScenario, "Фича (feature)"),
		msg.Text(msg.TaskStage, "План фичи (plan-feature), круг 1"),
		msg.Text(msg.ProgressLine, "1 из 5"),
		msg.Text(msg.TaskTakenAt, "2026-10-09 12:30"),
		msg.Text(msg.TaskCancelledByOperator, "2026-10-09 12:30"),
		msg.Text(msg.TaskFlowApplied, "2026-10-09 12:30"),
		msg.Text(msg.CancelReasonLine, "Отложено."),
		"",
		"Ветка (branch), круг 1",
		msg.Text(msg.OutcomeLine, msg.Text(msg.ExitKindResult)),
		msg.Text(msg.ExitTextLine, "Ветка создана"),
		msg.Text(msg.TransitionLine, "plan"),
		msg.Text(msg.RecordedLine, msg.Text(msg.RecordedByAgent)),
		msg.Text(msg.ClosedLine, "2026-10-09 12:30"),
		"",
		"План фичи (plan-feature), круг 1",
		msg.Text(msg.OutcomeLine, noValue),
		"",
		msg.Text(msg.StepsNone),
		"",
	) + table(
		[]string{msg.Text(msg.ColArtifact), msg.Text(msg.ColKind), msg.Text(msg.ColSaved), msg.Text(msg.ColPlace)},
		[]string{"plan.md", msg.Text(msg.ArtifactKindFile), "2026-10-09 12:30", "/work/plan.md"},
	) + lines(
		"",
		"Заметки:",
		"  1. Ветка (branch), круг 1:",
		"     Промокоды в таблице promo.",
		"",
		msg.Text(msg.DecisionsHeading),
		"  "+msg.Text(msg.DecisionHeading, 1, "Ветка (branch), круг 1", msg.Text(msg.TaskSourceOperator)),
		"     "+msg.Text(msg.DecisionAnswer)+": Только для карт.",
	)
	wantText(t, "attempt", func(p *page) { attemptText(p, out, shopNames()) }, text, "")
}
