// Package flow reads the flow of a project from its process directory and
// checks it by sections 7.1-7.2 of the technical solution: the files of the
// flow and their fields, the pairs of stage files, references to stages,
// parts and subagents, and the graph of each scenario.
package flow

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	fieldsExt    = ".yaml"
	textExt      = ".md"
)

// Finish is the target of a transition that ends the scenario.
const Finish = "finish"

// Executors of a stage other than a subagent.
const (
	Orchestrator = "orchestrator" // the main agent session
	Operator     = "operator"     // the operator
)

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
	Parts     []string   // by identifier
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
	ID       string
	Title    string
	Exit     string
	Executor string   // Orchestrator, Operator or a subagent
	Include  []string // parts, in the order of the stage file
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

// Only returns the flow of scenario s alone: its stages and their parts.
func (f *Flow) Only(s Scenario) *Flow {
	out := &Flow{Scenarios: []Scenario{s}, Parts: []string{}}
	for _, st := range f.Stages {
		if slices.ContainsFunc(s.Nodes, func(n Node) bool { return n.Stage == st.ID }) {
			out.Stages = append(out.Stages, st)
		}
	}
	for _, p := range f.Parts {
		if slices.ContainsFunc(out.Stages, func(st Stage) bool { return slices.Contains(st.Include, p) }) {
			out.Parts = append(out.Parts, p)
		}
	}
	return out
}

// Dirs locates a flow and the subagents its stages may name.
type Dirs struct {
	Flow   string   // the flow directory
	Agents []string // directories of subagents, the project ones first
}

