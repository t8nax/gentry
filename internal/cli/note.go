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
	asJSON := f.Bool("json")
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
		return fail(env, missingField(cmd, "text", msg.Text(msg.ErrNoteMissing), msg.Text(msg.HintCommandHelp, cmd)))
	}
	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	t, n, err := task.AddNote(w.st, w.task.ID, note, source())
	if err != nil {
		return w.fail(env, wayFailure(cmd, err))
	}
	if *asJSON {
		if err := writeJSON(env, contract.NoteAddOutput{Task: t.Key(), Note: noteJSON(n)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(msg.NoteAdded, n.Number))
	return contract.ExitOK
}

// noteJSON returns a note as the contract has it.
func noteJSON(n state.Note) contract.TaskNote {
	return contract.TaskNote{Number: n.Number, Text: n.Text, Node: n.Node, Stage: n.Stage, Round: n.Round,
		Source: contract.TaskNoteSource(n.Source), Added: n.Added}
}

func runNoteList(args []string, env Env) int {
	f := newFlags("note list")
	key := f.String("task")
	asJSON := f.Bool("json")
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
	if *asJSON {
		out := contract.NoteListOutput{Task: w.task.Key(), Notes: []contract.TaskNote{}}
		for _, n := range notes {
			out.Notes = append(out.Notes, noteJSON(n))
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if len(notes) == 0 {
		fmt.Fprintln(env.Stdout, msg.Text(msg.NotesNone))
		return contract.ExitOK
	}
	v, bad := w.view()
	if bad != nil {
		return w.fail(env, *bad)
	}
	var b strings.Builder
	for i, n := range notes {
		if i > 0 {
			b.WriteString("\n")
		}
		stage := msg.Text(msg.StageRound, named(v.StageTitleOf(n.Stage), n.Stage), n.Round)
		fmt.Fprintln(&b, msg.Text(msg.NoteHeading, n.Number, stage))
		writeIndented(&b, n.Text, strings.Repeat(" ", len(fmt.Sprintf("%d. ", n.Number))))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// writeIndented prints a text written by a person as it is, each line with
// indent; empty lines stay empty.
func writeIndented(b *strings.Builder, text, indent string) {
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "%s%s\n", indent, line)
	}
}
