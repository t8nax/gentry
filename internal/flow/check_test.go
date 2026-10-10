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
