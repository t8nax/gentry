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

	record, _ := lookup("task decision add")
	got = describe(record)
	if !strings.Contains(got, "Поля одним объектом JSON: question, options, answer, allow_return.\n") ||
		!strings.Contains(got, "Варианты в options — объекты с полями:\n") {
		t.Errorf("task decision add --help:\n%s", got)
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
	got = describe(flowShow)
	for _, part := range []string{
		lines(
			"Показать сценарии, этапы и субагентов действующего флоу проекта.",
			"С флагом объекта выводится этот объект подробно.",
			"Если во флоу есть ошибки, выводится их перечень.",
		),
		"  gentry flow show [--scenario <сценарий> | --stage <этап> | --agent <субагент> | --part <фрагмент>] [--draft] [--project <идентификатор>] [--json]\n",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("flow show --help:\n%s\nwant within:\n%s", got, part)
		}
	}
}

// part returns the group or the action of a group named by the words of its
// name, such as task, flow.
func part(t *testing.T, words ...string) command {
	t.Helper()
	c, ok := topLevel(words[0])
	for _, w := range words[1:] {
		if !ok {
			break
		}
		c, ok = c.action(w)
	}
	if !ok {
		t.Fatalf("no command %v", words)
	}
	return c
}

// TestSchemeHelpText states the help of the groups, parts and actions the
// scheme gentry <object> [<part>] <verb> brings: the list of commands, the
// group task with its parts, the parts and their actions.
func TestSchemeHelpText(t *testing.T) {
	for _, tt := range []struct {
		words []string
		want  string
	}{
		{[]string{"task"}, lines(
			"Взять задачу в работу или отменить её, показать задачу или перечень задач и работать с частями задачи.",
			"",
			"Использование:",
			"  gentry task <действие> [аргументы] [флаги]",
			"  gentry task <часть> <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  take       Взять задачу в работу",
			"  show       Показать задачу",
			"  list       Показать задачи",
			"  cancel     Отменить задачу",
			"",
			"Части задачи:",
			"  flow       Показать флоу задачи",
			"  stage      Показать этап задачи",
			"  note       Добавить или показать заметки задачи",
			"  decision   Записать решение оператора",
			"  attempt    Показать попытки задачи",
			"",
			"Посмотреть описание действия: gentry task <действие> --help",
			"Посмотреть действия части: gentry task <часть> --help",
		)},
		{[]string{"task", "flow"}, lines(
			"Показать флоу, по которому идёт задача.",
			"",
			"Использование:",
			"  gentry task flow <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  show   Показать флоу задачи",
			"",
			"Посмотреть описание действия: gentry task flow <действие> --help",
		)},
		{[]string{"task", "decision"}, lines(
			"Записать решение оператора по задаче.",
			"",
			"Использование:",
			"  gentry task decision <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  add   Записать решение оператора",
			"",
			"Посмотреть описание действия: gentry task decision <действие> --help",
		)},
		{[]string{"task", "attempt"}, lines(
			"Показать попытки задачи.",
			"Каждое взятие отменённой задачи заново начинает следующую попытку.",
			"",
			"Использование:",
			"  gentry task attempt <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  list   Показать попытки задачи",
			"  show   Показать одну попытку целиком",
			"",
			"Посмотреть описание действия: gentry task attempt <действие> --help",
		)},
		{[]string{"task", "stage"}, lines(
			"Показать текущий этап задачи.",
			"",
			"Использование:",
			"  gentry task stage <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  show   Показать текущий этап задачи",
			"",
			"Посмотреть описание действия: gentry task stage <действие> --help",
		)},
		{[]string{"process", "remote"}, lines(
			"Подключить удалённый репозиторий процесса.",
			"",
			"Использование:",
			"  gentry process remote <действие> [аргументы] [флаги]",
			"",
			"Действия:",
			"  set   Подключить удалённый репозиторий процесса или заменить его адрес",
			"",
			"Посмотреть описание действия: gentry process remote <действие> --help",
		)},
	} {
		if got := groupHelp(part(t, tt.words...)); got != tt.want {
			t.Errorf("%s --help:\n%s\nwant:\n%s", strings.Join(tt.words, " "), got, tt.want)
		}
	}

	for _, tt := range []struct {
		words []string
		parts []string
	}{
		{[]string{"process"}, []string{lines(
			"Синхронизировать процесс оператора между машинами.",
			"Процесс — флоу проектов и общие субагенты; он хранится в git и передаётся через удалённый репозиторий.",
		), lines(
			"Действия:",
			"  remote   Подключить удалённый репозиторий процесса",
			"  sync     Синхронизировать процесс",
			"  show     Показать состояние процесса",
		)}},
		{[]string{"agents"}, []string{lines(
			"Действия:",
			"  deploy   Разложить субагентов флоу во все рабочие копии пула проекта",
		)}},
	} {
		got := groupHelp(part(t, tt.words...))
		for _, p := range tt.parts {
			if !strings.Contains(got, p) {
				t.Errorf("%s --help:\n%s\nwant within:\n%s", strings.Join(tt.words, " "), got, p)
			}
		}
	}

	for _, tt := range []struct {
		words []string
		parts []string
	}{
		{[]string{"task", "take"}, []string{
			"\nОтменённую задачу можно взять заново: укажите её номер аргументом.\n",
			"  gentry task take [<задача>] [--scenario <сценарий>] [--title <название>]",
			"  <задача>                Отменённая задача, которую нужно взять заново.\n" +
				"                          Название и постановка берутся из неё.\n",
			"  --input <файл>          Поля задачи одним объектом JSON: scenario, title, statement, worktree.\n" +
				"                          Вместо файла можно указать «-» — стандартный ввод. Вместе с флагами полей не указывается.\n",
		}},
		{[]string{"flow", "diff"}, []string{lines(
			"Показать, чем черновик флоу проекта и общих субагентов отличается от действующего.",
			"Подробности со схемами сценариев записываются в файл изменений.",
			"",
		)}},
		{[]string{"flow", "apply"}, []string{lines(
			"Проверить черновик флоу проекта и общих субагентов и сделать его действующим на всех машинах.",
			"Черновик с ошибками не применяется.",
			"Изменения общих субагентов касаются всех проектов, которые их называют.",
			"",
		)}},
		{[]string{"flow", "discard"}, []string{lines(
			"Вернуть папку флоу проекта и папку общих субагентов к действующему виду.",
			"Изменения черновика теряются.",
			"",
		)}},
		{[]string{"task", "cancel"}, []string{
			"\nОтменённую задачу можно взять заново: gentry task take <задача> --scenario <сценарий>\n",
		}},
		{[]string{"task", "flow", "show"}, []string{
			lines(
				"Показать сценарии, этапы и субагентов флоу, по которому идёт задача.",
				"Задача идёт по флоу, сохранённому при её взятии, даже если действующий флоу проекта изменён.",
				"С флагом объекта выводится этот объект подробно.",
			),
			"  gentry task flow show [--scenario <сценарий> | --stage <этап> | --agent <субагент> | --part <фрагмент>] [--task <задача>] [--json]\n",
		}},
		{[]string{"task", "attempt", "show"}, []string{
			"  gentry task attempt show <попытка> [--task <задача>] [--json]\n",
			"  <попытка>         Номер попытки.\n",
		}},
		{[]string{"task", "attempt", "list"}, []string{
			"Показать попытки задачи: состояние, сценарий, время взятия и завершения, причину отмены.\n",
		}},
		{[]string{"task", "attempt", "show"}, []string{
			"Показать одну попытку задачи целиком: состояние, пройденный путь с шагами, артефакты, заметки и решения оператора.\n",
		}},
		{[]string{"process", "remote", "set"}, []string{lines(
			"Подключить удалённый репозиторий процесса или заменить его адрес.",
			"Репозиторий нужно создать заранее и сделать закрытым.",
			"Если процесс есть и на машине, и в репозитории, они сводятся, а отличия этой машины сохраняются как черновики.",
			"",
			"Использование:",
			"  gentry process remote set <адрес> [--json]",
		)}},
		{[]string{"task", "decision", "add"}, []string{lines(
			"Записать решение оператора по задаче: ответ на вопрос или указание без вопроса.",
			"Слова оператора указываются дословно.",
			"Варианты ответа передаются через --input.",
			"Решения оператора не изменяются и не удаляются; чтобы отменить решение, запишите новое.",
		)}},
	} {
		got := describe(part(t, tt.words...))
		for _, p := range tt.parts {
			if !strings.Contains(got, p) {
				t.Errorf("%s --help:\n%s\nwant within:\n%s", strings.Join(tt.words, " "), got, p)
			}
		}
	}
}

