package update

import (
	"slices"

	"github.com/t8nax/gentry/internal/changelog"
	"github.com/t8nax/gentry/internal/releases"
)

// Breaking is the incompatible changes of a version in the journal of
// changes, an entry a line.
type Breaking struct {
	Version string
	Lines   []string
}

// BreakingOf returns the incompatible changes of the versions an update from
// the version from to the version to skips: the entries of the subsection
// «Несовместимые изменения» of the sections of the journal of a version
// greater than from and not greater than to, in the order of the versions;
// a version with none is left out.
func BreakingOf(journal, from, to string) []Breaking {
	var out []Breaking
	for _, s := range changelog.Sections(journal) {
		if !releases.IsRelease(s.Version) || releases.Compare(s.Version, from) <= 0 || releases.Compare(s.Version, to) > 0 {
			continue
		}
		if lines := s.Entries[changelog.Breaking]; len(lines) > 0 {
			out = append(out, Breaking{Version: s.Version, Lines: lines})
		}
	}
	slices.SortStableFunc(out, func(a, b Breaking) int { return releases.Compare(a.Version, b.Version) })
	return out
}
