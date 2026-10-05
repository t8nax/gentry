// Package flow reads the flow of a project and checks it by sections 7.1-7.2
// and 10.2 of the technical solution: the files of the flow and their fields,
// the pairs of stage and subagent files, references to stages, parts and
// subagents, and the graph of each scenario. The active flow is a version
// snapshot in the state store; its files exist only as the draft, the one
// form of the flow the operator and the agent edit.
package flow

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// Files and directories of a flow (section 7.2).
const (
	commonFile   = "flow.yaml"
	scenariosDir = "scenarios"
	stagesDir    = "stages"
	partsDir     = "parts"
	agentsDir    = "agents"
	fieldsExt    = ".yaml"
	textExt      = ".md"
)

// Places of the flow in the process directory of the operator.
const (
	draftDirName   = "flow-draft" // the draft, in the directory of the project
	libraryDirName = "agents"     // the library of subagents, for all projects
)

// DraftDir returns the directory of the flow draft of project in the process
// directory.
func DraftDir(process, project string) string {
	return filepath.Join(process, project, draftDirName)
}

// LibraryDir returns the directory of the library of subagents in the process
// directory.
func LibraryDir(process string) string { return filepath.Join(process, libraryDirName) }

// Finish is the target of a transition that ends the scenario.
const Finish = "finish"

// Executors of a stage other than a subagent.
const (
	Orchestrator = "orchestrator" // the main agent session
	Operator     = "operator"     // the operator
)

// Capabilities a subagent may have (section 10.2).
var Capabilities = []string{"read", "search", "edit", "run", "web"}

// Fields that come with later versions of Gentry: tracker actions with stage
// 8, procedures of the knowledge with stage 7. Until then they are refused, so
// that the operator does not take them for working.
var (
	laterTaskFields  = []string{"on_take", "on_close", "on_cancel"}
	laterStageFields = []string{"on_enter", "on_exit", "procedures"}
)

// Flow is a flow without problems.
type Flow struct {
	Scenarios []Scenario // by identifier
	Stages    []Stage    // by identifier
	Parts     []Part     // by identifier
	Agents    []Agent    // the project subagents and the library ones the stages name, by identifier
}

// Scenario is the graph of a scenario.
type Scenario struct {
	ID    string
	Title string
	Start string
	Nodes []Node // in the order of the scenario file
}

// Node is a node of a scenario: a stage at its place in the graph.
type Node struct {
	ID    string
	Stage string
	Next  []Transition
}

// Transition is an edge of a scenario graph.
type Transition struct {
	To        string // a node or Finish
	If        string // the condition; empty for the default path
	MaxRounds int    // the limit of rounds of the loop it closes; 0 if none
}

// Stage is a stage of the flow, described once for all scenarios.
type Stage struct {
	ID          string
	Title       string
	Exit        string
	Executor    string   // Orchestrator, Operator or a subagent
	Include     []string // parts, in the order of the stage file
	Instruction string   // markdown
}

// Part is a part included in the instructions of stages.
type Part struct {
	ID   string
	Text string // markdown
}

// Agent is a subagent of the flow: of the project or of the library.
type Agent struct {
	ID           string
	Library      bool // from the library of subagents, not of the project
	Purpose      string
	Capabilities []string // in the order of the file
	Instruction  string   // markdown
}

