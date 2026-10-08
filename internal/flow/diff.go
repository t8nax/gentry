package flow

import (
	"slices"

	"github.com/t8nax/gentry/internal/process"
)

// ScenarioDiff is how a scenario of the draft differs from the active one:
// by the nodes and transitions of its graph, not by the lines of its file.
type ScenarioDiff struct {
	ID         string
	Change     string    // Added, Modified or Removed
	Old, New   *Scenario // nil if absent
	Unreadable bool      // the scenario of the draft has problems of its own: its graph is not compared
	// Nodes are the nodes that differ, by identifier: Added, Removed, or
	// Modified if the node has another stage or its stage changed. Only for a
	// modified scenario.
	Nodes       map[string]string
	Transitions []TransitionDiff // in the order of the nodes of the draft, the removed ones last
}

// TransitionDiff is a transition that differs between two variants of a
// scenario. A transition is known by the nodes it joins.
type TransitionDiff struct {
	From, To string
	Change   string     // Added, Modified or Removed
	Old, New Transition // Old for Modified and Removed, New for Added and Modified
	Return   bool       // the transition closes a loop
}

// DiffScenario compares scenario cur of the draft with old of the active
// flow; either may be nil. stages are the stages of the flow that changed,
// by identifier, as Diff names them.
func DiffScenario(old, cur *Scenario, stages map[string]string) ScenarioDiff {
	d := ScenarioDiff{Old: old, New: cur, Nodes: map[string]string{}}
	switch {
	case old == nil:
		d.ID, d.Change = cur.ID, Added
	case cur == nil:
		d.ID, d.Change = old.ID, Removed
	default:
		d.ID, d.Change = cur.ID, Modified
	}
	if d.Change != Modified {
		return d
	}
	before := nodesByID(old)
	for _, n := range cur.Nodes {
		o, ok := before[n.ID]
		switch {
		case !ok:
			d.Nodes[n.ID] = Added
		case o.Stage != n.Stage || stages[n.Stage] == Modified:
			d.Nodes[n.ID] = Modified
		}
		for _, t := range n.Next {
			td := TransitionDiff{From: n.ID, To: t.To, New: t, Return: closesLoop(cur, n.ID, t)}
			ot, ok := transitionTo(o, t.To)
			switch {
			case !ok:
				td.Change = Added
			case ot != t:
				td.Change, td.Old = Modified, ot
			default:
				continue
			}
			d.Transitions = append(d.Transitions, td)
		}
	}
	after := nodesByID(cur)
	for _, o := range old.Nodes {
		n, ok := after[o.ID]
		if !ok {
			d.Nodes[o.ID] = Removed
		}
		for _, t := range o.Next {
			if _, ok := transitionTo(n, t.To); !ok {
				d.Transitions = append(d.Transitions, TransitionDiff{
					From: o.ID, To: t.To, Change: Removed, Old: t, Return: closesLoop(old, o.ID, t),
				})
			}
		}
	}
	return d
}

func nodesByID(s *Scenario) map[string]Node {
	out := map[string]Node{}
	for _, n := range s.Nodes {
		out[n.ID] = n
	}
	return out
}

// transitionTo returns the transition of node n to node to.
func transitionTo(n Node, to string) (Transition, bool) {
	for _, t := range n.Next {
		if t.To == to {
			return t, true
		}
	}
	return Transition{}, false
}

// closesLoop reports whether transition t of node from leads back: the node
// is reachable from its target by transitions without a limit, as Loops
// counts it. A limit marks such a transition in a valid flow; a draft with
// problems may lack it.
func closesLoop(s *Scenario, from string, t Transition) bool {
	switch {
	case t.MaxRounds > 0:
		return true
	case t.If == "":
		// A return has a condition: the path by default leads forward.
		return false
	}
	next := map[string][]string{}
	for _, n := range s.Nodes {
		for _, nt := range n.Next {
			if nt.MaxRounds == 0 {
				next[n.ID] = append(next[n.ID], nt.To)
			}
		}
	}
	return reachable(t.To, next)[from]
}

// LibraryAgents returns the subagents of the library files as far as they
// can be read, by identifier.
func LibraryAgents(files process.Files) map[string]Agent {
	inside := map[string]string{}
	for p, text := range files {
		inside[agentsDir+"/"+p] = text
	}
	l := newLoader(treeOf(inside), mapLibrary(nil))
	l.inLibrary = true
	for _, e := range l.src.entries("") {
		if e.dir && e.name == agentsDir {
			l.scan(agentsDir)
		}
	}
	l.loadAgents()
	out := map[string]Agent{}
	for id, a := range l.agents {
		out[id] = Agent{
			ID: id, Library: true, Purpose: a.purpose,
			Capabilities: append([]string{}, a.capabilities...), Instruction: a.instruction,
		}
	}
	return out
}

// Users returns the stages of flow f that subagent id of the library
// carries out: none if a subagent of the project of the same name replaces
// it.
func (f *Flow) Users(id string) []string {
	if slices.ContainsFunc(f.Agents, func(a Agent) bool { return a.ID == id && !a.Library }) {
		return nil
	}
	return f.StagesBy(id)
}
