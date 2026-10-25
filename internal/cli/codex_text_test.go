package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/msg"
)

// The texts of Codex are stated word for word by the tests of the functions
// that print them, here; the words of the hints by TestHintText.

// codexSetup is the output of setup codex of a state with nothing to tell.
func codexSetup(action string) contract.SetupOutput {
	return contract.SetupOutput{
		Tool: codex.Tool, Dir: "/home/dev/.gentry/integrations/codex", PluginVersion: "1", Action: action, Enabled: true,
		Settings: ptr("/home/dev/.codex/config.toml"), HooksFile: ptr("/home/dev/.codex/hooks.json"),
		HookTrust: ptr(codex.HookTrusted), HooksFeature: ptr(codex.HooksOn), Sandbox: ptr(codex.SandboxAdded),
		WorktreesTrusted: []string{}, WorktreesDistrusted: []string{},
	}
}

func TestSetupCodexText(t *testing.T) {
	installed := []string{"Gentry подключён к Codex.", "Плагин начнёт работать со следующей сессии."}
	wantText(t, "installed", func(p *page) { setupText(p, codexSetup(codex.Installed), setupExtra{}) }, lines(installed...), "")
	wantText(t, "updated", func(p *page) { setupText(p, codexSetup(codex.Updated), setupExtra{}) },
		lines("Плагин Gentry в Codex обновлён.", "Изменения вступят в силу со следующей сессии."), "")
	wantText(t, "unchanged", func(p *page) { setupText(p, codexSetup(codex.Unchanged), setupExtra{}) },
		lines("Gentry уже подключён к Codex."), "")

	switched := codexSetup(codex.Installed)
	switched.Action, switched.PreviousDir = setupSwitched, ptr("/home/dev/.gentry-old/integrations/codex")
	wantText(t, "switched", func(p *page) { setupText(p, switched, setupExtra{}) }, lines(
		"Плагин Gentry в Codex переключён на папку данных этой команды.",
		"Изменения вступят в силу со следующей сессии.",
		"Прежняя папка плагина: /home/dev/.gentry-old/integrations/codex",
	), "")

	// Each warning a block, in the order of the specification; the hints in
	// the last block. The key of a record is the text of a string of TOML.
	all := codexSetup(codex.Unchanged)
	all.Enabled = false
	all.HookTrust, all.HookError = ptr(codex.HookUntrusted), ptr("codex: нет ответа")
	all.HooksFeature, all.Sandbox = ptr(codex.HooksManagedOff), ptr(codex.SandboxProfile)
	all.WorktreesDistrusted = []string{"/work/shop", "/work/shop-fix"}
	x := setupExtra{distrustKeys: []string{`C:\Dev\shop`}}
	wantText(t, "warnings", func(p *page) { setupText(p, all, x) }, lines(
		"Gentry уже подключён к Codex.",
		"",
		"Плагин Gentry отключён в Codex.",
		"",
		"Хук запуска сессии записан, но не доверен в Codex.",
		"Причина: codex: нет ответа",
		"",
		"Хуки выключены администратором Codex: агент не получит вводных сведений в начале сессии.",
		"",
		"Песочница Codex задана профилем разрешений: папка данных Gentry в ней не открыта.",
		"",
		"Рабочие копии, которым оператор не доверил в Codex, оставлены как есть:",
		"  /work/shop",
		"  /work/shop-fix",
		"",
		msg.Text(msg.HintCodexPluginEnable),
		msg.Text(msg.HintCodexHookTrust),
		msg.Text(msg.HintCodexTrust, `C:\\Dev\\shop`),
	), "")

	for _, tt := range []struct {
		state, line string
	}{
		{codex.HookFailed, "Хук запуска сессии не записан: настройки хуков Codex не прочитаны."},
		{codex.HookUnwritten, "Хук запуска сессии не записан: настройки хуков Codex не записаны."},
	} {
		out := codexSetup(codex.Unchanged)
		out.HookTrust = ptr(tt.state)
		block := []string{"Gentry уже подключён к Codex.", "", tt.line, "Настройки хуков: /home/dev/.codex/hooks.json"}
		wantText(t, "hooks "+tt.state, func(p *page) { setupText(p, out, setupExtra{}) },
			lines(append(block, "", HintFor(msg.HintCodexHooksFix, "tool", "codex"))...), lines(block...))
	}
}

