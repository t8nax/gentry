package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
)

// report is the file of changes: the summary of flow diff as a table, a
// schema of each changed scenario in Mermaid and the changed objects in full,
// in markdown. Lines of a block of "Поле: значение" end with two spaces, a
// line break of markdown.
func (d diffView) report(now time.Time) string {
	res := d.res
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.ReportTitle, d.project))
	b.WriteString("\n")
	head := []string{msg.Text(msg.FlowProject, d.project), flowAppliedLine(d.res)}
	if res.Library != nil {
		head = append(head, msg.Text(msg.LibraryAppliedAt, appliedText(res.Library.Applied)))
	}
	problems := d.problems()
	if len(problems) == 0 {
		head = append(head, msg.Text(msg.ReportProblemsNone))
	}
	head = append(head, msg.Text(msg.ReportCreated, localTime(now)))
	writeLines(&b, head)
	if len(problems) > 0 {
		fmt.Fprintf(&b, "\n%s\n\n", msg.Text(msg.ReportProblems))
		for _, p := range problems {
			fmt.Fprintf(&b, "- %s\n", p.Message)
		}
	}
	if hints := d.hints(); len(hints) > 0 {
		b.WriteString("\n")
		writeLines(&b, hints)
	}

	fmt.Fprintf(&b, "\n%s\n\n", msg.Text(msg.ReportSummary))
	if !res.Draft {
		fmt.Fprintf(&b, "%s\n\n", msg.Text(msg.DiffFlowUnchanged))
	}
	rows := [][]string{{msg.Text(msg.ColObject), msg.Text(msg.ColChange)}}
	for _, c := range res.Changes {
		rows = append(rows, []string{d.objectName(c), changeWord(c)})
	}
	if res.Library != nil {
		for _, c := range res.Library.Changes {
			word := changeWord(flow.Change{Object: flow.ObjectAgent, Change: c.Change})
			rows = append(rows, []string{msg.Text(msg.FlowObjLibraryAgent, c.ID), word + "; " + usedBy(c.Projects)})
		}
	}
	if len(rows) > 1 {
		writeMarkdownTable(&b, rows)
	}
	if len(res.Scenarios) > 0 {
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.ReportLegend))
	}

	for _, c := range res.Changes {
		if c.Object == flow.ObjectCommon {
			fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.ReportHeadingChange, msg.Text(msg.FlowObjCommon), changeWord(c)))
		}
	}
	for _, sd := range res.Scenarios {
		d.writeScenario(&b, sd)
	}
	for _, c := range res.Changes {
		switch c.Object {
		case flow.ObjectStage:
			d.writeStage(&b, c)
		case flow.ObjectPart:
			d.writePart(&b, c)
		case flow.ObjectAgent:
			fmt.Fprintf(&b, "\n%s\n\n", msg.Text(msg.ReportHeadingChange, msg.Text(msg.FlowObjAgent, c.ID), changeWord(c)))
			d.writeAgent(&b, c.Change, projectAgent(res.Old, c.ID), projectAgent(res.New, c.ID), func(fl *flow.Flow) string {
				return msg.Text(msg.FlowStages, joined(fl.StagesBy(c.ID)))
			})
		}
	}
	if res.Library != nil && len(res.Library.Changes) > 0 {
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.ReportLibrary))
		for _, c := range res.Library.Changes {
			word := changeWord(flow.Change{Object: flow.ObjectAgent, Change: c.Change})
			fmt.Fprintf(&b, "\n%s\n\n", msg.Text(msg.ReportLibraryAgent, c.ID, word))
			projects := msg.Text(msg.ReportProjects, joined(c.Projects))
			d.writeAgent(&b, c.Change, c.Old, c.New, func(*flow.Flow) string { return projects })
		}
	}
	return b.String()
}

