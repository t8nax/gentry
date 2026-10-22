package cli

import (
	"errors"
	"fmt"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/msg"
)

// codexTrust trusts in Codex the worktrees of a command that adds worktrees
// to a pool, if Codex is connected: those without a record of trust that
// applies to them are trusted, those the operator did not trust are told.
// It returns nil without Codex, and the keys of the records the operator
// did not trust, which the hints name. Settings of Codex that cannot be read
// or written are left as they are, and told.
func codexTrust(worktrees []string) (*contract.CodexTrust, []string) {
	if !connected(codex.Tool) {
		return nil, nil
	}
	out := &contract.CodexTrust{Trusted: []string{}, Distrusted: []string{}}
	home, err := codex.Home()
	if err != nil {
		reason := codex.Unreadable
		out.Error = &reason
		return out, nil
	}
	res, err := codex.TrustWorktrees(codex.ConfigPath(home), worktrees)
	var se *codex.SettingsError
	switch {
	case errors.As(err, &se):
		out.Error = &se.Reason
		return out, nil
	case err != nil:
		reason := codex.Unwritable
		out.Error = &reason
		return out, nil
	}
	out.Trusted = append(out.Trusted, res.Trusted...)
	for _, d := range res.Distrusted {
		out.Distrusted = append(out.Distrusted, d.Path)
	}
	return out, distrustKeys(res.Distrusted)
}

// codexTrustText prints the trust in Codex of the worktrees of a command, as
// blocks each preceded by an empty line: that the settings of Codex could
// not be used, then the worktrees the operator did not trust. It returns
// the hints: to fix the settings and repeat setup codex, to trust the
// worktrees of each record. What the command trusted is told by --json
// only.
func codexTrustText(p *page, t *contract.CodexTrust, keys []string) []hint {
	if t == nil {
		return nil
	}
	var hints []hint
	if t.Error != nil {
		k := msg.CodexTrustUnread
		if *t.Error == codex.Unwritable {
			k = msg.CodexTrustUnwritten
		}
		fmt.Fprintf(p, "\n%s\n", msg.Text(k))
		hints = append(hints, hintOf(msg.HintCodexSettingsFix).set("tool", codex.Tool))
	}
	if len(t.Distrusted) > 0 {
		p.WriteString("\n")
		writeList(&p.Builder, msg.Text(msg.CodexDistrusted), t.Distrusted)
		for _, k := range keys {
			hints = append(hints, hintOf(msg.HintCodexTrust, k))
		}
	}
	return hints
}