// TestSettingsInvalidText checks the refusal of settings of Codex that
// cannot be read or written: the settings and the hint to repeat setup
// codex.
func TestSettingsInvalidText(t *testing.T) {
	f := settingsInvalid(codex.Tool, "/home/dev/.codex/config.toml", codex.Unreadable)
	refusal := []string{"Настройки Codex не прочитаны.", "Настройки: /home/dev/.codex/config.toml"}
	wantFail(t, "unreadable", f, lines(append(refusal, "", HintFor(msg.HintCodexSettingsFix, "tool", "codex"))...), lines(refusal...))
	if f.code != contract.CodeSettingsInvalid || f.details["reason"] != codex.Unreadable || f.details["tool"] != codex.Tool {
		t.Errorf("failure: %+v", f)
	}
	f = settingsInvalid(codex.Tool, "/c", codex.Unwritable)
	wantFail(t, "unwritable", f, lines("Настройки Codex не записаны.", "Настройки: /c", "", HintFor(msg.HintCodexSettingsFix, "tool", "codex")),
		lines("Настройки Codex не записаны.", "Настройки: /c"))
}

// TestUpdateCodexText checks what the update tells of both AI tools: the
// lines of Claude Code first, then those of Codex, and the hints in their
// order.
func TestUpdateCodexText(t *testing.T) {
	denied := claude.PermissionUnwritten
	claudeOut := contract.SetupOutput{Tool: claude.Tool, Dir: "/d", PluginVersion: "1", Action: claude.Unchanged, Permission: &denied}
	codexOut := codexSetup(codex.Unchanged)
	codexOut.Enabled = false
	codexOut.HookTrust, codexOut.HookError = ptr(codex.HookUntrusted), ptr("нет ответа")
	codexOut.HooksFeature, codexOut.Sandbox = ptr(codex.HooksManagedOff), ptr(codex.SandboxProfile)
	codexOut.WorktreesDistrusted = []string{"/work/shop"}
	out := contract.UpdateOutput{Action: "current", Version: "0.3.0", Program: "/p", Integrations: []contract.SetupOutput{claudeOut, codexOut}}
	wantText(t, "both", func(p *page) { updateText(p, out, updateExtra{connected: true}) }, lines(
		msg.Text(msg.UpdateCurrent, "0.3.0"),
		msg.Text(msg.SetupPermissionUnwritten),
		msg.Text(msg.SetupDisabled, "Claude Code"),
		msg.Text(msg.SetupDisabled, "Codex"),
		msg.Text(msg.SetupCodexHookUntrusted),
		msg.Text(msg.SetupCodexManagedHooks),
		msg.Text(msg.SetupCodexProfile),
		"",
		msg.Text(msg.HintPluginEnable),
		msg.Text(msg.HintCodexPluginEnable),
		msg.Text(msg.HintSetupPermission, claude.PermissionRule),
		msg.Text(msg.HintCodexHookTrust),
	), "")
	codexOut = codexSetup(codex.Unchanged)
	codexOut.HookTrust = ptr(codex.HookFailed)
	out.Integrations = []contract.SetupOutput{codexOut}
	wantText(t, "hooks not read", func(p *page) { updateText(p, out, updateExtra{connected: true}) }, lines(
		msg.Text(msg.UpdateCurrent, "0.3.0"),
		msg.Text(msg.SetupCodexHooksUnread),
		"",
		HintFor(msg.HintCodexHooksFix, "tool", "codex"),
	), lines(
		msg.Text(msg.UpdateCurrent, "0.3.0"),
		msg.Text(msg.SetupCodexHooksUnread),
	))
}