// flowAppliedLine is the line of when the active flow was applied.
func flowAppliedLine(res flow.DiffResult) string {
	return msg.Text(msg.FlowAppliedAt, appliedText(res.Applied))
}

// writeLines prints lines as one block of markdown, each on a line of its own.
func writeLines(b *strings.Builder, lines []string) {
	for i, l := range lines {
		if i < len(lines)-1 {
			l += "  "
		}
		fmt.Fprintln(b, l)
	}
}

// writeMarkdownTable prints rows, the header first, as a table of markdown.
func writeMarkdownTable(b *strings.Builder, rows [][]string) {
	for i, r := range rows {
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = strings.ReplaceAll(c, "|", `\|`)
		}
		fmt.Fprintf(b, "| %s |\n", strings.Join(cells, " | "))
		if i == 0 {
			fmt.Fprintf(b, "|%s\n", strings.Repeat(" --- |", len(r)))
		}
	}
}

// objectName names a changed object of the flow in the table of the file.
func (d diffView) objectName(c flow.Change) string {
	switch c.Object {
	case flow.ObjectScenario:
		return msg.Text(msg.ReportObjScenario, d.scenarioTitleOf(c), c.ID)
	case flow.ObjectStage:
		if title := d.stageTitle(c); title != "" {
			return msg.Text(msg.ReportObjStage, title, c.ID)
		}
		return msg.Text(msg.FlowObjStage, c.ID)
	}
	return changeObject(c)
}

// scenarioTitleOf is the title of a changed scenario.
func (d diffView) scenarioTitleOf(c flow.Change) string {
	fl := d.res.New
	if c.Change == flow.Removed {
		fl = d.res.Old
	}
	if s, ok := fl.Scenario(c.ID); ok {
		return scenarioTitle(&s)
	}
	return c.ID
}

// stageTitle is the title of a changed stage; empty if it has none.
func (d diffView) stageTitle(c flow.Change) string {
	fl := d.res.New
	if c.Change == flow.Removed {
		fl = d.res.Old
	}
	st, _ := fl.Stage(c.ID)
	return oneLine(st.Title)
}

