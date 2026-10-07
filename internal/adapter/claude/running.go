package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// PluginRootEnv is set by Claude Code for the hooks of a plugin: the
// directory of the copy of the plugin it runs.
const PluginRootEnv = "CLAUDE_PLUGIN_ROOT"

// RunningVersion returns the version of the plugin whose hook runs gentry;
// false outside a hook of a plugin or if its manifest cannot be read.
func RunningVersion() (string, bool) {
	root := os.Getenv(PluginRootEnv)
	if root == "" {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "", false
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &manifest) != nil || manifest.Version == "" {
		return "", false
	}
	return manifest.Version, true
}
