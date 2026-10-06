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

// readInput reads the fields of a command given by --input: one JSON object
// in the file named by value, or on the standard input for "-". Every field
// must be one of names and a string. The commands of parts 10–12 read their
// fields the same way.
func readInput(cmd, value string, stdin io.Reader, names []string) (map[string]string, *failure) {
	refuse := func(cause, field string) (map[string]string, *failure) {
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
	fields := map[string]string{}
	// Fields in a stable order, so that the refusal names the same one each time.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !slices.Contains(names, k) {
			return refuse(msg.Text(msg.InputUnknownField, k), k)
		}
		var s *string
		if err := json.Unmarshal(raw[k], &s); err != nil || s == nil {
			return refuse(msg.Text(msg.InputNotString, k), k)
		}
		fields[k] = *s
	}
	return fields, nil
}
