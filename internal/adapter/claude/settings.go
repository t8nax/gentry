package claude

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/t8nax/gentry/internal/integration"
)

// ConfigDirEnv names the directory of the settings of Claude Code; without it
// the settings are in .claude in the home directory.
const ConfigDirEnv = "CLAUDE_CONFIG_DIR"

// PermissionRule is the rule of the settings of Claude Code that allows every
// tool of the server of Gentry: Claude Code names a tool of a server of a
// plugin mcp__plugin_<plugin>_<server>__<tool>.
const PermissionRule = "mcp__plugin_" + integration.Name + "_" + integration.ServerName

// Results of AllowTools.
const (
	PermissionAdded   = "added"   // the rule is added to the settings
	PermissionPresent = "present" // the settings have the rule
	PermissionFailed  = "failed"  // the settings cannot be read or written: they are left as they are
)

// SettingsPath returns the file of the user settings of Claude Code.
func SettingsPath() (string, error) {
	dir := os.Getenv(ConfigDirEnv)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json"), nil
}

// AllowTools adds PermissionRule to permissions.allow of the settings at
// path. The rule is put into the text of the file after the last rule, so
// the rest of the text stays as it is; a file without permissions.allow is
// written anew with its keys in their order, and a file that does not exist
// is created. A symbolic link is followed, and the file keeps its mode. A
// file that is not a JSON object, whose permissions or allow has another
// type, or that cannot be written is left as it is: PermissionFailed.
func AllowTools(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return PermissionFailed
	default:
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	}
	text := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(text)) == 0 {
		text = []byte("{}")
	}
	settings, ok := readObject(text)
	if !ok {
		return PermissionFailed
	}
	permissions := &object{}
	if raw, ok := settings.get("permissions"); ok {
		if permissions, ok = readObject(raw); !ok {
			return PermissionFailed
		}
	}
	var allow []json.RawMessage
	raw, hasAllow := permissions.get("allow")
	if hasAllow && (json.Unmarshal(raw, &allow) != nil || allow == nil) {
		return PermissionFailed
	}
	for _, r := range allow {
		var s string
		if json.Unmarshal(r, &s) == nil && s == PermissionRule {
			return PermissionPresent
		}
	}
	var out []byte
	if hasAllow {
		out, ok = insertRule(data)
		if !ok {
			return PermissionFailed
		}
	} else {
		permissions.set("allow", []byte(`["`+PermissionRule+`"]`))
		b, _ := permissions.MarshalJSON()
		settings.set("permissions", b)
		b, _ = settings.MarshalJSON()
		var indented bytes.Buffer
		if json.Indent(&indented, b, "", "  ") != nil {
			return PermissionFailed
		}
		indented.WriteByte('\n')
		out = indented.Bytes()
	}
	if writeFile(path, out, mode) != nil {
		return PermissionFailed
	}
	return PermissionAdded
}

// insertRule puts PermissionRule into the text of the settings after the
// last rule of permissions.allow, in the layout of the array: on a line of
// its own with the indent of the last rule, or after a comma on the same
// line.
func insertRule(data []byte) ([]byte, bool) {
	start, end, last, ok := allowArray(data)
	if !ok {
		return nil, false
	}
	rule := `"` + PermissionRule + `"`
	var insert string
	at := start + 1
	switch {
	case last < 0:
		insert = rule
	case bytes.IndexByte(data[start:end], '\n') >= 0:
		lineStart := bytes.LastIndexByte(data[:last], '\n') + 1
		indent := data[lineStart:last]
		for i, b := range indent {
			if b != ' ' && b != '\t' {
				indent = indent[:i]
				break
			}
		}
		insert, at = ",\n"+string(indent)+rule, lastEnd(data, last, end)
	default:
		insert, at = ", "+rule, lastEnd(data, last, end)
	}
	out := make([]byte, 0, len(data)+len(insert))
	out = append(out, data[:at]...)
	out = append(out, insert...)
	return append(out, data[at:]...), true
}

// lastEnd is the end of the last rule that starts at last, in an array that
// ends at end: before the spaces and line ends that follow it.
func lastEnd(data []byte, last, end int) int {
	i := end
	for i > last && bytes.IndexByte([]byte(" \t\r\n"), data[i-1]) >= 0 {
		i--
	}
	return i
}

// allowArray finds permissions.allow in the text of the settings: the
// offsets of its brackets and of the start of its last element, -1 if it is
// empty. The offsets are of data, with a byte order mark if it has one.
func allowArray(data []byte) (start, end, last int, ok bool) {
	text := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	bom := len(data) - len(text)
	dec := json.NewDecoder(bytes.NewReader(text))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') || !seekKey(dec, "permissions") {
		return 0, 0, 0, false
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('{') || !seekKey(dec, "allow") {
		return 0, 0, 0, false
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return 0, 0, 0, false
	}
	start, last = int(dec.InputOffset())-1, -1
	for dec.More() {
		before := int(dec.InputOffset())
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return 0, 0, 0, false
		}
		// The element starts after the comma and the spaces before it.
		last = before + bytes.Index(text[before:], v) + bom
	}
	if t, err := dec.Token(); err != nil || t != json.Delim(']') {
		return 0, 0, 0, false
	}
	return start + bom, int(dec.InputOffset()) - 1 + bom, last, true
}

// seekKey reads the keys of the object the decoder is in, skipping their
// values, up to key; false if the object has no such key.
func seekKey(dec *json.Decoder, key string) bool {
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return false
		}
		if t == key {
			return true
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
}

// writeFile replaces the file at path with data through a file beside it, so
// that a failure midway leaves the old file whole; the file gets mode.
func writeFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	tmp := path + ".tmp-" + hex.EncodeToString(suffix)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// object is a JSON object that keeps the order of its keys.
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
		if err != nil {
			return nil, false
		}
		key := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		o.set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return o, true
}

func (o *object) get(key string) (json.RawMessage, bool) {
	v, ok := o.values[key]
	return v, ok
}

func (o *object) set(key string, v json.RawMessage) {
	if o.values == nil {
		o.values = map[string]json.RawMessage{}
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = v
}

// MarshalJSON writes the object with its keys in their order and its values
// as they are, without escaping them again.
func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(o.values[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
