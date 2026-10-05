package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/project"
)

// runFlowShow prints the flow of a project, or one scenario of it. It only
// reads: the flow is read and checked anew at each call.
func runFlowShow(args []string, env Env) int {
	f := newFlags("flow show")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	name, named := "", len(f.args) > 0
	if named {
		name = f.args[0]
	}
	wd, err := os.Getwd()
	if err != nil {
		return fail(env, internal(err))
	}
	dir, err := paths.Canonical(wd)
	if err != nil {
		return fail(env, internal(err))
	}
	projects, worktrees, bad := readPool()
	if bad != nil {
		return fail(env, *bad)
	}
	p, err := project.Resolve(projects, worktrees, dir, proj.Value)
	if err != nil {
		if rf, ok := resolveFailure(err); ok {
			return fail(env, rf)
		}
		return fail(env, internal(err))
	}
	process, err := home.Process()
	if err != nil {
		return fail(env, homeUnknown())
	}
	if process, err = paths.Canonical(process); err != nil {
		return fail(env, internal(err))
	}
	d := flow.ProjectDirs(process, p.ID)
	fl, err := flow.Load(d)
	if err != nil {
		return fail(env, flowFailure(err, p.ID))
	}

	var b strings.Builder
	if named {
		s, ok := fl.Scenario(name)
		if !ok {
			return fail(env, failure{
				exit:    contract.ExitError,
				code:    contract.CodeScenarioNotFound,
				message: msg.Text(msg.ErrScenarioNotFound, p.ID, name),
				hint:    msg.Text(msg.HintScenarioNotFound),
				details: map[string]any{"project": p.ID, "scenario": name},
			})
		}
		if *asJSON {
			return writeFlow(env, p.ID, d.Flow, fl.Only(s))
		}
		writeScenario(&b, fl, s)
	} else {
		if *asJSON {
			return writeFlow(env, p.ID, d.Flow, fl)
		}
		writeFlowText(&b, p.ID, d.Flow, fl)
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// writeFlowText prints the scenarios and stages of the flow as two tables.
func writeFlowText(b *strings.Builder, projectID, dir string, fl *flow.Flow) {
	fmt.Fprintln(b, msg.Text(msg.FlowProject, projectID))
	fmt.Fprintln(b, msg.Text(msg.FlowDir, dir))
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColScenario), msg.Text(msg.ColTitle)}}
	for _, s := range fl.Scenarios {
		rows = append(rows, []string{s.ID, oneLine(s.Title)})
	}
	writeTable(b, rows)
	b.WriteString("\n")
	rows = [][]string{{msg.Text(msg.ColStage), msg.Text(msg.ColTitle), msg.Text(msg.ColExecutor), msg.Text(msg.ColExit)}}
	for _, st := range fl.Stages {
		rows = append(rows, []string{st.ID, oneLine(st.Title), st.Executor, oneLine(st.Exit)})
	}
	writeTable(b, rows)
	fmt.Fprintln(b, msg.Text(msg.HintFlowScenario))
}

// writeScenario prints a scenario: its default path from left to right, its
// conditional transitions and its nodes. A full drawing of the graph is left
// to the panel: arrows of forks and several loops tangle in a terminal.
func writeScenario(b *strings.Builder, fl *flow.Flow, s flow.Scenario) {
	fmt.Fprintln(b, msg.Text(msg.FlowScenario, s.ID))
	fmt.Fprintln(b, msg.Text(msg.FlowTitle, oneLine(s.Title)))
	b.WriteString("\n")
	var path []string
	for _, id := range s.DefaultPath() {
		path = append(path, nodeName(id))
	}
	writeList(b, msg.Text(msg.FlowDefaultPath), []string{strings.Join(path, " → ")})

	var conditional []string
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			switch {
			case t.If == "":
			case t.MaxRounds > 0:
				conditional = append(conditional, msg.Text(msg.FlowTransitionLimit, n.ID, nodeName(t.To),
					msg.Count(msg.FlowRounds, t.MaxRounds), oneLine(t.If)))
			default:
				conditional = append(conditional, msg.Text(msg.FlowTransition, n.ID, nodeName(t.To), oneLine(t.If)))
			}
		}
	}
	if len(conditional) > 0 {
		b.WriteString("\n")
		writeList(b, msg.Text(msg.FlowConditional), conditional)
	}

	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColNode), msg.Text(msg.ColStage), msg.Text(msg.ColExecutor)}}
	for _, n := range s.Nodes {
		st, _ := fl.Stage(n.Stage)
		rows = append(rows, []string{n.ID, n.Stage, st.Executor})
	}
	writeTable(b, rows)
}

