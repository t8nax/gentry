// Package buildinfo reports the version of the executable.
package buildinfo

import "runtime/debug"

// version is set when building a release:
// go build -ldflags "-X github.com/t8nax/gentry/internal/buildinfo.version=1.2.3"
var version string

// Version returns the product version. A build without a version reports
// "dev" with the short git revision and a "dirty" mark for uncommitted changes.
func Version() string {
	if version != "" {
		return version
	}
	return devVersion(debug.ReadBuildInfo())
}

func devVersion(info *debug.BuildInfo, ok bool) string {
	v := "dev"
	if !ok {
		return v
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return v
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	v += "+" + revision
	if modified {
		v += ".dirty"
	}
	return v
}
