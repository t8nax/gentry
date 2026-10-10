package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

// shopRemote is the remote repository of the process as the texts name it.
const shopRemote = "git@example.com:shop/process.git"

// flowItem and libraryItem are the flow of the shop and the library as the
// contract names them.
var (
	flowItem    = contract.ProcessItem{Kind: contract.ProcessItemKindFlow, Project: ptr("shop")}
	libraryItem = contract.ProcessItem{Kind: contract.ProcessItemKindLibrary}
)

func TestProcessRemoteText(t *testing.T) {
	tests := []struct {
		name       string
		out        contract.ProcessRemoteOutput
		cli, agent string
	}{
		{"sent", contract.ProcessRemoteOutput{Remote: shopRemote, Result: "sent", Drafts: []contract.ProcessItem{}}, lines(
			"Удалённый репозиторий подключён: git@example.com:shop/process.git",
			"Процесс: отправлен в удалённый репозиторий",
		), ""},
		{"received", contract.ProcessRemoteOutput{Remote: shopRemote, Result: "received", Drafts: []contract.ProcessItem{}}, lines(
			msg.Text(msg.ProcessRemoteSet, shopRemote),
			"Процесс: получен из удалённого репозитория",
		), ""},
		{"unchanged", contract.ProcessRemoteOutput{Remote: shopRemote, Result: "unchanged", Drafts: []contract.ProcessItem{}}, lines(
			msg.Text(msg.ProcessRemoteSet, shopRemote),
			"Процесс: совпадает с удалённым репозиторием",
		), ""},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { processRemoteText(p, tt.out) }, tt.cli, tt.agent)
	}
}

func TestProcessStatusText(t *testing.T) {
	inUTC(t)
	none := []contract.ProcessItem{}
	tests := []struct {
		name       string
		out        contract.ProcessStatusOutput
		cli, agent string
	}{
		{"no remote", contract.ProcessStatusOutput{Drafts: none, Unsent: none, Conflicts: none}, lines(
			"Удалённый репозиторий не подключён.",
			"",
			hintLineOf(msg.HintProcessRemote, "gentry process remote <адрес>"),
		), lines(
			"Удалённый репозиторий не подключён.",
			"",
			hintLineOf(msg.HintProcessRemote, "process_remote (remote)"),
		)},
		{"synchronized", contract.ProcessStatusOutput{Remote: ptr(shopRemote), Synced: &shopTime, Drafts: none, Unsent: none, Conflicts: none}, lines(
			"Удалённый репозиторий: git@example.com:shop/process.git",
			"Синхронизирован: 2026-10-09 12:30",
		), ""},
		{"a conflict", contract.ProcessStatusOutput{Remote: ptr(shopRemote), Synced: &shopTime,
			Drafts: []contract.ProcessItem{flowItem}, Unsent: none, Conflicts: []contract.ProcessItem{flowItem}}, lines(
			msg.Text(msg.ProcessRemote, shopRemote),
			msg.Text(msg.ProcessSyncedAt, "2026-10-09 12:30"),
			"",
			"Черновики:",
			"  флоу shop",
			"",
			"Конфликты:",
			"  флоу shop",
		), ""},
		{"not sent", contract.ProcessStatusOutput{Remote: ptr(shopRemote), Synced: &shopTime,
			Drafts: none, Unsent: []contract.ProcessItem{flowItem}, Conflicts: none}, lines(
			msg.Text(msg.ProcessRemote, shopRemote),
			msg.Text(msg.ProcessSyncedAt, "2026-10-09 12:30"),
			"",
			"Не отправлено:",
			"  "+msg.Text(msg.KindFlow, "shop"),
			"",
			hintLineOf(msg.HintProcessSync, "gentry process sync"),
		), lines(
			msg.Text(msg.ProcessRemote, shopRemote),
			msg.Text(msg.ProcessSyncedAt, "2026-10-09 12:30"),
			"",
			"Не отправлено:",
			"  "+msg.Text(msg.KindFlow, "shop"),
			"",
			hintLineOf(msg.HintProcessSync, "process_sync"),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { processStatusText(p, tt.out) }, tt.cli, tt.agent)
	}
}

func TestProcessSyncText(t *testing.T) {
	none := []contract.ProcessItem{}
	laid := &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix}}}
	tests := []struct {
		name string
		out  contract.ProcessSyncOutput
		cli  string
	}{
		{"nothing new", contract.ProcessSyncOutput{Received: none, Sent: none, Conflicts: none, Restored: none}, "Процесс синхронизирован.\n"},
		{"received", contract.ProcessSyncOutput{Received: []contract.ProcessItem{libraryItem}, Sent: none, Conflicts: none, Restored: none}, lines(
			msg.Text(msg.ProcessSynced),
			"",
			"Получено:",
			"  библиотека субагентов",
		)},
		{"sent", contract.ProcessSyncOutput{Received: none, Sent: []contract.ProcessItem{flowItem}, Conflicts: none, Restored: none}, lines(
			msg.Text(msg.ProcessSynced),
			"",
			"Отправлено:",
			"  "+msg.Text(msg.KindFlow, "shop"),
		)},
		{"received and laid out", contract.ProcessSyncOutput{Received: []contract.ProcessItem{libraryItem}, Sent: none, Conflicts: none,
			Restored: none, Agents: laid}, lines(
			msg.Text(msg.ProcessSynced),
			"",
			msg.Text(msg.ProcessReceived),
			"  "+msg.Text(msg.KindLibrary),
			"",
			msg.Text(msg.AgentsFreeSynced, "shop"),
			msg.Text(msg.AgentsNextSession),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { processSyncText(p, tt.out, map[string]string{shopFix: "shop"}) }, tt.cli, "")
	}
}

