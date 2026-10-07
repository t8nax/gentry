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
	asJSON := f.Bool("json")
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
		AllowReturn: text(values, "allow_return"), Source: source(),
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
	res, err := task.RecordDecision(w.st, req)
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}

	if *asJSON {
		out := contract.OperatorRecordOutput{Task: res.Task.Key(), Decision: decisionJSON(res.Decision)}
		if r := res.Return; r != nil {
			out.Return = &contract.OperatorRecordOutputReturn{Node: r.Node, To: r.To, Returns: r.Returns,
				MaxReturns: r.Limit, AllowedReturns: r.Allowed}
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.DecisionRecorded))
	if r := res.Return; r != nil {
		v, bad := w.view()
		if bad != nil {
			return w.fail(env, *bad)
		}
		fmt.Fprintln(&b, msg.Text(msg.AllowedReturnLine, named(v.NodeStageTitle(r.To), r.To)))
		fmt.Fprintln(&b, msg.Text(msg.ReturnsLine, r.Returns, r.Limit))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
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
			exit:    contract.ExitUsage,
			code:    contract.CodeInputInvalid,
			message: msg.Text(msg.ErrInputInvalid, cause),
			hint:    msg.Text(msg.HintCommandHelp, cmd),
			details: map[string]any{"input": input, "field": "options"},
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
	help := msg.Text(msg.HintCommandHelp, cmd)
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
		return refuse(fieldInvalid("options", optionsTooFew, msg.Text(msg.ErrOptionsTooFew), msg.Text(msg.HintOptionsTooFew)))
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
			f := fieldInvalid("options", reason, msg.Text(k, i+1), msg.Text(msg.HintOptionLabel))
			f.details["option"] = i + 1
			return &f
		}
		if o.Recommended {
			recommended++
		}
	}
	if recommended > 1 {
		return refuse(fieldInvalid("options", optionsRecommendedMany, msg.Text(msg.ErrOptionsRecommendedMany),
			msg.Text(msg.HintOptionsRecommended)))
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

// writeDecisions prints the decisions of the operator of a task under their
// heading, each a block: the stage and the round it was recorded at, who
// recorded it, the question with its options, the answer and the return it
// allows.
func writeDecisions(b *strings.Builder, v task.View, decisions []state.Decision) {
	fmt.Fprintln(b, msg.Text(msg.DecisionsHeading))
	for i, d := range decisions {
		if i > 0 {
			b.WriteString("\n")
		}
		stage := msg.Text(msg.StageRound, named(v.StageTitleOf(d.Stage), d.Stage), d.Round)
		fmt.Fprintf(b, "  %s\n", msg.Text(msg.DecisionHeading, d.Number, stage, sourceWord(d.Source)))
		indent := strings.Repeat(" ", 2+len(fmt.Sprintf("%d. ", d.Number)))
		if d.Question != "" {
			writeField(b, indent, msg.Text(msg.DecisionQuestion), d.Question)
		}
		if len(d.Options) > 0 {
			fmt.Fprintf(b, "%s%s\n", indent, msg.Text(msg.DecisionOptions))
			for j, o := range d.Options {
				number := fmt.Sprintf("%d. ", j+1)
				label := oneLine(o.Label)
				if o.Recommended {
					label = msg.Text(msg.OptionRecommended, label)
				}
				writeField(b, indent+"  "+number, label, o.Description)
			}
		}
		writeField(b, indent, msg.Text(msg.DecisionAnswer), d.Answer)
		if d.AllowReturn != "" {
			fmt.Fprintf(b, "%s%s\n", indent, msg.Text(msg.AllowedReturnLine, named(v.NodeStageTitle(d.AllowReturn), d.AllowReturn)))
		}
	}
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