// nodeName returns the node as the scenario text names it: the end of the
// scenario is a word, not the reserved identifier.
func nodeName(id string) string {
	if id == flow.Finish {
		return msg.Text(msg.FlowEnd)
	}
	return id
}

// oneLine joins the lines of a text written as a block, so that a table row
// or a list item stays on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// writeFlow prints fl as the output of flow show --json.
func writeFlow(env Env, projectID, dir string, fl *flow.Flow) int {
	out := contract.FlowShowOutput{
		Project:   projectID,
		Dir:       dir,
		Scenarios: []contract.FlowScenario{},
		Stages:    []contract.FlowStage{},
		Parts:     append([]string{}, fl.Parts...),
	}
	for _, s := range fl.Scenarios {
		cs := contract.FlowScenario{Id: s.ID, Title: s.Title, Start: s.Start, Nodes: []contract.FlowNode{}}
		for _, n := range s.Nodes {
			cn := contract.FlowNode{Id: n.ID, Stage: n.Stage, Next: []contract.FlowTransition{}}
			for _, t := range n.Next {
				ct := contract.FlowTransition{To: t.To}
				if t.If != "" {
					cond := t.If
					ct.If = &cond
				}
				if t.MaxRounds > 0 {
					rounds := t.MaxRounds
					ct.MaxRounds = &rounds
				}
				cn.Next = append(cn.Next, ct)
			}
			cs.Nodes = append(cs.Nodes, cn)
		}
		out.Scenarios = append(out.Scenarios, cs)
	}
	for _, st := range fl.Stages {
		out.Stages = append(out.Stages, contract.FlowStage{
			Id: st.ID, Title: st.Title, Exit: st.Exit, Executor: st.Executor, Include: append([]string{}, st.Include...),
		})
	}
	if err := writeJSON(env, out); err != nil {
		return fail(env, internal(err))
	}
	return contract.ExitOK
}

// flowFailure turns an error of reading the flow of project projectID into a
// failure.
func flowFailure(err error, projectID string) failure {
	var (
		notFound *flow.NotFoundError
		invalid  *flow.InvalidError
		pathErr  *fs.PathError
	)
	switch {
	case errors.As(err, &notFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowNotFound,
			message: msg.Text(msg.ErrFlowNotFound, projectID),
			hint:    msg.Text(msg.HintFlowNotFound, notFound.Dir),
			details: map[string]any{"project": projectID, "dir": notFound.Dir},
		}
	case errors.As(err, &invalid):
		var more strings.Builder
		fmt.Fprintln(&more, msg.Text(msg.FlowDir, invalid.Dir))
		more.WriteString("\n")
		problems := make([]contract.FlowProblem, 0, len(invalid.Problems))
		var lines []string
		for _, p := range invalid.Problems {
			cp := contract.FlowProblem{Code: p.Code, Message: p.Message}
			if p.File != "" {
				file := p.File
				cp.File = &file
			}
			if p.Line > 0 {
				line := p.Line
				cp.Line = &line
			}
			problems = append(problems, cp)
			lines = append(lines, p.Message)
		}
		writeList(&more, msg.Text(msg.FlowProblems), lines)
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowInvalid,
			message: msg.Text(msg.ErrFlowInvalid, projectID),
			more:    more.String(),
			details: map[string]any{"project": projectID, "dir": invalid.Dir, "problems": problems},
		}
	case errors.As(err, &pathErr):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeIOError,
			message: msg.Text(msg.ErrIORead, pathErr.Path),
			details: map[string]any{"path": pathErr.Path},
		}
	}
	return internal(err)
}
