package main

import (
	"regexp"
	"strconv"
	"strings"
)

// A version is a version of a release: X.Y.Z of semver with no prefix and no
// pre-release or build part.
type version [3]int

// versionPattern is the form of a version: three numbers apart by dots, with
// no leading zero, as semver has them.
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// parseVersion parses a version of the form X.Y.Z.
func parseVersion(s string) (version, bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	var v version
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, false // too big a number
		}
		v[i] = n
	}
	return v, true
}

// compare returns -1, 0 or 1 as v is lower than, equal to or greater than w.
func (v version) compare(w version) int {
	for i := range v {
		switch {
		case v[i] < w[i]:
			return -1
		case v[i] > w[i]:
			return 1
		}
	}
	return 0
}

func (v version) String() string {
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2])
}

// tagPrefix makes the tag of a release of its version.
const tagPrefix = "v"

// releasedVersions returns the versions of the tags of the releases,
// v<version>, in the order of the tags; other tags are not releases.
func releasedVersions(tags []string) []version {
	var out []version
	for _, tag := range tags {
		s, found := strings.CutPrefix(tag, tagPrefix)
		if !found {
			continue
		}
		if v, valid := parseVersion(s); valid {
			out = append(out, v)
		}
	}
	return out
}

// latestRelease returns the greatest version among the tags of the releases,
// v<version>; other tags are not releases. ok is false when there is none.
func latestRelease(tags []string) (latest version, ok bool) {
	for _, v := range releasedVersions(tags) {
		if !ok || v.compare(latest) > 0 {
			latest, ok = v, true
		}
	}
	return latest, ok
}
