package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// The text of a task is built from the task as the contract has it. The
// contract names a stage by its identifier; the text names it by its title
// too, from the snapshot of the flow of the task: names.

// names are the stages of a task as its snapshot of the flow names them.
type names struct {
	read   bool                  // the snapshot is read
	stages map[string]flow.Stage // by identifier
	nodes  map[string]string     // the stage of each node of the scenario of the task
}

// namesOf returns the names of the snapshot of the task of v.
func namesOf(v task.View) names {
	n := names{stages: map[string]flow.Stage{}, nodes: map[string]string{}}
	if v.Flow == nil {
		return n
	}
	n.read = true
	for _, st := range v.Flow.Stages {
		n.stages[st.ID] = st
	}
	if sc, ok := v.Flow.Scenario(v.Scenario); ok {
		for _, node := range sc.Nodes {
			n.nodes[node.ID] = node.Stage
		}
	}
	return n
}

// title returns the title of stage, or "" if the snapshot has no such stage.
func (n names) title(stage string) string { return n.stages[stage].Title }

// nodeTitle returns the title of the stage of node, or "" if the snapshot
// has no such node or stage.
func (n names) nodeTitle(node string) string {
	stage, ok := n.nodes[node]
	if !ok {
		return ""
	}
	return n.title(stage)
}

// byOperator reports whether the operator executes stage.
func (n names) byOperator(stage string) bool {
	st, ok := n.stages[stage]
	return ok && st.Executor == flow.Operator
}

// stageLine is the current stage of t with its round, or a word that the
// scenario is passed.
func stageLine(t contract.Task) string {
	if t.Stage == nil {
		return msg.Text(msg.StageFinished)
	}
	return msg.Text(msg.StageRound, named(t.Stage.Title, t.Stage.Id), t.Stage.Round)
}

// progressLine is the progress of a task: 4 из 5, or 2 из 4–5 ahead of a
// fork.
func progressLine(p contract.TaskProgress) string {
	if p.TotalMin == p.TotalMax {
		return msg.Text(msg.ProgressValue, p.Passed, p.TotalMax)
	}
	return msg.Text(msg.ProgressRange, p.Passed, p.TotalMin, p.TotalMax)
}

// writeTaskFields prints the fields of t, one «name: value» a line: its
// state, scenario, stage, progress, times and worktree. The progress needs
// the snapshot.
func writeTaskFields(b *strings.Builder, t contract.Task, n names) {
	fmt.Fprintln(b, msg.Text(msg.TaskProject, t.Project))
	fmt.Fprintln(b, msg.Text(msg.TaskState, stateWord(string(t.State))))
	fmt.Fprintln(b, msg.Text(msg.TaskScenario, named(t.Scenario.Title, t.Scenario.Id)))
	fmt.Fprintln(b, msg.Text(msg.TaskStage, stageLine(t)))
	if n.read {
		fmt.Fprintln(b, msg.Text(msg.ProgressLine, progressLine(t.Progress)))
	}
	fmt.Fprintln(b, msg.Text(msg.TaskTakenAt, localTime(t.Taken)))
	if t.Ended != nil {
		fmt.Fprintln(b, endedLine(t))
	}
	fmt.Fprintln(b, msg.Text(msg.TaskFlowApplied, localTime(t.Flow.Time)))
	if t.Ended != nil && t.Ended.Reason != nil {
		fmt.Fprintln(b, msg.Text(msg.CancelReasonLine, oneLine(*t.Ended.Reason)))
	}
	if t.Worktree != nil {
		fmt.Fprintln(b, msg.Text(msg.TaskWorktree, *t.Worktree))
	}
}

// endedLine names who closed or cancelled t and when.
func endedLine(t contract.Task) string {
	agent := t.Ended.Source == contract.EndedSourceAgent
	k := msg.TaskClosedByOperator
	switch {
	case t.State == contract.TaskStateClosed && agent:
		k = msg.TaskClosedByAgent
	case t.State == contract.TaskStateCancelled && agent:
		k = msg.TaskCancelledByAgent
	case t.State == contract.TaskStateCancelled:
		k = msg.TaskCancelledByOperator
	}
	return msg.Text(k, localTime(t.Ended.Time))
}