// Scenario returns the scenario with identifier id.
func (f *Flow) Scenario(id string) (Scenario, bool) {
	for _, s := range f.Scenarios {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// Stage returns the stage with identifier id.
func (f *Flow) Stage(id string) (Stage, bool) {
	for _, s := range f.Stages {
		if s.ID == id {
			return s, true
		}
	}
	return Stage{}, false
}

// Part returns the part with identifier id.
func (f *Flow) Part(id string) (Part, bool) {
	for _, p := range f.Parts {
		if p.ID == id {
			return p, true
		}
	}
	return Part{}, false
}

// Agent returns the subagent with identifier id.
func (f *Flow) Agent(id string) (Agent, bool) {
	for _, a := range f.Agents {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// ScenariosOf returns the identifiers of the scenarios where stage stands.
func (f *Flow) ScenariosOf(stage string) []string {
	var out []string
	for _, s := range f.Scenarios {
		if slices.ContainsFunc(s.Nodes, func(n Node) bool { return n.Stage == stage }) {
			out = append(out, s.ID)
		}
	}
	return out
}

// StagesOf returns the identifiers of the stages that include part.
func (f *Flow) StagesOf(part string) []string {
	var out []string
	for _, st := range f.Stages {
		if slices.Contains(st.Include, part) {
			out = append(out, st.ID)
		}
	}
	return out
}

// StagesBy returns the identifiers of the stages subagent agent carries out.
func (f *Flow) StagesBy(agent string) []string {
	var out []string
	for _, st := range f.Stages {
		if st.Executor == agent {
			out = append(out, st.ID)
		}
	}
	return out
}

// OnlyScenario returns the flow of scenario s alone: its stages, their parts
// and subagents.
func (f *Flow) OnlyScenario(s Scenario) *Flow {
	var stages []Stage
	for _, st := range f.Stages {
		if slices.ContainsFunc(s.Nodes, func(n Node) bool { return n.Stage == st.ID }) {
			stages = append(stages, st)
		}
	}
	out := f.withStages(stages)
	out.Scenarios = []Scenario{s}
	return out
}

// OnlyStage returns the flow of stage st alone: it, its parts and subagent.
func (f *Flow) OnlyStage(st Stage) *Flow { return f.withStages([]Stage{st}) }

// OnlyPart returns the flow of part p alone.
func (f *Flow) OnlyPart(p Part) *Flow {
	return &Flow{Scenarios: []Scenario{}, Stages: []Stage{}, Parts: []Part{p}, Agents: []Agent{}}
}

// OnlyAgent returns the flow of subagent a alone.
func (f *Flow) OnlyAgent(a Agent) *Flow {
	return &Flow{Scenarios: []Scenario{}, Stages: []Stage{}, Parts: []Part{}, Agents: []Agent{a}}
}

// withStages returns stages with what they refer to: their parts and
// subagents.
func (f *Flow) withStages(stages []Stage) *Flow {
	out := &Flow{Scenarios: []Scenario{}, Stages: append([]Stage{}, stages...), Parts: []Part{}, Agents: []Agent{}}
	for _, p := range f.Parts {
		if slices.ContainsFunc(stages, func(st Stage) bool { return slices.Contains(st.Include, p.ID) }) {
			out.Parts = append(out.Parts, p)
		}
	}
	for _, a := range f.Agents {
		if slices.ContainsFunc(stages, func(st Stage) bool { return st.Executor == a.ID }) {
			out.Agents = append(out.Agents, a)
		}
	}
	return out
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ValidID reports whether id is a valid identifier of a scenario, stage, part,
// node or subagent: up to 64 lowercase Latin letters, digits and hyphens,
// starting with a letter.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Result is a flow as read: the flow itself if it has no problems, and what
// it consists of either way.
type Result struct {
	Flow     *Flow     // nil if there are problems
	Problems []Problem // in the order of files and lines
	// Snapshot holds the files of the flow and the library subagents its
	// stages name. With problems it is as complete as the problems allow.
	Snapshot Snapshot
}

// ReadDraft reads and checks the flow draft in directory dir; the library of
// subagents is in directory library. It returns an error satisfying
// errors.As(err, *fs.PathError) if a file cannot be read.
func ReadDraft(dir, library string) (*Result, error) {
	t, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	return readFlow(t, dirLibrary(library))
}

// ReadSnapshot checks the flow of a version snapshot. A snapshot was checked
// when it was applied, but a later Gentry may check more.
func ReadSnapshot(s Snapshot) (*Result, error) {
	return readFlow(treeOf(s.Files), mapLibrary(s.Library))
}

func readFlow(t *tree, lib library) (*Result, error) {
	l := &loader{
		src:         t,
		lib:         lib,
		fields:      map[string]bool{},
		texts:       map[string]bool{},
		parts:       map[string]bool{},
		agentFields: map[string]bool{},
		agentTexts:  map[string]bool{},
		stages:      map[string]*stage{},
		agents:      map[string]*agent{},
		library:     map[string]*agent{},
		libFiles:    map[string]string{},
		contents:    map[string]string{},
	}
	if err := l.load(); err != nil {
		return nil, err
	}
	if err := l.check(); err != nil {
		return nil, err
	}
	files := map[string]string{}
	for p, b := range t.files {
		files[p] = string(b)
	}
	res := &Result{Snapshot: Snapshot{Files: files, Library: l.libFiles}}
	if problems := sortProblems(l.problems, l.libProblems); len(problems) > 0 {
		res.Problems = problems
		return res, nil
	}
	res.Flow = l.flow()
	return res, nil
}

// loader reads a flow and collects its problems.
type loader struct {
	src         *tree
	lib         library
	problems    []Problem
	libProblems []Problem // of library subagents, in the order they are found

	common        bool            // flow.yaml exists
	scenarioFiles []string        // identifiers of the scenario files with valid names
	scenarioCount int             // scenario files, with invalid names too
	fields        map[string]bool // stages with fields
	texts         map[string]bool // stages with an instruction
	parts         map[string]bool
	agentFields   map[string]bool // project subagents with fields
	agentTexts    map[string]bool // project subagents with an instruction

	scenarios []*scenario
	stages    map[string]*stage
	agents    map[string]*agent // project subagents as read
	library   map[string]*agent // library subagents the stages name; nil if there is none
	libFiles  map[string]string // the files of the library subagents read, for the snapshot
	contents  map[string]string // texts of instructions and parts, by path
}

// scenario is a scenario as read, with the lines of its parts.
type scenario struct {
	id, file  string
	title     string
	start     string
	startLine int
	nodes     []*node
	malformed bool // the file has problems of its own: the graph is not checked
}

type node struct {
	id, stage string
	line      int
	stageLine int
	next      []*transition
}

type transition struct {
	to, cond string
	max      int
	line     int
}

// stage is a stage as read, with the lines of its references.
type stage struct {
	id, file     string
	title, exit  string
	executor     string
	executorLine int
	include      []string
	includeLines []int
}

// agent is a subagent as read.
type agent struct {
	id           string
	library      bool
	purpose      string
	capabilities []string
	instruction  string
}

// load reads the files of the flow.
func (l *loader) load() error {
	for _, e := range l.src.entries("") {
		switch {
		case e.name == commonFile && !e.dir:
			l.common = true
		case e.dir && (e.name == scenariosDir || e.name == stagesDir || e.name == partsDir || e.name == agentsDir):
			l.scan(e.name)
		default:
			l.extra(e)
		}
	}

	if l.common {
		l.loadCommon()
	}
	for _, id := range l.scenarioFiles {
		l.loadScenario(id)
	}
	for _, id := range sorted(l.fields) {
		l.loadStage(id)
	}
	for _, id := range sorted(l.texts) {
		l.readText(stagesDir+"/"+id+textExt, objStage(id))
	}
	for _, id := range sorted(l.parts) {
		l.readText(partsDir+"/"+id+textExt, objPart(id))
	}
	for _, id := range sorted(l.agentFields) {
		o := object{name: objAgent(id), file: agentsDir + "/" + id + fieldsExt}
		v, ok := l.readYAML(l.src.files[o.file], o)
		a := &agent{id: id}
		l.agents[id] = a
		if ok {
			l.loadAgent(o, v, a)
		}
	}
	for _, id := range sorted(l.agentTexts) {
		if text, ok := l.readText(agentsDir+"/"+id+textExt, objAgent(id)); ok && l.agents[id] != nil {
			l.agents[id].instruction = text
		}
	}
	return nil
}

// entry is a file or directory of the flow; rel is its path inside the flow
// with forward slashes.
type entry struct {
	name, rel string
	dir       bool
}

// scan notes the files of directory sub of the flow.
func (l *loader) scan(sub string) {
	invalid := map[string]bool{}
	for _, e := range l.src.entries(sub) {
		ext := filepath.Ext(e.name)
		id := strings.TrimSuffix(e.name, ext)
		var into map[string]bool
		var obj func(string) string
		switch {
		case e.dir:
		case sub == scenariosDir && ext == fieldsExt:
			l.scenarioCount++
			obj = objScenario
		case sub == stagesDir && ext == fieldsExt:
			into, obj = l.fields, objStage
		case sub == stagesDir && ext == textExt:
			into, obj = l.texts, objStage
		case sub == partsDir && ext == textExt:
			into, obj = l.parts, objPart
		case sub == agentsDir && ext == fieldsExt:
			into, obj = l.agentFields, objAgent
		case sub == agentsDir && ext == textExt:
			into, obj = l.agentTexts, objAgent
		}
		switch {
		case obj == nil:
			l.extra(e)
		case !ValidID(id):
			// The fields and the instruction of a stage are named once.
			if !invalid[id] {
				invalid[id] = true
				l.report(contract.ProblemInvalidID, e.rel, 0, msg.ProblemInvalidID, obj(id))
			}
		case into == nil:
			l.scenarioFiles = append(l.scenarioFiles, id)
		default:
			into[id] = true
		}
	}
}

// extra reports a file or directory that does not belong to the flow.
func (l *loader) extra(e entry) {
	k := msg.ProblemExtraFile
	if e.dir {
		k = msg.ProblemExtraDir
	}
	l.report(contract.ProblemExtraFile, e.rel, 0, k, e.rel)
}

// readText returns file rel of the flow as text and keeps it for the flow. If
// the text is not in UTF-8, it reports the problem for object obj and returns
// ok false.
func (l *loader) readText(rel, obj string) (string, bool) {
	b := l.src.files[rel]
	if !utf8.Valid(b) {
		l.report(contract.ProblemSyntax, rel, 0, msg.ProblemEncoding, obj)
		return "", false
	}
	l.contents[rel] = string(b)
	return string(b), true
}

// readYAML parses text b of the file of object o. It returns ok false after
// reporting a problem of syntax or encoding.
func (l *loader) readYAML(b []byte, o object) (v *value, ok bool) {
	if !utf8.Valid(b) {
		l.reportOn(o, contract.ProblemSyntax, 0, msg.ProblemEncoding, o.name)
		return nil, false
	}
	f, err := parser.ParseBytes(b, 0)
	if err != nil {
		line := 0
		var ye yaml.Error
		if errors.As(err, &ye) && ye.GetToken() != nil && ye.GetToken().Position != nil {
			line = ye.GetToken().Position.Line
		}
		l.reportOn(o, contract.ProblemSyntax, line, msg.ProblemSyntax, o.name)
		return nil, false
	}
	switch len(f.Docs) {
	case 0:
		return &value{}, true
	case 1:
		return convert(f.Docs[0].Body, map[string]*value{}), true
	}
	// A file of the flow is one document.
	l.reportOn(o, contract.ProblemSyntax, lineOf(f.Docs[1]), msg.ProblemSyntax, o.name)
	return nil, false
}

func (l *loader) loadCommon() {
	o := object{name: objCommon(), file: commonFile}
	if v, ok := l.readYAML(l.src.files[o.file], o); ok {
		l.mapping(o, v, 0, nil, laterTaskFields)
	}
}

func (l *loader) loadScenario(id string) {
	o := object{name: objScenario(id), file: scenariosDir + "/" + id + fieldsExt}
	s := &scenario{id: id, file: o.file, malformed: true}
	l.scenarios = append(l.scenarios, s)
	v, ok := l.readYAML(l.src.files[o.file], o)
	if !ok {
		return
	}
	before := len(l.problems)
	defer func() { s.malformed = len(l.problems) > before }()

	m, ok := l.mapping(o, v, 0, []string{"title", "start", "nodes"}, laterTaskFields)
	if !ok {
		return
	}
	s.title, _ = l.text(o, m, "title", 0)
	s.start, s.startLine = l.text(o, m, "start", 0)
	nodes, present := m["nodes"]
	switch {
	case !present || nodes.value.kind == null:
		l.report(contract.ProblemMissingField, o.file, nodes.line, msg.ProblemMissingField, o.name, "nodes")
		return
	case nodes.value.kind != mapping:
		l.report(contract.ProblemInvalidValue, o.file, nodes.line, msg.ProblemNodes, o.name)
		return
	}
	for _, f := range nodes.value.fields {
		no := object{name: objNode(id, f.name), file: o.file}
		switch {
		case f.name == Finish:
			l.report(contract.ProblemReservedNode, o.file, f.line, msg.ProblemReservedNode, o.name)
			continue
		case !ValidID(f.name):
			l.report(contract.ProblemInvalidID, o.file, f.line, msg.ProblemInvalidID, no.name)
			continue
		}
		n := &node{id: f.name, line: f.line}
		s.nodes = append(s.nodes, n)
		nm, ok := l.mapping(no, f.value, f.line, []string{"stage", "next"}, nil)
		if !ok {
			continue
		}
		n.stage, n.stageLine = l.text(no, nm, "stage", f.line)
		n.next = l.transitions(no, nm, f.line)
	}
}

// transitions reads field next of a node: one node identifier, a transition
// without a condition, or a list of transitions.
func (l *loader) transitions(o object, m map[string]field, line int) []*transition {
	f, present := m["next"]
	if !present || f.value.kind == null || f.value.scalar == "" {
		if present {
			line = f.line
		}
		l.report(contract.ProblemMissingField, o.file, line, msg.ProblemMissingField, o.name, "next")
		return nil
	}
	if to, ok := f.value.scalar.(string); ok {
		return []*transition{{to: to, line: f.line}}
	}
	if f.value.kind != sequence {
		l.report(contract.ProblemInvalidValue, o.file, f.line, msg.ProblemNext, o.name)
		return nil
	}
	var out []*transition
	for _, item := range f.value.items {
		if item.kind != mapping {
			l.report(contract.ProblemInvalidValue, o.file, item.line, msg.ProblemNext, o.name)
			continue
		}
		tm, _ := l.mapping(o, item, item.line, []string{"to", "if", "max_rounds"}, nil)
		t := &transition{line: item.line}
		if to, ok := tm["to"]; ok {
			t.line = to.line
		}
		t.to, _ = l.text(o, tm, "to", t.line)
		t.cond, _, _ = l.optionalText(o, tm, "if")
		if mr, ok := tm["max_rounds"]; ok {
			n, ok := integer(mr.value)
			if !ok || n < 1 {
				l.report(contract.ProblemInvalidValue, o.file, mr.line, msg.ProblemMaxRounds, o.name)
			} else {
				t.max = n
			}
		}
		out = append(out, t)
	}
	return out
}

func (l *loader) loadStage(id string) {
	o := object{name: objStage(id), file: stagesDir + "/" + id + fieldsExt}
	st := &stage{id: id, file: o.file}
	l.stages[id] = st
	v, ok := l.readYAML(l.src.files[o.file], o)
	if !ok {
		return
	}
	m, ok := l.mapping(o, v, 0, []string{"title", "exit", "executor", "include"}, laterStageFields)
	if !ok {
		return
	}
	st.title, _ = l.text(o, m, "title", 0)
	st.exit, _ = l.text(o, m, "exit", 0)
	st.executor, st.executorLine = l.text(o, m, "executor", 0)
	inc, present := m["include"]
	if !present || inc.value.kind == null {
		return
	}
	ids := inc.value.kind == sequence
	for _, item := range inc.value.items {
		s, ok := item.scalar.(string)
		ids = ids && ok && ValidID(s)
	}
	if !ids {
		l.report(contract.ProblemInvalidValue, o.file, inc.line, msg.ProblemInclude, o.name)
		return
	}
	for _, item := range inc.value.items {
		st.include = append(st.include, item.scalar.(string))
		st.includeLines = append(st.includeLines, item.line)
	}
}

// loadAgent reads the fields of subagent a from v (section 10.2).
func (l *loader) loadAgent(o object, v *value, a *agent) {
	m, ok := l.mapping(o, v, 0, []string{"purpose", "capabilities"}, nil)
	if !ok {
		return
	}
	a.purpose, _ = l.text(o, m, "purpose", 0)
	caps, present := m["capabilities"]
	if !present || caps.value.kind == null {
		l.reportOn(o, contract.ProblemMissingField, caps.line, msg.ProblemMissingField, o.name, "capabilities")
		return
	}
	valid := caps.value.kind == sequence
	for _, item := range caps.value.items {
		s, ok := item.scalar.(string)
		valid = valid && ok && slices.Contains(Capabilities, s)
	}
	if !valid {
		l.reportOn(o, contract.ProblemInvalidValue, caps.line, msg.ProblemCapabilities, o.name, strings.Join(Capabilities, ", "))
		return
	}
	a.capabilities = []string{}
	for _, item := range caps.value.items {
		a.capabilities = append(a.capabilities, item.scalar.(string))
	}
}

// object is what problems are about: the name messages give it and its file.
// A subagent of the library has no file in the flow.
type object struct {
	name    string
	file    string
	library bool
}

// mapping returns the fields of v, which must be a mapping or empty, by name.
// It reports the fields that are neither known nor later; a later field comes
// with a later version of Gentry. line is where v is, for a problem of v as a
// whole.
func (l *loader) mapping(o object, v *value, line int, known, later []string) (map[string]field, bool) {
	m := map[string]field{}
	switch v.kind {
	case null:
		return m, true
	case mapping:
	default:
		l.reportOn(o, contract.ProblemInvalidValue, max(v.line, line), msg.ProblemNotMapping, o.name)
		return nil, false
	}
	for _, f := range v.fields {
		switch {
		case slices.Contains(known, f.name):
			m[f.name] = f
		case slices.Contains(later, f.name):
			l.reportOn(o, contract.ProblemUnsupportedField, f.line, msg.ProblemUnsupportedField, o.name, f.name)
		default:
			l.reportOn(o, contract.ProblemUnknownField, f.line, msg.ProblemUnknownField, o.name, f.name)
		}
	}
	return m, true
}

// optionalText returns string field name of m and its line; the value is
// empty if the field is absent or empty. A value that is not a string is
// reported, and ok is false.
func (l *loader) optionalText(o object, m map[string]field, name string) (s string, line int, ok bool) {
	f, present := m[name]
	if !present {
		return "", 0, true
	}
	if f.value.kind == null {
		return "", f.line, true
	}
	s, isString := f.value.scalar.(string)
	if f.value.kind != scalar || !isString {
		l.reportOn(o, contract.ProblemInvalidValue, f.line, msg.ProblemNotString, o.name, name)
		return "", f.line, false
	}
	return s, f.line, true
}

// text returns the required string field name of m and its line; line is
// where m is, for the problem of a missing field.
func (l *loader) text(o object, m map[string]field, name string, line int) (string, int) {
	s, at, ok := l.optionalText(o, m, name)
	if at == 0 {
		at = line
	}
	if ok && s == "" {
		l.reportOn(o, contract.ProblemMissingField, at, msg.ProblemMissingField, o.name, name)
	}
	return s, at
}

// subagent reports whether subagent id exists: among the subagents of the
// project, or else in the library. A library subagent is read and checked
// the first time a stage names it, and its files go to the snapshot.
func (l *loader) subagent(id string) (bool, error) {
	if !ValidID(id) {
		return false, nil
	}
	if l.agentFields[id] {
		return true, nil
	}
	if a, ok := l.library[id]; ok {
		return a != nil, nil
	}
	fields, ok, err := l.lib.file(id + fieldsExt)
	if err != nil || !ok {
		l.library[id] = nil
		return false, err
	}
	o := object{name: objLibraryAgent(id), library: true}
	a := &agent{id: id, library: true}
	l.library[id] = a
	l.libFiles[id+fieldsExt] = string(fields)
	if v, ok := l.readYAML(fields, o); ok {
		l.loadAgent(o, v, a)
	}
	text, ok, err := l.lib.file(id + textExt)
	switch {
	case err != nil:
		return false, err
	case !ok:
		l.reportOn(o, contract.ProblemMissingInstruction, 0, msg.ProblemMissingInstruction, o.name)
	case !utf8.Valid(text):
		l.reportOn(o, contract.ProblemSyntax, 0, msg.ProblemEncoding, o.name)
	default:
		l.libFiles[id+textExt] = string(text)
		a.instruction = string(text)
	}
	return true, nil
}

// flow returns the flow as read, once it has no problems.
func (l *loader) flow() *Flow {
	f := &Flow{Scenarios: []Scenario{}, Stages: []Stage{}, Parts: []Part{}, Agents: []Agent{}}
	for _, s := range l.scenarios {
		out := Scenario{ID: s.id, Title: s.title, Start: s.start}
		for _, n := range s.nodes {
			on := Node{ID: n.id, Stage: n.stage}
			for _, t := range n.next {
				on.Next = append(on.Next, Transition{To: t.to, If: t.cond, MaxRounds: t.max})
			}
			out.Nodes = append(out.Nodes, on)
		}
		f.Scenarios = append(f.Scenarios, out)
	}
	for _, id := range sorted(l.fields) {
		st := l.stages[id]
		f.Stages = append(f.Stages, Stage{
			ID: id, Title: st.title, Exit: st.exit, Executor: st.executor,
			Include: append([]string{}, st.include...), Instruction: l.contents[stagesDir+"/"+id+textExt],
		})
	}
	for _, id := range sorted(l.parts) {
		f.Parts = append(f.Parts, Part{ID: id, Text: l.contents[partsDir+"/"+id+textExt]})
	}
	var agents []*agent
	for _, a := range l.agents {
		agents = append(agents, a)
	}
	for _, a := range l.library {
		if a != nil {
			agents = append(agents, a)
		}
	}
	slices.SortFunc(agents, func(a, b *agent) int { return strings.Compare(a.id, b.id) })
	for _, a := range agents {
		f.Agents = append(f.Agents, Agent{
			ID: a.id, Library: a.library, Purpose: a.purpose,
			Capabilities: append([]string{}, a.capabilities...), Instruction: a.instruction,
		})
	}
	return f
}

// sorted returns the keys of m in order.
func sorted(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// value is a YAML value as the flow needs it: anchors, aliases and tags are
// resolved, and every part knows its line.
type value struct {
	kind   kind
	line   int
	scalar any      // string, int64, uint64, float64 or bool, for kind scalar
	fields []field  // for kind mapping, in the order of the file
	items  []*value // for kind sequence
}

type kind int

const (
	null kind = iota
	scalar
	mapping
	sequence
)

// field is a key of a mapping and its value; line is the line of the key.
type field struct {
	name  string
	line  int
	value *value
}

// convert turns a node of the YAML syntax tree into a value. anchors holds
// the values of the anchors met so far.
func convert(n ast.Node, anchors map[string]*value) *value {
	switch n := n.(type) {
	case nil:
		return &value{}
	case *ast.NullNode:
		return &value{line: lineOf(n)}
	case *ast.TagNode:
		return convert(n.Value, anchors)
	case *ast.AnchorNode:
		v := convert(n.Value, anchors)
		if n.Name != nil {
			anchors[n.Name.String()] = v
		}
		return v
	case *ast.AliasNode:
		if n.Value != nil {
			if v, ok := anchors[n.Value.String()]; ok {
				return v
			}
		}
		return &value{line: lineOf(n)}
	case *ast.MappingNode:
		v := &value{kind: mapping, line: lineOf(n)}
		for _, mv := range n.Values {
			v.fields = append(v.fields, convertField(mv, anchors))
		}
		return v
	case *ast.MappingValueNode:
		f := convertField(n, anchors)
		return &value{kind: mapping, line: f.line, fields: []field{f}}
	case *ast.SequenceNode:
		v := &value{kind: sequence, line: lineOf(n)}
		for _, item := range n.Values {
			v.items = append(v.items, convert(item, anchors))
		}
		return v
	case *ast.LiteralNode:
		// A block of text, | or >.
		if n.Value != nil {
			return &value{kind: scalar, line: lineOf(n), scalar: n.Value.Value}
		}
		return &value{line: lineOf(n)}
	case ast.ScalarNode:
		return &value{kind: scalar, line: lineOf(n), scalar: n.GetValue()}
	}
	return &value{kind: scalar, line: lineOf(n), scalar: n.String()}
}

func convertField(mv *ast.MappingValueNode, anchors map[string]*value) field {
	name := ""
	if k, ok := mv.Key.(ast.ScalarNode); ok && k.GetValue() != nil {
		name = fmt.Sprint(k.GetValue())
	} else if mv.Key != nil {
		name = mv.Key.String()
	}
	return field{name: name, line: lineOf(mv.Key), value: convert(mv.Value, anchors)}
}

// lineOf returns the line of n from 1, or 0 if unknown.
func lineOf(n ast.Node) int {
	if n == nil {
		return 0
	}
	t := n.GetToken()
	if t == nil || t.Position == nil {
		return 0
	}
	return t.Position.Line
}

// integer returns v as an int if it is a YAML integer.
func integer(v *value) (int, bool) {
	switch n := v.scalar.(type) {
	case int64:
		return int(n), n >= -1<<31 && n <= 1<<31
	case uint64:
		return int(n), n <= 1<<31
	}
	return 0, false
}
