package contract

import (
	"bytes"
	"encoding/json"
)

// MarshalJSON writes the lists of worktrees of Codex when they are set,
// empty ones too: a list absent and an empty one tell different things,
// while the tags of the generated type omit an empty list. Fields keep the
// order of the other outputs: by name. The signs &, < and > are written as
// they are, as in the output of every command.
func (s SetupOutput) MarshalJSON() ([]byte, error) {
	type plain SetupOutput
	b, err := marshalPlain(plain(s))
	if err != nil {
		return nil, err
	}
	if s.WorktreesTrusted == nil && s.WorktreesDistrusted == nil {
		return b, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	for name, list := range map[string][]string{"worktrees_trusted": s.WorktreesTrusted, "worktrees_distrusted": s.WorktreesDistrusted} {
		if list != nil {
			if fields[name], err = marshalPlain(list); err != nil {
				return nil, err
			}
		}
	}
	return marshalPlain(fields)
}

// marshalPlain marshals v without escaping &, < and >.
func marshalPlain(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
