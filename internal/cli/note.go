package cli

import (
	"fmt"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// noteFields are the fields of note add; the text is the argument.
var noteFields = textFields("text")

func runNoteAdd(args []string, env Env) int {
	const cmd = "note add"
	f := newFlags(cmd)
	key := f.String("task")
	input := f.String("input")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, nil, f.args, env.Stdin, noteFields)
	if bad != nil {
		return fail(env, *bad)
	}
	note := text(values, "text")
	if !input.Set && len(f.args) > 0 {
		note = f.args[0]
	}
	if strings.TrimSpace(note) == "" {
		return fail(env, missingField(cmd, "text", msg.Text(msg.ErrNoteMissing), helpHint(msg.HintCommandHelp, cmd)))
	}
	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	t, n, err := task.AddNote(w.st, w.task.ID, note, source(env))
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}
	return emit(env, contract.NoteAddOutput{Task: t.Key(), Note: noteJSON(n)}, noteAddText)
}

// noteAddText prints the number of the note added.
func noteAddText(p *page, out contract.NoteAddOutput) {
	fmt.Fprintln(p, msg.Text(msg.NoteAdded, out.Note.Number))
}

// noteJSON returns a note as the contract has it.
func noteJSON(n state.Note) contract.TaskNote {
	return contract.TaskNote{Number: n.Number, Text: n.Text, Node: n.Node, Stage: n.Stage, Round: n.Round,
		Source: contract.TaskNoteSource(n.Source), Added: n.Added}
}

func runNoteList(args []string, env Env) int {
	f := newFlags("note list")
	key := f.String("task")
	if code, done := f.parse(args, env); done {
		return code
	}
	w, bad := openWayTask(key, true)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	notes, err := w.st.Notes(w.task.ID)
	if err != nil {
		return w.fail(env, stateFailure(err))
	}
	out := contract.NoteListOutput{Task: w.task.Key(), Notes: []contract.TaskNote{}}
	for _, n := range notes {
		out.Notes = append(out.Notes, noteJSON(n))
	}
	// The stages of the notes are named by the snapshot of the task.
	var n names
	if len(notes) > 0 {
		v, bad := w.view()
		if bad != nil {
			return w.fail(env, *bad)
		}
		n = namesOf(v)
	}
	return emit(env, out, func(p *page, out contract.NoteListOutput) { noteListText(p, out, n) })
}

// noteListText prints the notes of the task, their stages named by n, the
// names of its snapshot.
func noteListText(p *page, out contract.NoteListOutput, n names) {
	if len(out.Notes) == 0 {
		fmt.Fprintln(p, msg.Text(msg.NotesNone))
		return
	}
	writeNotes(&p.Builder, out.Notes, n, "")
}
