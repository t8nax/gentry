package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// shopLibrary is the library of subagents of the process as the texts name
// it.
const shopLibrary = "/work/process/agents"

func TestLibraryText(t *testing.T) {
	inUTC(t)
	applied := &contract.Applied{Commit: "c0ffee", Time: shopTime}
	diff := contract.LibraryDiffOutput{Dir: shopLibrary, Applied: applied,
		Changes: []contract.LibraryChange{{Id: "reviewer", Change: contract.LibraryDiffOutputChangesElemChangeModified}}}
	body := lines(
		"Библиотека применена: 2026-10-09 12:30",
		"",
		msg.Text(msg.FlowChanges),
		"  "+msg.Text(msg.FlowChange, msg.Text(msg.FlowObjAgent, "reviewer"), msg.Text(msg.FlowChangeModified)),
		"",
	)
	wantText(t, "diff", func(p *page) { libraryDiffText(p, diff) },
		body+lines(hintLineOf(msg.HintLibraryApply, "gentry library apply")),
		body+lines(hintLineOf(msg.HintLibraryApply, "library_apply")))
	apply := contract.LibraryApplyOutput{Applied: *applied,
		Agents: &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix}}}}
	wantText(t, "apply", func(p *page) { libraryApplyText(p, apply) }, lines(
		"Изменения библиотеки применены.",
		msg.Text(msg.LibraryAppliedAt, "2026-10-09 12:30"),
		"",
		msg.Text(msg.AgentsFreeSyncedAll),
		msg.Text(msg.AgentsNextSession),
	), "")
	wantText(t, "discard", func(p *page) { libraryDiscardText(p, contract.LibraryDiscardOutput{Dir: shopLibrary}) },
		"Изменения библиотеки отменены.\n", "")
}

func TestLibraryFailureText(t *testing.T) {
	problems := []flow.Problem{
		{Code: contract.ProblemMissingField, File: "tester.yaml", Message: msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjAgent, "tester"), "purpose")},
		{Code: contract.ProblemExtraFile, File: "notes.txt", Message: msg.Text(msg.ProblemLibraryExtraFile, "notes.txt")},
	}
	broken := []flow.Problem{{Code: contract.ProblemUnknownExecutor, File: "stages/review.yaml", Line: 3,
		Message: msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewer")}}
	tests := []struct {
		name string
		err  error
		cli  string
	}{
		{"no draft", &flow.NoLibraryDraftError{Dir: shopLibrary}, lines(
			"У библиотеки субагентов нет черновика.",
			"Папка библиотеки: /work/process/agents",
		)},
		{"draft with problems", &flow.LibraryInvalidError{Dir: shopLibrary, Problems: problems}, lines(
			"В изменениях библиотеки есть ошибки.",
			msg.Text(msg.LibraryDir, shopLibrary),
			"",
			msg.Text(msg.FlowProblems),
			"  "+problems[0].Message,
			"  "+problems[1].Message,
		)},
		{"draft that breaks flows", &flow.LibraryInvalidError{Dir: shopLibrary, Problems: []flow.Problem{},
			Flows: []flow.FlowProblems{{Project: "shop", Problems: broken}}}, lines(
			"Изменения библиотеки вносят ошибки во флоу проектов.",
			msg.Text(msg.LibraryDir, shopLibrary),
			"",
			"Ошибки флоу проекта shop:",
			"  "+broken[0].Message,
		)},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, libraryFailure(tt.err), tt.cli, "")
	}
}
