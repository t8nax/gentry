package flow

// Loop is the loop a return closes. A return is a transition with a limit of
// returns: the checks of a flow allow a limit only on a transition that
// closes a loop, and every loop has one.
type Loop struct {
	From, To string // the return: from the node back to an earlier one
	Max      int    // the limit of returns
	// Nodes are the nodes from the target of the return to its node by
	// transitions without a limit, both included.
	Nodes map[string]bool
}

// Within reports whether loop l lies within loop o: its nodes are a part of
// the nodes of o, and not all of them. A return by o starts the returns of l
// anew.
func (l Loop) Within(o Loop) bool {
	if len(l.Nodes) >= len(o.Nodes) {
		return false
	}
	for id := range l.Nodes {
		if !o.Nodes[id] {
			return false
		}
	}
	return true
}

// Loops returns the loops of the returns of s, in the order of the nodes and
// their transitions. Transitions with a limit are left out of the graph of a
// loop: through another return every node of a loop would reach every other,
// and nested loops could not be told apart.
func (s Scenario) Loops() []Loop {
	forward := map[string][]string{}
	back := map[string][]string{}
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			if t.MaxRounds == 0 {
				forward[n.ID] = append(forward[n.ID], t.To)
				back[t.To] = append(back[t.To], n.ID)
			}
		}
	}
	var loops []Loop
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			if t.MaxRounds == 0 {
				continue
			}
			from := reachable(t.To, forward)
			to := reachable(n.ID, back)
			nodes := map[string]bool{}
			for id := range from {
				if to[id] {
					nodes[id] = true
				}
			}
			loops = append(loops, Loop{From: n.ID, To: t.To, Max: t.MaxRounds, Nodes: nodes})
		}
	}
	return loops
}

// reachable returns the nodes reachable from id by edges, id included.
func reachable(id string, edges map[string][]string) map[string]bool {
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, to := range edges[cur] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	return seen
}