// current reports whether p goes on: it has no outcome yet.
func current(p contract.TaskPass) bool { return p.Outcome == nil }

// writePath prints the path of task t as a table, one pass per row, and the
// steps of its current stage.
func writePath(b *strings.Builder, t contract.Task, path []contract.TaskPass, n names) {
	if len(path) == 0 {
		return
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColStage), msg.Text(msg.ColRound), msg.Text(msg.ColOutcome), msg.Text(msg.ColTransition)}}
	for _, p := range path {
		next := msg.Text(msg.ValueNone)
		if !current(p) {
			next = nodeName(*p.To)
		}
		rows = append(rows, []string{passStage(p, n), strconv.Itoa(p.Round), outcomeWord(t, p), next})
	}
	writeTable(b, rows)
	if last := path[len(path)-1]; current(last) {
		b.WriteString("\n")
		writeStepTable(b, last.Steps)
	}
}

// writePasses prints the path of task t in full, each pass a block: its
// outcome, exit, transition, reason, who closed it and its steps.
func writePasses(b *strings.Builder, t contract.Task, path []contract.TaskPass, n names) {
	for _, p := range path {
		b.WriteString("\n")
		fmt.Fprintln(b, msg.Text(msg.StageRound, named(n.title(p.Stage), p.Stage), p.Round))
		fmt.Fprintln(b, msg.Text(msg.OutcomeLine, outcomeWord(t, p)))
		if current(p) {
			b.WriteString("\n")
			writeStepTable(b, p.Steps)
			continue
		}
		if p.Exit != nil {
			fmt.Fprintln(b, msg.Text(msg.ExitTextLine, oneLine(p.Exit.Text)))
		}
		fmt.Fprintln(b, msg.Text(msg.TransitionLine, nodeName(*p.To)))
		if p.Reason != nil {
			fmt.Fprintln(b, msg.Text(msg.ReasonLine, oneLine(*p.Reason)))
		}
		fmt.Fprintln(b, msg.Text(msg.RecordedLine, recordedWord(p, n)))
		fmt.Fprintln(b, msg.Text(msg.ClosedLine, localTime(*p.Closed)))
		if len(p.Steps) > 0 {
			b.WriteString("\n")
			writeStepTable(b, p.Steps)
		}
	}
}

// passStage names the stage of a pass by its title, or by its identifier.
func passStage(p contract.TaskPass, n names) string {
	if t := oneLine(n.title(p.Stage)); t != "" {
		return t
	}
	return p.Stage
}

// outcomeWord names how a pass of task t was closed, or that it goes on; the
// pass a cancelled task stopped at has no outcome.
func outcomeWord(t contract.Task, p contract.TaskPass) string {
	switch {
	case current(p) && t.State == contract.TaskStateCancelled:
		return msg.Text(msg.ValueNone)
	case current(p):
		return msg.Text(msg.OutcomeCurrent)
	case p.Exit == nil:
		return msg.Text(msg.OutcomeSkip)
	}
	return exitWord(*p.Exit)
}

// exitWord names the kind of an exit for the operator.
func exitWord(e contract.TaskPassExit) string {
	artifact := ""
	if e.Artifact != nil {
		artifact = *e.Artifact
	}
	return exitKindWord(string(e.Kind), artifact)
}

// recordedWord names who closed a pass: a stage of the operator closed by
// the agent is recorded from the operator's words.
func recordedWord(p contract.TaskPass, n names) string {
	switch {
	case *p.Source != contract.TaskPassSourceAgent:
		return msg.Text(msg.TaskSourceOperator)
	case n.byOperator(p.Stage):
		return msg.Text(msg.TaskSourceAgent)
	}
	return msg.Text(msg.RecordedByAgent)
}

