package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/msg"
)

// TestHelpText states the words of the help: the list of commands around its
// sections, the help of a command and of a group of commands. The other
// tests of the help check every command by the catalog.
func TestHelpText(t *testing.T) {
	_, list, _ := run("help")
	head := lines(
		"Gentry ведёт задачи агента по флоу проекта и хранит их состояние.",
		"",
		"Использование:",
		"  gentry <команда> [аргументы] [флаги]",
	)
	if !strings.HasPrefix(list, head) || !strings.HasSuffix(list, "\nПосмотреть описание команды: gentry <команда> --help\n") {
		t.Errorf("the list of commands:\n%s", list)
	}

	setup, _ := lookup("setup")
	want := lines(
		"Подключить Gentry к ИИ-инструменту: сформировать и зарегистрировать плагин.",
		"После обновления Gentry команду нужно повторить.",
		"",
		"Использование:",
		"  gentry setup <ИИ-инструмент> [--switch] [--json]",
		"",
		"Аргументы:",
		"  <ИИ-инструмент>   ИИ-инструмент для подключения.",
		"                    Допустимые значения: claude",
		"",
		"Флаги:",
		"  --switch          Переключить на папку данных этой команды плагин Gentry в Claude Code,",
		"                    подключённый из другой папки данных.",
		"  --json            Вывести результат в формате JSON.",
	)
	if got := describe(setup); got != want {
		t.Errorf("setup --help:\n%s\nwant:\n%s", got, want)
	}

	projectAdd, _ := lookup("project add")
	got := describe(projectAdd)
	for _, part := range []string{
		"  gentry project add [<идентификатор>] --knowledge <путь> [--prefix <префикс>] [--json]\n",
		"  --prefix <префикс>   Префикс номеров задач: 2–10 заглавных латинских букв.\n" +
			"                       По умолчанию — буквы идентификатора.\n",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("project add --help has no\n%s\noutput:\n%s", part, got)
		}
	}

	record, _ := lookup("operator record")
	got = describe(record)
	if !strings.Contains(got, "Поля одним объектом JSON: question, options, answer, allow_return.\n") ||
		!strings.Contains(got, "Варианты в options — объекты с полями:\n") {
		t.Errorf("operator record --help:\n%s", got)
	}

	flowGroup, _ := topLevel("flow")
	if got, want := groupHelp(flowGroup), lines(
		"Показать или изменить флоу проекта.",
		"Изменения в папке флоу — черновик; действующим он становится после применения.",
		"",
		"Использование:",
		"  gentry flow <действие> [аргументы] [флаги]",
		"",
		"Действия:",
		"  show      Показать флоу проекта",
		"  diff      Показать изменения черновика",
		"  apply     Применить черновик флоу",
		"  discard   Отменить изменения флоу",
		"",
		"Посмотреть описание действия: gentry flow <действие> --help",
	); got != want {
		t.Errorf("flow --help:\n%s\nwant:\n%s", got, want)
	}
	flowShow, _ := lookup("flow show")
	if usage := "  gentry flow show [--scenario <сценарий> | --stage <этап> | --agent <субагент> | --part <фрагмент>] [--draft] [--project <идентификатор>] [--json]\n"; !strings.Contains(describe(flowShow), usage) {
		t.Errorf("flow show --help:\n%s\nwant within:\n%s", describe(flowShow), usage)
	}
}

// TestArgText states the placeholders of the arguments and the values of
// flags: the other tests take them from the catalog, the commands of hints
// too.
func TestArgText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		want string
	}{
		{msg.ArgTool, "<ИИ-инструмент>"},
		{msg.ArgNumber, "<номер>"},
		{msg.ArgProjectID, "<идентификатор>"},
		{msg.ArgPath, "<путь>"},
		{msg.ArgPrefix, "<префикс>"},
		{msg.ArgScenario, "<сценарий>"},
		{msg.ArgStage, "<этап>"},
		{msg.ArgAgent, "<субагент>"},
		{msg.ArgPart, "<фрагмент>"},
		{msg.ArgRemote, "<адрес>"},
		{msg.ArgURL, "<адрес>"},
		{msg.ArgText, "<текст>"},
		{msg.ArgKind, "<вид>"},
		{msg.ArgReason, "<обоснование>"},
		{msg.ArgStep, "<шаг>"},
		{msg.ArgAnswer, "<ответ>"},
		{msg.ArgAttempt, "<попытка>"},
	} {
		if got := msg.Text(c.k); got != c.want {
			t.Errorf("%s: %q, want %q", c.k, got, c.want)
		}
	}
}

