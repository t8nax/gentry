package task

import (
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/state"
)

// Progress is the progress of a task (section 7.4): nodes passed of all
// nodes of its way to the end. Ahead of a fork the total is a range.
type Progress struct {
	Passed   int // nodes closed by an exit or a skip; a return does not lower it
	Min, Max int // Passed and the nodes not passed yet on the shortest and the longest way to the end
}

// progressOf returns the progress of a task at node along passes in sc.
// Returns are left out of the way to the end: the checks of a flow leave no
// loop without them, so the rest of the graph has no cycles.
func progressOf(sc flow.Scenario, passes []state.Pass, node string) Progress {
	passed := map[string]bool{}
	for _, p := range passes {
		if !p.Current() {
			passed[p.Node] = true
		}
	}
	pr := Progress{Passed: len(passed), Min: len(passed), Max: len(passed)}
	if node == flow.Finish {
		return pr
	}
	next := map[string][]string{}
	for _, n := range sc.Nodes {
		for _, t := range n.Next {
			if t.MaxRounds == 0 {
				next[n.ID] = append(next[n.ID], t.To)
			}
		}
	}
	type span struct{ min, max int }
	memo := map[string]span{}
	var ahead func(id string) span
	ahead = func(id string) span {
		if id == flow.Finish {
			return span{}
		}
		if s, ok := memo[id]; ok {
			return s
		}
		// A guard against a graph the checks would refuse: a node is
		// counted once on any way.
		memo[id] = span{}
		var s span
		for i, to := range next[id] {
			a := ahead(to)
			if i == 0 || a.min < s.min {
				s.min = a.min
			}
			s.max = max(s.max, a.max)
		}
		if !passed[id] {
			s.min++
			s.max++
		}
		memo[id] = s
		return s
	}
	s := ahead(node)
	pr.Min += s.min
	pr.Max += s.max
	return pr
}