// writeStepTable prints steps as a table, or a line that there are none.
func writeStepTable(b *strings.Builder, steps []contract.TaskStep) {
	if len(steps) == 0 {
		fmt.Fprintln(b, msg.Text(msg.StepsNone))
		return
	}
	rows := [][]string{{msg.Text(msg.ColStepNumber), msg.Text(msg.ColState), msg.Text(msg.ColStep), msg.Text(msg.ColComment)}}
	for _, s := range steps {
		comment := s.Check
		if s.State == contract.TaskStepStateDropped {
			comment = s.Reason
		}
		c := ""
		if comment != nil {
			c = *comment
		}
		rows = append(rows, []string{strconv.Itoa(s.Number), stepStateWord(string(s.State)), s.Title, orNone(oneLine(c))})
	}
	writeTable(b, rows)
}

// stepStateWord names the state of a step for the operator.
func stepStateWord(s string) string {
	switch s {
	case state.StepDone:
		return msg.Text(msg.StepStateDone)
	case state.StepDropped:
		return msg.Text(msg.StepStateDropped)
	}
	return msg.Text(msg.StepStatePlanned)
}

// writeArtifacts prints the artifacts of a task as a table.
func writeArtifacts(b *strings.Builder, artifacts []contract.TaskArtifact) {
	rows := [][]string{{msg.Text(msg.ColArtifact), msg.Text(msg.ColKind), msg.Text(msg.ColSaved), msg.Text(msg.ColPlace)}}
	for _, a := range artifacts {
		kind, place := msg.Text(msg.ArtifactKindLink), ""
		if a.Url != nil {
			place = *a.Url
		}
		if a.Kind == contract.TaskArtifactKindFile {
			kind = msg.Text(msg.ArtifactKindFile)
			place = ""
			if a.Path != nil {
				place = *a.Path
			}
		}
		rows = append(rows, []string{a.Name, kind, localTime(a.Saved), place})
	}
	writeTable(b, rows)
}

// writeNotes prints notes, each under its number and stage, every line after
// indent.
func writeNotes(b *strings.Builder, notes []contract.TaskNote, n names, indent string) {
	for i, note := range notes {
		if i > 0 {
			b.WriteString("\n")
		}
		stage := msg.Text(msg.StageRound, named(n.title(note.Stage), note.Stage), note.Round)
		fmt.Fprintln(b, indent+msg.Text(msg.NoteHeading, note.Number, stage))
		writeIndented(b, note.Text, indent+strings.Repeat(" ", len(fmt.Sprintf("%d. ", note.Number))))
	}
}

// writeIndented prints a text written by a person as it is, each line with
// indent; empty lines stay empty.
func writeIndented(b *strings.Builder, text, indent string) {
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "%s%s\n", indent, line)
	}
}

// writeDecisions prints the decisions of the operator of a task under their
// heading, each a block: the stage and the round it was recorded at, who
// recorded it, the question with its options, the answer and the return it
// allows.
func writeDecisions(b *strings.Builder, decisions []contract.OperatorDecision, n names) {
	fmt.Fprintln(b, msg.Text(msg.DecisionsHeading))
	for i, d := range decisions {
		if i > 0 {
			b.WriteString("\n")
		}
		stage := msg.Text(msg.StageRound, named(n.title(d.Stage), d.Stage), d.Round)
		fmt.Fprintf(b, "  %s\n", msg.Text(msg.DecisionHeading, d.Number, stage, sourceWord(string(d.Source))))
		indent := strings.Repeat(" ", 2+len(fmt.Sprintf("%d. ", d.Number)))
		if d.Question != nil {
			writeField(b, indent, msg.Text(msg.DecisionQuestion), *d.Question)
		}
		if len(d.Options) > 0 {
			fmt.Fprintf(b, "%s%s\n", indent, msg.Text(msg.DecisionOptions))
			for j, o := range d.Options {
				number := fmt.Sprintf("%d. ", j+1)
				label := oneLine(o.Label)
				if o.Recommended != nil && *o.Recommended {
					label = msg.Text(msg.OptionRecommended, label)
				}
				desc := ""
				if o.Description != nil {
					desc = *o.Description
				}
				writeField(b, indent+"  "+number, label, desc)
			}
		}
		writeField(b, indent, msg.Text(msg.DecisionAnswer), d.Answer)
		if d.AllowReturn != nil {
			fmt.Fprintf(b, "%s%s\n", indent, msg.Text(msg.AllowedReturnLine, named(n.nodeTitle(*d.AllowReturn), *d.AllowReturn)))
		}
	}
}