// writeScenario prints the section of a changed scenario: its schema and,
// for a modified one, what changed in words.
func (d diffView) writeScenario(b *strings.Builder, sd flow.ScenarioDiff) {
	s, fl := sd.New, d.res.New
	if sd.Change == flow.Removed {
		s, fl = sd.Old, d.res.Old
	}
	name := msg.Text(msg.ReportObjScenario, scenarioTitle(s), sd.ID)
	if sd.Change == flow.Modified {
		fmt.Fprintf(b, "\n%s\n\n", msg.Text(msg.ReportHeading, name))
	} else {
		fmt.Fprintf(b, "\n%s\n\n", msg.Text(msg.ReportHeadingChange, name, changeWord(flow.Change{Object: flow.ObjectScenario, Change: sd.Change})))
	}
	if sd.Unreadable {
		fmt.Fprintln(b, msg.Text(msg.ReportUnreadable))
		return
	}
	g := schema{new: fl, old: d.res.Old, s: s}
	if sd.Change == flow.Modified {
		g.diff = &sd
	}
	b.WriteString(g.mermaid())
	if sd.Change != flow.Modified {
		return
	}
	var lines []string
	if sd.Old.Title != sd.New.Title {
		lines = append(lines, msg.Text(msg.ReportTitleChanged, scenarioTitle(sd.New), scenarioTitle(sd.Old)))
	}
	if sd.Old.Start != sd.New.Start {
		lines = append(lines, msg.Text(msg.ReportStartChanged, sd.New.Start, sd.Old.Start))
	}
	for _, n := range sd.New.Nodes {
		switch sd.Nodes[n.ID] {
		case flow.Added:
			lines = append(lines, msg.Text(msg.ReportNodeAdded, nodeTitle(d.res.New, sd.New, n.ID)))
		case flow.Modified:
			old := nodeTitle(d.res.Old, sd.Old, n.ID)
			if o := nodeOf(sd.Old, n.ID); o.Stage != n.Stage {
				lines = append(lines, msg.Text(msg.ReportNodeStage, n.ID, old, nodeTitle(d.res.New, sd.New, n.ID)))
			} else {
				lines = append(lines, msg.Text(msg.ReportNodeStageChanged, nodeTitle(d.res.New, sd.New, n.ID)))
			}
		}
	}
	for _, n := range sd.Old.Nodes {
		if sd.Nodes[n.ID] == flow.Removed {
			lines = append(lines, msg.Text(msg.ReportNodeRemoved, nodeTitle(d.res.Old, sd.Old, n.ID)))
		}
	}
	for _, td := range sd.Transitions {
		if l, ok := d.transitionSentence(sd, td); ok {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n\n", msg.Text(msg.ReportChanges))
	for _, l := range lines {
		fmt.Fprintf(b, "- %s\n", l)
	}
}

// nodeOf returns node id of scenario s.
func nodeOf(s *flow.Scenario, id string) flow.Node {
	for _, n := range s.Nodes {
		if n.ID == id {
			return n
		}
	}
	return flow.Node{}
}

// transitionSentence tells in a sentence how a transition changed.
func (d diffView) transitionSentence(sd flow.ScenarioDiff, td flow.TransitionDiff) (string, bool) {
	subject := d.transitionSubject(sd, td)
	switch td.Change {
	case flow.Added:
		if td.New.If != "" {
			subject += ": " + strings.Join(conditionDetails(td.New), ", ")
		}
		return msg.Text(msg.ReportAdded, subject), true
	case flow.Removed:
		return msg.Text(msg.ReportRemoved, subject), true
	}
	var details []string
	switch {
	case td.New.If == "":
		details = []string{msg.Text(msg.DiffNoCondition)}
	case td.Old.If == "":
		details = conditionDetails(td.New)
	default:
		details, _ = transitionChange(td.Old, td.New)
	}
	if len(details) == 0 {
		return "", false
	}
	return msg.Text(msg.ReportModified, subject, strings.Join(details, ", ")), true
}

// writeStage prints the section of a changed stage.
func (d diffView) writeStage(b *strings.Builder, c flow.Change) {
	name := msg.Text(msg.FlowObjStage, c.ID)
	if title := d.stageTitle(c); title != "" {
		name = msg.Text(msg.ReportObjStage, title, c.ID)
	}
	fmt.Fprintf(b, "\n%s\n\n", msg.Text(msg.ReportHeadingChange, name, changeWord(c)))
	old, oldOK := d.res.Old.Stage(c.ID)
	cur, curOK := d.res.New.Stage(c.ID)
	switch {
	case c.Change == flow.Removed && oldOK:
		writeStageFields(b, old, d.res.Old)
		writeQuoted(b, msg.Text(msg.FlowInstruction), old.Instruction)
	case c.Change == flow.Added && curOK:
		writeStageFields(b, cur, d.res.New)
		writeQuoted(b, msg.Text(msg.FlowInstruction), cur.Instruction)
	case c.Change == flow.Modified && oldOK && curOK:
		fmt.Fprintf(b, "%s\n", msg.Text(msg.FlowScenarios, joined(d.res.New.ScenariosOf(c.ID))))
		writeFieldChanges(b, []fieldChange{
			{msg.ReportFieldTitle, oneLine(old.Title), oneLine(cur.Title)},
			{msg.ReportFieldExit, oneLine(old.Exit), oneLine(cur.Exit)},
			{msg.ReportFieldExecutor, old.Executor, cur.Executor},
			{msg.ReportFieldParts, joined(old.Include), joined(cur.Include)},
		})
		writeTextDiff(b, msg.Text(msg.FlowInstruction), old.Instruction, cur.Instruction)
	}
}

// writeStageFields prints the fields of stage st of flow fl.
func writeStageFields(b *strings.Builder, st flow.Stage, fl *flow.Flow) {
	writeLines(b, []string{
		msg.Text(msg.FlowExit, orNone(oneLine(st.Exit))),
		msg.Text(msg.FlowExecutor, orNone(st.Executor)),
		msg.Text(msg.FlowParts, joined(st.Include)),
		msg.Text(msg.FlowScenarios, joined(fl.ScenariosOf(st.ID))),
	})
}

// writePart prints the section of a changed part.
func (d diffView) writePart(b *strings.Builder, c flow.Change) {
	fmt.Fprintf(b, "\n%s\n\n", msg.Text(msg.ReportHeadingChange, msg.Text(msg.FlowObjPart, c.ID), changeWord(c)))
	old, oldOK := d.res.Old.Part(c.ID)
	cur, curOK := d.res.New.Part(c.ID)
	switch {
	case c.Change == flow.Removed && oldOK:
		fmt.Fprintf(b, "%s\n", msg.Text(msg.FlowStages, joined(d.res.Old.StagesOf(c.ID))))
		writeQuoted(b, msg.Text(msg.FlowText), old.Text)
	case c.Change == flow.Added && curOK:
		fmt.Fprintf(b, "%s\n", msg.Text(msg.FlowStages, joined(d.res.New.StagesOf(c.ID))))
		writeQuoted(b, msg.Text(msg.FlowText), cur.Text)
	case c.Change == flow.Modified && oldOK && curOK:
		fmt.Fprintf(b, "%s\n", msg.Text(msg.FlowStages, joined(d.res.New.StagesOf(c.ID))))
		writeTextDiff(b, msg.Text(msg.FlowText), old.Text, cur.Text)
	}
}

// projectAgent returns subagent id of the project in flow fl; nil if there
// is none.
func projectAgent(fl *flow.Flow, id string) *flow.Agent {
	for _, a := range fl.Agents {
		if a.ID == id && !a.Library {
			return &a
		}
	}
	return nil
}

// writeAgent prints the fields of a changed subagent. lead is the line
// about where it is used: the stages of a subagent of the project, the
// projects of one of the library.
func (d diffView) writeAgent(b *strings.Builder, change string, old, cur *flow.Agent, lead func(*flow.Flow) string) {
	fields := func(a *flow.Agent, fl *flow.Flow) {
		writeLines(b, []string{
			msg.Text(msg.FlowPurpose, orNone(oneLine(a.Purpose))),
			msg.Text(msg.FlowCapabilities, joined(a.Capabilities)),
			lead(fl),
		})
		writeQuoted(b, msg.Text(msg.FlowInstruction), a.Instruction)
	}
	switch {
	case change == flow.Removed && old != nil:
		fields(old, d.res.Old)
	case change == flow.Added && cur != nil:
		fields(cur, d.res.New)
	case change == flow.Modified && old != nil && cur != nil:
		fmt.Fprintln(b, lead(d.res.New))
		writeFieldChanges(b, []fieldChange{
			{msg.ReportFieldPurpose, oneLine(old.Purpose), oneLine(cur.Purpose)},
			{msg.ReportFieldCapabilities, joined(old.Capabilities), joined(cur.Capabilities)},
		})
		writeTextDiff(b, msg.Text(msg.FlowInstruction), old.Instruction, cur.Instruction)
	default:
		fmt.Fprintln(b, lead(d.res.New))
	}
}

// fieldChange is a field of an object before and after.
type fieldChange struct {
	name     msg.Key
	was, now string
}

// writeFieldChanges prints the fields that changed as a table.
func writeFieldChanges(b *strings.Builder, fields []fieldChange) {
	rows := [][]string{{msg.Text(msg.ColField), msg.Text(msg.ColWas), msg.Text(msg.ColNow)}}
	for _, f := range fields {
		if f.was != f.now {
			rows = append(rows, []string{msg.Text(f.name), orNone(f.was), orNone(f.now)})
		}
	}
	if len(rows) > 1 {
		b.WriteString("\n")
		writeMarkdownTable(b, rows)
	}
}

// writeQuoted prints a text written by the operator under heading as a
// quote, so that its markdown keeps its look.
func writeQuoted(b *strings.Builder, heading, text string) {
	fmt.Fprintf(b, "\n%s\n\n", heading)
	text = strings.TrimRight(normalizeText(text), "\n")
	if strings.TrimSpace(text) == "" {
		fmt.Fprintf(b, "%s\n", msg.Text(msg.ValueNone))
		return
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString(">\n")
			continue
		}
		fmt.Fprintf(b, "> %s\n", strings.TrimRight(line, " \t"))
	}
}

// writeTextDiff prints how a text changed under heading as a diff by lines;
// nothing if it did not.
func writeTextDiff(b *strings.Builder, heading, old, cur string) {
	old, cur = normalizeText(old), normalizeText(cur)
	if old == cur {
		return
	}
	lines := lineDiff(splitLines(old), splitLines(cur))
	fence := "```"
	for strings.Contains(old+cur, fence) {
		fence += "`"
	}
	fmt.Fprintf(b, "\n%s\n\n%sdiff\n", heading, fence)
	for _, l := range lines {
		fmt.Fprintln(b, l)
	}
	fmt.Fprintln(b, fence)
}

// normalizeText gives a text line ends of \n.
func normalizeText(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// splitLines splits a text into lines without a last empty one.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lineDiff returns the lines of a and b as a unified diff without headers:
// a line of both starts with a space, of a alone with "-", of b alone with
// "+". It finds the longest common subsequence: instructions are short.
func lineDiff(a, b []string) []string {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, " "+a[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "-"+a[i])
			i++
		default:
			out = append(out, "+"+b[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+a[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+b[j])
	}
	return out
}

// schema draws scenario s of flow new in Mermaid. With diff, the nodes and
// transitions that changed are colored, and the removed ones of the active
// flow old are drawn too.
type schema struct {
	new, old *flow.Flow
	s        *flow.Scenario
	diff     *flow.ScenarioDiff
}

// Colors of the schemas: added, changed, removed.
const (
	colorAdded   = "#2e7d32"
	colorChanged = "#b8860b"
	colorRemoved = "#c62828"
)

func (g schema) mermaid() string {
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart TD\n")
	ids := map[string]string{}
	var classes []struct{ id, class string }
	node := func(fl *flow.Flow, s *flow.Scenario, n flow.Node, class string) {
		id := fmt.Sprintf("n%d", len(ids)+1)
		ids[n.ID] = id
		label := mermaidText(nodeTitle(fl, s, n.ID))
		if st, ok := fl.Stage(n.Stage); ok {
			switch st.Executor {
			case flow.Orchestrator, "":
			case flow.Operator:
				label += "<br/>" + mermaidText(msg.Text(msg.SchemaExecutor, msg.Text(msg.SchemaOperator)))
			default:
				label += "<br/>" + mermaidText(msg.Text(msg.SchemaExecutor, st.Executor))
			}
		}
		fmt.Fprintf(&b, "    %s[\"%s\"]\n", id, label)
		if class != "" {
			classes = append(classes, struct{ id, class string }{id, class})
		}
	}
	nodeClass := func(id string) string {
		if g.diff == nil {
			return ""
		}
		return map[string]string{flow.Added: "added", flow.Modified: "changed", flow.Removed: "removed"}[g.diff.Nodes[id]]
	}
	for _, n := range g.s.Nodes {
		node(g.new, g.s, n, nodeClass(n.ID))
	}
	if g.diff != nil {
		for _, n := range g.diff.Old.Nodes {
			if g.diff.Nodes[n.ID] == flow.Removed {
				node(g.old, g.diff.Old, n, "removed")
			}
		}
	}

	type edge struct {
		from, to string
		t        flow.Transition
		was      int // the limit before, if it changed
		change   string
	}
	var edges []edge
	changes := map[[2]string]flow.TransitionDiff{}
	if g.diff != nil {
		for _, td := range g.diff.Transitions {
			changes[[2]string{td.From, td.To}] = td
		}
	}
	for _, n := range g.s.Nodes {
		for _, t := range n.Next {
			e := edge{from: n.ID, to: t.To, t: t}
			if td, ok := changes[[2]string{n.ID, t.To}]; ok {
				e.change = td.Change
				if td.Change == flow.Modified && td.Old.MaxRounds > 0 && td.Old.MaxRounds != t.MaxRounds {
					e.was = td.Old.MaxRounds
				}
			}
			edges = append(edges, e)
		}
	}
	if g.diff != nil {
		for _, td := range g.diff.Transitions {
			if td.Change == flow.Removed {
				edges = append(edges, edge{from: td.From, to: td.To, t: td.Old, change: flow.Removed})
			}
		}
	}
	for _, e := range edges {
		if e.to == flow.Finish && ids[flow.Finish] == "" {
			ids[flow.Finish] = "fin"
			fmt.Fprintf(&b, "    fin((\"%s\"))\n", mermaidText(msg.Text(msg.FlowEnd)))
		}
	}
	styles := map[string][]string{}
	drawn := 0 // linkStyle counts the links drawn
	for _, e := range edges {
		from, to := ids[e.from], ids[e.to]
		if from == "" || to == "" {
			continue
		}
		i := drawn
		drawn++
		if e.t.If == "" {
			fmt.Fprintf(&b, "    %s --> %s\n", from, to)
		} else {
			label := mermaidText(oneLine(e.t.If))
			if e.t.MaxRounds > 0 {
				label += "<br/>" + mermaidText(msg.Count(msg.DiffRounds, e.t.MaxRounds))
				if e.was > 0 {
					label += " " + mermaidText(msg.Text(msg.SchemaWas, e.was))
				}
			}
			fmt.Fprintf(&b, "    %s -.->|\"%s\"| %s\n", from, label, to)
		}
		if e.change != "" {
			styles[e.change] = append(styles[e.change], fmt.Sprint(i))
		}
	}
	if len(classes) > 0 || len(styles) > 0 {
		b.WriteString("\n")
	}
	defs := []struct{ class, change, style, link string }{
		{"added", flow.Added, "fill:#d4f4dd,stroke:" + colorAdded + ",color:#1b1b1b", "stroke:" + colorAdded + ",stroke-width:2px"},
		{"changed", flow.Modified, "fill:#fff4c2,stroke:" + colorChanged + ",color:#1b1b1b", "stroke:" + colorChanged + ",stroke-width:2px"},
		{"removed", flow.Removed, "fill:#fde2e2,stroke:" + colorRemoved + ",stroke-dasharray:5 5,color:#1b1b1b", "stroke:" + colorRemoved + ",stroke-width:2px,stroke-dasharray:5 5"},
	}
	for _, def := range defs {
		var members []string
		for _, c := range classes {
			if c.class == def.class {
				members = append(members, c.id)
			}
		}
		if len(members) > 0 {
			fmt.Fprintf(&b, "    classDef %s %s\n    class %s %s\n", def.class, def.style, strings.Join(members, ","), def.class)
		}
	}
	for _, def := range defs {
		if links := styles[def.change]; len(links) > 0 {
			fmt.Fprintf(&b, "    linkStyle %s %s\n", strings.Join(links, ","), def.link)
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// mermaidText escapes a text for a quoted label of Mermaid.
func mermaidText(s string) string {
	return strings.NewReplacer("#", "#35;", `"`, "#quot;", "<", "#lt;", ">", "#gt;").Replace(s)
}
