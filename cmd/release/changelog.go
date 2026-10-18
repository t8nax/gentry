package main

import (
	"strings"

	"github.com/t8nax/gentry/internal/changelog"
)

// changelogFile is the journal of changes, in the root of the repository.
const changelogFile = changelog.File

// The subsections of a section of a version in the journal that the page of
// the release shows.
const (
	journalBreaking = changelog.Breaking
	journalNew      = changelog.New
	journalChanged  = changelog.Changed
	journalFixed    = changelog.Fixed
	journalSecurity = changelog.Security
)

// journalSubsections are the subsections of a section of a version that the
// page of the release shows, in the order the page and the message to the
// group show them.
var journalSubsections = []string{journalBreaking, journalNew, journalChanged, journalFixed, journalSecurity}

// notes returns the text of the release of the version: the lines of its
// section in the journal, each ended by \n. ok is false when the journal has
// no section of the version.
func notes(journal string, v version) (text string, ok bool) {
	s, ok := changelog.Find(journal, v.String())
	if !ok {
		return "", false
	}
	var b strings.Builder
	for _, l := range s.Lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String(), true
}

// A journalSection is the section of a version in the journal, read for the
// page of the release.
type journalSection struct {
	date string // of the heading, YYYY-MM-DD
	// phrase is the phrase about the main thing: the first paragraph of the
	// section before its first subsection, its lines joined by a space.
	phrase string
	// entries are the entries of the subsections by their heading, each
	// entry its lines joined by a space; a subsection with no entries is
	// there too, with none.
	entries map[string][]string
}

// has reports whether the section has the subsection.
func (s journalSection) has(sub string) bool {
	_, ok := s.entries[sub]
	return ok
}

// readSection reads the section of the version in the journal. ok is false
// when the journal has no section of the version.
func readSection(journal string, v version) (journalSection, bool) {
	s, ok := changelog.Find(journal, v.String())
	if !ok {
		return journalSection{}, false
	}
	return journalSection{date: s.Date, phrase: s.Phrase, entries: s.Entries}, true
}