func TestSetupText(t *testing.T) {
	settings := "/home/dev/.claude/settings.json"
	previous := "/home/dev/.gentry-old/integrations/claude"
	setup := func(action, permission string, enabled bool) contract.SetupOutput {
		out := contract.SetupOutput{Tool: claude.Tool, Dir: "/home/dev/.gentry/integrations/claude", PluginVersion: "1",
			Action: action, Enabled: enabled, Permission: &permission}
		if permission != claude.PermissionPresent {
			out.Settings = &settings
		}
		return out
	}
	switched := setup("switched", claude.PermissionPresent, true)
	switched.PreviousDir = &previous
	disabled := switched
	disabled.Enabled = false
	tests := []struct {
		name       string
		out        contract.SetupOutput
		cli, agent string
	}{
		{"installed", setup(claude.Installed, claude.PermissionAdded, true), lines(
			"Gentry подключён к Claude Code.",
			"Плагин начнёт работать со следующей сессии.",
			"Инструменты Gentry разрешены в настройках Claude Code.",
			"Настройки: /home/dev/.claude/settings.json",
		), ""},
		{"updated", setup(claude.Updated, claude.PermissionPresent, true), lines(
			"Плагин Gentry в Claude Code обновлён.",
			"Изменения вступят в силу со следующей сессии.",
		), ""},
		{"unchanged", setup(claude.Unchanged, claude.PermissionPresent, true), "Gentry уже подключён к Claude Code.\n", ""},
		{"settings not read", setup(claude.Unchanged, claude.PermissionFailed, true), lines(
			msg.Text(msg.SetupUnchanged),
			"Разрешить инструменты Gentry не удалось: настройки Claude Code не прочитаны.",
			msg.Text(msg.SetupSettings, settings),
			"",
			msg.Text(msg.HintSetupPermission, claude.PermissionRule),
		), ""},
		{"settings not written", setup(claude.Unchanged, claude.PermissionUnwritten, true), lines(
			msg.Text(msg.SetupUnchanged),
			"Разрешить инструменты Gentry не удалось: настройки Claude Code не записаны.",
			msg.Text(msg.SetupSettings, settings),
			"",
			msg.Text(msg.HintSetupPermission, claude.PermissionRule),
		), ""},
		{"switched", switched, lines(
			"Плагин Gentry в Claude Code переключён на папку данных этой команды.",
			"Изменения вступят в силу со следующей сессии.",
			"Прежняя папка плагина: /home/dev/.gentry-old/integrations/claude",
		), ""},
		{"disabled", disabled, lines(
			msg.Text(msg.SetupSwitched, previous),
			"",
			"Плагин Gentry отключён в Claude Code.",
			"",
			msg.Text(msg.HintPluginEnable),
		), ""},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { setupText(p, tt.out) }, tt.cli, tt.agent)
	}
}

// TestSetupRefusalText checks the refusals of the argument of setup.
func TestSetupRefusalText(t *testing.T) {
	wantRefusal(t, "no tool", "setup", nil, lines(
		"Не указан ИИ-инструмент.",
		"",
		hintLineOf(msg.HintCommandHelp, "gentry setup --help"),
	), "Не указан ИИ-инструмент.\n")
	wantRefusal(t, "unknown tool", "setup", []string{"foo"}, lines(
		"Неизвестный ИИ-инструмент «foo».",
		"",
		hintLineOf(msg.HintCommandHelp, "gentry setup --help"),
	), "Неизвестный ИИ-инструмент «foo».\n")
}

// TestHookRefusalText checks the refusals of the event of hook.
func TestHookRefusalText(t *testing.T) {
	events := msg.Text(msg.HintHookEvents, "session-start")
	wantRefusal(t, "no event", "hook", nil, lines("Не указано событие хука.", "", events), "")
	wantRefusal(t, "unknown event", "hook", []string{"foo"}, lines("Неизвестное событие хука «foo».", "", events), "")
}