// TestWriteSyncText checks the block of what a synchronization inside a
// command did, which the commands of the flow, the library and the process
// print.
func TestWriteSyncText(t *testing.T) {
	yes := true
	conflict := conflictExtra{dir: "/work/process/shop/conflict", objects: []string{msg.Text(msg.FlowObjStage, "review")}}
	problem := msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjStage, "review"), "exit")
	tests := []struct {
		name       string
		s          *contract.Sync
		x          syncExtra
		cli, agent string
	}{
		{"skipped", &contract.Sync{Unavailable: &yes}, syncExtra{}, lines(
			"Синхронизация пропущена: удалённый репозиторий недоступен.",
			"",
		), ""},
		{"deferred", &contract.Sync{Unavailable: &yes}, syncExtra{apply: true}, lines(
			"Отправка отложена: удалённый репозиторий недоступен.",
			"",
		), ""},
		{"received", &contract.Sync{Received: []contract.ProcessItem{flowItem}}, syncExtra{}, lines(
			"Получены изменения с другой машины:",
			"  флоу shop",
			"",
		), ""},
		{"conflict", &contract.Sync{Conflicts: []contract.ProcessItem{flowItem}}, syncExtra{conflicts: []conflictExtra{conflict}}, lines(
			"Флоу проекта shop изменён на другой машине; изменения этой машины сохранены как черновик.",
			"Версии файлов другой машины: /work/process/shop/conflict",
			"",
			"Изменены на обеих машинах:",
			"  "+msg.Text(msg.FlowObjStage, "review"),
			"",
			hintLineOf(msg.HintFlowDiffProject, "gentry flow diff --project shop"),
			"",
		), lines(
			"Флоу проекта shop изменён на другой машине; изменения этой машины сохранены как черновик.",
			"Версии файлов другой машины: /work/process/shop/conflict",
			"",
			"Изменены на обеих машинах:",
			"  "+msg.Text(msg.FlowObjStage, "review"),
			"",
			hintLineOf(msg.HintFlowDiffProject, "flow_diff (project: shop)"),
			"",
		)},
		{"restored", &contract.Sync{Restored: []contract.ProcessItem{flowItem}}, syncExtra{restored: [][]string{{problem}}}, lines(
			"Изменение флоу проекта shop, внесённое без Gentry, содержит ошибки и сохранено как черновик.",
			"",
			msg.Text(msg.FlowProblems),
			"  "+problem,
			"",
			hintLineOf(msg.HintFlowDiffProject, "gentry flow diff --project shop"),
			"",
		), lines(
			"Изменение флоу проекта shop, внесённое без Gentry, содержит ошибки и сохранено как черновик.",
			"",
			msg.Text(msg.FlowProblems),
			"  "+problem,
			"",
			hintLineOf(msg.HintFlowDiffProject, "flow_diff (project: shop)"),
			"",
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { writeSync(p, tt.s, tt.x, syncText{received: true, conflicts: true}) }, tt.cli, tt.agent)
	}
}

func TestProcessFailureText(t *testing.T) {
	wantFail(t, "remote unavailable", remoteUnavailable(shopRemote, "fatal: unreachable"), lines(
		"Удалённый репозиторий недоступен: git@example.com:shop/process.git",
		"",
		msg.Text(msg.HintRemoteAccess, shopRemote),
	), "")
	wantRefusal(t, "no address", "process remote", nil, lines(
		"Не указан адрес удалённого репозитория.",
		"",
		hintLineOf(msg.HintCommandHelp, "gentry process remote --help"),
	), "Не указан адрес удалённого репозитория.\n")
	wantFail(t, "not a process", processFailure(&process.RemoteInvalidError{Remote: shopMain, Reason: "not_process"}),
		"Репозиторий не является репозиторием процесса: /work/shop\n", "")
}
