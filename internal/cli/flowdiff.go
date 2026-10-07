package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// runFlowDiff shows how the drafts of the flow of a project and of the
// library differ from the active ones: a summary in the terminal, and the
// file of changes with the schemas of the scenarios and the texts in full.
func runFlowDiff(args []string, env Env) int {
	f := newFlags("flow diff")
	proj := f.String("project")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if proj.Set && proj.Value == "" {
		return fail(env, flagValueMissing("--project"))
	}
	r, places, bad := openFlow(proj.Value)
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := syncFlow(env, r)
	if bad != nil {
		return fail(env, *bad)
	}
	res, err := flow.DiffDraft(r, places)
	var none *flow.NoDraftsError
	switch {
	case errors.As(err, &none):
		return fail(env, failure{
			exit:    contract.ExitError,
			code:    contract.CodeFlowDraftNotFound,
			message: msg.Text(msg.ErrNoDrafts, places.Project),
			more:    msg.Text(msg.FlowDir, none.Dir) + "\n",
			details: map[string]any{"project": places.Project, "dir": none.Dir},
		})
	case err != nil:
		return fail(env, flowFailure(err, places))
	}
	d := diffView{project: places.Project, res: res}
	report := r.ChangesFile(places.Project)
	if err := r.WriteChanges(places.Project, d.report(time.Now())); err != nil {
		return fail(env, failure{
			exit:    contract.ExitError,
			code:    contract.CodeIOError,
			message: msg.Text(msg.ErrReportWrite, report),
			details: map[string]any{"path": report},
		})
	}
	if *asJSON {
		out := contract.FlowDiffOutput{Project: places.Project, Applied: appliedJSON(res.Applied), Changes: []contract.FlowChange{}, Report: report, Sync: syncJSON(s)}
		for _, c := range res.Changes {
			cc := contract.FlowChange{
				Object: contract.FlowDiffOutputChangesElemObject(c.Object),
				Change: contract.FlowDiffOutputChangesElemChange(c.Change),
			}
			if c.ID != "" {
				id := c.ID
				cc.Id = &id
			}
			out.Changes = append(out.Changes, cc)
		}
		if lib := res.Library; lib != nil {
			out.Library = &contract.FlowDiffOutputLibrary{Applied: appliedJSON(lib.Applied), Changes: []contract.FlowDiffLibraryChange{}}
			for _, c := range lib.Changes {
				out.Library.Changes = append(out.Library.Changes, contract.FlowDiffLibraryChange{
					Id: c.ID, Change: contract.FlowDiffOutputLibraryChangesElemChange(c.Change), Projects: c.Projects,
				})
			}
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprint(env.Stdout, d.summary(report))
	return contract.ExitOK
}

// diffView shows the result of flow.DiffDraft in the words of the operator.
type diffView struct {
	project string
	res     flow.DiffResult
}

// mark is the sign of a change in the summary.
func mark(change string) string {
	return map[string]string{flow.Added: "+", flow.Modified: "~", flow.Removed: "−"}[change]
}

// summary is the text of flow diff in the terminal: blocks separated by an
// empty line, the hints last.
func (d diffView) summary(report string) string {
	res := d.res
	var blocks []string
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.FlowProject, d.project))
	writeFlowApplied(&b, appliedJSON(res.Applied))
	if res.Library != nil {
		fmt.Fprintln(&b, msg.Text(msg.LibraryAppliedAt, appliedText(res.Library.Applied)))
	}
	fmt.Fprintln(&b, msg.Text(msg.DiffReportFile, report))
	blocks = append(blocks, b.String())

	if !res.Draft {
		blocks = append(blocks, msg.Text(msg.DiffFlowUnchanged)+"\n")
	}
	var scenarios, stages, parts, agents []string
	for _, c := range res.Changes {
		switch c.Object {
		case flow.ObjectCommon:
			blocks = append(blocks, msg.Text(msg.FlowChange, msg.Text(msg.FlowObjCommon), changeWord(c))+"\n")
		case flow.ObjectStage:
			stages = append(stages, mark(c.Change)+" "+d.stageName(c))
		case flow.ObjectPart:
			parts = append(parts, mark(c.Change)+" "+c.ID)
		case flow.ObjectAgent:
			agents = append(agents, mark(c.Change)+" "+c.ID)
		}
	}
	for _, sd := range res.Scenarios {
		scenarios = append(scenarios, d.scenarioLines(sd)...)
	}
	var library []string
	if res.Library != nil {
		for _, c := range res.Library.Changes {
			library = append(library, mark(c.Change)+" "+msg.Text(msg.DiffItem, c.ID, usedBy(c.Projects)))
		}
	}
	for _, sec := range []struct {
		heading msg.Key
		lines   []string
	}{
		{msg.DiffScenarios, scenarios}, {msg.DiffStages, stages}, {msg.DiffParts, parts},
		{msg.DiffAgents, agents}, {msg.DiffLibraryAgents, library},
	} {
		if len(sec.lines) == 0 {
			continue
		}
		var sb strings.Builder
		writeList(&sb, msg.Text(sec.heading), sec.lines)
		blocks = append(blocks, sb.String())
	}
	if problems := d.problems(); len(problems) > 0 {
		var sb strings.Builder
		writeProblems(&sb, problems)
		blocks = append(blocks, sb.String())
	}
	blocks = append(blocks, msg.Text(msg.DiffLegend)+"\n")
	if hints := d.hints(); len(hints) > 0 {
		blocks = append(blocks, strings.Join(hints, "\n")+"\n")
	}
	return strings.Join(blocks, "\n")
}

