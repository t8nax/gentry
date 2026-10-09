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
	PermissionAdded     = "added"     // the rule is added to the settings
	PermissionPresent   = "present"   // the settings have the rule
	PermissionFailed    = "failed"    // the settings cannot be read: they are left as they are
	PermissionUnwritten = "unwritten" // the settings cannot be written: they are left as they are
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

// rule is PermissionRule as a JSON string.
const rule = `"` + PermissionRule + `"`

// newSettings are the settings written where there were none.
const newSettings = "{\n  \"permissions\": {\n    \"allow\": [\n      " + rule + "\n    ]\n  }\n}\n"

// AllowTools adds PermissionRule to permissions.allow of the settings at
// path. The rule goes into the text of the file: after the last rule, or
// with the allow or the permissions it needs after the last key of the object
// it goes in, so the rest of the text stays as it is. A file that does not
// exist is created. A symbolic link is followed, and the file keeps its
// mode. A file that is not a JSON object, or whose permissions or allow has
// another type, is left as it is: PermissionFailed; a file that cannot be
// written, PermissionUnwritten.
func AllowTools(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return PermissionFailed
	default:
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	}
	text := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(text)) == 0 {
		if writeSettings(path, []byte(newSettings), mode) != nil {
			return PermissionUnwritten
		}
		return PermissionAdded
	}
	settings, ok := readObject(text)
	if !ok {
		return PermissionFailed
	}
	var out []byte
	if raw, has := settings["permissions"]; !has {
		out, ok = insertItem(data, `"permissions": {"allow": [`+rule+`]}`)
	} else if permissions, isObject := readObject(raw); !isObject {
		return PermissionFailed
	} else if raw, has := permissions["allow"]; !has {
		out, ok = insertItem(data, `"allow": [`+rule+`]`, "permissions")
	} else {
		var allow []json.RawMessage
		if json.Unmarshal(raw, &allow) != nil || allow == nil {
			return PermissionFailed
		}
		for _, r := range allow {
			var s string
			if json.Unmarshal(r, &s) == nil && s == PermissionRule {
				return PermissionPresent
			}
		}
		out, ok = insertItem(data, rule, "permissions", "allow")
	}
	if !ok {
		return PermissionFailed
	}
	if writeSettings(path, out, mode) != nil {
		return PermissionUnwritten
	}
	return PermissionAdded
}

// container is an object or an array in the text of the settings: the
// offsets of its brackets and of the start and the end of its last element,
// or key with its value; -1 if it is empty.
type container struct {
	open, close, lastStart, lastEnd int
}

// locate finds the object or the array at the keys in the text of the
// settings, from the root object. The offsets are of data, with a byte order
// mark if it has one.
func locate(data []byte, keys ...string) (container, bool) {
	text := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	bom := len(data) - len(text)
	dec := json.NewDecoder(bytes.NewReader(text))
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return container{}, false
	}
	for _, k := range keys {
		if !seekKey(dec, k) {
			return container{}, false
		}
		if t, err = dec.Token(); err != nil || (t != json.Delim('{') && t != json.Delim('[')) {
			return container{}, false
		}
	}
	c := container{open: int(dec.InputOffset()) - 1, lastStart: -1, lastEnd: -1}
	object := t == json.Delim('{')
	for dec.More() {
		before := int(dec.InputOffset())
		if object {
			if _, err := dec.Token(); err != nil {
				return container{}, false
			}
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return container{}, false
		}
		// The element, or the key, starts after the comma and the spaces
		// before it.
		c.lastStart = before + bytes.IndexAny(text[before:], `"{[-0123456789tfn`)
		c.lastEnd = int(dec.InputOffset())
	}
	if _, err := dec.Token(); err != nil {
		return container{}, false
	}
	c.close = int(dec.InputOffset()) - 1
	c.open, c.close = c.open+bom, c.close+bom
	if c.lastStart >= 0 {
		c.lastStart, c.lastEnd = c.lastStart+bom, c.lastEnd+bom
	}
	return c, true
}

// insertItem puts item into the object or the array at the keys in the text
// of the settings, in its layout: after the last element, on a line of its
// own with its indent or after a comma on the same line; in an empty
// container written over lines, on a line of its own before the closing
// bracket. The line ends of the file are kept.
func insertItem(data []byte, item string, keys ...string) ([]byte, bool) {
	c, ok := locate(data, keys...)
	if !ok {
		return nil, false
	}
	nl := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		nl = "\r\n"
	}
	lineEnd := bytes.LastIndexByte(data[:c.close], '\n')
	overLines := lineEnd > c.open
	var insert string
	var at int
	switch {
	case c.lastStart < 0 && overLines:
		// Before the line end of the line of the closing bracket.
		at = lineEnd
		if data[at-1] == '\r' {
			at--
		}
		insert = nl + indentOf(data, c.close) + "  " + item
	case c.lastStart < 0:
		insert, at = item, c.open+1
	case overLines:
		insert, at = ","+nl+indentOf(data, c.lastStart)+item, c.lastEnd
	default:
		insert, at = ", "+item, c.lastEnd
	}
	out := make([]byte, 0, len(data)+len(insert))
	out = append(out, data[:at]...)
	out = append(out, insert...)
	return append(out, data[at:]...), true
}

// indentOf is the spaces and tabs at the start of the line of offset.
func indentOf(data []byte, offset int) string {
	start := bytes.LastIndexByte(data[:offset], '\n') + 1
	end := start
	for end < offset && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[start:end])
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

// writeSettings writes the settings; tests replace it to fail.
var writeSettings = writeFile

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

// readObject reads a JSON object into its values by key; false if data is
// not one.
func readObject(data []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var o map[string]json.RawMessage
	if err := dec.Decode(&o); err != nil || o == nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return o, true
}
