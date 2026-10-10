package flow

import (
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// TestProblemText states the words of the problems of a flow and of the
// objects they name: the other tests take them from the catalog, so a change
// of the words of a problem changes this test alone.
func TestProblemText(t *testing.T) {
	for _, c := range []struct {
		k    msg.Key
		args []any
		want string
	}{
		{msg.FlowObjAgent, []any{"auditor"}, "Субагент auditor"},
		{msg.FlowObjCommon, []any{}, "Общие правила флоу"},
		{msg.FlowObjLibraryAgent, []any{"reviewer"}, "Субагент reviewer из библиотеки"},
		{msg.FlowObjNode, []any{"bug", "review"}, "Сценарий bug, узел review"},
		{msg.FlowObjPart, []any{"plan-format"}, "Фрагмент plan-format"},
		{msg.FlowObjScenario, []any{"bug"}, "Сценарий bug"},
		{msg.FlowObjStage, []any{"review"}, "Этап review"},
		{msg.ProblemCapabilities, []any{"Субагент auditor", "read, search, edit, run, web, task, progress"}, "Субагент auditor: значение поля «capabilities» должно быть списком из read, search, edit, run, web, task, progress."},
		{msg.ProblemDeadEnd, []any{"Сценарий bug", "review"}, "Сценарий bug: из узла review нельзя дойти до конца сценария."},
		{msg.ProblemDuplicateTransition, []any{"Сценарий bug, узел review", "merge"}, "Сценарий bug, узел review: два перехода к узлу merge."},
		{msg.ProblemEncoding, []any{"Этап review"}, "Этап review: текст не в кодировке UTF-8."},
		{msg.ProblemExecutorInDraftLibrary, []any{"Этап review", "tester"}, "Этап review: субагент tester есть только в черновике библиотеки."},
		{msg.ProblemExtraDir, []any{"scenarios/old"}, "Папка не относится к флоу: scenarios/old"},
		{msg.ProblemExtraFile, []any{"README.md"}, "Файл не относится к флоу: README.md"},
		{msg.ProblemInclude, []any{"Этап plan-bug"}, "Этап plan-bug: значение поля «include» должно быть списком идентификаторов."},
		{msg.ProblemInvalidID, []any{"Субагент Auditor_2"}, "Субагент Auditor_2: недопустимый идентификатор; допустимы до 64 строчных латинских букв, цифр и дефисов, первая — буква."},
		{msg.ProblemLimitOutsideLoop, []any{"Сценарий bug", "plan", "review"}, "Сценарий bug: у перехода plan → review указан предел возвратов, но переход не замыкает цикл."},
		{msg.ProblemLimitWithoutCondition, []any{"Сценарий bug", "review", "review"}, "Сценарий bug: у перехода review → review с пределом возвратов нет условия."},
		{msg.ProblemMaxRounds, []any{"Сценарий bug, узел plan"}, "Сценарий bug, узел plan: значение поля «max_rounds» должно быть целым числом больше 0."},
		{msg.ProblemMissingField, []any{"Субагент auditor", "purpose"}, "Субагент auditor: не заполнено поле «purpose»."},
		{msg.ProblemMissingInstruction, []any{"Субагент auditor"}, "Субагент auditor: нет инструкции."},
		{msg.ProblemNext, []any{"Сценарий bug, узел branch"}, "Сценарий bug, узел branch: значение поля «next» должно быть идентификатором узла или списком переходов."},
		{msg.ProblemNoScenarios, []any{}, "Во флоу нет ни одного сценария."},
		{msg.ProblemNodes, []any{"Сценарий bug"}, "Сценарий bug: значение поля «nodes» должно быть набором узлов."},
		{msg.ProblemNotMapping, []any{"Сценарий bug, узел review"}, "Сценарий bug, узел review: описание должно состоять из полей."},
		{msg.ProblemNotString, []any{"Этап review", "title"}, "Этап review: значение поля «title» должно быть строкой."},
		{msg.ProblemOrphanAgentInstruction, []any{"Субагент old"}, "Субагент old: есть инструкция, но нет полей субагента."},
		{msg.ProblemOrphanInstruction, []any{"Этап old"}, "Этап old: есть инструкция, но нет полей этапа."},
		{msg.ProblemReservedNode, []any{"Сценарий bug"}, "Сценарий bug: finish обозначает конец сценария и не может быть узлом."},
		{msg.ProblemSeveralDefaults, []any{"Сценарий bug, узел review"}, "Сценарий bug, узел review: переход без условия может быть только один."},
		{msg.ProblemSyntax, []any{"Этап plan-bug"}, "Этап plan-bug: ошибка синтаксиса YAML."},
		{msg.ProblemUnknownExecutor, []any{"Этап review", "reviewer"}, "Этап review: субагент reviewer не найден."},
		{msg.ProblemUnknownField, []any{"Субагент auditor", "model"}, "Субагент auditor: неизвестное поле «model»."},
		{msg.ProblemUnknownPart, []any{"Этап plan-bug", "plan-formt"}, "Этап plan-bug: фрагмент plan-formt не найден."},
		{msg.ProblemUnknownStage, []any{"Сценарий bug, узел plan", "plan-bugg"}, "Сценарий bug, узел plan: этап plan-bugg не найден."},
		{msg.ProblemUnknownStart, []any{"Сценарий bug", "brnch"}, "Сценарий bug: начальный узел brnch не найден."},
		{msg.ProblemUnknownTarget, []any{"Сценарий bug, узел review", "merj"}, "Сценарий bug, узел review: узел перехода merj не найден."},
		{msg.ProblemUnlimitedLoop, []any{"Сценарий bug", "implementation → review → implementation"}, "Сценарий bug: у цикла implementation → review → implementation нет предела возвратов."},
		{msg.ProblemUnreachable, []any{"Сценарий bug", "cleanup"}, "Сценарий bug: узел cleanup недостижим из начального узла."},
		{msg.ProblemUnsupportedField, []any{"Общие правила флоу", "on_take"}, "Общие правила флоу: поле «on_take» не поддерживается этой версией Gentry."},
	} {
		if got := msg.Text(c.k, c.args...); got != c.want {
			t.Errorf("%s: %q, want %q", c.k, got, c.want)
		}
	}
}
