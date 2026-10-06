package task

import (
	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/state"
)

// EventNoteAdded is the event of a note added to a task.
const EventNoteAdded = "note.added"

// AddNote adds a note to a task at its current pass; once the scenario is
// passed, at its last one.
func AddNote(st *state.Store, task int64, text, source string) (state.Task, state.Note, error) {
	var t state.Task
	var n state.Note
	err := st.Write(func(tx *state.Tx) error {
		var err error
		if t, err = tx.Task(task); err != nil {
			return err
		}
		passes, err := tx.TaskPath(t.ID)
		if err != nil {
			return err
		}
		if len(passes) == 0 {
			return errUnreadable
		}
		p := passes[len(passes)-1]
		if n, err = tx.AddNote(t.ID, state.Note{Text: text, Pass: p.ID, Source: source}); err != nil {
			return err
		}
		n.Node, n.Stage, n.Round = p.Node, p.Stage, p.Round
		_, err = tx.AddEvent(EventNoteAdded, t.Project, t.Key(), contract.NoteAddedData{
			Number: n.Number, Text: n.Text, Node: n.Node, Round: n.Round, Source: contract.NoteAddedDataSource(source),
		})
		return err
	})
	if err != nil {
		return state.Task{}, state.Note{}, err
	}
	return t, n, nil
}