// problems are the problems of the flow draft and of the library draft.
func (d diffView) problems() []flow.Problem {
	out := append([]flow.Problem{}, d.res.Problems...)
	if d.res.Library != nil {
		out = append(out, d.res.Library.Problems...)
	}
	return out
}

// hints say what to do next: apply the drafts without problems, look at the
// flow draft with them.
func (d diffView) hints() []string {
	var out []string
	if lib := d.res.Library; lib != nil && len(lib.Problems) == 0 {
		out = append(out, msg.Text(msg.HintDiffLibraryApply))
	}
	switch {
	case !d.res.Draft:
	case len(d.res.Problems) > 0:
		out = append(out, msg.Text(msg.HintFlowShowDraft))
	default:
		out = append(out, msg.Text(msg.HintFlowApply))
	}
	return out
}

// usedBy names the projects a subagent of the library concerns.
func usedBy(projects []string) string {
	if len(projects) == 0 {
		return msg.Text(msg.DiffUnused)
	}
	return msg.Text(msg.DiffUsedBy, strings.Join(projects, ", "))
}

// stageName names a changed stage by its title and identifier: from the
// draft, or from the active flow if it is removed.
func (d diffView) stageName(c flow.Change) string {
	fl := d.res.New
	if c.Change == flow.Removed {
		fl = d.res.Old
	}
	if st, ok := fl.Stage(c.ID); ok && strings.TrimSpace(st.Title) != "" {
		return msg.Text(msg.DiffStage, oneLine(st.Title), c.ID)
	}
	return c.ID
}

// scenarioTitle is the title of a scenario, or its identifier without one.
func scenarioTitle(s *flow.Scenario) string {
	if t := oneLine(s.Title); t != "" {
		return t
	}
	return s.ID
}

// nodeTitle names node id of scenario s by the title of its stage in fl.
func nodeTitle(fl *flow.Flow, s *flow.Scenario, id string) string {
	if id == flow.Finish {
		return msg.Text(msg.FlowEnd)
	}
	for _, n := range s.Nodes {
		if n.ID != id {
			continue
		}
		if st, ok := fl.Stage(n.Stage); ok && strings.TrimSpace(st.Title) != "" {
			return oneLine(st.Title)
		}
		if n.Stage != "" {
			return n.Stage
		}
	}
	return id
}

// scenarioLines are the lines of a changed scenario in the summary: its
// default path with the marks of its nodes, then its conditional
// transitions that changed, indented.
func (d diffView) scenarioLines(sd flow.ScenarioDiff) []string {
	switch sd.Change {
	case flow.Removed:
		return []string{mark(flow.Removed) + " " + scenarioTitle(sd.Old)}
	case flow.Added:
		line := mark(flow.Added) + " " + scenarioTitle(sd.New)
		if sd.Unreadable {
			return []string{msg.Text(msg.DiffItem, line, msg.Text(msg.DiffScenarioUnreadable))}
		}
		lines := []string{msg.Text(msg.DiffItem, line, d.path(sd.New, nil))}
		for _, td := range addedTransitions(sd.New) {
			if l, ok := d.transitionLine(sd, td); ok {
				lines = append(lines, "  "+l)
			}
		}
		return lines
	}
	if sd.Unreadable {
		return []string{msg.Text(msg.DiffItem, scenarioTitle(sd.New), msg.Text(msg.DiffScenarioUnreadable))}
	}
	lines := []string{msg.Text(msg.DiffItem, scenarioTitle(sd.New), d.path(sd.New, sd.Nodes))}
	for _, td := range sd.Transitions {
		if l, ok := d.transitionLine(sd, td); ok {
			lines = append(lines, "  "+l)
		}
	}
	return lines
}

