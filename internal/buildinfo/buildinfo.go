// Package buildinfo reports the version of the executable.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// fallback is the version of a build without module version information,
// such as a test binary.
const fallback = "0.0.0-dev"

// Version returns the product version in semver without the leading v.
//
// Go stamps the main module version from git when building: a commit tagged
// v0.1.0 gives 0.1.0, any other commit gives a pseudo-version such as
// 0.1.1-0.20261004175847-74a0120d2d39, with +dirty for uncommitted changes.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallback
	}
	return fromModule(info.Main.Version)
}

func fromModule(v string) string {
	if v == "" || v == "(devel)" {
		return fallback
	}
	return strings.TrimPrefix(v, "v")
}
