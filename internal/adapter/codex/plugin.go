// Package codex is the adapter for Codex: it turns the neutral integration
// description into a Codex plugin served from a local marketplace
// directory, writes the session start hook of Gentry into the settings of
// the hooks of Codex and the settings Gentry needs into the settings of
// Codex, and lays out subagents as Codex reads them from a project.
//
// Codex reads its settings from the files of its folder (CODEX_HOME): the
// hook of Gentry reads them there, without the program, to answer in time.
package codex

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/integration"
)

// Tool is the name of Codex in `gentry setup`.
const Tool = "codex"

// toolTimeout is the time limit of a call of a tool of Gentry in seconds, a
// day: Codex cuts a call by its limit, and the commands of checks of a
// stage run while task_stage_exit waits.
const toolTimeout = 86400

// Plugin is a built plugin: files of the marketplace directory by
// slash-separated relative path, and the plugin version.
type Plugin struct {
	Version string
	Files   map[string][]byte
}

// Build turns the description into a Codex plugin for the marketplace
// directory dir. The plugin version is the gentry version without build
// metadata plus a fingerprint of the plugin content, so it changes whenever
// the content does: Codex installs a plugin anew only by codex plugin add,
// which Gentry runs when the version changes. The hooks of the description
// are not in the plugin: Codex resets the trust of the hook of a plugin at
// each change of the plugin, so the hook goes to the settings of the hooks
// of Codex (writeHook).
func Build(d integration.Description, gentryVersion, dir string) (Plugin, error) {
	unversioned, err := buildFiles(d, "", dir)
	if err != nil {
		return Plugin{}, err
	}
	base, _, _ := strings.Cut(gentryVersion, "+")
	version := base + "+" + integration.Fingerprint(unversioned)
	files, err := buildFiles(d, version, dir)
	if err != nil {
		return Plugin{}, err
	}
	return Plugin{Version: version, Files: files}, nil
}

// buildFiles builds the files of the plugin. The servers allow their tools
// without asking the operator, unless the operator set another mode for the
// server in the settings of Codex, and give a call a day.
func buildFiles(d integration.Description, version, dir string) (map[string][]byte, error) {
	plugin := map[string]any{
		"name":        d.Name,
		"version":     version,
		"description": d.Description,
		"skills":      "./skills/",
	}
	out := map[string]any{}
	if len(d.Servers) > 0 {
		servers := map[string]any{}
		for _, s := range d.Servers {
			servers[s.Name] = map[string]any{
				"command":                     s.Command[0],
				"args":                        s.Command[1:],
				"default_tools_approval_mode": "approve",
				"tool_timeout_sec":            toolTimeout,
			}
		}
		plugin["mcpServers"] = "./.mcp.json"
		out[d.Name+"/.mcp.json"] = map[string]any{"mcpServers": servers}
	}
	out[d.Name+"/.codex-plugin/plugin.json"] = plugin
	out[".agents/plugins/marketplace.json"] = map[string]any{
		"name": d.Name,
		"plugins": []any{map[string]any{
			"name":   d.Name,
			"source": map[string]any{"source": "local", "path": "./" + d.Name},
			"policy": map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
		}},
	}
	files := map[string][]byte{}
	for _, s := range d.Skills {
		skillDir := filepath.Join(dir, d.Name, "skills", s.Name)
		files[d.Name+"/skills/"+s.Name+"/SKILL.md"] = skillFile(s, skillDir)
		for name, text := range s.Files {
			files[d.Name+"/skills/"+s.Name+"/"+name] = []byte(skillText(text, skillDir))
		}
	}
	for path, v := range out {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		files[path] = append(b, '\n')
	}
	return files, nil
}

// SkillRef is how Codex names the skill name of the plugin.
func SkillRef(name string) string { return integration.Name + ":" + name }

// skillPath matches the place of the directory of a skill in its text with
// the path of a file after it.
var skillPath = regexp.MustCompile(regexp.QuoteMeta(agenttext.SkillDir) + `(/[^\s` + "`" + `]*)?`)

// skillText is the text of a skill or of a file of it with the references to
// skills named as Codex names them and the files of the skill by their full
// paths in the folder of the integration: Codex gives the agent no
// directory of the skill, and a path from it the agent could read from the
// folder of the session. The path is as the system gives it.
func skillText(text, skillDir string) string {
	text = agenttext.ResolveSkills(text, SkillRef)
	return skillPath.ReplaceAllStringFunc(text, func(m string) string {
		rel := strings.TrimPrefix(m, agenttext.SkillDir)
		return filepath.Join(skillDir, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
	})
}

// skillFile is the SKILL.md of a skill: a YAML front matter with its name and
// description, then its text. A Go quoted string is a valid YAML
// double-quoted scalar for the texts of Gentry: they have no control
// characters.
func skillFile(s integration.Skill, skillDir string) []byte {
	description := agenttext.ResolveSkills(s.Description, SkillRef)
	return []byte("---\nname: " + s.Name + "\ndescription: " + strconv.Quote(description) + "\n---\n\n" + skillText(s.Text, skillDir))
}

// Write replaces dir with the plugin files. The files are written to a
// sibling directory first, so a failure midway leaves the old plugin intact.
func Write(dir string, p Plugin) error { return integration.WriteDir(dir, p.Files) }
