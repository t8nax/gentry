package task

import (
	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/state"
)

// EventDecisionRecorded is the event of a decision of the operator recorded.
const EventDecisionRecorded = "operator_decision.recorded"

// ReturnNotFoundError means the current stage has no return to the node
// named: no transition to it, or one forward.
type ReturnNotFoundError struct {
	Node, To string
	Stage    flow.Stage // the stage of the node
	Returns  []string   // the nodes the node has returns to
}

func (e *ReturnNotFoundError) Error() string { return e.Node + ": no return to " + e.To }

// Decision is a decision of the operator to record.
type Decision struct {
	Task        int64 // the row of the task
	Question    string
	Options     []state.Option
	Answer      string
	AllowReturn string // the node of one more return from the current stage; empty if none
	Source      string
}

// Recorded is the result of recording a decision of the operator.
type Recorded struct {
	Task     state.Task
	Decision state.Decision
	Return   *Return // the return the decision allows; nil if none
}

// Return is a return from the current node of a task, with its limit.
type Return struct {
	Node, To string
	Returns  int // returns made by it, counted anew after a return by an outer loop
	Limit    int // the limit of the flow with the returns the operator allowed
	Allowed  int // returns of the limit the operator allowed
}

// RecordDecision records a decision of the operator at the current pass of
// the task, in one transaction with its event; once the scenario is passed,
// at its last pass. A decision that allows a return is checked against the
// returns of the current stage in the same transaction.
func RecordDecision(st *state.Store, req Decision) (Recorded, error) {
	var res Recorded
	err := st.Write(func(tx *state.Tx) error {
		t, err := active(tx, req.Task)
		if err != nil {
			return err
		}
		var pass state.Pass
		var sc flow.Scenario
		var loop flow.Loop
		if req.AllowReturn != "" {
			fl, err := txFlow(tx, t)
			if err != nil {
				return err
			}
			var node flow.Node
			var stage flow.Stage
			if sc, node, stage, pass, err = current(tx, t, fl); err != nil {
				return err
			}
			var ok bool
			if loop, ok = loopOf(sc.Loops(), node.ID, req.AllowReturn); !ok {
				return &ReturnNotFoundError{Node: node.ID, To: req.AllowReturn, Stage: stage, Returns: returnsOf(sc, node.ID)}
			}
		} else {
			passes, err := tx.TaskPath(t.ID)
			if err != nil {
				return err
			}
			if len(passes) == 0 {
				return errUnreadable
			}
			pass = passes[len(passes)-1]
		}
		d, err := tx.AddDecision(t.ID, state.Decision{
			Question: req.Question, Options: req.Options, Answer: req.Answer, Pass: pass.ID,
			AllowReturn: req.AllowReturn, Source: req.Source,
		})
		if err != nil {
			return err
		}
		d.Node, d.Stage, d.Round = pass.Node, pass.Stage, pass.Round
		res = Recorded{Task: t, Decision: d}
		if req.AllowReturn != "" {
			passes, err := tx.TaskPath(t.ID)
			if err != nil {
				return err
			}
			ds, err := tx.Decisions(t.ID)
			if err != nil {
				return err
			}
			allowed := allowedReturns(ds)[loopKey(loop)]
			res.Return = &Return{Node: loop.From, To: loop.To, Returns: countReturns(sc.Loops(), passes)[loopKey(loop)],
				Limit: loop.Max + allowed, Allowed: allowed}
		}
		_, err = tx.AddEvent(EventDecisionRecorded, t.Project, t.Key(), decisionData(d))
		return err
	})
	if err != nil {
		return Recorded{}, err
	}
	return res, nil
}

// allowedReturns counts the returns the operator allowed, by return: the
// node of the pass a decision was recorded at and the node it allows a
// return to. A return by an outer loop keeps them.
func allowedReturns(ds []state.Decision) map[[2]string]int {
	allowed := map[[2]string]int{}
	for _, d := range ds {
		if d.AllowReturn != "" {
			allowed[[2]string{d.Node, d.AllowReturn}]++
		}
	}
	return allowed
}

// returnsOf returns the nodes that node has returns to in sc.
func returnsOf(sc flow.Scenario, node string) []string {
	targets := []string{}
	for _, l := range sc.Loops() {
		if l.From == node {
			targets = append(targets, l.To)
		}
	}
	return targets
}

// decisionData returns the data of the event of a decision recorded.
func decisionData(d state.Decision) contract.OperatorDecisionRecordedData {
	data := contract.OperatorDecisionRecordedData{
		Number: d.Number, Options: OptionsJSON(d.Options), Answer: d.Answer, Node: d.Node, Round: d.Round,
		Source: contract.OperatorDecisionRecordedDataSource(d.Source),
	}
	if d.Question != "" {
		q := d.Question
		data.Question = &q
	}
	if d.AllowReturn != "" {
		a := d.AllowReturn
		data.AllowReturn = &a
	}
	return data
}

// OptionsJSON returns options of an answer as the contract has them; nil if
// there are none.
func OptionsJSON(options []state.Option) []contract.QuestionOption {
	var out []contract.QuestionOption
	for _, o := range options {
		co := contract.QuestionOption{Label: o.Label}
		if o.Description != "" {
			desc := o.Description
			co.Description = &desc
		}
		if o.Recommended {
			rec := true
			co.Recommended = &rec
		}
		out = append(out, co)
	}
	return out
}
