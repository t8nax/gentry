package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/integration"
)

// TestBuild checks the plugin of Codex: the marketplace, the manifest, the
// server with the tools allowed and a day for a call, the skills naming one
// another as Codex does and the guide by its full path.
func TestBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "integrations", "codex")
	p, err := Build(integration.Gentry(testExe), "0.3.0+dev", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Version, "0.3.0+") || len(p.Version) != len("0.3.0+")+8 {
		t.Errorf("version %q: want the gentry version without build metadata plus a fingerprint", p.Version)
	}
	var plugin map[string]any
	json.Unmarshal(p.Files["gentry/.codex-plugin/plugin.json"], &plugin)
	if plugin["name"] != "gentry" || plugin["version"] != p.Version || plugin["mcpServers"] != "./.mcp.json" || plugin["skills"] != "./skills/" {
		t.Errorf("plugin.json = %v", plugin)
	}
	var servers struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	json.Unmarshal(p.Files["gentry/.mcp.json"], &servers)
	core := servers.MCPServers["core"]
	if core["command"] != testExe || core["default_tools_approval_mode"] != "approve" || core["tool_timeout_sec"] != float64(86400) {
		t.Errorf("the server core = %v", core)
	}
	var market struct {
		Name    string
		Plugins []struct {
			Name   string
			Source struct{ Source, Path string }
		}
	}
	json.Unmarshal(p.Files[".agents/plugins/marketplace.json"], &market)
	if market.Name != "gentry" || len(market.Plugins) != 1 || market.Plugins[0].Source.Path != "./gentry" {
		t.Errorf("marketplace.json = %+v", market)
	}
	for _, s := range []string{agenttext.WorkingOnTask, agenttext.CancelingTask, agenttext.EditingFlow} {
		text := string(p.Files["gentry/skills/"+s+"/SKILL.md"])
		if !strings.HasPrefix(text, "---\nname: "+s+"\n") || strings.Contains(text, "<skill:") || strings.Contains(text, agenttext.SkillDir) {
			t.Errorf("SKILL.md of %s:\n%s", s, text)
		}
	}
	guide := filepath.Join(dir, "gentry", "skills", agenttext.EditingFlow, filepath.FromSlash(agenttext.FlowGuidePath))
	if text := string(p.Files["gentry/skills/editing-flow/SKILL.md"]); !strings.Contains(text, "`"+guide+"`") || !strings.Contains(text, "`gentry:working-on-task`") {
		t.Errorf("editing-flow does not name the guide %s and the skill gentry:working-on-task:\n%s", guide, text)
	}
	if _, ok := p.Files["gentry/skills/editing-flow/"+agenttext.FlowGuidePath]; !ok {
		t.Error("the plugin has no guide")
	}
	other, _ := Build(integration.Gentry(testExe), "0.3.0+dev", filepath.Join(t.TempDir(), "other"))
	if other.Version == p.Version {
		t.Error("the plugin of another folder has the same version")
	}
}
