// Package claude is the adapter for Claude Code: it turns the neutral
// integration description into a Claude Code plugin served from a local
// marketplace directory.
package claude

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/integration"
)

// Tool is the name of Claude Code in `gentry setup`.
const Tool = "claude"

// hookTimeout limits a hook run in seconds, in case it hangs; a normal run
// takes milliseconds.
const hookTimeout = 10

// events maps neutral hook events to Claude Code hook events. SessionStart
// without a matcher fires on every kind of start: startup, resume, clear and
// compact.
var events = map[string]string{
	hook.SessionStart: "SessionStart",
}

// Plugin is a built plugin: files of the marketplace directory by
// slash-separated relative path, and the plugin version.
type Plugin struct {
	Version string
	Files   map[string][]byte
}

// Build turns the description into a Claude Code plugin. The plugin version is
// the gentry version without build metadata plus a fingerprint of the plugin
// content, so it changes whenever the content does: Claude Code updates an
// installed plugin only when its version changes.
func Build(d integration.Description, gentryVersion string) (Plugin, error) {
	unversioned, err := buildFiles(d, "")
	if err != nil {
		return Plugin{}, err
	}
	base, _, _ := strings.Cut(gentryVersion, "+")
	version := base + "+" + fingerprint(unversioned)
	files, err := buildFiles(d, version)
	if err != nil {
		return Plugin{}, err
	}
	return Plugin{Version: version, Files: files}, nil
}

func buildFiles(d integration.Description, version string) (map[string][]byte, error) {
	hooks := map[string][]any{}
	for _, h := range d.Hooks {
		event, ok := events[h.Event]
		if !ok {
			return nil, fmt.Errorf("claude: unsupported hook event %q", h.Event)
		}
		hooks[event] = append(hooks[event], map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": shellCommand(h.Command),
				"timeout": hookTimeout,
			}},
		})
	}
	plugin := map[string]any{
		"name":        d.Name,
		"version":     version,
		"description": d.Description,
	}
	marketplace := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"owner":       map[string]any{"name": "Gentry"},
		"plugins": []any{map[string]any{
			"name":        d.Name,
			"source":      "./" + d.Name,
			"description": d.Description,
			"version":     version,
		}},
	}
	out := map[string]any{
		".claude-plugin/marketplace.json":      marketplace,
		d.Name + "/.claude-plugin/plugin.json": plugin,
		d.Name + "/hooks/hooks.json":           map[string]any{"hooks": hooks},
	}
	files := map[string][]byte{}
	for path, v := range out {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		files[path] = append(b, '\n')
	}
	return files, nil
}

// shellCommand joins a command for the shell Claude Code runs hooks in. The
// program path uses forward slashes, which every shell on Windows accepts,
// and is quoted in case it contains spaces.
func shellCommand(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if i == 0 {
			a = filepath.ToSlash(a)
		}
		if i == 0 || strings.ContainsAny(a, " \t\"'") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

// fingerprint is a short hash of the files.
func fingerprint(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(files[p]))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// Write replaces dir with the plugin files. The files are written to a
// sibling directory first, so a failure midway leaves the old plugin intact.
func Write(dir string, p Plugin) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	tmp := dir + ".tmp-" + hex.EncodeToString(suffix)
	if err := writeFiles(tmp, p.Files); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

func writeFiles(dir string, files map[string][]byte) error {
	for path, b := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
