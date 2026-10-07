package task

import (
	"strings"
	"unicode/utf8"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/state"
)

// Events of steps.
const (
	EventStepAdded   = "step.added"
	EventStepDone    = "step.done"
	EventStepDropped = "step.dropped"
)

// StepMax is the limit of a step in characters.
const StepMax = 120

// CheckStep returns why a step is refused, as the contract names it, or ""
// if it is fine.
func CheckStep(step string) string {
	switch {
	case strings.ContainsAny(step, "\r\n"):
		return TitleMultiline
	case utf8.RuneCountInString(step) > StepMax:
		return TitleTooLong
	}
	return ""
}

// StepNotFoundError means the current pass has no step of the number.
type StepNotFoundError struct{ Step int }

func (e *StepNotFoundError) Error() string { return "no step" }

// StepClosedError means the step is done or dropped already.
type StepClosedError struct {
	Step  int
	State string
}

func (e *StepClosedError) Error() string { return "step " + e.State }

// Steps is the current pass of a task after a change of its steps.
type Steps struct {
	Task  state.Task
	Pass  state.Pass
	Added []int // the numbers of the steps added
}

// AddSteps adds steps after the steps of the current pass of a task.
func AddSteps(st *state.Store, task int64, titles []string, source string) (Steps, error) {
	return changeSteps(st, task, func(tx *state.Tx, t state.Task, p state.Pass) ([]int, error) {
		numbers, err := tx.AddSteps(p.ID, titles, source)
		if err != nil {
			return nil, err
		}
		data := contract.StepAddedData{Node: p.Node, Round: p.Round, Source: contract.StepAddedDataSource(source)}
		for i, n := range numbers {
			data.Steps = append(data.Steps, contract.StepAddedStep{Number: n, Title: titles[i]})
		}
		_, err = tx.AddEvent(EventStepAdded, t.Project, t.Key(), data)
		return numbers, err
	})
}

// CloseStep marks a step of the current pass of a task done, with how it was
// checked, or dropped, with the reason.
func CloseStep(st *state.Store, task int64, number int, done bool, text, source string) (Steps, error) {
	return changeSteps(st, task, func(tx *state.Tx, t state.Task, p state.Pass) ([]int, error) {
		var step *state.Step
		for i := range p.Steps {
			if p.Steps[i].Number == number {
				step = &p.Steps[i]
			}
		}
		switch {
		case step == nil:
			return nil, &StepNotFoundError{Step: number}
		case step.State != state.StepPlanned:
			return nil, &StepClosedError{Step: number, State: step.State}
		}
		to, typ := state.StepDropped, EventStepDropped
		var data any = contract.StepDroppedData{Node: p.Node, Round: p.Round, Number: number, Reason: text,
			Source: contract.StepDroppedDataSource(source)}
		if done {
			to, typ = state.StepDone, EventStepDone
			d := contract.StepDoneData{Node: p.Node, Round: p.Round, Number: number, Source: contract.StepDoneDataSource(source)}
			if text != "" {
				d.Check = &text
			}
			data = d
		}
		if err := tx.CloseStep(p.ID, number, to, text, source); err != nil {
			return nil, err
		}
		_, err := tx.AddEvent(typ, t.Project, t.Key(), data)
		return nil, err
	})
}

// changeSteps runs change on the current pass of a task in a transaction
// and returns the pass after it.
func changeSteps(st *state.Store, task int64, change func(*state.Tx, state.Task, state.Pass) ([]int, error)) (Steps, error) {
	var res Steps
	err := st.Write(func(tx *state.Tx) error {
		t, err := active(tx, task)
		if err != nil {
			return err
		}
		p, ok, err := tx.CurrentPass(t.ID)
		if err != nil {
			return err
		}
		if !ok {
			return &FinishedError{Task: t.Key()}
		}
		added, err := change(tx, t, p)
		if err != nil {
			return err
		}
		if p, _, err = tx.CurrentPass(t.ID); err != nil {
			return err
		}
		res = Steps{Task: t, Pass: p, Added: added}
		return nil
	})
	if err != nil {
		return Steps{}, err
	}
	return res, nil
}