// ProjectDirs returns the dirs of project in the process directory: its flow,
// its subagents and the shared subagents of the operator (section 10.2).
func ProjectDirs(process, project string) Dirs {
	return Dirs{
		Flow:   filepath.Join(process, project, "flow"),
		Agents: []string{filepath.Join(process, project, "agents"), filepath.Join(process, "agents")},
	}
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ValidID reports whether id is a valid identifier of a scenario, stage, part
// or node: up to 64 lowercase Latin letters, digits and hyphens, starting with
// a letter.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Load reads and checks the flow. It returns NotFoundError if there is no flow
// directory, InvalidError with all problems if the flow has any, and an error
// satisfying errors.As(err, *fs.PathError) if a file cannot be read.
func Load(d Dirs) (*Flow, error) {
	fi, err := os.Stat(d.Flow)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir() {
		return nil, &NotFoundError{Dir: d.Flow}
	}
	if err != nil {
		return nil, err
	}
	l := &loader{
		dirs:   d,
		fields: map[string]bool{},
		texts:  map[string]bool{},
		parts:  map[string]bool{},
		stages: map[string]*stage{},
		agents: map[string]bool{},
	}
	if err := l.load(); err != nil {
		return nil, err
	}
	l.check()
	if len(l.problems) > 0 {
		sortProblems(l.problems)
		return nil, &InvalidError{Dir: d.Flow, Problems: l.problems}
	}
	return l.flow(), nil
}

// loader reads a flow and collects its problems.
type loader struct {
	dirs     Dirs
	problems []Problem

	common        bool            // flow.yaml exists
	scenarioFiles []string        // identifiers of the scenario files with valid names
	scenarioCount int             // scenario files, with invalid names too
	fields        map[string]bool // stages with fields
	texts         map[string]bool // stages with an instruction
	parts         map[string]bool

	scenarios []*scenario
	stages    map[string]*stage
	agents    map[string]bool // whether a subagent exists, by identifier
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

// load reads the files of the flow.
func (l *loader) load() error {
	entries, err := l.entries("")
	if err != nil {
		return err
	}
	for _, e := range entries {
		switch {
		case e.name == commonFile && !e.dir:
			l.common = true
		case e.dir && (e.name == scenariosDir || e.name == stagesDir || e.name == partsDir):
			if err := l.scan(e.name); err != nil {
				return err
			}
		default:
			l.extra(e)
		}
	}

	if l.common {
		if err := l.loadCommon(); err != nil {
			return err
		}
	}
	for _, id := range l.scenarioFiles {
		if err := l.loadScenario(id); err != nil {
			return err
		}
	}
	for _, id := range sorted(l.fields) {
		if err := l.loadStage(id); err != nil {
			return err
		}
	}
	for _, id := range sorted(l.texts) {
		if _, _, err := l.read(stagesDir+"/"+id+textExt, objStage(id)); err != nil {
			return err
		}
	}
	for _, id := range sorted(l.parts) {
		if _, _, err := l.read(partsDir+"/"+id+textExt, objPart(id)); err != nil {
			return err
		}
	}
	return nil
}

// entry is a file or directory of the flow; rel is its path inside the flow
// directory with forward slashes.
type entry struct {
	name, rel string
	dir       bool
}

// entries returns the entries of directory rel of the flow by name. Hidden
// files, those whose name starts with a dot, are not part of the flow.
func (l *loader) entries(rel string) ([]entry, error) {
	dir := filepath.Join(l.dirs.Flow, filepath.FromSlash(rel))
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		e := entry{name: name, rel: name, dir: de.IsDir()}
		if rel != "" {
			e.rel = rel + "/" + name
		}
		// A symbolic link counts as what it points to.
		if de.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				e.dir = fi.IsDir()
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// scan notes the files of directory sub of the flow.
func (l *loader) scan(sub string) error {
	entries, err := l.entries(sub)
	if err != nil {
		return err
	}
	invalid := map[string]bool{}
	for _, e := range entries {
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
	return nil
}

// extra reports a file or directory that does not belong to the flow.
func (l *loader) extra(e entry) {
	k := msg.ProblemExtraFile
	if e.dir {
		k = msg.ProblemExtraDir
	}
	l.report(contract.ProblemExtraFile, e.rel, 0, k, e.rel)
}

// read returns the text of file rel of the flow. If the text is not in UTF-8,
// it reports the problem for object obj and returns ok false.
func (l *loader) read(rel, obj string) (text []byte, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(l.dirs.Flow, filepath.FromSlash(rel)))
	if err != nil {
		return nil, false, err
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}) // a byte order mark is not part of the text
	if !utf8.Valid(b) {
		l.report(contract.ProblemSyntax, rel, 0, msg.ProblemEncoding, obj)
		return nil, false, nil
	}
	return b, true, nil
}

// readYAML reads and parses file rel of the flow. It returns ok false after
// reporting a problem of syntax or encoding for object obj.
func (l *loader) readYAML(rel, obj string) (v *value, ok bool, err error) {
	b, ok, err := l.read(rel, obj)
	if !ok {
		return nil, false, err
	}
	f, err := parser.ParseBytes(b, 0)
	if err != nil {
		line := 0
		var ye yaml.Error
		if errors.As(err, &ye) && ye.GetToken() != nil && ye.GetToken().Position != nil {
			line = ye.GetToken().Position.Line
		}
		l.report(contract.ProblemSyntax, rel, line, msg.ProblemSyntax, obj)
		return nil, false, nil
	}
	switch len(f.Docs) {
	case 0:
		return &value{}, true, nil
	case 1:
		return convert(f.Docs[0].Body, map[string]*value{}), true, nil
	}
	// A file of the flow is one document.
	l.report(contract.ProblemSyntax, rel, lineOf(f.Docs[1]), msg.ProblemSyntax, obj)
	return nil, false, nil
}

func (l *loader) loadCommon() error {
	o := object{name: objCommon(), file: commonFile}
	v, ok, err := l.readYAML(o.file, o.name)
	if ok {
		l.mapping(o, v, 0, nil, laterTaskFields)
	}
	return err
}

func (l *loader) loadScenario(id string) error {
	o := object{name: objScenario(id), file: scenariosDir + "/" + id + fieldsExt}
	s := &scenario{id: id, file: o.file, malformed: true}
	l.scenarios = append(l.scenarios, s)
	v, ok, err := l.readYAML(o.file, o.name)
	if !ok {
		return err
	}
	before := len(l.problems)
	defer func() { s.malformed = len(l.problems) > before }()

	m, ok := l.mapping(o, v, 0, []string{"title", "start", "nodes"}, laterTaskFields)
	if !ok {
		return nil
	}
	s.title, _ = l.text(o, m, "title", 0)
	s.start, s.startLine = l.text(o, m, "start", 0)
	nodes, present := m["nodes"]
	switch {
	case !present || nodes.value.kind == null:
		l.report(contract.ProblemMissingField, o.file, nodes.line, msg.ProblemMissingField, o.name, "nodes")
		return nil
	case nodes.value.kind != mapping:
		l.report(contract.ProblemInvalidValue, o.file, nodes.line, msg.ProblemNodes, o.name)
		return nil
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
	return nil
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

func (l *loader) loadStage(id string) error {
	o := object{name: objStage(id), file: stagesDir + "/" + id + fieldsExt}
	st := &stage{id: id, file: o.file}
	l.stages[id] = st
	v, ok, err := l.readYAML(o.file, o.name)
	if !ok {
		return err
	}
	m, ok := l.mapping(o, v, 0, []string{"title", "exit", "executor", "include"}, laterStageFields)
	if !ok {
		return nil
	}
	st.title, _ = l.text(o, m, "title", 0)
	st.exit, _ = l.text(o, m, "exit", 0)
	st.executor, st.executorLine = l.text(o, m, "executor", 0)
	inc, present := m["include"]
	if !present || inc.value.kind == null {
		return nil
	}
	ids := inc.value.kind == sequence
	for _, item := range inc.value.items {
		s, ok := item.scalar.(string)
		ids = ids && ok && ValidID(s)
	}
	if !ids {
		l.report(contract.ProblemInvalidValue, o.file, inc.line, msg.ProblemInclude, o.name)
		return nil
	}
	for _, item := range inc.value.items {
		st.include = append(st.include, item.scalar.(string))
		st.includeLines = append(st.includeLines, item.line)
	}
	return nil
}

// object is what problems are about: the name messages give it and its file.
type object struct {
	name string
	file string
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
		l.report(contract.ProblemInvalidValue, o.file, max(v.line, line), msg.ProblemNotMapping, o.name)
		return nil, false
	}
	for _, f := range v.fields {
		switch {
		case slices.Contains(known, f.name):
			m[f.name] = f
		case slices.Contains(later, f.name):
			l.report(contract.ProblemUnsupportedField, o.file, f.line, msg.ProblemUnsupportedField, o.name, f.name)
		default:
			l.report(contract.ProblemUnknownField, o.file, f.line, msg.ProblemUnknownField, o.name, f.name)
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
		l.report(contract.ProblemInvalidValue, o.file, f.line, msg.ProblemNotString, o.name, name)
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
		l.report(contract.ProblemMissingField, o.file, at, msg.ProblemMissingField, o.name, name)
	}
	return s, at
}

// subagent reports whether subagent id exists among the subagents of the
// project or the shared ones. Only its existence is checked here; its
// description is checked when subagents are laid out.
func (l *loader) subagent(id string) bool {
	if !ValidID(id) {
		return false
	}
	if found, ok := l.agents[id]; ok {
		return found
	}
	found := false
	for _, dir := range l.dirs.Agents {
		if fi, err := os.Stat(filepath.Join(dir, id+fieldsExt)); err == nil && fi.Mode().IsRegular() {
			found = true
			break
		}
	}
	l.agents[id] = found
	return found
}

// flow returns the flow as read, once it has no problems.
func (l *loader) flow() *Flow {
	f := &Flow{Scenarios: []Scenario{}, Stages: []Stage{}, Parts: sorted(l.parts)}
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
		f.Stages = append(f.Stages, Stage{ID: id, Title: st.title, Exit: st.exit, Executor: st.executor, Include: append([]string{}, st.include...)})
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
