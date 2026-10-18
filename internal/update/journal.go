package update

import (
	"regexp"
	"slices"
	"strings"
)

// breakingHeading is the subsection of a section of a version in the journal
// of changes with what the operator has to do himself after the update.
const breakingHeading = "Несовместимые изменения"

// Breaking is the incompatible changes of a version in the journal of
// changes, an entry a line.
type Breaking struct {
	Version string
	Lines   []string
}

// versionHeading is the heading of the section of a version in the journal:
// ## X.Y.Z — YYYY-MM-DD; its group is the version.
var versionHeading = regexp.MustCompile(`^## (\S+)(?: |$)`)

// BreakingOf returns the incompatible changes of the versions an update from
// the version from to the version to skips: the sections of the journal of a
// version greater than from and not greater than to, in the order of the
// versions; a version with none is left out. An entry is a line that starts
// with «* » in the subsection «### Несовместимые изменения»; the lines after
// it up to a blank line or the next entry go on with it, joined by a space.
func BreakingOf(journal, from, to string) []Breaking {
	var out []Breaking
	cur := -1 // the section of a version skipped, in out
	sub, open := "", false
	for _, l := range strings.Split(strings.ReplaceAll(journal, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## "):
			cur, sub, open = -1, "", false
			m := versionHeading.FindStringSubmatch(l)
			if m == nil || !IsRelease(m[1]) || Compare(m[1], from) <= 0 || Compare(m[1], to) > 0 {
				continue
			}
			out, cur = append(out, Breaking{Version: m[1]}), len(out)
		case cur < 0:
		case strings.HasPrefix(l, "### "):
			sub, open = strings.TrimSpace(l[len("### "):]), false
		case sub != breakingHeading:
		case strings.HasPrefix(l, "* "):
			out[cur].Lines, open = append(out[cur].Lines, strings.TrimSpace(l[2:])), true
		case t == "":
			open = false
		case open:
			out[cur].Lines[len(out[cur].Lines)-1] += " " + t
		}
	}
	out = slices.DeleteFunc(out, func(b Breaking) bool { return len(b.Lines) == 0 })
	slices.SortStableFunc(out, func(a, b Breaking) int { return Compare(a.Version, b.Version) })
	return out
}
