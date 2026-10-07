package task

import (
	"testing"

	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/state"
)

// acceptance is a scenario with a loop within a loop: review returns to
// implementation, acceptance returns to implementation too, around review.
//
//	branch → criteria → implementation → review → acceptance → finish
//	                      ↑   ↑────────────┘ (2)     │
//	                      └──────────────────────────┘ (1)
var acceptance = flow.Scenario{ID: "feature", Start: "branch", Nodes: []flow.Node{
	{ID: "branch", Stage: "branch", Next: []flow.Transition{{To: "criteria"}}},
	{ID: "criteria", Stage: "criteria", Next: []flow.Transition{{To: "implementation"}}},
	{ID: "implementation", Stage: "implementation", Next: []flow.Transition{{To: "review"}}},
	{ID: "review", Stage: "review", Next: []flow.Transition{
		{To: "implementation", If: "есть замечания", MaxRounds: 2},
		{To: "acceptance"},
	}},
	{ID: "acceptance", Stage: "acceptance", Next: []flow.Transition{
		{To: "implementation", If: "приёмка не пройдена", MaxRounds: 1},
		{To: "finish"},
	}},
}}

// way returns the closed passes of the nodes in order, each taking the
// transition to the next one, and the current pass of the last node.
func way(nodes ...string) []state.Pass {
	var ps []state.Pass
	for i, n := range nodes {
		p := state.Pass{Node: n, Stage: n}
		if i < len(nodes)-1 {
			p.Outcome, p.Next = state.OutcomeExit, nodes[i+1]
		}
		ps = append(ps, p)
	}
	return ps
}

func TestLoopsNested(t *testing.T) {
	loops := acceptance.Loops()
	if len(loops) != 2 {
		t.Fatalf("loops %+v", loops)
	}
	review, accept := loops[0], loops[1]
	if len(review.Nodes) != 2 || !review.Nodes["implementation"] || !review.Nodes["review"] {
		t.Errorf("loop of review %v", review.Nodes)
	}
	if len(accept.Nodes) != 3 || !accept.Nodes["acceptance"] {
		t.Errorf("loop of acceptance %v", accept.Nodes)
	}
	if !review.Within(accept) || accept.Within(review) || review.Within(review) {
		t.Error("the loop of review must lie within the loop of acceptance, and only so")
	}
}

func TestCountReturns(t *testing.T) {
	loops := acceptance.Loops()
	review, accept := [2]string{"review", "implementation"}, [2]string{"acceptance", "implementation"}
	tests := []struct {
		name           string
		path           []state.Pass
		review, accept int
	}{
		{"no returns", way("branch", "criteria", "implementation", "review"), 0, 0},
		{"two from review", way("branch", "criteria", "implementation", "review", "implementation", "review", "implementation", "review"), 2, 0},
		// A return from acceptance starts the returns from review anew.
		{"acceptance resets review", way("branch", "criteria", "implementation", "review", "implementation", "review",
			"implementation", "review", "acceptance", "implementation", "review"), 0, 1},
		{"review after the reset", way("branch", "criteria", "implementation", "review", "implementation", "review",
			"acceptance", "implementation", "review", "implementation", "review"), 1, 1},
	}
	for _, tt := range tests {
		counts := countReturns(loops, tt.path)
		if counts[review] != tt.review || counts[accept] != tt.accept {
			t.Errorf("%s: review %d, acceptance %d; want %d, %d", tt.name, counts[review], counts[accept], tt.review, tt.accept)
		}
	}
}

func TestCountReturnsOverlapping(t *testing.T) {
	// a → b → c → d: c returns to a, d returns to b; neither loop lies
	// within the other, so the counts are independent.
	sc := flow.Scenario{Start: "a", Nodes: []flow.Node{
		{ID: "a", Next: []flow.Transition{{To: "b"}}},
		{ID: "b", Next: []flow.Transition{{To: "c"}}},
		{ID: "c", Next: []flow.Transition{{To: "a", If: "x", MaxRounds: 3}, {To: "d"}}},
		{ID: "d", Next: []flow.Transition{{To: "b", If: "y", MaxRounds: 3}, {To: "finish"}}},
	}}
	counts := countReturns(sc.Loops(), way("a", "b", "c", "a", "b", "c", "d", "b", "c", "d"))
	if counts[[2]string{"c", "a"}] != 1 || counts[[2]string{"d", "b"}] != 1 {
		t.Errorf("counts %v", counts)
	}
}