// TestArgText states the placeholders of the arguments and the values of
// flags that TestHelpText does not: the other tests take them from the
// catalog, the commands of hints too.
func TestArgText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		want string
	}{
		{msg.ArgNumber, "<номер>"},
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
		wantText(t, tt.name, func(p *page) { setupText(p, tt.out, setupExtra{}) }, tt.cli, tt.agent)
	}

	// With a project connected, an updated or switched plugin asks to lay
	// out the subagents anew, last; a plugin installed or unchanged does not.
	deploy := "Переложить субагентов в рабочие копии каждого проекта: gentry agents deploy --project <идентификатор>"
	deployTool := hintLineOf(msg.HintAgentsDeployAll, "agents_deploy (project)")
	withProjects := []struct {
		name       string
		out        contract.SetupOutput
		cli, agent string
	}{
		{"updated with projects", setup(claude.Updated, claude.PermissionPresent, true), lines(
			msg.Text(msg.SetupUpdated),
			"",
			deploy,
		), lines(
			msg.Text(msg.SetupUpdated),
			"",
			deployTool,
		)},
		{"switched and disabled with projects", disabled, lines(
			msg.Text(msg.SetupSwitched, previous),
			"",
			msg.Text(msg.SetupDisabled),
			"",
			msg.Text(msg.HintPluginEnable),
			deploy,
		), lines(
			msg.Text(msg.SetupSwitched, previous),
			"",
			msg.Text(msg.SetupDisabled),
			"",
			msg.Text(msg.HintPluginEnable),
			deployTool,
		)},
		{"installed with projects", setup(claude.Installed, claude.PermissionPresent, true), msg.Text(msg.SetupInstalled) + "\n", ""},
		{"unchanged with projects", setup(claude.Unchanged, claude.PermissionPresent, true), msg.Text(msg.SetupUnchanged) + "\n", ""},
	}
	for _, tt := range withProjects {
		wantText(t, tt.name, func(p *page) { setupText(p, tt.out, setupExtra{projects: true}) }, tt.cli, tt.agent)
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
