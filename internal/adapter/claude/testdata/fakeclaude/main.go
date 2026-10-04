// Command fakeclaude imitates the `claude plugin` commands Gentry uses, with
// the same JSON output, so that `gentry setup claude` can be tested without
// touching a real Claude Code. State is kept in the JSON file named by
// FAKECLAUDE_STATE (default: fakeclaude-state.json next to the binary).
// FAKECLAUDE_FAIL=<action> makes that command fail: install, update,
// marketplace-add and so on.
//
// Build: go build -o fakeclaude.exe ./internal/adapter/claude/testdata/fakeclaude
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type marketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
}

type plugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
}

type state struct {
	Marketplaces []marketplace `json:"marketplaces"`
	Plugins      []plugin      `json:"plugins"`
	Log          []string      `json:"log"` // commands run, one line each
}

func main() {
	path := os.Getenv("FAKECLAUDE_STATE")
	if path == "" {
		exe, _ := os.Executable()
		path = filepath.Join(filepath.Dir(exe), "fakeclaude-state.json")
	}
	var s state
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s)
	}
	args := os.Args[1:]
	s.Log = append(s.Log, strings.Join(args, " "))
	code := run(&s, args)
	b, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(path, b, 0o644)
	os.Exit(code)
}

func run(s *state, args []string) int {
	var pos []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && a != "user" {
			pos = append(pos, a)
		}
	}
	cmd := strings.Join(pos[:min(len(pos), 3)], " ")
	if fail := os.Getenv("FAKECLAUDE_FAIL"); fail != "" && fail == action(pos) {
		return failed(fail, "simulated failure")
	}
	// Positional arguments each command needs, counting "plugin" and the
	// command words.
	need := map[string]int{
		"marketplace-add": 4, "marketplace-remove": 4, "marketplace-update": 4,
		"install": 3, "update": 3, "disable": 3,
	}
	if n, ok := need[action(pos)]; ok && len(pos) < n {
		return failed(action(pos), "missing required argument")
	}
	switch {
	case cmd == "plugin marketplace list":
		return out(s.Marketplaces)
	case strings.HasPrefix(cmd, "plugin marketplace add"):
		dir := pos[3]
		s.Marketplaces = append(s.Marketplaces, marketplace{Name: "gentry", Source: "directory", Path: dir})
		return ok("marketplace-add")
	case strings.HasPrefix(cmd, "plugin marketplace remove"):
		var kept []marketplace
		for _, m := range s.Marketplaces {
			if m.Name != pos[3] {
				kept = append(kept, m)
			}
		}
		s.Marketplaces = kept
		var plugins []plugin
		for _, p := range s.Plugins {
			if !strings.HasSuffix(p.ID, "@"+pos[3]) {
				plugins = append(plugins, p)
			}
		}
		s.Plugins = plugins
		return ok("marketplace-remove")
	case strings.HasPrefix(cmd, "plugin marketplace update"):
		return ok("marketplace-update")
	case cmd == "plugin list":
		return out(s.Plugins)
	case strings.HasPrefix(cmd, "plugin install"):
		v, err := folderVersion(s, pos[2])
		if err != nil {
			return failed("install", err.Error())
		}
		s.Plugins = append(s.Plugins, plugin{ID: pos[2], Version: v, Scope: "user", Enabled: true})
		return ok("install")
	case strings.HasPrefix(cmd, "plugin update"):
		v, err := folderVersion(s, pos[2])
		if err != nil {
			return failed("update", err.Error())
		}
		for i := range s.Plugins {
			if s.Plugins[i].ID == pos[2] {
				s.Plugins[i].Version = v
			}
		}
		return ok("update")
	case strings.HasPrefix(cmd, "plugin disable"):
		for i := range s.Plugins {
			if s.Plugins[i].ID == pos[2] {
				s.Plugins[i].Enabled = false
			}
		}
		return ok("disable")
	}
	return failed(cmd, "unknown command")
}

// action names a command as Claude Code does in its JSON output:
// "plugin install" is install, "plugin marketplace add" is marketplace-add.
func action(pos []string) string {
	switch {
	case len(pos) >= 3 && pos[1] == "marketplace":
		return "marketplace-" + pos[2]
	case len(pos) >= 2:
		return pos[1]
	}
	return ""
}

// folderVersion reads the plugin version from its marketplace directory.
func folderVersion(s *state, id string) (string, error) {
	name, market, _ := strings.Cut(id, "@")
	for _, m := range s.Marketplaces {
		if m.Name == market {
			b, err := os.ReadFile(filepath.Join(m.Path, name, ".claude-plugin", "plugin.json"))
			if err != nil {
				return "", err
			}
			var p struct{ Version string }
			json.Unmarshal(b, &p)
			return p.Version, nil
		}
	}
	return "", fmt.Errorf("Plugin %q not found in marketplace %q", name, market)
}

func out(v any) int {
	b, _ := json.MarshalIndent(v, "", "  ")
	if string(b) == "null" {
		b = []byte("[]")
	}
	fmt.Println(string(b))
	return 0
}

func ok(command string) int {
	fmt.Printf(`{"command":%q,"outcome":"ok"}`+"\n", command)
	return 0
}

func failed(command, message string) int {
	b, _ := json.Marshal(map[string]string{"command": command, "outcome": "failed", "message": message})
	fmt.Println(string(b))
	fmt.Fprintln(os.Stderr, "✘ "+message)
	return 1
}
