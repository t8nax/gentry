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
			hintLineOf(msg.HintAttempt, "gentry task attempts "+msg.Text(msg.ArgAttempt)), hintLineOf(msg.HintAttempt, "task_attempts (attempt)")},
		{hintOf(msg.HintAttempt).forTask("SHOP-1"),
			hintLineOf(msg.HintAttempt, "gentry task attempts SHOP-1 "+msg.Text(msg.ArgAttempt)), hintLineOf(msg.HintAttempt, "task_attempts (task: SHOP-1, attempt)")},
		{hintOf(msg.HintStepDrop), "", hintLineOf(msg.HintStepDrop, "step_drop (step, reason)")},
		{hintOf(msg.HintAllowReturn).set("allow_return", "plan"),
			hintLineOf(msg.HintAllowReturn, "gentry operator record --answer "+msg.Text(msg.ArgAnswer)+" --allow-return plan"),
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
	want := []string{hintLineOf(msg.HintStageExit, "gentry stage exit --kind "+msg.Text(msg.ArgKind)+" --text "+msg.Text(msg.ArgText)), hintLineOf(msg.HintCommandHelp, "gentry task close --help")}
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

// TestHintText states the words of the hints: the other tests take them from
// the catalog, so a change of the words changes this test alone.
func TestHintText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		args []any
		want string
	}{
		{msg.HintUnknownCommand, nil, "Посмотреть перечень команд"},
		{msg.HintPluginEnable, nil, "Включить плагин: claude plugin enable gentry@gentry"},
		{msg.HintPluginElsewhere, nil, "Переключить плагин всех сессий Claude Code на папку данных этой команды"},
		{msg.HintStateNewer, nil, "Обновите Gentry."},
		{msg.HintStateNewerAgent, nil, "Сообщить оператору и попросить перезапустить сессию."},
		{msg.HintActions, nil, "Посмотреть перечень действий"},
		{msg.HintProjectAdd, nil, "Подключить проект"},
		{msg.HintNotGitRepo, nil, "Выполните команду в рабочей копии кода проекта."},
		{msg.HintProjectUnpooled, nil, "Внести копию в пул"},
		{msg.HintProjectClone, nil, "Внести эту копию в пул"},
		{msg.HintProjectUndetermined, nil, "Укажите --project или выполните команду в рабочей копии проекта."},
		{msg.HintProjectNotFound, nil, "Посмотреть перечень проектов"},
		{msg.HintEventsAfter, nil, "Укажите номер события — целое число не меньше 0."},
		{msg.HintKnowledgeFlag, nil, "Укажите папку знания: --knowledge <путь>"},
		{msg.HintProjectIDMissing, nil, "Укажите идентификатор"},
		{msg.HintProjectIDInvalid, nil, "Укажите 2–32 строчные латинские буквы, цифры и дефисы, первая — буква."},
		{msg.HintPrefixInvalid, nil, "Укажите от 2 до 10 заглавных латинских букв."},
		{msg.HintFlowShowDraft, nil, "Посмотреть черновик"},
		{msg.HintFlowStage, nil, "Посмотреть этап подробно"},
		{msg.HintFlowApply, nil, "Применить черновик"},
		{msg.HintFlowObjects, nil, "Посмотреть перечень объектов"},
		{msg.HintDraftObjects, nil, "Посмотреть перечень объектов"},
		{msg.HintCommandHelp, nil, "Посмотреть описание команды"},
		{msg.HintLibraryApply, nil, "Применить изменения"},
		{msg.HintProcessRemote, nil, "Подключить удалённый репозиторий"},
		{msg.HintProcessSync, nil, "Синхронизировать"},
		{msg.HintFlowDiffProject, nil, "Посмотреть отличия"},
		{msg.HintLibraryDiff, nil, "Посмотреть отличия"},
		{msg.HintTaskShow, nil, "Посмотреть задачу"},
		{msg.HintWorktreeAdd, nil, "Внести копию в пул"},
		{msg.HintWorktreeDirty, nil, "Закоммитьте или отмените изменения и повторите команду."},
		{msg.HintFlowScenarios, nil, "Посмотреть сценарии"},
		{msg.HintTitle, nil, "Укажите название одной строкой не длиннее 80 знаков."},
		{msg.HintTaskListAll, nil, "Посмотреть все задачи"},
		{msg.HintTaskList, nil, "Посмотреть задачи"},
		{msg.HintTaskKey, nil, "Укажите номер с префиксом проекта: <префикс>-<число>."},
		{msg.HintStageExit, nil, "Закрыть этап"},
		{msg.HintStageShow, nil, "Посмотреть этап"},
		{msg.HintStatement, nil, "Посмотреть постановку"},
		{msg.HintNotes, nil, "Посмотреть заметки"},
		{msg.HintStageTransitions, nil, "Посмотреть переходы"},
		{msg.HintOtherTransitions, nil, "Посмотреть другие переходы"},
		{msg.HintStepAdd, nil, "Добавить шаги"},
		{msg.HintStepDone, nil, "Отметить шаг выполненным"},
		{msg.HintStepDrop, nil, "Снять шаг"},
		{msg.HintSteps, nil, "Посмотреть шаги"},
		{msg.HintStep, nil, "Укажите шаг одной строкой не длиннее 120 знаков."},
		{msg.HintArtifactSave, nil, "Сохранить артефакт"},
		{msg.HintArtifactName, nil, "Укажите имя из латинских букв, цифр, точки, дефиса и подчёркивания."},
		{msg.HintArtifactURL, nil, "Укажите адрес с http:// или https:// в начале."},
		{msg.HintArtifactFile, nil, "Проверьте путь к файлу и повторите команду."},
		{msg.HintArtifactLink, nil, "Сохранить ссылку вместо файла"},
		{msg.HintStatementDecisions, nil, "Посмотреть постановку и решения оператора"},
		{msg.HintAllowReturn, nil, "Записать разрешение оператора"},
		{msg.HintOptionsTooFew, nil, "Укажите не меньше двух вариантов или ни одного."},
		{msg.HintOptionsRecommended, nil, "Отметьте рекомендованным не больше одного варианта."},
		{msg.HintOptionLabel, nil, "Укажите название варианта одной строкой не длиннее 120 знаков."},
		{msg.HintTaskClose, nil, "Закрыть задачу"},
		{msg.HintTaskAgain, nil, "Взять задачу заново"},
		{msg.HintAttempts, nil, "Посмотреть прежние попытки"},
		{msg.HintAttemptsList, nil, "Посмотреть попытки"},
		{msg.HintAttempt, nil, "Посмотреть попытку"},
		{msg.HintWorktreeListProject, nil, "Посмотреть рабочие копии проекта"},
		{msg.HintIntroPluginStale, nil, "Сообщить оператору и по его слову обновить плагин"},
		{msg.HintDiffLibraryApply, nil, "Применить изменения библиотеки"},
		{msg.HintAgentsConflict, nil, "Необходимо переименовать субагента во флоу или удалить файл из рабочей копии, затем повторить команду."},
		{msg.HintAgentsConflictWarning, nil, "Разложить субагентов после разбора файлов"},
		{msg.HintAgentsSyncFailed, nil, "Повторить раскладку"},
		{msg.HintHookEvents, []any{"session-start"}, "Допустимые значения: session-start"},
		{msg.HintSetupPermission, []any{"mcp__plugin_gentry_gentry"}, "Разрешить инструменты вручную: правило mcp__plugin_gentry_gentry в permissions.allow"},
		{msg.HintToolNotFound, []any{"Claude Code"}, "Установите Claude Code и повторите"},
		{msg.HintRemoteAccess, []any{"git@example.com:shop/process.git"}, "Проверить доступ: git ls-remote git@example.com:shop/process.git"},
		{msg.HintIntroTake, []any{"/gentry:take"}, "Взять задачу, когда оператор её поставит: скилл /gentry:take"},
		{msg.HintIntroContinue, []any{"/gentry:continue"}, "Продолжить задачу по слову оператора: скилл /gentry:continue"},
	} {
		if got := msg.Text(c.k, c.args...); got != c.want {
			t.Errorf("%s: %q, want %q", c.k, got, c.want)
		}
	}
}
