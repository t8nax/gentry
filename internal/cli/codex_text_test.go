package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/msg"
)

// The texts of Codex are stated word for word by the acceptance tests of
// GEN-70: the tests here build them from the catalog and check the blocks
// and their order in each channel.

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
	deploy := HintText(msg.HintAgentsDeployAll)
	deployTool := hintLineOf(msg.HintAgentsDeployAll, "agents_deploy (project)")
	first := lines(msg.Text(msg.SetupInstalled, "Codex"), "", deploy)
	wantText(t, "installed with projects", func(p *page) { setupText(p, codexSetup(codex.Installed), setupExtra{projects: true}) },
		first, lines(msg.Text(msg.SetupInstalled, "Codex"), "", deployTool))
	wantText(t, "unchanged with projects", func(p *page) { setupText(p, codexSetup(codex.Unchanged), setupExtra{projects: true}) },
		lines(msg.Text(msg.SetupUnchanged, "Codex")), "")

	switched := codexSetup(codex.Installed)
	switched.Action, switched.PreviousDir = setupSwitched, ptr("/home/dev/.gentry-old/integrations/codex")
	wantText(t, "switched", func(p *page) { setupText(p, switched, setupExtra{}) },
		lines(msg.Text(msg.SetupSwitched, "Codex", "/home/dev/.gentry-old/integrations/codex")), "")

	// Each warning a block, in the order of the specification; the hints in
	// the last block.
	all := codexSetup(codex.Unchanged)
	all.Enabled = false
	all.HookTrust, all.HookError = ptr(codex.HookUntrusted), ptr("codex: нет ответа")
	all.HooksFeature, all.Sandbox = ptr(codex.HooksManagedOff), ptr(codex.SandboxProfile)
	all.WorktreesDistrusted = []string{"/work/shop", "/work/shop-fix"}
	// The key of the record is written as the text of a string of TOML.
	x := setupExtra{distrustKeys: []string{`C:\Dev\shop`}}
	warnings := []string{
		msg.Text(msg.SetupUnchanged, "Codex"),
		"",
		msg.Text(msg.SetupDisabled, "Codex"),
		"",
		msg.Text(msg.SetupCodexHookUntrusted),
		msg.Text(msg.SetupCodexHookReason, "codex: нет ответа"),
		"",
		msg.Text(msg.SetupCodexManagedHooks),
		"",
		msg.Text(msg.SetupCodexProfile),
		"",
		msg.Text(msg.CodexDistrusted),
		"  /work/shop",
		"  /work/shop-fix",
		"",
		msg.Text(msg.HintCodexPluginEnable),
		msg.Text(msg.HintCodexHookTrust),
		msg.Text(msg.HintCodexTrust, `C:\\Dev\\shop`),
	}
	wantText(t, "warnings", func(p *page) { setupText(p, all, x) }, lines(warnings...), "")

	for _, tt := range []struct {
		state string
		line  msg.Key
	}{{codex.HookFailed, msg.SetupCodexHooksUnread}, {codex.HookUnwritten, msg.SetupCodexHooksUnwritten}} {
		out := codexSetup(codex.Unchanged)
		out.HookTrust = ptr(tt.state)
		wantText(t, "hooks "+tt.state, func(p *page) { setupText(p, out, setupExtra{}) }, lines(
			msg.Text(msg.SetupUnchanged, "Codex"),
			"",
			msg.Text(tt.line),
			msg.Text(msg.SetupCodexHooksFile, "/home/dev/.codex/hooks.json"),
			"",
			HintFor(msg.HintCodexHooksFix, "tool", "codex"),
		), lines(
			msg.Text(msg.SetupUnchanged, "Codex"),
			"",
			msg.Text(tt.line),
			msg.Text(msg.SetupCodexHooksFile, "/home/dev/.codex/hooks.json"),
		))
	}
}

// TestSettingsInvalidText checks the refusal of settings of Codex that
// cannot be read or written: the settings and the hint to repeat setup
// codex, or gentry update.
func TestSettingsInvalidText(t *testing.T) {
	f := settingsInvalid(codex.Tool, "/home/dev/.codex/config.toml", codex.Unreadable)
	wantFail(t, "unreadable", f, lines(
		msg.Text(msg.ErrCodexSettingsUnread, "/home/dev/.codex/config.toml"),
		"",
		HintFor(msg.HintCodexSettingsFix, "tool", "codex"),
	), lines(msg.Text(msg.ErrCodexSettingsUnread, "/home/dev/.codex/config.toml")))
	if f.code != contract.CodeSettingsInvalid || f.details["reason"] != codex.Unreadable || f.details["tool"] != codex.Tool {
		t.Errorf("failure: %+v", f)
	}
	if f := settingsInvalid(codex.Tool, "/c", codex.Unwritable); f.message != msg.Text(msg.ErrCodexSettingsUnwrit, "/c") {
		t.Errorf("unwritable: %q", f.message)
	}
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
	unread := codex.Unreadable
	out.CodexTrust = &contract.CodexTrust{Trusted: []string{}, Distrusted: []string{}, Error: &unread}
	wantText(t, "settings not read", func(p *page) { worktreeAddText(p, out, nil) }, lines(
		msg.Text(msg.WorktreeAdded, "shop", shopFix),
		"",
		msg.Text(msg.CodexTrustUnread),
		"",
		HintFor(msg.HintCodexSettingsFix, "tool", "codex"),
	), lines(
		msg.Text(msg.WorktreeAdded, "shop", shopFix),
		"",
		msg.Text(msg.CodexTrustUnread),
	))
	out.CodexTrust = &contract.CodexTrust{Trusted: []string{shopFix}, Distrusted: []string{}}
	wantText(t, "trusted", func(p *page) { worktreeAddText(p, out, nil) }, lines(msg.Text(msg.WorktreeAdded, "shop", shopFix)), "")
}
