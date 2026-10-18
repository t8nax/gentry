package cli

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/releases"
	"github.com/t8nax/gentry/internal/update"
)

// TestUpdateText checks the result of gentry update: the versions, the line
// of the new sessions, what needs attention, the incompatible changes and
// the page of the release. What was built anew and laid out is not told.
func TestUpdateText(t *testing.T) {
	const program = "/home/anna/.gentry/bin/gentry"
	link := releases.Page("0.3.0")
	plugin := func(action string, enabled bool) contract.SetupOutput {
		return contract.SetupOutput{Tool: claude.Tool, Dir: "/home/anna/.gentry/integrations/claude", PluginVersion: "0.3.0+1a2b3c4d", Action: action, Enabled: enabled}
	}
	updated := func(from string, integrations ...contract.SetupOutput) contract.UpdateOutput {
		return contract.UpdateOutput{Action: "updated", Previous: &from, Version: "0.3.0", Program: program, Page: &link, Integrations: append([]contract.SetupOutput{}, integrations...)}
	}
	current := func(integrations ...contract.SetupOutput) contract.UpdateOutput {
		return contract.UpdateOutput{Action: "current", Version: "0.3.0", Program: program, Integrations: append([]contract.SetupOutput{}, integrations...)}
	}

	breaking := updated("0.1.0", plugin(claude.Updated, true))
	breaking.Breaking = []contract.UpdateBreaking{
		{Version: "0.2.0", Lines: []string{"Поле `price` заказа заменено на `total`. Отчёты магазина нужно поправить."}},
		{Version: "0.3.0", Lines: []string{"Команда `shop export` убрана.", "Скидки считаются от цены без доставки."}},
	}
	attention := updated("0.1.0", plugin(claude.Updated, false))
	attention.ChangelogError = ptr("https://github.com/t8nax/gentry/raw/v0.3.0/CHANGELOG.md: HTTP 404 Not Found")
	conflict := contract.AgentsConflict{Worktree: "/home/anna/shop-fix", Agent: "reviewer", File: ".claude/agents/reviewer.md", Reason: agents.Tracked}
	attention.Agents = []contract.UpdateAgents{
		{Project: "admin", Changed: true, Worktrees: []contract.AgentsWorktree{}},
		{Project: "shop", Changed: true, Worktrees: []contract.AgentsWorktree{}, Conflicts: []contract.AgentsConflict{conflict}},
	}
	table := render(cliChannel, func(p *page) { writeConflicts(&p.Builder, []contract.AgentsConflict{conflict}) })

	tests := []struct {
		name       string
		out        contract.UpdateOutput
		cli, agent string
	}{
		{"updated", breaking, lines(
			"Gentry обновлён: 0.1.0 → 0.3.0",
			"Изменения вступят в силу в новых сессиях ИИ-инструментов.",
			"",
			"Несовместимые изменения 0.2.0:",
			"  Поле `price` заказа заменено на `total`. Отчёты магазина нужно поправить.",
			"Несовместимые изменения 0.3.0:",
			"  Команда `shop export` убрана.",
			"  Скидки считаются от цены без доставки.",
			"",
			"Страница выпуска: https://t8nax.github.io/gentry/0.3.0/",
		), ""},
		{"no integrations", updated("0.2.0"), lines(
			msg.Text(msg.UpdateUpdated, "0.2.0", "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
			"Интеграции с ИИ-инструментами не подключены.",
			"",
			msg.Text(msg.ReleasePageLink, link),
			"",
			hintLineOf(msg.HintUpdateSetup, "gentry setup <ИИ-инструмент>"),
		), lines(
			msg.Text(msg.UpdateUpdated, "0.2.0", "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
			msg.Text(msg.UpdateNoIntegrations),
			"",
			msg.Text(msg.ReleasePageLink, link),
		)},
		{"what needs attention", attention, lines(
			msg.Text(msg.UpdateUpdated, "0.1.0", "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
			msg.Text(msg.SetupDisabled),
			"",
			msg.Text(msg.AgentsConflictWarning),
			"",
		) + table + lines(
			"",
			"Не удалось прочитать журнал изменений.",
			"Ошибка: https://github.com/t8nax/gentry/raw/v0.3.0/CHANGELOG.md: HTTP 404 Not Found",
			"",
			msg.Text(msg.ReleasePageLink, link),
			"",
			msg.Text(msg.HintPluginEnable),
			hintLineOf(msg.HintAgentsConflictWarning, "gentry agents deploy --project shop"),
		), lines(
			msg.Text(msg.UpdateUpdated, "0.1.0", "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
			msg.Text(msg.SetupDisabled),
			"",
			msg.Text(msg.AgentsConflictWarning),
			"",
		) + table + lines(
			"",
			msg.Text(msg.UpdateChangelogFailed, *attention.ChangelogError),
			"",
			msg.Text(msg.ReleasePageLink, link),
			"",
			msg.Text(msg.HintPluginEnable),
			hintLineOf(msg.HintAgentsConflictWarning, "agents_deploy (project: shop)"),
		)},
		{"current", current(plugin(claude.Unchanged, true)), lines(
			"Установлена последняя версия Gentry.",
			"Версия: 0.3.0",
		), ""},
		{"current, plugin built anew", current(plugin(claude.Updated, true)), lines(
			msg.Text(msg.UpdateCurrent, "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
		), ""},
	}
	for _, tt := range tests {
		x := updateExtra{connected: len(tt.out.Integrations) > 0}
		wantText(t, tt.name, func(p *page) { updateText(p, tt.out, x) }, tt.cli, tt.agent)
	}
	// The rule of permission setup could not read or write, with the plugin
	// disabled: the lines of setup in the block of the versions, its hints
	// last.
	for permission, line := range map[string]msg.Key{claude.PermissionFailed: msg.SetupPermissionFailed, claude.PermissionUnwritten: msg.SetupPermissionUnwritten} {
		denied := plugin(claude.Updated, false)
		denied.Permission = &permission
		wantText(t, "permission "+permission, func(p *page) { updateText(p, updated("0.1.0", denied), updateExtra{connected: true}) }, lines(
			msg.Text(msg.UpdateUpdated, "0.1.0", "0.3.0"),
			msg.Text(msg.UpdateNewSessions),
			msg.Text(line),
			msg.Text(msg.SetupDisabled),
			"",
			msg.Text(msg.ReleasePageLink, link),
			"",
			msg.Text(msg.HintPluginEnable),
			msg.Text(msg.HintSetupPermission, claude.PermissionRule),
		), "")
	}
	// An integration that could not be built anew is connected all the same:
	// its failure is told after the result.
	wantText(t, "integration failed", func(p *page) { updateText(p, updated("0.1.0"), updateExtra{connected: true}) }, lines(
		msg.Text(msg.UpdateUpdated, "0.1.0", "0.3.0"),
		msg.Text(msg.UpdateNewSessions),
		"",
		msg.Text(msg.ReleasePageLink, link),
	), "")
}

// TestUpdateRefusalText checks the refusals of gentry update, before and
// after the program is replaced: the errors of the scenario and of the
// releases turned into the text and the code of each.
func TestUpdateRefusalText(t *testing.T) {
	const (
		running   = "/home/anna/Downloads/gentry"
		installed = "/home/anna/.gentry/bin/gentry"
		spaced    = "/home/anna/Gentry data/bin/gentry"
	)
	install := releases.InstallCommand()
	tests := []struct {
		name       string
		f          failure
		cli, agent string
	}{
		{"program elsewhere", updateRefusal(&update.ElsewhereError{Running: running, Installed: installed}), lines(
			"Обновляется только программа из папки данных.",
			"Программа: /home/anna/Downloads/gentry",
			"Программа в папке данных: /home/anna/.gentry/bin/gentry",
			"",
			hintLineOf(msg.HintUpdateProgram, "/home/anna/.gentry/bin/gentry update"),
		), ""},
		{"program elsewhere, a path with a space", programElsewhere(running, spaced), lines(
			msg.Text(msg.ErrProgramElsewhere, running, spaced),
			"",
			hintLineOf(msg.HintUpdateProgram, `"`+spaced+`" update`),
		), ""},
		{"no program in the data root", updateRefusal(&update.ElsewhereError{Running: running}), lines(
			msg.Text(msg.ErrProgramElsewhere, running, "не установлена"),
			"",
			msg.Text(msg.HintInstallGentry, install),
		), ""},
		{"no Claude Code", updateRefusal(&update.ToolNotFoundError{Tool: claude.Tool}), lines(
			msg.Text(msg.ErrToolNotFound, "claude"),
			"",
			msg.Text(msg.HintToolNotFound, "Claude Code")+": gentry update",
		), msg.Text(msg.ErrToolNotFound, "claude") + "\n"},
		{"no release build", updateRefusal(&update.NotReleaseError{Version: "0.0.0-dev"}), lines(
			"Установлена сборка не из выпуска.",
			"Версия: 0.0.0-dev",
			"",
			msg.Text(msg.HintInstallRelease, install),
		), ""},
		{"newer than the release", updateRefusal(&update.NewerError{Version: "0.4.0", Latest: "0.3.0"}), lines(
			"Установленная версия новее последнего выпуска.",
			"Версия: 0.4.0",
			"Последний выпуск: 0.3.0",
		), ""},
		{"no latest release", updateRefusal(&releases.NotFoundError{}), lines(
			"Выпуск не найден.",
			"Версия: последняя",
			"",
			msg.Text(msg.HintReleases, "https://github.com/t8nax/gentry/releases"),
		), ""},
		{"no release of the version", updateRefusal(&releases.NotFoundError{Version: "0.3.0"}), lines(
			msg.Text(msg.ErrReleaseNotFound, "0.3.0"),
			"",
			msg.Text(msg.HintReleases, releases.GitHub),
		), ""},
		{"download failed", updateRefusal(&releases.DownloadError{URL: "https://github.com/t8nax/gentry/releases/latest", Err: errors.New("connection refused")}), lines(
			"Не удалось скачать файл.",
			"Адрес: https://github.com/t8nax/gentry/releases/latest",
			"Ошибка: connection refused",
		), ""},
		{"checksum mismatch", updateRefusal(&releases.ChecksumError{File: "gentry-linux-amd64", Want: "aa11", Got: "bb22"}), lines(
			"Контрольная сумма скачанного файла не совпала.",
			"Файл: gentry-linux-amd64",
			"Ожидалась: aa11",
			"Получена: bb22",
		), ""},
		{"program not written", updateRefusal(&releases.WriteError{Program: installed, Err: errors.New("permission denied")}), lines(
			"Не удалось записать программу.",
			"Программа: /home/anna/.gentry/bin/gentry",
			"Ошибка: permission denied",
		), ""},
		{"integration failed", integrationsFailed([]integrationFailure{{tool: claude.Tool, reason: "claude отказал"}}), lines(
			"Не удалось пересоздать интеграцию с ИИ-инструментом.",
			"ИИ-инструмент: Claude Code",
			"Ошибка: claude отказал",
			"",
			hintLineOf(msg.HintIntegrationSetup, "gentry setup claude"),
		), msg.Text(msg.ErrIntegrationFailed, "Claude Code", "claude отказал") + "\n"},
		{"subagents failed", agentsFailed([]agentsFailure{{project: "shop", reason: "git не признаёт копию"}, {project: "admin", reason: "флоу не прочитан"}}), lines(
			msg.Text(msg.AgentsSyncFailed),
			msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.AgentsSyncFailedReason, "git не признаёт копию"),
			"",
			msg.Text(msg.TaskProject, "admin"),
			msg.Text(msg.AgentsSyncFailedReason, "флоу не прочитан"),
			"",
			hintLineOf(msg.HintAgentsDeployFailed, "gentry agents deploy --project shop"),
			hintLineOf(msg.HintAgentsDeployFailed, "gentry agents deploy --project admin"),
		), lines(
			msg.Text(msg.AgentsSyncFailed),
			msg.Text(msg.TaskProject, "shop"),
			msg.Text(msg.AgentsSyncFailedReason, "git не признаёт копию"),
			"",
			msg.Text(msg.TaskProject, "admin"),
			msg.Text(msg.AgentsSyncFailedReason, "флоу не прочитан"),
			"",
			hintLineOf(msg.HintAgentsDeployFailed, "agents_deploy (project: shop)"),
			hintLineOf(msg.HintAgentsDeployFailed, "agents_deploy (project: admin)"),
		)},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, tt.f, tt.cli, tt.agent)
	}

	// The code of each refusal, and of those whose texts are of setup.
	codes := map[string]string{
		"program elsewhere":                      contract.CodeProgramElsewhere,
		"program elsewhere, a path with a space": contract.CodeProgramElsewhere,
		"no program in the data root":            contract.CodeProgramElsewhere,
		"no Claude Code":                         contract.CodeToolNotFound,
		"no release build":                       contract.CodeNotRelease,
		"newer than the release":                 contract.CodeNewerThanRelease,
		"no latest release":                      contract.CodeReleaseNotFound,
		"no release of the version":              contract.CodeReleaseNotFound,
		"download failed":                        contract.CodeDownloadFailed,
		"checksum mismatch":                      contract.CodeChecksumMismatch,
		"program not written":                    contract.CodeIOError,
		"integration failed":                     contract.CodeIntegrationFailed,
		"subagents failed":                       contract.CodeAgentsFailed,
	}
	for _, tt := range tests {
		if tt.f.code != codes[tt.name] {
			t.Errorf("%s: the code %s, want %s", tt.name, tt.f.code, codes[tt.name])
		}
	}
	other := "/home/anna/.gentry-old/integrations/claude"
	for _, c := range []struct {
		f    failure
		code string
	}{
		{updateRefusal(&update.PluginElsewhereError{Tool: claude.Tool, Registered: other, Dir: "/home/anna/.gentry/integrations/claude"}), contract.CodePluginElsewhere},
		{updateRefusal(&update.CheckError{Tool: claude.Tool, Err: &failureError{toolFailed(claude.Tool, "claude plugin marketplace list --json", "нет ответа")}}), contract.CodeToolFailed},
		{updateRefusal(errors.New("сбой")), contract.CodeInternal},
	} {
		if c.f.code != c.code {
			t.Errorf("%s: the code %s, want %s", c.f.message, c.f.code, c.code)
		}
	}
	// A program that could not get its name back is told with the reason.
	restore := updateRefusal(&releases.WriteError{Program: installed, Err: errors.New("busy"), Restore: errors.New("denied")})
	wantFail(t, "program not given its name back", restore, lines(msg.Text(msg.ErrProgramWrite, installed, "busy; denied")), "")
}

// TestUpdateFailures checks that the failures after the program is
// replaced are one refusal: of the integrations, with the subagents under
// it; each failure is told by the first line of its message, with its code
// in the details, and without its hints.
func TestUpdateFailures(t *testing.T) {
	r := &update.Result{
		Action: update.Updated, Version: "0.3.0", Program: "/home/anna/.gentry/bin/gentry",
		Tools:    []update.ToolFailure{{Tool: claude.Tool, Err: &failureError{failure{code: contract.CodeToolFailed, message: "claude отказал\nподробности claude", hints: []hint{hintOf(msg.HintPluginEnable)}}}}},
		Projects: []update.ProjectFailure{{Project: "shop", Err: &failureError{failure{code: contract.CodeGitFailed, message: "git не признаёт копию\nfatal: not a git repository"}}}},
	}
	f := updateFailed(r)
	if f == nil || f.code != contract.CodeIntegrationFailed {
		t.Fatalf("failure %+v, want %s", f, contract.CodeIntegrationFailed)
	}
	want := lines(
		msg.Text(msg.ErrIntegrationFailed, "Claude Code", "claude отказал"),
		"",
		msg.Text(msg.AgentsSyncFailed),
		msg.Text(msg.TaskProject, "shop"),
		msg.Text(msg.AgentsSyncFailedReason, "git не признаёт копию"),
		"",
		hintLineOf(msg.HintIntegrationSetup, "gentry setup claude"),
		hintLineOf(msg.HintAgentsDeployFailed, "gentry agents deploy --project shop"),
	)
	wantIn(t, "integrations and subagents failed", "command line", failText(cliChannel, *f), want)
	tools, _ := f.details["integrations"].([]map[string]any)
	projects, _ := f.details["agents"].([]map[string]any)
	if len(tools) != 1 || tools[0]["code"] != contract.CodeToolFailed || len(projects) != 1 || projects[0]["code"] != contract.CodeGitFailed {
		t.Errorf("details %v, want integrations and agents with the codes of their failures", f.details)
	}
	if updateFailed(&update.Result{Action: update.Updated}) != nil {
		t.Error("an update with nothing failed is a failure")
	}
}

// TestUpdateElsewhere checks that gentry update of a program outside the bin
// of the data root, such as the program of the tests, refuses with
// program_elsewhere and names no program of the data root, where there is
// none.
func TestUpdateElsewhere(t *testing.T) {
	exit, stdout, _ := run("update", "--json")
	if exit != contract.ExitError {
		t.Errorf("exit code %d, want %d", exit, contract.ExitError)
	}
	validate(t, "schemas/error.json", stdout)
	var out contract.ErrorOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	exe, _ := executable()
	if out.Error.Code != contract.CodeProgramElsewhere || out.Error.Details["program"] != exe || out.Error.Details["installed"] != nil {
		t.Errorf("update --json: %s\nwant program_elsewhere of %s", stdout, exe)
	}
	if out.Error.Hint == nil || *out.Error.Hint != msg.Text(msg.HintInstallGentry, releases.InstallCommand()) {
		t.Errorf("update --json: %s\nwant the hint of the installation", stdout)
	}
}

// TestUpdateAdapters checks that each AI tool of setup has the adapter the
// update needs of it: without one the tool would drop out of the update.
func TestUpdateAdapters(t *testing.T) {
	for _, tool := range tools {
		a, ok := updateAdapters[tool]
		if !ok || a.program == "" || a.elsewhere == nil || a.current == nil {
			t.Errorf("the AI tool %s has no adapter of update: %+v", tool, a)
		}
	}
	if got := updateTools("0.3.0"); len(got) != len(tools) {
		t.Errorf("updateTools gives %d tools, want %d, those of setup", len(got), len(tools))
	}
}
