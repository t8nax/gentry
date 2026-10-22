package codex

import (
	"os"
	"path/filepath"

	"github.com/t8nax/gentry/internal/integration"
)

// PluginMatches reports whether Codex has the plugin of Gentry the program
// exe of the version builds in the folder of the integration dir, read from
// the files of Codex without the program, as the hook needs it: the
// settings of Codex have the marketplace and the plugin of Gentry, and the
// cache of Codex has the plugin of the version. Codex removes a
// marketplace from its settings, with its plugin from the list of plugins,
// but leaves the plugin in the cache: so a plugin is missing if the
// settings have not the marketplace or the plugin. Settings that cannot be
// read leave the cache alone to tell.
func PluginMatches(dir, exe, version string) bool {
	home, err := Home()
	if err != nil {
		return false
	}
	if c, err := readConfig(ConfigPath(home)); err == nil {
		if _, ok := c.value("marketplaces", integration.Name); !ok {
			return false
		}
		if _, ok := c.value("plugins", pluginID); !ok {
			return false
		}
	}
	p, err := Build(integration.Gentry(exe), version, dir)
	if err != nil {
		return false
	}
	fi, err := os.Stat(filepath.Join(home, "plugins", "cache", integration.Name, integration.Name, p.Version))
	return err == nil && fi.IsDir()
}
