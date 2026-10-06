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

// writeNotes prints the notes of a task as a numbered list; the lines of a
// note of several lines are indented under its text.
func writeNotes(b *strings.Builder, notes []state.Note) {
	fmt.Fprintln(b, msg.Text(msg.NotesHeading))
	for _, n := range notes {
		mark := fmt.Sprintf("%d. ", n.Number)
		indent := strings.Repeat(" ", 2+len(mark))
		for i, line := range strings.Split(strings.TrimRight(n.Text, "\n"), "\n") {
			line = strings.TrimRight(line, " \t\r")
			switch {
			case i == 0:
				fmt.Fprintf(b, "  %s%s\n", mark, line)
			case line == "":
				b.WriteString("\n")
			default:
				fmt.Fprintf(b, "%s%s\n", indent, line)
			}
		}
	}
}
