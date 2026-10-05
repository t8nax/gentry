package flow

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// Problem is one error of a flow.
type Problem struct {
	Code    string // kind of the problem, one of contract.Problem*
	File    string // path inside the flow directory with forward slashes; empty for the whole flow
	Line    int    // line of File from 1; 0 if unknown
	Message string // for the operator: names the object, not the file
}

// InvalidError means the flow has problems. They are all collected, in the
// order of files and lines.
type InvalidError struct {
	Dir      string
	Problems []Problem
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("%s: %d problems in the flow", e.Dir, len(e.Problems))
}

// NotFoundError means the project has no flow directory.
type NotFoundError struct{ Dir string }

func (e *NotFoundError) Error() string { return e.Dir + ": no flow" }

// Objects of the flow as problem messages name them.

func objCommon() string                  { return msg.Text(msg.FlowObjCommon) }
func objScenario(id string) string       { return msg.Text(msg.FlowObjScenario, id) }
func objNode(scenario, id string) string { return msg.Text(msg.FlowObjNode, scenario, id) }
func objStage(id string) string          { return msg.Text(msg.FlowObjStage, id) }
func objPart(id string) string           { return msg.Text(msg.FlowObjPart, id) }

// report adds a problem of file at line.
func (l *loader) report(code, file string, line int, k msg.Key, args ...any) {
	l.problems = append(l.problems, Problem{Code: code, File: file, Line: line, Message: msg.Text(k, args...)})
}

// rank orders the problems: those of the whole flow first, then the common
// rules, scenarios, stages and parts, and files that do not belong to the
// flow last, as the operator removes them separately.
func rank(p Problem) int {
	switch {
	case p.Code == contract.ProblemExtraFile:
		return 5
	case p.File == "":
		return 0
	case p.File == commonFile:
		return 1
	case strings.HasPrefix(p.File, scenariosDir+"/"):
		return 2
	case strings.HasPrefix(p.File, stagesDir+"/"):
		return 3
	}
	return 4
}

// sortProblems puts the problems in the order of files and lines; problems
// of one line keep the order in which they were found.
func sortProblems(ps []Problem) {
	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
}
