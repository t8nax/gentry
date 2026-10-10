package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// decisionFields are the fields of operator record; options are given only
// by --input.
var decisionFields = []field{
	{name: "question"},
	{name: "options", kind: rawField},
	{name: "answer"},
	{name: "allow_return", flag: "allow-return"},
}

// optionLabelMax is the limit of the name of an option in characters.
const optionLabelMax = 120

// Reasons an option is refused, as the contract names them.
const (
	optionsTooFew          = "too_few"
	optionsRecommendedMany = "recommended_many"
	optionLabelEmpty       = "empty"
	optionLabelMultiline   = "multiline"
	optionLabelTooLong     = "too_long"
)

func runOperatorRecord(args []string, env Env) int {
	const cmd = "operator record"
	f := newFlags(cmd)
	flags := map[string]*stringFlag{
		"question":     f.String("question"),
		"answer":       f.String("answer"),
		"allow_return": f.String("allow-return"),
	}
	key := f.String("task")
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, flags, nil, env.Stdin, decisionFields)
	if bad != nil {
		return fail(env, *bad)
	}
	options, bad := readOptions(cmd, input.Value, values["options"])
	if bad != nil {
		return fail(env, *bad)
	}
	req := task.Decision{
		Question: text(values, "question"), Options: options, Answer: text(values, "answer"),
		AllowReturn: text(values, "allow_return"), Source: source(env),
	}
	if strings.TrimSpace(req.Question) == "" {
		req.Question = ""
	}
	if bad := checkDecision(cmd, req); bad != nil {
		return fail(env, *bad)
	}

	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	req.Task = w.task.ID
	// The node a return goes to is named by the stage of the snapshot. The
	// snapshot is read before the decision is recorded: once it is, the
	// command does not fail, or a repeated call would record it twice.
	var n names
	if req.AllowReturn != "" {
		v, bad := w.view()
		if bad != nil {
			return w.fail(env, *bad)
		}
		n = namesOf(v)
	}
	res, err := task.RecordDecision(w.st, req)
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}

	out := contract.OperatorRecordOutput{Task: res.Task.Key(), Decision: decisionJSON(res.Decision)}
	if r := res.Return; r != nil {
		out.Return = &contract.OperatorRecordOutputReturn{Node: r.Node, To: r.To, Returns: r.Returns,
			MaxReturns: r.Limit, AllowedReturns: r.Allowed}
	}
	return emit(env, out, func(p *page, out contract.OperatorRecordOutput) { operatorRecordText(p, out, n) })
}

// operatorRecordText prints the decision recorded and the return it allows,
// the node of the return named by n, the names of the snapshot of the task.
func operatorRecordText(p *page, out contract.OperatorRecordOutput, n names) {
	fmt.Fprintln(p, msg.Text(msg.DecisionRecorded))
	if r := out.Return; r != nil {
		fmt.Fprintln(p, msg.Text(msg.AllowedReturnLine, named(n.nodeTitle(r.To), r.To)))
		fmt.Fprintln(p, msg.Text(msg.ReturnsLine, r.Returns, r.MaxReturns))
	}
}

// readOptions reads the options of an answer given by --input: an array of
// objects with a name, a description and a mark of the recommended one. An
// option without a name is read with an empty one, for checkDecision to
// refuse it by its number.
func readOptions(cmd, input string, raw any) ([]state.Option, *failure) {
	data, ok := raw.(json.RawMessage)
	if !ok {
		return nil, nil
	}
	refuse := func(cause string) ([]state.Option, *failure) {
		return nil, &failure{
			exit:         contract.ExitUsage,
			code:         contract.CodeInputInvalid,
			message:      msg.Text(msg.ErrInputInvalid, cause),
			agentMessage: msg.Text(msg.ErrToolFields, cause),
			hints:        []hint{helpHint(msg.HintCommandHelp, cmd)},
			details:      map[string]any{"input": input, "field": "options"},
		}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil || items == nil {
		return refuse(msg.Text(msg.InputNotObjects, "options"))
	}
	options := []state.Option{}
	for i, item := range items {
		var o struct {
			Label       *string `json:"label"`
			Description *string `json:"description"`
			Recommended *bool   `json:"recommended"`
		}
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil || bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return refuse(msg.Text(msg.InputBadOption, i+1))
		}
		var opt state.Option
		if o.Label != nil {
			opt.Label = *o.Label
		}
		if o.Description != nil && strings.TrimSpace(*o.Description) != "" {
			opt.Description = *o.Description
		}
		opt.Recommended = o.Recommended != nil && *o.Recommended
		options = append(options, opt)
	}
	return options, nil
}

