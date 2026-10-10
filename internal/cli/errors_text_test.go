package cli

import (
	"errors"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
)

// TestUsageFailureText checks the refusals that any command may give: of its
// arguments, flags and actions, and of the state store.
func TestUsageFailureText(t *testing.T) {
	group, _ := topLevel("task")
	tests := []struct {
		name       string
		f          failure
		cli, agent string
	}{
		{"unknown command", unknownCommand("foo"), lines(
			"Неизвестная команда «foo».",
			"",
			hintLineOf(msg.HintUnknownCommand, "gentry --help"),
		), "Неизвестная команда «foo».\n"},
		{"unexpected arguments", unexpectedArgs("events"), "Команда events не принимает аргументов.\n", ""},
		{"extra arguments", extraArgs("hook", []string{"extra"}), "Лишние аргументы команды hook: extra\n", ""},
		{"unknown flag", unknownFlag("version", "--foo"), "Команда version не поддерживает флаг --foo.\n", ""},
		{"no value of a flag", flagValueMissing("--project"), "Не указано значение флага --project.\n", ""},
		{"invalid value of a flag", flagValueInvalid("--state", "done", msg.Text(msg.ErrFlagValueInvalid, "--state", "done"),
			helpHint(msg.HintCommandHelp, "task list")), lines(
			"Недопустимое значение флага --state: «done».",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task list --help"),
		), "Недопустимое значение флага --state: «done».\n"},
		{"conflicting flags", conflictingFlags("task list", []string{"--all", "--state"}), lines(
			"Флаги --all и --state нельзя указывать вместе.",
			"",
			hintLineOf(msg.HintCommandHelp, "gentry task list --help"),
		), "Флаги --all и --state нельзя указывать вместе.\n"},
		{"no action", missingAction(group), lines(
			"Не указано действие команды task.",
			"",
			hintLineOf(msg.HintActions, "gentry task --help"),
		), "Не указано действие команды task.\n"},
		{"unknown action", unknownAction(group, "start"), lines(
			"Неизвестное действие «start» команды task.",
			"",
			hintLineOf(msg.HintActions, "gentry task --help"),
		), "Неизвестное действие «start» команды task.\n"},
		{"internal", internal(errors.New("корзина")), "Внутренняя ошибка Gentry: корзина\n", ""},
		{"no tool", toolNotFound("claude", "claude", "Claude Code"), lines(
			"Программа claude не найдена.",
			"",
			msg.Text(msg.HintToolNotFound, "Claude Code")+": gentry setup claude",
		), lines(
			"Программа claude не найдена.",
			"",
			msg.Text(msg.HintToolNotFound, "Claude Code")+": setup (tool: claude)",
		)},
		{"plugin of another data folder", pluginElsewhere("claude", "/home/dev/.gentry-old/claude", "/home/dev/.gentry/claude"), lines(
			"Плагин Gentry в Claude Code подключён из другой папки данных.",
			"Папка плагина: /home/dev/.gentry-old/claude",
			"Папка плагина этой команды: /home/dev/.gentry/claude",
			"",
			hintLineOf(msg.HintPluginElsewhere, "gentry setup claude --switch"),
		), lines(
			"Плагин Gentry в Claude Code подключён из другой папки данных.",
			"Папка плагина: /home/dev/.gentry-old/claude",
			"Папка плагина этой команды: /home/dev/.gentry/claude",
			"",
			hintLineOf(msg.HintPluginElsewhere, "setup (tool: claude, switch)"),
		)},
		{"newer state store", stateFailure(&state.NewerError{Path: "/home/dev/.gentry/state/state.db", Schema: 9, Supported: 8}), lines(
			"Хранилище состояния создано более новой версией Gentry: схема 9, поддерживается до 8.",
			"",
			msg.Text(msg.HintStateNewer),
		), lines(
			"Хранилище состояния создано более новой версией Gentry: схема 9, поддерживается до 8.",
			"",
			msg.Text(msg.HintStateNewerAgent),
		)},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, tt.f, tt.cli, tt.agent)
	}
}
