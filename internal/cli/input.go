package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
)

// fieldKind is the type of a field of --input.
type fieldKind int

const (
	textField   fieldKind = iota // a string
	numberField                  // an integer
	listField                    // an array of strings
)

// field is a field of a command: a flag or an argument, and a field of
// --input of the same name.
type field struct {
	name string
	kind fieldKind
}

// textFields returns fields of strings named names.
func textFields(names ...string) []field {
	fs := make([]field, len(names))
	for i, n := range names {
		fs[i] = field{name: n}
	}
	return fs
}

// readInput reads the fields of a command given by --input: one JSON object
// in the file named by value, or on the standard input for "-". Every field
// must be one of names and a string.
func readInput(cmd, value string, stdin io.Reader, names []string) (map[string]string, *failure) {
	values, bad := readFields(cmd, value, stdin, textFields(names...))
	if bad != nil {
		return nil, bad
	}
	texts := map[string]string{}
	for k, v := range values {
		texts[k] = v.(string)
	}
	return texts, nil
}

// readFields reads the fields of a command given by --input, as readInput
// does, each of the type of its field: a string, an int or a []string.
func readFields(cmd, value string, stdin io.Reader, fields []field) (map[string]any, *failure) {
	refuse := func(cause, field string) (map[string]any, *failure) {
		f := failure{
			exit:    contract.ExitUsage,
			code:    contract.CodeInputInvalid,
			message: msg.Text(msg.ErrInputInvalid, cause),
			hint:    msg.Text(msg.HintCommandHelp, cmd),
			details: map[string]any{"input": value},
		}
		if field != "" {
			f.details["field"] = field
		}
		return nil, &f
	}
	var data []byte
	var err error
	if value == "-" {
		if stdin != nil {
			data, err = io.ReadAll(stdin)
		}
	} else {
		data, err = os.ReadFile(value)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return refuse(msg.Text(msg.InputNotFound), "")
	case err != nil:
		return refuse(msg.Text(msg.InputUnreadable), "")
	}
	// A file saved by a Windows editor may start with a byte order mark.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil || raw == nil {
		return refuse(msg.Text(msg.InputNotObject), "")
	}
	// Nothing may follow the object.
	if _, err := dec.Token(); err != io.EOF {
		return refuse(msg.Text(msg.InputNotObject), "")
	}
	values := map[string]any{}
	// Fields in a stable order, so that the refusal names the same one each time.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		i := slices.IndexFunc(fields, func(f field) bool { return f.name == k })
		if i < 0 {
			return refuse(msg.Text(msg.InputUnknownField, k), k)
		}
		switch fields[i].kind {
		case numberField:
			var n *int
			if err := json.Unmarshal(raw[k], &n); err != nil || n == nil {
				return refuse(msg.Text(msg.InputNotInteger, k), k)
			}
			values[k] = *n
		case listField:
			var l []*string
			if err := json.Unmarshal(raw[k], &l); err != nil || l == nil || slices.Contains(l, nil) {
				return refuse(msg.Text(msg.InputNotList, k), k)
			}
			list := make([]string, len(l))
			for j, s := range l {
				list[j] = *s
			}
			values[k] = list
		default:
			var s *string
			if err := json.Unmarshal(raw[k], &s); err != nil || s == nil {
				return refuse(msg.Text(msg.InputNotString, k), k)
			}
			values[k] = *s
		}
	}
	return values, nil
}

// commandFields gathers the fields of a command: from --input if it is
// given, otherwise from the flags of the fields, by the names of fields.
// Arguments are fields too: with --input there must be none.
func commandFields(cmd string, input *stringFlag, flags map[string]*stringFlag, args []string, stdin io.Reader, fields []field) (map[string]any, *failure) {
	if input.Set {
		if input.Value == "" {
			f := flagValueMissing("--input")
			return nil, &f
		}
		for _, fl := range fields {
			if v, ok := flags[fl.name]; ok && v.Set {
				f := conflictingFlags(cmd, []string{"--" + fl.name, "--input"})
				return nil, &f
			}
		}
		if len(args) > 0 {
			f := failure{
				exit:    contract.ExitUsage,
				code:    contract.CodeConflictingFlags,
				message: msg.Text(msg.ErrInputWithArgs),
				hint:    msg.Text(msg.HintCommandHelp, cmd),
				details: map[string]any{"command": cmd, "flags": []string{"--input"}, "args": args},
			}
			return nil, &f
		}
		return readFields(cmd, input.Value, stdin, fields)
	}
	values := map[string]any{}
	for _, fl := range fields {
		v, ok := flags[fl.name]
		if !ok || !v.Set {
			continue
		}
		if v.Value == "" {
			f := flagValueMissing("--" + fl.name)
			return nil, &f
		}
		values[fl.name] = v.Value
	}
	return values, nil
}

// text returns the string field name of values, "" if absent.
func text(values map[string]any, name string) string {
	s, _ := values[name].(string)
	return s
}
