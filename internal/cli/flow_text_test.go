package cli

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

// The flow of the tests of the output is the flow of the flow testdata, read
// from its files with no process repository.

// shopFlowDir is the flow directory of the shop as the texts name it.
const shopFlowDir = "/work/process/shop/flow"

// testdataFiles returns the files of directory rel of the process of the flow
// testdata with changes; an empty text removes the file.
func testdataFiles(t *testing.T, rel string, changes map[string]string) process.Files {
	t.Helper()
	files, err := process.ReadDir(filepath.Join("..", "flow", "testdata", "process", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	files = maps.Clone(files)
	for name, text := range changes {
		if text == "" {
			delete(files, name)
		} else {
			files[name] = text
		}
	}
	return files
}

// readShopFlow reads the flow of the shop with changes of its files.
func readShopFlow(t *testing.T, changes map[string]string) *flow.Result {
	t.Helper()
	res, err := flow.Read(testdataFiles(t, "shop/flow", changes), testdataFiles(t, "agents", nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// security is the change of the example of the plan: a stage security after
// the review, carried out by the project subagent auditor, and a new
// instruction of the review.
func security(t *testing.T) map[string]string {
	t.Helper()
	feature := testdataFiles(t, "shop/flow", nil)["scenarios/feature.yaml"]
	feature = strings.Replace(feature, "      - to: merge\n", "      - to: security\n", 1)
	feature = strings.Replace(feature, "  merge:", "  security:        { stage: security, next: merge }\n  merge:", 1)
	return map[string]string{
		"scenarios/feature.yaml": feature,
		"stages/review.md":       "## Замечания\nЗаписать замечания ревью и проверить, что каждое исправлено\nили отклонено с обоснованием.\n",
		"stages/security.yaml":   "title: Безопасность\nexit: проверка безопасности пройдена\nexecutor: auditor\n",
		"stages/security.md":     "Проверить изменения на уязвимости.\n",
		"agents/auditor.yaml":    "purpose: проверка безопасности изменений\ncapabilities: [read, search]\n",
		"agents/auditor.md":      "Найти уязвимости в изменениях задачи.\n",
	}
}

func TestFlowShowText(t *testing.T) {
	inUTC(t)
	shown := func(changes map[string]string, applied bool, draft *contract.FlowDraftInfo) contract.FlowShowOutput {
		out := contract.FlowShowOutput{Project: "shop", Dir: shopFlowDir, Draft: draft}
		if applied {
			out.Applied = &contract.Applied{Commit: "c0ffee", Time: shopTime}
		}
		return flowJSON(out, readShopFlow(t, changes).Flow)
	}
	object := func(out contract.FlowShowOutput, changes map[string]string, flag, id string) (contract.FlowShowOutput, flowShowExtra) {
		fl, uses, ok := flowObject(readShopFlow(t, changes).Flow, flag, id)
		if !ok {
			t.Fatalf("no %s %s", flag, id)
		}
		return flowJSON(out, fl), flowShowExtra{object: flag, uses: uses}
	}
	tables := table(
		[]string{msg.Text(msg.ColScenario), msg.Text(msg.ColTitle)},
		[]string{"bug", "Баг"},
		[]string{"feature", "Фича"},
	) + "\n" + table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColTitle), msg.Text(msg.ColExecutor), msg.Text(msg.ColExit)},
		[]string{"branch", "Ветка", "orchestrator", "создана ветка задачи"},
		[]string{"implementation", "Реализация", "orchestrator", "изменения сделаны, тесты проходят"},
		[]string{"merge", "Слияние", "operator", "ветка задачи влита в main"},
		[]string{"plan-bug", "План бага", "orchestrator", "причина установлена, план исправления согласован"},
		[]string{"plan-feature", "План фичи", "orchestrator", "план согласован с оператором"},
		[]string{"review", "Ревью", "reviewer", "замечания ревью записаны и разобраны"},
	) + "\n"
	agentsHeader := []string{msg.Text(msg.ColAgent), msg.Text(msg.ColSource), msg.Text(msg.ColStages)}
	library := table(agentsHeader, []string{"reviewer", "библиотека", "review"})
	withAuditor := table(agentsHeader, []string{"auditor", "проект", "security"}, []string{"reviewer", "библиотека", "review"})
	head := lines(
		"Проект: shop",
		"Флоу применён: 2026-10-09 12:30",
		"Папка флоу: /work/process/shop/flow",
	)
	draftHead := lines(
		msg.Text(msg.FlowProject, "shop"),
		msg.Text(msg.FlowAppliedAt, "—"),
		msg.Text(msg.FlowDir, shopFlowDir),
	)
	conditional := map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: triage\nnodes:\n" +
		"  triage:\n    stage: plan-bug\n    next:\n      - to: merge\n        if: срочно\n      - to: finish\n        if: |\n          не воспроизводится\n          на main\n" +
		"  merge: { stage: merge, next: finish }\n"}
	base := contract.FlowShowOutput{Project: "shop", Dir: shopFlowDir, Applied: &contract.Applied{Commit: "c0ffee", Time: shopTime}}
	feature, featureX := object(base, nil, "scenario", "feature")
	bugFork, bugForkX := object(base, conditional, "scenario", "bug")
	review, reviewX := object(base, nil, "stage", "review")
	merge, mergeX := object(base, nil, "stage", "merge")
	reviewer, reviewerX := object(base, nil, "agent", "reviewer")
	checklist, checklistX := object(base, nil, "part", "review-checklist")
	securityText := head + "\n" + table(
		[]string{msg.Text(msg.ColScenario), msg.Text(msg.ColTitle)},
		[]string{"bug", "Баг"},
		[]string{"feature", "Фича"},
	) + "\n" + table(
		[]string{msg.Text(msg.ColStage), msg.Text(msg.ColTitle), msg.Text(msg.ColExecutor), msg.Text(msg.ColExit)},
		[]string{"branch", "Ветка", "orchestrator", "создана ветка задачи"},
		[]string{"implementation", "Реализация", "orchestrator", "изменения сделаны, тесты проходят"},
		[]string{"merge", "Слияние", "operator", "ветка задачи влита в main"},
		[]string{"plan-bug", "План бага", "orchestrator", "причина установлена, план исправления согласован"},
		[]string{"plan-feature", "План фичи", "orchestrator", "план согласован с оператором"},
		[]string{"review", "Ревью", "reviewer", "замечания ревью записаны и разобраны"},
		[]string{"security", "Безопасность", "auditor", "проверка безопасности пройдена"},
	) + "\n" + withAuditor + "\n"
	tests := []struct {
		name       string
		out        contract.FlowShowOutput
		x          flowShowExtra
		cli, agent string
	}{
		{"flow", shown(nil, true, nil), flowShowExtra{}, head + "\n" + tables + library + "\n" +
			lines(hintLineOf(msg.HintFlowStage, "gentry flow show --stage "+msg.Text(msg.ArgStage))),
			head + "\n" + tables + library + "\n" + lines(hintLineOf(msg.HintFlowStage, "flow_show (stage)"))},
		{"flow with a draft", shown(nil, true, &contract.FlowDraftInfo{}), flowShowExtra{}, head + "Черновик существует.\n\n" + tables + library + "\n" +
			lines(hintLineOf(msg.HintFlowShowDraft, "gentry flow show --draft")),
			head + "Черновик существует.\n\n" + tables + library + "\n" + lines(hintLineOf(msg.HintFlowShowDraft, "flow_show (draft)"))},
		{"draft never applied", shown(nil, false, &contract.FlowDraftInfo{}), flowShowExtra{draft: true}, draftHead + "\n" + tables + library + "\n" +
			lines(hintLineOf(msg.HintFlowApply, "gentry flow apply")),
			draftHead + "\n" + tables + library + "\n" + lines(hintLineOf(msg.HintFlowApply, "flow_apply"))},
		{"draft with a subagent of the project", shown(security(t), true, &contract.FlowDraftInfo{}), flowShowExtra{draft: true},
			securityText + lines(hintLineOf(msg.HintFlowApply, "gentry flow apply")),
			securityText + lines(hintLineOf(msg.HintFlowApply, "flow_apply"))},
		{"scenario", feature, featureX, lines(
			"Сценарий: feature",
			"Название: Фича",
			"",
			"Путь по умолчанию:",
			"  branch → plan → implementation → review → merge → конец",
			"",
			"Условные переходы:",
			"  review → implementation, не более 3 возвратов: ревью выявило существенные замечания",
			"",
		) + table(
			[]string{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)},
			[]string{"branch", "branch", "orchestrator"},
			[]string{"plan", "plan-feature", "orchestrator"},
			[]string{"implementation", "implementation", "orchestrator"},
			[]string{"review", "review", "reviewer"},
			[]string{"merge", "merge", "operator"},
		), ""},
		{"scenario with a fork", bugFork, bugForkX, lines(
			msg.Text(msg.FlowScenario, "bug"),
			msg.Text(msg.FlowTitle, "Баг"),
			"",
			msg.Text(msg.FlowDefaultPath),
			"  triage",
			"",
			msg.Text(msg.FlowConditional),
			"  triage → merge: срочно",
			"  triage → конец: не воспроизводится на main",
			"",
		) + table(
			[]string{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)},
			[]string{"triage", "plan-bug", "orchestrator"},
			[]string{"merge", "merge", "operator"},
		), ""},
		{"stage", review, reviewX, lines(
			"Этап: review",
			msg.Text(msg.FlowTitle, "Ревью"),
			"Исполнитель: reviewer",
			"Выход: замечания ревью записаны и разобраны",
			"Фрагменты: review-checklist",
			"Сценарии: bug, feature",
			"",
			"Инструкция:",
			"  Проверить изменения задачи по списку.",
		), ""},
		{"stage without parts", merge, mergeX, lines(
			msg.Text(msg.FlowStage, "merge"),
			msg.Text(msg.FlowTitle, "Слияние"),
			msg.Text(msg.FlowExecutor, "operator"),
			msg.Text(msg.FlowExit, "ветка задачи влита в main"),
			msg.Text(msg.FlowParts, "—"),
			msg.Text(msg.FlowScenarios, "bug, feature"),
			"",
			msg.Text(msg.FlowInstruction),
			"  Влить ветку задачи в main.",
		), ""},
		{"subagent", reviewer, reviewerX, lines(
			"Субагент: reviewer",
			"Источник: библиотека",
			"Назначение: ревью изменений задачи — поведение и текст",
			"Возможности: read, search",
			"Этапы: review",
			"",
			msg.Text(msg.FlowInstruction),
			"  Проверить изменения задачи: поведение, тесты и тексты для оператора.",
		), ""},
		{"part", checklist, checklistX, lines(
			"Фрагмент: review-checklist",
			msg.Text(msg.FlowStages, "review"),
			"",
			"Текст:",
			"  ## Список ревью",
			"  - поведение",
			"  - тесты",
			"  - тексты",
		), ""},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { flowShowText(p, tt.out, tt.x) }, tt.cli, tt.agent)
	}
}

