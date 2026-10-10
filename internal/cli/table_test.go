package cli

import (
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

func TestWriteTable(t *testing.T) {
	var b strings.Builder
	writeTable(&b, [][]string{
		{"ПРОЕКТ", "РАБОЧАЯ КОПИЯ", "ВЕТКА"},
		{"shop", `C:\Dev\shop`, "main"},
		{"cart", `C:\Dev\cart-long-name`, "—"},
	})
	want := "ПРОЕКТ  РАБОЧАЯ КОПИЯ          ВЕТКА\n" +
		`shop    C:\Dev\shop            main` + "\n" +
		`cart    C:\Dev\cart-long-name  —` + "\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteList(t *testing.T) {
	var b strings.Builder
	writeList(&b, "Рабочие копии:", []string{`C:\Dev\shop-2`, `C:\Dev\shop-fix`})
	if want := "Рабочие копии:\n  " + `C:\Dev\shop-2` + "\n  " + `C:\Dev\shop-fix` + "\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

// table returns rows printed as a table, the header row first.
func table(rows ...[]string) string {
	var b strings.Builder
	writeTable(&b, rows)
	return b.String()
}

// TestColumnText states the headers of the columns of the tables: the other
// tests take them from the catalog, so a change of a header changes this
// test alone.
func TestColumnText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		want string
	}{
		{msg.ColProject, "ПРОЕКТ"},
		{msg.ColPrefix, "ПРЕФИКС"},
		{msg.ColKnowledge, "ЗНАНИЕ"},
		{msg.ColMainWorktree, "ОСНОВНАЯ КОПИЯ"},
		{msg.ColWorktree, "РАБОЧАЯ КОПИЯ"},
		{msg.ColBranch, "ВЕТКА"},
		{msg.ColState, "СОСТОЯНИЕ"},
		{msg.ColScenario, "СЦЕНАРИЙ"},
		{msg.ColTitle, "НАЗВАНИЕ"},
		{msg.ColStage, "ЭТАП"},
		{msg.ColExecutor, "ИСПОЛНИТЕЛЬ"},
		{msg.ColExit, "ВЫХОД"},
		{msg.ColNode, "УЗЕЛ"},
		{msg.ColAgent, "СУБАГЕНТ"},
		{msg.ColSource, "ИСТОЧНИК"},
		{msg.ColStages, "ЭТАПЫ"},
		{msg.ColNumber, "НОМЕР"},
		{msg.ColTransition, "ПЕРЕХОД"},
		{msg.ColCondition, "УСЛОВИЕ"},
		{msg.ColReturns, "ВОЗВРАТЫ"},
		{msg.ColStepNumber, "№"},
		{msg.ColStep, "ШАГ"},
		{msg.ColComment, "ПОЯСНЕНИЕ"},
		{msg.ColRound, "КРУГ"},
		{msg.ColOutcome, "ИТОГ"},
		{msg.ColArtifact, "АРТЕФАКТ"},
		{msg.ColKind, "ВИД"},
		{msg.ColSaved, "СОХРАНЁН"},
		{msg.ColPlace, "МЕСТО"},
		{msg.ColAttempt, "ПОПЫТКА"},
		{msg.ColTaken, "ВЗЯТА"},
		{msg.ColEnded, "ЗАВЕРШЕНА"},
		{msg.ColCancelReason, "ОБОСНОВАНИЕ ОТМЕНЫ"},
		{msg.ColObject, "Объект"},
		{msg.ColChange, "Изменение"},
		{msg.ColField, "Поле"},
		{msg.ColWas, "Было"},
		{msg.ColNow, "Стало"},
		{msg.ColTask, "ЗАДАЧА"},
		{msg.ColAgents, "СУБАГЕНТЫ"},
		{msg.ColChanges, "ИЗМЕНЕНИЯ"},
		{msg.ColFile, "ФАЙЛ"},
		{msg.ColReason, "ПРИЧИНА"},
	} {
		if got := msg.Text(c.k); got != c.want {
			t.Errorf("%s: %q, want %q", c.k, got, c.want)
		}
	}
}

// noValue is the mark of a missing value, from the catalog.
var noValue = msg.Text(msg.ValueNone)

// TestValueText states the short values that the texts print in tables and
// lines «Поле: значение»: the states, the outcomes, the kinds and the sources,
// the round of a stage and the progress.
// The other tests take them from the catalog, so a change of a value changes
// this test alone.
func TestValueText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		args []any
		want string
	}{
		{msg.ValueNone, nil, "—"},
		{msg.FlowEnd, nil, "конец"},
		{msg.StageFinished, nil, "сценарий пройден"},
		{msg.TaskStateActive, nil, "в работе"},
		{msg.TaskStateClosed, nil, "закрыта"},
		{msg.TaskStateCancelled, nil, "отменена"},
		{msg.StepStateDone, nil, "выполнен"},
		{msg.StepStateDropped, nil, "снят"},
		{msg.OutcomeCurrent, nil, "идёт"},
		{msg.OutcomeSkip, nil, "пропуск"},
		{msg.ExitKindResult, nil, "результат"},
		{msg.ArtifactKindFile, nil, "файл"},
		{msg.ArtifactKindLink, nil, "ссылка"},
		{msg.RecordedByAgent, nil, "агентом"},
		{msg.TaskSourceOperator, nil, "оператором"},
		{msg.TaskSourceAgent, nil, "агентом со слов оператора"},
		{msg.FlowSourceProject, nil, "проект"},
		{msg.FlowSourceLibrary, nil, "библиотека"},
		{msg.WorktreeMain, nil, "основная"},
		{msg.WorktreeFree, nil, "свободна"},
		{msg.WorktreeMissing, nil, "папка не найдена"},
		{msg.FlowChangeAdded, nil, "добавлен"},
		{msg.FlowChangeModified, nil, "изменён"},
		{msg.FlowChangeRemoved, nil, "удалён"},
		{msg.SchemaOperator, nil, "оператор"},
		{msg.StageRound, []any{"Ветка (branch)", 1}, "Ветка (branch), круг 1"},
		{msg.ProgressValue, []any{0, 5}, "0 из 5"},
	} {
		if got := msg.Text(c.k, c.args...); got != c.want {
			t.Errorf("%s: %q, want %q", c.k, got, c.want)
		}
	}
}
