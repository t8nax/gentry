package flow

import (
	"cmp"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// Problem is one error of a flow.
type Problem struct {
	Code    string // kind of the problem, one of contract.Problem*
	File    string // path inside the flow with forward slashes; empty for the whole flow and the library
	Line    int    // line of File from 1; 0 if unknown
	Message string // for the operator: names the object, not the file
}

// Objects of the flow as problem messages name them.

func objCommon() string                  { return msg.Text(msg.FlowObjCommon) }
func objScenario(id string) string       { return msg.Text(msg.FlowObjScenario, id) }
func objNode(scenario, id string) string { return msg.Text(msg.FlowObjNode, scenario, id) }
func objStage(id string) string          { return msg.Text(msg.FlowObjStage, id) }
func objPart(id string) string           { return msg.Text(msg.FlowObjPart, id) }
func objAgent(id string) string          { return msg.Text(msg.FlowObjAgent, id) }
func objLibraryAgent(id string) string   { return msg.Text(msg.FlowObjLibraryAgent, id) }

// report adds a problem of file at line.
func (l *loader) report(code, file string, line int, k msg.Key, args ...any) {
	l.problems = append(l.problems, Problem{Code: code, File: file, Line: line, Message: msg.Text(k, args...)})
}

// reportOn adds a problem of object o at line of its file. A library subagent
// has no file in the flow, and so no line either.
func (l *loader) reportOn(o object, code string, line int, k msg.Key, args ...any) {
	if o.library {
		l.libProblems = append(l.libProblems, Problem{Code: code, Message: msg.Text(k, args...)})
		return
	}
	l.report(code, o.file, line, k, args...)
}

// rank orders the problems of the files of the flow: those of the whole flow
// first, then the common rules, scenarios, stages, parts and project
// subagents, and files that do not belong to the flow last, as the operator
// removes them separately.
func rank(p Problem) int {
	switch {
	case p.Code == contract.ProblemExtraFile:
		return 7
	case p.File == "":
		return 0
	case p.File == commonFile:
		return 1
	case strings.HasPrefix(p.File, scenariosDir+"/"):
		return 2
	case strings.HasPrefix(p.File, stagesDir+"/"):
		return 3
	case strings.HasPrefix(p.File, agentsDir+"/"):
		return 5
	}
	return 4
}

// sortProblems puts the problems of the files of the flow in the order of
// files and lines, and those of library subagents, which have no file in the
// flow, before the files that do not belong to it. Problems of one line keep
// the order in which they were found.
func sortProblems(ps, library []Problem) []Problem {
	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	i := slices.IndexFunc(ps, func(p Problem) bool { return p.Code == contract.ProblemExtraFile })
	if i < 0 {
		i = len(ps)
	}
	return slices.Concat(ps[:i], library, ps[i:])
}
