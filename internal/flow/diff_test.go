package flow

import (
	"reflect"
	"testing"
)

// graph builds a scenario of nodes: id, stage and transitions.
func graph(nodes ...Node) *Scenario {
	return &Scenario{ID: "feature", Title: "Фича", Start: nodes[0].ID, Nodes: nodes}
}

func to(node string) Transition { return Transition{To: node} }

func back(node, cond string, max int) Transition {
	return Transition{To: node, If: cond, MaxRounds: max}
}

func TestDiffScenario(t *testing.T) {
	old := graph(
		Node{ID: "implementation", Stage: "implementation", Next: []Transition{to("review")}},
		Node{ID: "review", Stage: "review", Next: []Transition{back("implementation", "замечания", 2), to("merge")}},
		Node{ID: "merge", Stage: "merge", Next: []Transition{to(Finish)}},
	)
	tests := []struct {
		name        string
		cur         *Scenario
		stages      map[string]string
		nodes       map[string]string
		transitions []TransitionDiff
	}{
		{"the same", old, nil, map[string]string{}, nil},
		{"a node after review with a return", graph(
			Node{ID: "implementation", Stage: "implementation", Next: []Transition{to("review")}},
			Node{ID: "review", Stage: "review", Next: []Transition{back("implementation", "замечания", 2), to("security")}},
			Node{ID: "security", Stage: "security", Next: []Transition{back("implementation", "уязвимости", 1), to("merge")}},
			Node{ID: "merge", Stage: "merge", Next: []Transition{to(Finish)}},
		), nil, map[string]string{"security": Added}, []TransitionDiff{
			// The way forward through the new node is no return, though
			// the new node returns.
			{From: "review", To: "security", Change: Added, New: to("security")},
			{From: "security", To: "implementation", Change: Added, New: back("implementation", "уязвимости", 1), Return: true},
			{From: "security", To: "merge", Change: Added, New: to("merge")},
			{From: "review", To: "merge", Change: Removed, Old: to("merge")},
		}},
		{"the limit and the condition", graph(
			Node{ID: "implementation", Stage: "implementation", Next: []Transition{to("review")}},
			Node{ID: "review", Stage: "review", Next: []Transition{back("implementation", "существенные замечания", 3), to("merge")}},
			Node{ID: "merge", Stage: "merge", Next: []Transition{to(Finish)}},
		), nil, map[string]string{}, []TransitionDiff{
			{From: "review", To: "implementation", Change: Modified, Old: back("implementation", "замечания", 2),
				New: back("implementation", "существенные замечания", 3), Return: true},
		}},
		{"another stage and a changed stage", graph(
			Node{ID: "implementation", Stage: "coding", Next: []Transition{to("review")}},
			Node{ID: "review", Stage: "review", Next: []Transition{back("implementation", "замечания", 2), to("merge")}},
			Node{ID: "merge", Stage: "merge", Next: []Transition{to(Finish)}},
		), map[string]string{"review": Modified}, map[string]string{"implementation": Modified, "review": Modified}, nil},
		{"a node removed", graph(
			Node{ID: "implementation", Stage: "implementation", Next: []Transition{to("merge")}},
			Node{ID: "merge", Stage: "merge", Next: []Transition{to(Finish)}},
		), nil, map[string]string{"review": Removed}, []TransitionDiff{
			{From: "implementation", To: "merge", Change: Added, New: to("merge")},
			{From: "implementation", To: "review", Change: Removed, Old: to("review")},
			{From: "review", To: "implementation", Change: Removed, Old: back("implementation", "замечания", 2), Return: true},
			{From: "review", To: "merge", Change: Removed, Old: to("merge")},
		}},
	}
	for _, tt := range tests {
		d := DiffScenario(old, tt.cur, tt.stages)
		if d.Change != Modified || !reflect.DeepEqual(d.Nodes, tt.nodes) || !reflect.DeepEqual(d.Transitions, tt.transitions) {
			t.Errorf("%s:\nnodes %v, want %v\ntransitions %+v\nwant %+v", tt.name, d.Nodes, tt.nodes, d.Transitions, tt.transitions)
		}
	}

	if d := DiffScenario(nil, old, nil); d.Change != Added || d.ID != "feature" || len(d.Transitions) != 0 {
		t.Errorf("added: %+v", d)
	}
	if d := DiffScenario(old, nil, nil); d.Change != Removed || d.ID != "feature" {
		t.Errorf("removed: %+v", d)
	}
}

func TestClosesLoop(t *testing.T) {
	// A return without a limit in a draft with problems is still a return.
	s := graph(
		Node{ID: "plan", Stage: "plan", Next: []Transition{to("review")}},
		Node{ID: "review", Stage: "review", Next: []Transition{{To: "plan", If: "план не принят"}, to(Finish)}},
	)
	if !closesLoop(s, "review", s.Nodes[1].Next[0]) || closesLoop(s, "plan", s.Nodes[0].Next[0]) {
		t.Error("closesLoop")
	}
}

func TestUsers(t *testing.T) {
	f := &Flow{
		Stages: []Stage{{ID: "review", Executor: "reviewer"}, {ID: "audit", Executor: "auditor"}},
		Agents: []Agent{{ID: "reviewer", Library: true}, {ID: "auditor"}},
	}
	if got := f.Users("reviewer"); !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("reviewer: %v", got)
	}
	// A subagent of the project replaces the one of the library.
	if got := f.Users("auditor"); got != nil {
		t.Errorf("auditor: %v", got)
	}
}

func TestReadWithProblems(t *testing.T) {
	res := readShop(t, shop(t, map[string]string{
		"stages/review.yaml": "title: Ревью\nexecutor: reviewer\n",
		"scenarios/bug.yaml": "title: [\n",
	}))
	if res.Flow != nil || res.Read == nil || !res.Unreadable["bug"] || res.Unreadable["feature"] {
		t.Fatalf("flow %v, unreadable %v", res.Flow, res.Unreadable)
	}
	if st, ok := res.Read.Stage("review"); !ok || st.Title != "Ревью" {
		t.Errorf("review as read: %+v", st)
	}
	if s, ok := res.Read.Scenario("feature"); !ok || len(s.Nodes) != 5 {
		t.Errorf("feature as read: %+v", s)
	}
}
