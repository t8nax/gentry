package flow

import (
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// check checks what the files of the flow refer to and the graph of each
// scenario. Problems of a file itself are reported as it is read. It returns
// an error only if the library of subagents cannot be read.
func (l *loader) check() error {
	if l.scenarioCount == 0 {
		l.report(contract.ProblemNoScenarios, "", 0, msg.ProblemNoScenarios)
	}
	for _, id := range sorted(l.fields) {
		if !l.texts[id] {
			l.report(contract.ProblemMissingInstruction, stagesDir+"/"+id+fieldsExt, 0, msg.ProblemMissingInstruction, objStage(id))
		}
	}
	for _, id := range sorted(l.texts) {
		if !l.fields[id] {
			l.report(contract.ProblemOrphanInstruction, stagesDir+"/"+id+textExt, 0, msg.ProblemOrphanInstruction, objStage(id))
		}
	}
	l.checkAgents()
	for _, id := range sorted(l.fields) {
		st := l.stages[id]
		for i, p := range st.include {
			if !l.parts[p] {
				l.report(contract.ProblemUnknownPart, st.file, st.includeLines[i], msg.ProblemUnknownPart, objStage(id), p)
			}
		}
		if e := st.executor; e != "" && e != Orchestrator && e != Operator {
			found, err := l.subagent(e)
			if err != nil {
				return err
			}
			switch {
			case found:
			case l.draftLibrary[e]:
				l.report(contract.ProblemUnknownExecutor, st.file, st.executorLine, msg.ProblemExecutorInDraftLibrary, objStage(id), e)
			default:
				l.report(contract.ProblemUnknownExecutor, st.file, st.executorLine, msg.ProblemUnknownExecutor, objStage(id), e)
			}
		}
	}
	for _, s := range l.scenarios {
		for _, n := range s.nodes {
			if n.stage != "" && !l.fields[n.stage] {
				l.report(contract.ProblemUnknownStage, s.file, n.stageLine, msg.ProblemUnknownStage, objNode(s.id, n.id), n.stage)
			}
		}
		// A malformed file would give false problems of the graph, such as a
		// dead end at a node whose transitions cannot be read.
		if !s.malformed {
			l.checkGraph(s)
		}
	}
	return nil
}

// checkAgents checks that each subagent has both its fields and its
// instruction.
func (l *loader) checkAgents() {
	for _, id := range sorted(l.agentFields) {
		if !l.agentTexts[id] {
			l.report(contract.ProblemMissingInstruction, agentsDir+"/"+id+fieldsExt, 0, msg.ProblemMissingInstruction, objAgent(id))
		}
	}
	for _, id := range sorted(l.agentTexts) {
		if !l.agentFields[id] {
			l.report(contract.ProblemOrphanInstruction, agentsDir+"/"+id+textExt, 0, msg.ProblemOrphanAgentInstruction, objAgent(id))
		}
	}
}

// checkGraph checks the graph of scenario s by the rules of section 7.1: the
// start node and every target exist; every node is reachable from the start
// and reaches the end; every loop has a transition with a limit of rounds,
// and only a transition that closes a loop has one; at most one transition of
// a node is without a condition, and no two lead to one node.
func (l *loader) checkGraph(s *scenario) {
	so := objScenario(s.id)
	byID := map[string]*node{}
	for _, n := range s.nodes {
		byID[n.id] = n
	}
	startKnown := byID[s.start] != nil
	if s.start != "" && !startKnown {
		l.report(contract.ProblemUnknownNode, s.file, s.startLine, msg.ProblemUnknownStart, so, s.start)
	}

	// The graph keeps the transitions whose target exists.
	edges := map[string][]*transition{}
	unknown := !startKnown
	for _, n := range s.nodes {
		no := objNode(s.id, n.id)
		defaults := 0
		targets := map[string]bool{}
		for _, t := range n.next {
			if t.to == "" {
				continue
			}
			if t.cond == "" {
				if defaults++; defaults == 2 {
					l.report(contract.ProblemSeveralDefaults, s.file, t.line, msg.ProblemSeveralDefaults, no)
				}
				if t.max > 0 {
					l.report(contract.ProblemLimitWithoutCondition, s.file, t.line, msg.ProblemLimitWithoutCondition, so, n.id, t.to)
				}
			}
			if targets[t.to] {
				l.report(contract.ProblemDuplicateTransition, s.file, t.line, msg.ProblemDuplicateTransition, no, t.to)
			}
			targets[t.to] = true
			if t.to != Finish && byID[t.to] == nil {
				l.report(contract.ProblemUnknownNode, s.file, t.line, msg.ProblemUnknownTarget, no, t.to)
				unknown = true
				continue
			}
			edges[n.id] = append(edges[n.id], t)
		}
	}

	// A misspelt node would make the rest of the graph look broken: nodes
	// after it unreachable, nodes before it dead ends.
	if unknown {
		return
	}

	// reach returns what is reachable from node id by zero or more
	// transitions, Finish included; unlimited follows only transitions
	// without a limit of rounds.
	reach := func(id string, unlimited bool) map[string]bool {
		seen := map[string]bool{id: true}
		queue := []string{id}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, t := range edges[cur] {
				if unlimited && t.max > 0 || seen[t.to] {
					continue
				}
				seen[t.to] = true
				queue = append(queue, t.to)
			}
		}
		return seen
	}
	reaches := map[string]map[string]bool{}
	for _, n := range s.nodes {
		reaches[n.id] = reach(n.id, false)
	}

	for _, n := range s.nodes {
		if !reaches[s.start][n.id] {
			l.report(contract.ProblemUnreachable, s.file, n.line, msg.ProblemUnreachable, so, n.id)
		}
	}
	// Every node that leads into a dead end is one too. Only the dead ends
	// themselves are reported: groups of nodes that lead only to each other,
	// each by its last node in the order of the file, where the way out is
	// most likely missing.
	dead := func(id string) bool { return reaches[s.start][id] && !reaches[id][Finish] }
	reported := map[string]bool{}
	for i := len(s.nodes) - 1; i >= 0; i-- {
		n := s.nodes[i]
		if !dead(n.id) || reported[n.id] {
			continue
		}
		group := true
		for id := range reaches[n.id] {
			group = group && reaches[id][n.id]
		}
		if !group {
			continue
		}
		for id := range reaches[n.id] {
			reported[id] = true
		}
		l.report(contract.ProblemDeadEnd, s.file, n.line, msg.ProblemDeadEnd, so, n.id)
	}

	// A transition closes a loop when its target leads back to its node.
	for _, n := range s.nodes {
		for _, t := range edges[n.id] {
			if t.max > 0 && (t.to == Finish || !reaches[t.to][n.id]) {
				l.report(contract.ProblemLimitOutsideLoop, s.file, t.line, msg.ProblemLimitOutsideLoop, so, n.id, t.to)
			}
		}
	}

	// A loop without a limit is a cycle of transitions without one. Each such
	// group of nodes is reported once, by the shortest cycle through its
	// first node in the order of the file.
	covered := map[string]bool{}
	for _, n := range s.nodes {
		if covered[n.id] {
			continue
		}
		cycle := shortestCycle(n.id, edges)
		if cycle == nil {
			continue
		}
		from := reach(n.id, true)
		for _, m := range s.nodes {
			if from[m.id] && reach(m.id, true)[n.id] {
				covered[m.id] = true
			}
		}
		l.report(contract.ProblemUnlimitedLoop, s.file, n.line, msg.ProblemUnlimitedLoop, so, strings.Join(cycle, " → "))
	}
}

