package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// hintLineOf is a hint as a channel prints it: what to do from the catalog, then
// the command or the tool.
func hintLineOf(k msg.Key, command string) string { return msg.Text(k) + ": " + command }

// TestHintChannels checks a hint in the channel of the command line and of
// the agent: a placeholder, a value, the task by its argument or by --task,
// a hint of the help and a command only the agent runs.
func TestHintChannels(t *testing.T) {
	for _, c := range []struct {
		h          hint
		cli, agent string // empty if the channel has no such hint
	}{
		{hintOf(msg.HintStageExit), "", hintLineOf(msg.HintStageExit, "stage_exit (kind, text)")},
		{hintOf(msg.HintTaskShow).forTask("SHOP-1"),
			hintLineOf(msg.HintTaskShow, "gentry task show SHOP-1"), hintLineOf(msg.HintTaskShow, "task_show (task: SHOP-1)")},
		{hintOf(msg.HintStageShow).forTask("SHOP-1"),
			hintLineOf(msg.HintStageShow, "gentry stage show --task SHOP-1"), hintLineOf(msg.HintStageShow, "stage_show (task: SHOP-1)")},
		{hintOf(msg.HintStatement).forTask("SHOP-1"),
			hintLineOf(msg.HintStatement, "gentry task show SHOP-1 --statement"), hintLineOf(msg.HintStatement, "task_show (task: SHOP-1, statement)")},
		{hintOf(msg.HintAttempt),
			hintLineOf(msg.HintAttempt, "gentry task attempts <попытка>"), hintLineOf(msg.HintAttempt, "task_attempts (attempt)")},
		{hintOf(msg.HintAttempt).forTask("SHOP-1"),
			hintLineOf(msg.HintAttempt, "gentry task attempts SHOP-1 <попытка>"), hintLineOf(msg.HintAttempt, "task_attempts (task: SHOP-1, attempt)")},
		{hintOf(msg.HintStepDrop), "", hintLineOf(msg.HintStepDrop, "step_drop (step, reason)")},
		{hintOf(msg.HintAllowReturn).set("allow_return", "plan"),
			hintLineOf(msg.HintAllowReturn, "gentry operator record --answer <ответ> --allow-return plan"),
			hintLineOf(msg.HintAllowReturn, "operator_record (answer, allow_return: plan)")},
		{hintOf(msg.HintFlowShowDraft), hintLineOf(msg.HintFlowShowDraft, "gentry flow show --draft"), hintLineOf(msg.HintFlowShowDraft, "flow_show (draft)")},
		{hintOf(msg.HintTaskClose), "", hintLineOf(msg.HintTaskClose, "task_close")},
		{hintOf(msg.HintTaskClose).forTask("SHOP-1"), "", hintLineOf(msg.HintTaskClose, "task_close (task: SHOP-1)")},
		{helpHint(msg.HintCommandHelp, "step add"), hintLineOf(msg.HintCommandHelp, "gentry step add --help"), ""},
		{helpHint(msg.HintActions, "task"), hintLineOf(msg.HintActions, "gentry task --help"), ""},
		{helpHint(msg.HintUnknownCommand, ""), hintLineOf(msg.HintUnknownCommand, "gentry --help"), ""},
		{hintOf(msg.HintStateNewer), msg.Text(msg.HintStateNewer), msg.Text(msg.HintStateNewerAgent)},
		{hintOf(msg.HintTitle), msg.Text(msg.HintTitle), msg.Text(msg.HintTitle)},
	} {
		for _, ch := range []struct {
			channel channel
			want    string
		}{{cliChannel, c.cli}, {agentChannel, c.agent}} {
			got, ok := c.h.render(ch.channel)
			if ok != (ch.want != "") || got != ch.want {
				t.Errorf("%s in channel %d: %q %v, want %q", c.h.label, ch.channel, got, ok, ch.want)
			}
		}
	}
}

// TestHintJSON checks that JSON of a refusal has every hint as the operator
// types it, a command only the agent runs too.
func TestHintJSON(t *testing.T) {
	got := renderHints([]hint{hintOf(msg.HintStageExit), helpHint(msg.HintCommandHelp, "task close")}, jsonChannel)
	want := []string{hintLineOf(msg.HintStageExit, "gentry stage exit --kind <вид> --text <текст>"), hintLineOf(msg.HintCommandHelp, "gentry task close --help")}
	if !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

// TestHintWords checks that a change of the words of a hint in the catalog
// reaches both channels, and the agent still gets the tool.
func TestHintWords(t *testing.T) {
	defer msg.Override(msg.HintStageShow, "Прочитать текущий этап")()
	h := hintOf(msg.HintStageShow).forTask("SHOP-1")
	for ch, want := range map[channel]string{
		cliChannel:   "Прочитать текущий этап: gentry stage show --task SHOP-1",
		agentChannel: "Прочитать текущий этап: stage_show (task: SHOP-1)",
	} {
		if got, _ := h.render(ch); got != want {
			t.Errorf("channel %d: %q, want %q", ch, got, want)
		}
	}
}

// TestHintSpecs checks every hint that names a command: the command exists,
// and each field is a flag or an argument of it.
func TestHintSpecs(t *testing.T) {
	for k, h := range hintSpecs {
		c, ok := lookup(h.cmd)
		if !ok {
			t.Errorf("%s: no command %q", k, h.cmd)
			continue
		}
		for _, p := range h.params {
			found := slices.ContainsFunc(c.args, func(a argSpec) bool { return a.field == p.field })
			if p.flag {
				found = slices.ContainsFunc(c.flags, func(f flagSpec) bool { return fieldName(f.name) == p.field })
			}
			if !found {
				t.Errorf("%s: %s has no field %s", k, h.cmd, p.field)
			}
		}
		if text := msg.Text(k); strings.Contains(text, ":") {
			t.Errorf("%s: what to do names the command: %q", k, text)
		}
	}
}

// TestHintsOfCatalog checks every hint of the catalog: none names a command
// of gentry in its text, which the agent would get as it is. A command is
// data of hintSpecs.
func TestHintsOfCatalog(t *testing.T) {
	for _, k := range msg.Keys() {
		if strings.HasPrefix(string(k), "hint.") && strings.Contains(msg.Text(k), "gentry ") {
			t.Errorf("%s names a command: %q", k, msg.Text(k))
		}
	}
}

// TestHintsLast checks that a block of hints a channel has none of leaves no
// blank line behind.
func TestHintsLast(t *testing.T) {
	p := &page{ch: cliChannel}
	p.WriteString("Сценарий пройден.\n")
	p.hints(hintOf(msg.HintTaskClose))
	if got := p.String(); got != "Сценарий пройден.\n" {
		t.Errorf("%q", got)
	}
	p = &page{ch: cliChannel}
	p.WriteString("Готово.\n")
	p.hints(hintOf(msg.HintTaskClose), hintOf(msg.HintTaskShow))
	if got, want := p.String(), "Готово.\n\n"+hintLineOf(msg.HintTaskShow, "gentry task show")+"\n"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
