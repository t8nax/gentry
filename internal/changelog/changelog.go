// Package changelog reads the journal of changes of Gentry, CHANGELOG.md, in
// its format: a section of a version is «## X.Y.Z — YYYY-MM-DD», then a
// phrase about the main thing and the subsections «### <name>» with their
// entries «* …». The program of the release builds the text and the page of
// a release from the section of its version, and gentry update shows the
// incompatible changes of the versions it skips.
package changelog

import (
	"regexp"
	"strings"
)

// File is the journal of changes, in the root of the repository.
const File = "CHANGELOG.md"

// The subsections of a section of a version.
const (
	Breaking = "Несовместимые изменения"
	New      = "Новое"
	Changed  = "Изменено"
	Fixed    = "Исправлено"
	Security = "Безопасность"
)

// heading is the heading of the section of a version; its groups are the
// version and the date.
var heading = regexp.MustCompile(`^## ([0-9]+\.[0-9]+\.[0-9]+) — ([0-9]{4}-[0-9]{2}-[0-9]{2})$`)

// A Section is the section of a version in the journal.
type Section struct {
	Version string
	Date    string // of the heading, YYYY-MM-DD
	// Lines are the lines from the one after the heading to the next
	// heading of the first or the second level, with no empty lines at the
	// start and at the end.
	Lines []string
	// Phrase is the phrase about the main thing: the first paragraph before
	// the first subsection, its lines joined by a space.
	Phrase string
	// Entries are the entries of the subsections by their heading, each
	// entry its lines joined by a space; a subsection with no entries is
	// there too, with none.
	Entries map[string][]string
}

// Has reports whether the section has the subsection.
func (s Section) Has(sub string) bool {
	_, ok := s.Entries[sub]
	return ok
}

// Sections returns the sections of the versions of the journal, in its
// order. An entry is a line that starts with «* » in a subsection; the lines
// after it up to a blank line or the next entry go on with it.
func Sections(journal string) []Section {
	all := strings.Split(strings.ReplaceAll(journal, "\r\n", "\n"), "\n")
	var out []Section
	for i := 0; i < len(all); i++ {
		m := heading.FindStringSubmatch(all[i])
		if m == nil {
			continue
		}
		end := len(all)
		for j := i + 1; j < len(all); j++ {
			if strings.HasPrefix(all[j], "# ") || strings.HasPrefix(all[j], "## ") {
				end = j
				break
			}
		}
		out = append(out, read(m[1], m[2], all[i+1:end]))
		i = end - 1
	}
	return out
}

// Find returns the section of the version v; false if the journal has none.
func Find(journal, v string) (Section, bool) {
	for _, s := range Sections(journal) {
		if s.Version == v {
			return s, true
		}
	}
	return Section{}, false
}

// read reads the section of the version with the lines after its heading.
func read(version, date string, lines []string) Section {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	s := Section{Version: version, Date: date, Lines: lines, Entries: map[string][]string{}}
	var phrase []string
	sub, open := "", false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "### "):
			sub, open = strings.TrimSpace(l[len("### "):]), false
			if s.Entries[sub] == nil {
				s.Entries[sub] = []string{}
			}
		case sub == "":
			if t != "" && (len(phrase) == 0 || open) {
				phrase, open = append(phrase, t), true
			} else if t == "" {
				open = false
			}
		case strings.HasPrefix(l, "* "):
			s.Entries[sub], open = append(s.Entries[sub], strings.TrimSpace(l[2:])), true
		case t == "":
			open = false
		case open:
			e := s.Entries[sub]
			e[len(e)-1] += " " + t
		}
	}
	s.Phrase = strings.Join(phrase, " ")
	return s
}
