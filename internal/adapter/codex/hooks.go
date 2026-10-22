package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/t8nax/gentry/internal/hook"
)

// HookSuffix ends the command of the handler of Gentry in the settings of
// the hooks of Codex: a handler whose command ends with it is Gentry's, of
// whichever program, and is replaced.
const HookSuffix = "hook " + hook.SessionStart + " --tool " + Tool

// sessionStart is the event of Codex the hook of Gentry runs on.
const sessionStart = "SessionStart"

// hookTimeout limits a hook run in seconds, in case it hangs; a normal run
// takes milliseconds.
const hookTimeout = 10

// HookCommand is the command of the hook of Gentry for the program exe, as
// the shell of Codex runs it: the path with forward slashes, which every
// shell on Windows accepts, in double quotes if it has a space.
func HookCommand(exe string) string {
	p := filepath.ToSlash(exe)
	if strings.ContainsAny(p, " \t") {
		p = `"` + p + `"`
	}
	return p + " " + HookSuffix
}

// handler is the handler of Gentry for the program exe in the settings of
// the hooks of Codex. With additionalContextLimit 0 Codex gives the agent
// the whole introduction: by default it cuts the output of a hook to about
// 2500 tokens.
func handler(exe string) map[string]any {
	return map[string]any{
		"type":                   "command",
		"command":                HookCommand(exe),
		"timeout":                hookTimeout,
		"additionalContextLimit": 0,
	}
}

// The states of the hook of Gentry in the settings of the hooks of Codex,
// as the contract names them.
const (
	HookFailed    = "failed"    // the settings of the hooks cannot be read: they are left as they are
	HookUnwritten = "unwritten" // the settings of the hooks cannot be written: they are left as they are
)

// errHooks means the settings of the hooks of Codex are no JSON object of
// the form of Codex.
var errHooks = errors.New("the settings of the hooks of Codex cannot be read")

// object is a JSON object with its keys in the order of the text.
type object struct {
	keys   []string
	values map[string]json.RawMessage
}

// readObject reads a JSON object; false if data is not one.
func readObject(data []byte) (*object, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	o := &object{values: map[string]json.RawMessage{}}
	for dec.More() {
		t, err := dec.Token()
		k, ok := t.(string)
		if err != nil || !ok {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		if _, dup := o.values[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.values[k] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return o, true
}

// set sets the value of k; a new key goes last.
func (o *object) set(k string, v json.RawMessage) {
	if _, ok := o.values[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

// remove removes k.
func (o *object) remove(k string) {
	delete(o.values, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.values[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// raw marshals v, which is always possible for the values of Gentry.
func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// isGentry reports whether the raw handler is one of Gentry: its command
// ends with HookSuffix.
func isGentry(h json.RawMessage) bool {
	var v struct {
		Command string `json:"command"`
	}
	return json.Unmarshal(h, &v) == nil && strings.HasSuffix(strings.TrimSpace(v.Command), HookSuffix)
}

// sameJSON reports whether the raw value is the value v.
func sameJSON(a json.RawMessage, v any) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(raw(v), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// hookEdit works out the settings of the hooks of Codex in data with the
// handler of Gentry want once, of the event SessionStart: the handlers of
// Gentry are removed, with the groups and the events they leave empty, and
// want is added in a group of its own at the end of the event. The rest of
// the settings stays, its keys in their order. changed is false if the
// settings have the handler as it is wanted and no other of Gentry.
func hookEdit(data []byte, want map[string]any) (out []byte, changed bool, err error) {
	top := &object{values: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) > 0 {
		var ok bool
		if top, ok = readObject(data); !ok {
			return nil, false, errHooks
		}
	}
	events := &object{values: map[string]json.RawMessage{}}
	if v, ok := top.values["hooks"]; ok {
		if events, ok = readObject(v); !ok {
			return nil, false, errHooks
		}
	}
	found, matching := 0, false
	for _, e := range events.keys {
		var groups []json.RawMessage
		if json.Unmarshal(events.values[e], &groups) != nil {
			continue
		}
		for _, g := range groups {
			group, ok := readObject(g)
			if !ok {
				continue
			}
			var handlers []json.RawMessage
			if json.Unmarshal(group.values["hooks"], &handlers) != nil {
				continue
			}
			for _, h := range handlers {
				if isGentry(h) {
					found++
					matching = e == sessionStart && len(group.keys) == 1 && sameJSON(h, want)
				}
			}
		}
	}
	if found == 1 && matching {
		return data, false, nil
	}
	for _, e := range append([]string{}, events.keys...) {
		var groups []json.RawMessage
		if json.Unmarshal(events.values[e], &groups) != nil {
			continue
		}
		var kept []json.RawMessage
		removed := false
		for _, g := range groups {
			group, ok := readObject(g)
			var handlers []json.RawMessage
			if !ok || json.Unmarshal(group.values["hooks"], &handlers) != nil {
				kept = append(kept, g)
				continue
			}
			var rest []json.RawMessage
			for _, h := range handlers {
				if isGentry(h) {
					removed = true
					continue
				}
				rest = append(rest, h)
			}
			if len(rest) == len(handlers) {
				kept = append(kept, g)
				continue
			}
			if len(rest) > 0 {
				group.set("hooks", raw(rest))
				kept = append(kept, raw(group))
			}
		}
		if !removed {
			continue
		}
		if len(kept) == 0 {
			events.remove(e)
			continue
		}
		events.set(e, raw(kept))
	}
	var groups []json.RawMessage
	if v, ok := events.values[sessionStart]; ok {
		json.Unmarshal(v, &groups)
	}
	groups = append(groups, raw(map[string]any{"hooks": []any{want}}))
	events.set(sessionStart, raw(groups))
	top.set("hooks", raw(events))
	var b bytes.Buffer
	if err := json.Indent(&b, raw(top), "", "  "); err != nil {
		return nil, false, err
	}
	b.WriteByte('\n')
	return b.Bytes(), true, nil
}

// writeHook makes the settings of the hooks of Codex at path have the
// handler of Gentry for the program exe once. It returns HookFailed for
// settings that cannot be read and HookUnwritten for settings that cannot
// be written, left as they are; "" once they have the handler. A file that
// is not there is created.
func writeHook(path, exe string) string {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return HookFailed
	}
	out, changed, err := hookEdit(bytes.TrimPrefix(data, []byte(bom)), handler(exe))
	if err != nil {
		return HookFailed
	}
	if !changed {
		return ""
	}
	if writeFile(path, out) != nil {
		return HookUnwritten
	}
	return ""
}

// hookCurrent reports whether the settings of the hooks of Codex at path
// have the handler of Gentry for the program exe once, as writeHook writes
// it; nothing is written.
func hookCurrent(path, exe string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, changed, err := hookEdit(bytes.TrimPrefix(data, []byte(bom)), handler(exe))
	return err == nil && !changed
}