// checkDecision refuses a decision of the operator without an answer, and
// options of an answer that are not two or more with names, given with the
// question, at most one recommended.
func checkDecision(cmd string, req task.Decision) *failure {
	refuse := func(f failure) *failure { return &f }
	help := helpHint(msg.HintCommandHelp, cmd)
	if strings.TrimSpace(req.Answer) == "" {
		return refuse(missingField(cmd, "answer", msg.Text(msg.ErrAnswerMissing), help))
	}
	if len(req.Options) == 0 {
		return nil
	}
	if req.Question == "" {
		return refuse(missingField(cmd, "question", msg.Text(msg.ErrOptionsQuestionMissing), help))
	}
	if len(req.Options) < 2 {
		return refuse(fieldInvalid("options", optionsTooFew, msg.Text(msg.ErrOptionsTooFew), hintOf(msg.HintOptionsTooFew)))
	}
	recommended := 0
	for i, o := range req.Options {
		var reason string
		var k msg.Key
		switch {
		case strings.TrimSpace(o.Label) == "":
			reason, k = optionLabelEmpty, msg.ErrOptionLabelEmpty
		case strings.ContainsAny(o.Label, "\r\n"):
			reason, k = optionLabelMultiline, msg.ErrOptionLabelMultiline
		case utf8.RuneCountInString(o.Label) > optionLabelMax:
			reason, k = optionLabelTooLong, msg.ErrOptionLabelTooLong
		}
		if reason != "" {
			f := fieldInvalid("options", reason, msg.Text(k, i+1), hintOf(msg.HintOptionLabel))
			f.details["option"] = i + 1
			return &f
		}
		if o.Recommended {
			recommended++
		}
	}
	if recommended > 1 {
		return refuse(fieldInvalid("options", optionsRecommendedMany, msg.Text(msg.ErrOptionsRecommendedMany),
			hintOf(msg.HintOptionsRecommended)))
	}
	return nil
}

// decisionJSON returns a decision of the operator as the contract has it.
func decisionJSON(d state.Decision) contract.OperatorDecision {
	cd := contract.OperatorDecision{
		Number: d.Number, Options: task.OptionsJSON(d.Options), Answer: d.Answer, Node: d.Node, Stage: d.Stage,
		Round: d.Round, Source: contract.OperatorDecisionSource(d.Source), Recorded: d.Recorded,
	}
	if d.Question != "" {
		q := d.Question
		cd.Question = &q
	}
	if d.AllowReturn != "" {
		a := d.AllowReturn
		cd.AllowReturn = &a
	}
	return cd
}

// writeField prints a field as «name: value» after lead; a value of several
// lines goes under the name with an indent, and an empty value leaves the
// name alone. The lines after the first are indented under lead.
func writeField(b *strings.Builder, lead, name, value string) {
	value = strings.TrimRight(value, " \t\r\n")
	under := strings.Repeat(" ", utf8.RuneCountInString(lead)) + "  "
	switch {
	case value == "":
		fmt.Fprintf(b, "%s%s\n", lead, name)
	case !strings.Contains(value, "\n"):
		fmt.Fprintf(b, "%s%s: %s\n", lead, name, strings.TrimSpace(value))
	default:
		fmt.Fprintf(b, "%s%s:\n", lead, name)
		writeIndented(b, value, under)
	}
}
