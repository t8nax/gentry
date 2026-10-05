package flow

import (
	"reflect"
	"testing"

	"github.com/t8nax/gentry/contract"
)

// bug returns the bug scenario with the given nodes, which start at line 4.
func bug(start, nodes string) map[string]string {
	return map[string]string{"scenarios/bug.yaml": "title: Баг\nstart: " + start + "\nnodes:\n" + nodes}
}

func TestGraphProblems(t *testing.T) {
	tests := []struct {
		name    string
		changes map[string]string
		want    []Problem
	}{
		{"unknown start", bug("brnch",
			"  branch: { stage: branch, next: finish }\n"), []Problem{
			{contract.ProblemUnknownNode, "scenarios/bug.yaml", 2, "Сценарий bug: начальный узел brnch не найден."},
		}},
		{"unknown target", bug("branch",
			"  branch: { stage: branch, next: review }\n"+
				"  review: { stage: review, next: merj }\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			// Without the misspelt node the rest is not checked: merge
			// would be unreachable and review a dead end.
			{contract.ProblemUnknownNode, "scenarios/bug.yaml", 5, "Сценарий bug, узел review: узел перехода merj не найден."},
		}},
		{"missing start and next", map[string]string{"scenarios/bug.yaml": "title: Баг\nnodes:\n  branch: { stage: branch }\n"}, []Problem{
			{contract.ProblemMissingField, "scenarios/bug.yaml", 0, "Сценарий bug: не заполнено поле «start»."},
			{contract.ProblemMissingField, "scenarios/bug.yaml", 3, "Сценарий bug, узел branch: не заполнено поле «next»."},
		}},
		{"invalid nodes", bug("branch",
			"  branch: { stage: branch, next: 5 }\n"+
				"  Merge_Node: { stage: merge, next: finish }\n"+
				"  finish: { stage: merge, next: finish }\n"+
				"  review: review\n"+
				"  plan:\n    stage: plan-bug\n    next:\n      - finish\n      - to: finish\n        if: x\n        max_rounds: 0\n        when: y\n"), []Problem{
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 4, "Сценарий bug, узел branch: значение поля «next» должно быть идентификатором узла или списком переходов."},
			{contract.ProblemInvalidID, "scenarios/bug.yaml", 5, "Сценарий bug, узел Merge_Node: недопустимый идентификатор; допустимы до 64 строчных латинских букв, цифр и дефисов, первая — буква."},
			{contract.ProblemReservedNode, "scenarios/bug.yaml", 6, "Сценарий bug: finish обозначает конец сценария и не может быть узлом."},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 7, "Сценарий bug, узел review: описание должно состоять из полей."},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 11, "Сценарий bug, узел plan: значение поля «next» должно быть идентификатором узла или списком переходов."},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 14, "Сценарий bug, узел plan: значение поля «max_rounds» должно быть целым числом больше 0."},
			{contract.ProblemUnknownField, "scenarios/bug.yaml", 15, "Сценарий bug, узел plan: неизвестное поле «when»."},
		}},
		{"nodes not a mapping", bug("branch", "  - branch\n"), []Problem{
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 3, "Сценарий bug: значение поля «nodes» должно быть набором узлов."},
		}},
		{"unreachable", bug("branch",
			"  branch: { stage: branch, next: finish }\n"+
				"  cleanup: { stage: merge, next: finish }\n"), []Problem{
			{contract.ProblemUnreachable, "scenarios/bug.yaml", 5, "Сценарий bug: узел cleanup недостижим из начального узла."},
		}},
		{"dead end", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: review }\n"+
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			// Only the dead end itself, not every node that leads into it;
			// merge, past the dead end, is unreachable.
			{contract.ProblemDeadEnd, "scenarios/bug.yaml", 6, "Сценарий bug: из узла review нельзя дойти до конца сценария."},
			{contract.ProblemUnreachable, "scenarios/bug.yaml", 12, "Сценарий bug: узел merge недостижим из начального узла."},
		}},
		{"unlimited loop", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: review }\n"+
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n      - to: finish\n"), []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 5, "Сценарий bug: у цикла implementation → review → implementation нет предела кругов."},
		}},
		{"unlimited self loop", bug("review",
			"  review:\n    stage: review\n    next:\n      - to: review\n        if: ещё круг\n      - to: finish\n"), []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 4, "Сценарий bug: у цикла review → review нет предела кругов."},
		}},
		{"limits", bug("plan",
			"  plan:\n    stage: plan-bug\n    next:\n      - to: review\n        if: план готов\n        max_rounds: 2\n"+
				"  review:\n    stage: review\n    next:\n      - to: review\n        max_rounds: 3\n      - to: finish\n        if: замечаний нет\n"), []Problem{
			{contract.ProblemLimitOutsideLoop, "scenarios/bug.yaml", 7, "Сценарий bug: у перехода plan → review указан предел кругов, но переход не замыкает цикл."},
			{contract.ProblemLimitWithoutCondition, "scenarios/bug.yaml", 13, "Сценарий bug: у перехода review → review с пределом кругов нет условия."},
		}},
		{"limit to the end", bug("plan",
			"  plan:\n    stage: plan-bug\n    next:\n      - to: finish\n        if: план готов\n        max_rounds: 2\n"), []Problem{
			{contract.ProblemLimitOutsideLoop, "scenarios/bug.yaml", 7, "Сценарий bug: у перехода plan → finish указан предел кругов, но переход не замыкает цикл."},
		}},
		{"transitions of a node", bug("review",
			"  review:\n    stage: review\n    next:\n      - to: merge\n        if: замечаний нет\n      - to: merge\n      - to: finish\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			{contract.ProblemDuplicateTransition, "scenarios/bug.yaml", 9, "Сценарий bug, узел review: два перехода к узлу merge."},
			{contract.ProblemSeveralDefaults, "scenarios/bug.yaml", 10, "Сценарий bug, узел review: переход без условия может быть только один."},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := problems(t, shop(t, tt.changes))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("problems:\n%s\nwant:\n%s", dump(got), dump(tt.want))
			}
		})
	}
}

func TestDefaultPathAtFork(t *testing.T) {
	f := load(t, shop(t, bug("branch",
		"  branch: { stage: branch, next: triage }\n"+
			"  triage:\n    stage: plan-bug\n    next:\n      - to: hotfix\n        if: срочно\n      - to: merge\n        if: не срочно\n"+
			"  hotfix: { stage: implementation, next: merge }\n"+
			"  merge: { stage: merge, next: finish }\n")))
	s, _ := f.Scenario("bug")
	if got, want := s.DefaultPath(), []string{"branch", "triage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("default path %v, want %v", got, want)
	}
}
