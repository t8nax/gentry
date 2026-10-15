package flow

import (
	"reflect"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
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
			{contract.ProblemUnknownNode, "scenarios/bug.yaml", 2, msg.Text(msg.ProblemUnknownStart, msg.Text(msg.FlowObjScenario, "bug"), "brnch")},
		}},
		{"unknown target", bug("branch",
			"  branch: { stage: branch, next: review }\n"+
				"  review: { stage: review, next: merj }\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			// Without the misspelt node the rest is not checked: merge
			// would be unreachable and review a dead end.
			{contract.ProblemUnknownNode, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemUnknownTarget, msg.Text(msg.FlowObjNode, "bug", "review"), "merj")},
		}},
		{"missing start and next", map[string]string{"scenarios/bug.yaml": "title: Баг\nnodes:\n  branch: { stage: branch }\n"}, []Problem{
			{contract.ProblemMissingField, "scenarios/bug.yaml", 0, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjScenario, "bug"), "start")},
			{contract.ProblemMissingField, "scenarios/bug.yaml", 3, msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjNode, "bug", "branch"), "next")},
		}},
		{"invalid nodes", bug("branch",
			"  branch: { stage: branch, next: 5 }\n"+
				"  Merge_Node: { stage: merge, next: finish }\n"+
				"  finish: { stage: merge, next: finish }\n"+
				"  review: review\n"+
				"  plan:\n    stage: plan-bug\n    next:\n      - finish\n      - to: finish\n        if: x\n        max_rounds: 0\n        when: y\n"), []Problem{
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 4, msg.Text(msg.ProblemNext, msg.Text(msg.FlowObjNode, "bug", "branch"))},
			{contract.ProblemInvalidID, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemInvalidID, msg.Text(msg.FlowObjNode, "bug", "Merge_Node"))},
			{contract.ProblemReservedNode, "scenarios/bug.yaml", 6, msg.Text(msg.ProblemReservedNode, msg.Text(msg.FlowObjScenario, "bug"))},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 7, msg.Text(msg.ProblemNotMapping, msg.Text(msg.FlowObjNode, "bug", "review"))},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 11, msg.Text(msg.ProblemNext, msg.Text(msg.FlowObjNode, "bug", "plan"))},
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 14, msg.Text(msg.ProblemMaxRounds, msg.Text(msg.FlowObjNode, "bug", "plan"))},
			{contract.ProblemUnknownField, "scenarios/bug.yaml", 15, msg.Text(msg.ProblemUnknownField, msg.Text(msg.FlowObjNode, "bug", "plan"), "when")},
		}},
		{"nodes not a mapping", bug("branch", "  - branch\n"), []Problem{
			{contract.ProblemInvalidValue, "scenarios/bug.yaml", 3, msg.Text(msg.ProblemNodes, msg.Text(msg.FlowObjScenario, "bug"))},
		}},
		{"unreachable", bug("branch",
			"  branch: { stage: branch, next: finish }\n"+
				"  cleanup: { stage: merge, next: finish }\n"), []Problem{
			{contract.ProblemUnreachable, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemUnreachable, msg.Text(msg.FlowObjScenario, "bug"), "cleanup")},
		}},
		{"dead end", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: review }\n"+
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			// Only the dead end itself, not every node that leads into it;
			// merge, past the dead end, is unreachable.
			{contract.ProblemDeadEnd, "scenarios/bug.yaml", 6, msg.Text(msg.ProblemDeadEnd, msg.Text(msg.FlowObjScenario, "bug"), "review")},
			{contract.ProblemUnreachable, "scenarios/bug.yaml", 12, msg.Text(msg.ProblemUnreachable, msg.Text(msg.FlowObjScenario, "bug"), "merge")},
		}},
		{"unlimited loop", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: review }\n"+
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n      - to: finish\n"), []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemUnlimitedLoop, msg.Text(msg.FlowObjScenario, "bug"), "implementation → review → implementation")},
		}},
		{"unlimited self loop", bug("review",
			"  review:\n    stage: review\n    next:\n      - to: review\n        if: ещё круг\n      - to: finish\n"), []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 4, msg.Text(msg.ProblemUnlimitedLoop, msg.Text(msg.FlowObjScenario, "bug"), "review → review")},
		}},
		{"an unlimited return of the operator by default", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: merge }\n"+
				"  merge:\n    stage: merge\n    next:\n      - to: finish\n        if: работа принята\n      - to: implementation\n"), []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 5, msg.Text(msg.ProblemUnlimitedLoop, msg.Text(msg.FlowObjScenario, "bug"), "implementation → merge → implementation")},
		}},
		{"an unlimited return of the orchestrator", map[string]string{
			"scenarios/bug.yaml": "title: Баг\nstart: implementation\nnodes:\n" +
				"  implementation: { stage: implementation, next: merge }\n" +
				"  merge:\n    stage: merge\n    next:\n      - to: implementation\n        if: не так\n      - to: finish\n",
			"stages/merge.yaml": "title: Слияние\nexit: ветка задачи влита в main\nexecutor: orchestrator\n",
		}, []Problem{
			{contract.ProblemUnlimitedLoop, "scenarios/bug.yaml", 4, msg.Text(msg.ProblemUnlimitedLoop, msg.Text(msg.FlowObjScenario, "bug"), "implementation → merge → implementation")},
		}},
		{"limits", bug("plan",
			"  plan:\n    stage: plan-bug\n    next:\n      - to: review\n        if: план готов\n        max_rounds: 2\n"+
				"  review:\n    stage: review\n    next:\n      - to: review\n        max_rounds: 3\n      - to: finish\n        if: замечаний нет\n"), []Problem{
			{contract.ProblemLimitOutsideLoop, "scenarios/bug.yaml", 7, msg.Text(msg.ProblemLimitOutsideLoop, msg.Text(msg.FlowObjScenario, "bug"), "plan", "review")},
			{contract.ProblemLimitWithoutCondition, "scenarios/bug.yaml", 13, msg.Text(msg.ProblemLimitWithoutCondition, msg.Text(msg.FlowObjScenario, "bug"), "review", "review")},
		}},
		{"limit to the end", bug("plan",
			"  plan:\n    stage: plan-bug\n    next:\n      - to: finish\n        if: план готов\n        max_rounds: 2\n"), []Problem{
			{contract.ProblemLimitOutsideLoop, "scenarios/bug.yaml", 7, msg.Text(msg.ProblemLimitOutsideLoop, msg.Text(msg.FlowObjScenario, "bug"), "plan", "finish")},
		}},
		{"transitions of a node", bug("review",
			"  review:\n    stage: review\n    next:\n      - to: merge\n        if: замечаний нет\n      - to: merge\n      - to: finish\n"+
				"  merge: { stage: merge, next: finish }\n"), []Problem{
			{contract.ProblemDuplicateTransition, "scenarios/bug.yaml", 9, msg.Text(msg.ProblemDuplicateTransition, msg.Text(msg.FlowObjNode, "bug", "review"), "merge")},
			{contract.ProblemSeveralDefaults, "scenarios/bug.yaml", 10, msg.Text(msg.ProblemSeveralDefaults, msg.Text(msg.FlowObjNode, "bug", "review"))},
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

// returnsOf returns the returns of scenario s: whether each is without a
// limit, by its nodes.
func returnsOf(s Scenario) map[[2]string]bool {
	returns := map[[2]string]bool{}
	for _, n := range s.Nodes {
		for _, tr := range n.Next {
			if tr.Return() {
				returns[[2]string{n.ID, tr.To}] = tr.UnlimitedReturn
			}
		}
	}
	return returns
}

// TestOperatorReturns checks the returns the operator decides: a step back
// along the course of the scenario. The scenario is walked from the start, at
// each node by default first; a transition with a condition from a node of a
// stage of the operator to a node on the current way of the walk is a return
// without a limit, and its loop needs no other. Any other transition leads
// forward: though it lies on the loop of another return, or leads to a node
// the walk has passed by another way.
func TestOperatorReturns(t *testing.T) {
	// The plan of a bug is the operator's: plan → review leads around the
	// implementation.
	planOfOperator := "title: План бага\nexit: план исправления согласован\nexecutor: operator\n"
	tests := []struct {
		name    string
		changes map[string]string
		want    map[[2]string]bool // unlimited, by return
	}{
		{"a transition forward on the loop of another return", bug("branch",
			"  branch: { stage: branch, next: implementation }\n"+
				"  implementation: { stage: implementation, next: review }\n"+
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n      - to: merge\n"+
				"  merge:\n    stage: merge\n    next:\n      - to: release\n        if: нужен выпуск\n      - to: finish\n"+
				"  release:\n    stage: merge\n    next:\n      - to: implementation\n        if: выпуск не прошёл\n      - to: finish\n"),
			map[[2]string]bool{{"review", "implementation"}: false, {"release", "implementation"}: true}},
		{"a way around the node of the return", map[string]string{
			"stages/plan-bug.yaml": planOfOperator,
			"scenarios/bug.yaml": "title: Баг\nstart: branch\nnodes:\n" +
				"  branch: { stage: branch, next: plan }\n" +
				"  plan:\n    stage: plan-bug\n    next:\n      - to: implementation\n      - to: review\n        if: правка только в документации\n" +
				"  implementation: { stage: implementation, next: review }\n" +
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n      - to: merge\n" +
				"  merge:\n    stage: merge\n    next:\n      - to: implementation\n        if: оператор назвал, что не так\n      - to: finish\n",
		}, map[[2]string]bool{{"review", "implementation"}: false, {"merge", "implementation"}: true}},
		// Taken in the order of the file, plan → review would come first, and
		// merge → implementation would lead to a node not yet passed.
		{"by default first", map[string]string{
			"stages/plan-bug.yaml": planOfOperator,
			"scenarios/bug.yaml": "title: Баг\nstart: branch\nnodes:\n" +
				"  branch: { stage: branch, next: plan }\n" +
				"  plan:\n    stage: plan-bug\n    next:\n      - to: review\n        if: правка только в документации\n      - to: implementation\n" +
				"  implementation: { stage: implementation, next: review }\n" +
				"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n      - to: merge\n" +
				"  merge:\n    stage: merge\n    next:\n      - to: implementation\n        if: оператор назвал, что не так\n      - to: finish\n",
		}, map[[2]string]bool{{"review", "implementation"}: false, {"merge", "implementation"}: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := load(t, shop(t, tt.changes)).Scenario("bug")
			if got := returnsOf(s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("returns %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOperatorReturnLoops checks the loops of a return without a limit and a
// return with one within it.
func TestOperatorReturnLoops(t *testing.T) {
	f := load(t, shop(t, bug("branch",
		"  branch: { stage: branch, next: implementation }\n"+
			"  implementation: { stage: implementation, next: review }\n"+
			"  review:\n    stage: review\n    next:\n      - to: implementation\n        if: есть замечания\n        max_rounds: 3\n      - to: merge\n"+
			"  merge:\n    stage: merge\n    next:\n      - to: release\n        if: нужен выпуск\n      - to: finish\n"+
			"  release:\n    stage: merge\n    next:\n      - to: implementation\n        if: выпуск не прошёл\n      - to: finish\n")))
	s, _ := f.Scenario("bug")
	loops := s.Loops()
	if len(loops) != 2 || loops[1].From != "release" || loops[1].Max != 0 || len(loops[1].Nodes) != 4 || !loops[0].Within(loops[1]) {
		t.Errorf("loops %+v", loops)
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
