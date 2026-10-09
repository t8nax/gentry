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
	PermissionFailed  = "failed"  // the settings cannot be read: they are left as they are
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
// path, keeping everything else and the order of the keys. A file that does
// not exist is created. A file that is not a JSON object, or whose
// permissions or allow has another type, is left as it is: PermissionFailed.
// An error is a failure to write.
func AllowTools(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data = []byte("{}")
	} else if err != nil {
		return PermissionFailed, nil
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	settings, ok := readObject(data)
	if !ok {
		return PermissionFailed, nil
	}
	permissions := &object{}
	if raw, ok := settings.get("permissions"); ok {
		if permissions, ok = readObject(raw); !ok {
			return PermissionFailed, nil
		}
	}
	var allow []json.RawMessage
	if raw, ok := permissions.get("allow"); ok {
		if json.Unmarshal(raw, &allow) != nil || allow == nil {
			return PermissionFailed, nil
		}
	}
	for _, r := range allow {
		var s string
		if json.Unmarshal(r, &s) == nil && s == PermissionRule {
			return PermissionPresent, nil
		}
	}
	rule, _ := json.Marshal(PermissionRule)
	allow = append(allow, rule)
	b, err := json.Marshal(allow)
	if err != nil {
		return "", err
	}
	permissions.set("allow", b)
	if b, err = permissions.MarshalJSON(); err != nil {
		return "", err
	}
	settings.set("permissions", b)
	if b, err = settings.MarshalJSON(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return "", err
	}
	out.WriteByte('\n')
	if err := writeFile(path, out.Bytes()); err != nil {
		return "", err
	}
	return PermissionAdded, nil
}

// writeFile replaces the file at path with data through a file beside it, so
// that a failure midway leaves the old file whole.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	tmp := path + ".tmp-" + hex.EncodeToString(suffix)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
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