func TestFlowApplyText(t *testing.T) {
	inUTC(t)
	out := contract.FlowApplyOutput{Project: "shop", Applied: contract.Applied{Commit: "c0ffee", Time: shopTime},
		Agents: &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix}}}}
	wantText(t, "applied", func(p *page) { flowApplyText(p, out) }, lines(
		"Изменения флоу применены.",
		msg.Text(msg.FlowProject, "shop"),
		msg.Text(msg.FlowAppliedAt, "2026-10-09 12:30"),
		"",
		msg.Text(msg.AgentsFreeSynced, "shop"),
		msg.Text(msg.AgentsNextSession),
	), "")
	wantText(t, "discarded", func(p *page) {
		flowDiscardText(p, contract.FlowDiscardOutput{Project: "shop", Dir: shopFlowDir})
	}, lines(
		"Изменения флоу отменены.",
		msg.Text(msg.FlowProject, "shop"),
	), "")
}

func TestFlowFailureText(t *testing.T) {
	places := flow.Places{Project: "shop", Dir: shopFlowDir}
	problems := []flow.Problem{
		{Code: contract.ProblemMissingField, File: "stages/security.yaml", Message: msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "security"), "exit")},
		{Code: contract.ProblemMissingInstruction, File: "agents/auditor.yaml", Message: msg.Text(msg.ProblemMissingInstruction, msg.Text(msg.FlowObjAgent, "auditor"))},
	}
	tests := []struct {
		name string
		f    failure
		cli  string
	}{
		{"no flow", flowFailure(&flow.NoFlowError{Project: "shop", Dir: shopFlowDir}, places), lines(
			"У проекта shop нет флоу.",
			msg.Text(msg.FlowDir, shopFlowDir),
		)},
		{"no draft", flowFailure(&flow.NoDraftError{Project: "shop", Dir: shopFlowDir}, places), lines(
			"У проекта shop нет черновика флоу.",
			msg.Text(msg.FlowDir, shopFlowDir),
		)},
		{"draft with problems", flowFailure(&flow.DraftInvalidError{Dir: shopFlowDir, Problems: problems}, places), lines(
			"В черновике флоу проекта shop есть ошибки.",
			msg.Text(msg.FlowDir, shopFlowDir),
			"",
			"Ошибки:",
			"  "+problems[0].Message,
			"  "+problems[1].Message,
		)},
		{"active flow with problems", flowInvalid(places, problems[:1]), lines(
			"Во флоу проекта shop есть ошибки.",
			msg.Text(msg.FlowDir, shopFlowDir),
			"",
			msg.Text(msg.FlowProblems),
			"  "+problems[0].Message,
		)},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, tt.f, tt.cli, "")
	}
	wantFail(t, "no object", objectNotFound("shop", 1, "revew", false), lines(
		"Во флоу проекта shop нет этапа «revew».",
		"",
		hintLineOf(msg.HintFlowObjects, "gentry flow show"),
	), lines(
		"Во флоу проекта shop нет этапа «revew».",
		"",
		hintLineOf(msg.HintFlowObjects, "flow_show"),
	))
	wantFail(t, "no object of the draft", objectNotFound("shop", 2, "auditor", true), lines(
		"В черновике флоу проекта shop нет субагента «auditor».",
		"",
		msg.Text(msg.HintDraftObjects)+": gentry flow show --draft",
	), lines(
		"В черновике флоу проекта shop нет субагента «auditor».",
		"",
		msg.Text(msg.HintDraftObjects)+": flow_show (draft)",
	))
	wantFail(t, "no scenario", objectNotFound("shop", 0, "epic", false), lines(
		"Во флоу проекта shop нет сценария «epic».",
		"",
		hintLineOf(msg.HintFlowObjects, "gentry flow show"),
	), lines(
		"Во флоу проекта shop нет сценария «epic».",
		"",
		hintLineOf(msg.HintFlowObjects, "flow_show"),
	))
}