// shortestCycle returns the shortest cycle from node id back to it by
// transitions without a limit of rounds, both ends included; nil if none.
func shortestCycle(id string, edges map[string][]*transition) []string {
	parent := map[string]string{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, t := range edges[cur] {
			if t.max > 0 || t.to == Finish {
				continue
			}
			if t.to == id {
				cycle := []string{id}
				for at := cur; at != id; at = parent[at] {
					cycle = append(cycle, at)
				}
				cycle = append(cycle, id)
				// The cycle was collected backwards.
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				return cycle
			}
			if _, seen := parent[t.to]; seen {
				continue
			}
			parent[t.to] = cur
			queue = append(queue, t.to)
		}
	}
	return nil
}

// DefaultPath returns the default path of s (section 7.4): from the start
// node by transitions without a condition, ending with Finish if it reaches
// the end. At a fork, where all transitions have a condition, the path stops.
func (s Scenario) DefaultPath() []string {
	byID := map[string]Node{}
	for _, n := range s.Nodes {
		byID[n.ID] = n
	}
	var path []string
	seen := map[string]bool{}
	for id := s.Start; ; {
		if id == Finish {
			return append(path, Finish)
		}
		n, ok := byID[id]
		if !ok || seen[id] {
			return path
		}
		seen[id] = true
		path = append(path, id)
		id = ""
		for _, t := range n.Next {
			if t.If == "" {
				id = t.To
				break
			}
		}
		if id == "" {
			return path
		}
	}
}
