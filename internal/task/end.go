package task

import (
	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/state"
)

// Events of the end of a task.
const (
	EventClosed    = "task.closed"
	EventCancelled = "task.cancelled"
)

// EndedError means the task is closed or cancelled: it changes no more.
type EndedError struct{ Task, State string }

func (e *EndedError) Error() string { return e.Task + ": " + e.State }

// NotFinishedError means the scenario of the task is not passed, so the task
// cannot be closed.
type NotFinishedError struct{ Task, Node string }

func (e *NotFinishedError) Error() string { return e.Task + ": scenario not passed" }

// active returns the task of row id as the transaction sees it, unless the
// task is closed or cancelled.
func active(tx *state.Tx, id int64) (state.Task, error) {
	t, err := tx.Task(id)
	if err != nil {
		return state.Task{}, err
	}
	if t.IsEnded() {
		return state.Task{}, &EndedError{Task: t.Key(), State: t.State}
	}
	return t, nil
}

// End is the closing or the cancelling of a task.
type End struct {
	Task   int64 // the row of the task
	Cancel bool
	Reason string // why the task is cancelled; may be empty
	Source string
}

// Ended is a task closed or cancelled and the worktree it released.
type Ended struct {
	Task     state.Task
	Worktree string
}

// EndTask closes or cancels a task in one transaction with its event: the
// task releases its worktree. A task is closed only once its scenario is
// passed; both are checked in the transaction, so that of two commands
// ending one task only one does. The worktree is checked for changes before.
func EndTask(st *state.Store, req End) (Ended, error) {
	var res Ended
	err := st.Write(func(tx *state.Tx) error {
		t, err := active(tx, req.Task)
		if err != nil {
			return err
		}
		if !req.Cancel && t.Node != state.Finish {
			return &NotFinishedError{Task: t.Key(), Node: t.Node}
		}
		res.Worktree = t.Worktree
		to := state.TaskClosed
		if req.Cancel {
			to = state.TaskCancelled
		}
		if res.Task, err = tx.EndTask(t.ID, to, req.Source, req.Reason); err != nil {
			return err
		}
		var data any = contract.TaskClosedData{Attempt: t.Attempt, Worktree: res.Worktree,
			Source: contract.TaskClosedDataSource(req.Source)}
		typ := EventClosed
		if req.Cancel {
			d := contract.TaskCancelledData{Attempt: t.Attempt, Node: t.Node, Worktree: res.Worktree,
				Source: contract.TaskCancelledDataSource(req.Source)}
			if req.Reason != "" {
				d.Reason = &req.Reason
			}
			data, typ = d, EventCancelled
		}
		_, err = tx.AddEvent(typ, t.Project, t.Key(), data)
		return err
	})
	if err != nil {
		return Ended{}, err
	}
	return res, nil
}
