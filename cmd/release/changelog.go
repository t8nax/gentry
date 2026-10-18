package main

import (
	"regexp"
	"strings"
)

// changelogFile is the journal of changes, in the root of the repository.
const changelogFile = "CHANGELOG.md"

// The subsections of a section of a version in the journal that the page of
// the release shows.
const (
	journalBreaking = "Несовместимые изменения"
	journalNew      = "Новое"
	journalChanged  = "Изменено"
	journalFixed    = "Исправлено"
	journalSecurity = "Безопасность"
)

// journalSubsections are the subsections of a section of a version that the
// page of the release shows, in the order the page and the message to the
// group show them.
var journalSubsections = []string{journalBreaking, journalNew, journalChanged, journalFixed, journalSecurity}

// sectionHeading returns the pattern of the heading of the section of a
// version in the journal: ## X.Y.Z — YYYY-MM-DD; its group is the date.
func sectionHeading(v version) *regexp.Regexp {
	return regexp.MustCompile(`^## ` + regexp.QuoteMeta(v.String()) + ` — ([0-9]{4}-[0-9]{2}-[0-9]{2})$`)
}

// sectionLines returns the date of the section of the version in the
// journal and its lines, from the line after its heading to the next
// heading of the first or the second level, with no empty lines at the
// start and at the end. ok is false when the journal has no section of the
// version.
func sectionLines(journal string, v version) (date string, lines []string, ok bool) {
	heading := sectionHeading(v)
	all := strings.Split(strings.ReplaceAll(journal, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range all {
		if m := heading.FindStringSubmatch(l); m != nil {
			start, date = i+1, m[1]
			break
		}
	}
	if start < 0 {
		return "", nil, false
	}
	end := len(all)
	for i := start; i < len(all); i++ {
		if strings.HasPrefix(all[i], "# ") || strings.HasPrefix(all[i], "## ") {
			end = i
			break
		}
	}
	lines = all[start:end]
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return date, lines, true
}

// notes returns the text of the release of the version: the lines of its
// section in the journal, each ended by \n. ok is false when the journal has
// no section of the version.
func notes(journal string, v version) (text string, ok bool) {
	_, lines, ok := sectionLines(journal, v)
	if !ok {
		return "", false
	}
	var b strings.Builder
	for _, l := range lines {
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

// readSection reads the section of the version in the journal. An entry is
// a line that starts with «* » in a subsection «### <name>»; the lines
// after it up to a blank line or the next entry go on with it. ok is false
// when the journal has no section of the version.
func readSection(journal string, v version) (s journalSection, ok bool) {
	date, lines, ok := sectionLines(journal, v)
	if !ok {
		return s, false
	}
	s = journalSection{date: date, entries: map[string][]string{}}
	var phrase []string
	sub, open := "", false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "### "):
			sub, open = strings.TrimSpace(l[len("### "):]), false
			if s.entries[sub] == nil {
				s.entries[sub] = []string{}
			}
		case sub == "":
			if t != "" && (len(phrase) == 0 || open) {
				phrase, open = append(phrase, t), true
			} else if t == "" {
				open = false
			}
		case strings.HasPrefix(l, "* "):
			s.entries[sub], open = append(s.entries[sub], strings.TrimSpace(l[2:])), true
		case t == "":
			open = false
		case open:
			e := s.entries[sub]
			e[len(e)-1] += " " + t
		}
	}
	s.phrase = strings.Join(phrase, " ")
	return s, true
}