func TestProgress(t *testing.T) {
	// A fork ahead: from start, a short way through b or a long one through c, d.
	fork := flow.Scenario{Start: "start", Nodes: []flow.Node{
		{ID: "start", Next: []flow.Transition{{To: "b", If: "мелкая правка"}, {To: "c", If: "крупная правка"}}},
		{ID: "b", Next: []flow.Transition{{To: "e"}}},
		{ID: "c", Next: []flow.Transition{{To: "d"}}},
		{ID: "d", Next: []flow.Transition{{To: "e"}}},
		{ID: "e", Next: []flow.Transition{{To: "finish"}}},
	}}
	tests := []struct {
		name             string
		sc               flow.Scenario
		path             []state.Pass
		passed, min, max int
	}{
		{"at the start", acceptance, way("branch"), 0, 5, 5},
		{"after a return", acceptance, way("branch", "criteria", "implementation", "review", "implementation"), 4, 5, 5},
		{"at the end", acceptance, way("branch", "criteria", "implementation", "review", "acceptance", "finish"), 5, 5, 5},
		{"ahead of a fork", fork, way("start"), 0, 3, 4},
		{"after the fork", fork, way("start", "c"), 1, 4, 4},
	}
	for _, tt := range tests {
		node := tt.path[len(tt.path)-1].Node
		p := progressOf(tt.sc, tt.path, node)
		if p.Passed != tt.passed || p.Min != tt.min || p.Max != tt.max {
			t.Errorf("%s: %+v, want %d of %d–%d", tt.name, p, tt.passed, tt.min, tt.max)
		}
	}
}

func TestPick(t *testing.T) {
	review := acceptance.Nodes[3]
	single := flow.Node{ID: "plan", Next: []flow.Transition{{To: "implementation"}}}
	conditional := flow.Node{ID: "check", Next: []flow.Transition{{To: "finish", If: "проверки пройдены"}}}
	tests := []struct {
		name  string
		node  flow.Node
		req   Close
		to    string
		field string
	}{
		{"single", single, Close{}, "implementation", ""},
		{"single named", single, Close{To: "implementation"}, "implementation", ""},
		{"fork without to", review, Close{Reason: "р"}, "", "to"},
		{"fork without reason", review, Close{To: "acceptance"}, "", "reason"},
		{"fork default with reason", review, Close{To: "acceptance", Reason: "замечаний нет"}, "acceptance", ""},
		{"single with a condition needs a reason", conditional, Close{}, "", "reason"},
		{"single with a condition", conditional, Close{Reason: "проверки пройдены"}, "finish", ""},
	}
	for _, tt := range tests {
		tr, err := pick(tt.node, flow.Stage{}, tt.req)
		fe, isField := err.(*FieldError)
		switch {
		case tt.field != "" && (!isField || fe.Field != tt.field):
			t.Errorf("%s: %v, want the field %s missing", tt.name, err, tt.field)
		case tt.field == "" && (err != nil || tr.To != tt.to):
			t.Errorf("%s: %v, %v; want %s", tt.name, tr, err, tt.to)
		}
	}
	if _, err := pick(single, flow.Stage{}, Close{To: "review"}); err == nil {
		t.Error("a transition the node has not is taken")
	} else if te, ok := err.(*TransitionError); !ok || te.To != "review" || len(te.Transitions) != 1 {
		t.Errorf("error %v", err)
	}
}

// TestAllowedReturns counts the returns the operator allowed by return: a
// return by an outer loop starts the count of returns anew, not the
// allowances.
func TestAllowedReturns(t *testing.T) {
	review, accept := [2]string{"review", "implementation"}, [2]string{"acceptance", "implementation"}
	ds := []state.Decision{
		{Node: "review", AllowReturn: "implementation"},
		{Node: "review"},
		{Node: "acceptance", AllowReturn: "implementation"},
		{Node: "review", AllowReturn: "implementation"},
	}
	allowed := allowedReturns(ds)
	counts := countReturns(acceptance.Loops(), way("branch", "criteria", "implementation", "review", "implementation", "review",
		"implementation", "review", "acceptance", "implementation", "review"))
	if allowed[review] != 2 || allowed[accept] != 1 || len(allowed) != 2 {
		t.Errorf("allowed %v", allowed)
	}
	if counts[review] != 0 || counts[accept] != 1 {
		t.Errorf("counts %v", counts)
	}
}