// addedTransitions are the transitions of a new scenario, all added.
func addedTransitions(s *flow.Scenario) []flow.TransitionDiff {
	var out []flow.TransitionDiff
	for _, n := range s.Nodes {
		for _, t := range n.Next {
			out = append(out, flow.TransitionDiff{From: n.ID, To: t.To, Change: flow.Added, New: t, Return: t.MaxRounds > 0})
		}
	}
	return out
}

// path is the default path of scenario s by the titles of its stages, the
// end left out; marks are the changes of its nodes, if any.
func (d diffView) path(s *flow.Scenario, marks map[string]string) string {
	var names []string
	for _, id := range s.DefaultPath() {
		if id == flow.Finish {
			continue
		}
		name := nodeTitle(d.res.New, s, id)
		if m := marks[id]; m == flow.Added || m == flow.Modified {
			name = mark(m) + " " + name
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return msg.Text(msg.ValueNone)
	}
	return strings.Join(names, " → ")
}

// transitionSubject names a transition of a scenario diff: a return or a
// transition, from and to the titles of the stages of its nodes.
func (d diffView) transitionSubject(sd flow.ScenarioDiff, td flow.TransitionDiff) string {
	fl, s := d.res.New, sd.New
	if td.Change == flow.Removed {
		fl, s = d.res.Old, sd.Old
	}
	k := msg.DiffTransition
	if td.Return {
		k = msg.DiffReturn
	}
	return msg.Text(k, nodeTitle(fl, s, td.From), nodeTitle(fl, s, td.To))
}

// conditionDetails are the condition and the limit of a transition.
func conditionDetails(t flow.Transition) []string {
	out := []string{msg.Text(msg.DiffCondition, oneLine(t.If))}
	if t.MaxRounds > 0 {
		out = append(out, msg.Count(msg.DiffRounds, t.MaxRounds))
	}
	return out
}

// transitionChange tells what changed in a transition with a condition
// before and after: the condition, the limit. ok is false if the change is
// of neither.
func transitionChange(old, cur flow.Transition) (details []string, ok bool) {
	if old.If != cur.If {
		details = append(details, msg.Text(msg.DiffCondition, oneLine(cur.If)))
	}
	if old.MaxRounds != cur.MaxRounds && cur.MaxRounds > 0 {
		rounds := msg.Count(msg.DiffRounds, cur.MaxRounds)
		if old.MaxRounds > 0 {
			rounds = msg.Text(msg.DiffInstead, rounds, old.MaxRounds)
		}
		details = append(details, rounds)
	}
	return details, len(details) > 0
}

// transitionLine is the line of a changed transition in the summary; ok is
// false for a transition without a condition before and after: the path
// shows those.
func (d diffView) transitionLine(sd flow.ScenarioDiff, td flow.TransitionDiff) (string, bool) {
	before := td.Change != flow.Added && td.Old.If != ""
	after := td.Change != flow.Removed && td.New.If != ""
	subject := d.transitionSubject(sd, td)
	switch {
	case after && !before:
		return mark(flow.Added) + " " + subject + ": " + strings.Join(conditionDetails(td.New), ", "), true
	case before && !after:
		return mark(flow.Removed) + " " + subject, true
	case before && after:
		details, ok := transitionChange(td.Old, td.New)
		if !ok {
			return "", false
		}
		return mark(flow.Modified) + " " + subject + ": " + strings.Join(details, ", "), true
	}
	return "", false
}

// runFlowGuide prints the guide to the format of the flow: a text to read,
// without JSON, as the help.
func runFlowGuide(args []string, env Env) int {
	f := newFlags("flow guide")
	if code, done := f.parse(args, env); done {
		return code
	}
	fmt.Fprint(env.Stdout, agenttext.FlowGuide())
	return contract.ExitOK
}