// TestWorktreeAddCodexText checks the trust in Codex in the output of
// worktree add: after the layout, the settings not used, then the worktrees
// the operator did not trust; the hints last.
func TestWorktreeAddCodexText(t *testing.T) {
	out := contract.WorktreeAddOutput{Path: shopFix, Project: "shop", Action: actionAdded,
		CodexTrust: &contract.CodexTrust{Trusted: []string{}, Distrusted: []string{shopFix}}}
	wantText(t, "distrusted", func(p *page) { worktreeAddText(p, out, []string{`C:\Dev\"shop"`}) }, lines(
		msg.Text(msg.WorktreeAdded, "shop", shopFix),
		"",
		msg.Text(msg.CodexDistrusted),
		"  "+shopFix,
		"",
		msg.Text(msg.HintCodexTrust, `C:\\Dev\\\"shop\"`),
	), "")
	for _, tt := range []struct {
		reason, line string
	}{
		{codex.Unreadable, "Доверить рабочие копии в Codex не удалось: настройки Codex не прочитаны."},
		{codex.Unwritable, "Доверить рабочие копии в Codex не удалось: настройки Codex не записаны."},
	} {
		reason := tt.reason
		out.CodexTrust = &contract.CodexTrust{Trusted: []string{}, Distrusted: []string{}, Error: &reason}
		block := []string{msg.Text(msg.WorktreeAdded, "shop", shopFix), "", tt.line}
		wantText(t, "settings "+tt.reason, func(p *page) { worktreeAddText(p, out, nil) },
			lines(append(block, "", HintFor(msg.HintCodexSettingsFix, "tool", "codex"))...), lines(block...))
	}
	out.CodexTrust = &contract.CodexTrust{Trusted: []string{shopFix}, Distrusted: []string{}}
	wantText(t, "trusted", func(p *page) { worktreeAddText(p, out, nil) }, lines(msg.Text(msg.WorktreeAdded, "shop", shopFix)), "")
}

// TestProjectAddCodexText checks the place of the blocks of Codex in the
// output of project add: after the layout, before the worktrees of the
// repository outside the pool; the hints in that order in the last block.
func TestProjectAddCodexText(t *testing.T) {
	unread := codex.Unreadable
	out := contract.ProjectAddOutput{
		Project: "shop", Prefix: "SHOP", Knowledge: "/work/shop-knowledge", MainWorktree: shopMain, Worktrees: []string{shopMain}, Action: actionUnchanged,
		Agents:     &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopMain, Added: []string{"reviewer"}}}, Conflicts: []contract.AgentsConflict{}},
		CodexTrust: &contract.CodexTrust{Trusted: []string{}, Distrusted: []string{shopMain}, Error: &unread},
	}
	x := projectAddExtra{unpooled: []string{"/work/shop-wt"}, distrustKeys: []string{shopMain}}
	blocks := []string{
		msg.Text(msg.ProjectUnchanged, "shop"),
		"",
		msg.Text(msg.AgentsFreeSynced, "shop"),
		msg.Text(msg.AgentsNextSession),
		"",
		msg.Text(msg.CodexTrustUnread),
		"",
		msg.Text(msg.CodexDistrusted),
		"  " + shopMain,
		"",
		msg.Text(msg.ProjectUnpooled),
		"  /work/shop-wt",
		"",
	}
	trust := msg.Text(msg.HintCodexTrust, codex.KeyText(shopMain))
	// The agent has no command setup: its hint is left out.
	wantText(t, "blocks of Codex", func(p *page) { projectAddText(p, out, x) },
		lines(append(blocks, HintFor(msg.HintCodexSettingsFix, "tool", "codex"), trust, HintText(msg.HintProjectUnpooled))...),
		lines(append(blocks, trust, hintLineOf(msg.HintProjectUnpooled, "worktree_add (path)"))...))
}
