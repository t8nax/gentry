package main

import (
	"regexp"
	"strings"
)

// changelogFile is the journal of changes, in the root of the repository.
const changelogFile = "CHANGELOG.md"

// sectionHeading returns the pattern of the heading of the section of a
// version in the journal: ## X.Y.Z — YYYY-MM-DD.
func sectionHeading(v version) *regexp.Regexp {
	return regexp.MustCompile(`^## ` + regexp.QuoteMeta(v.String()) + ` — [0-9]{4}-[0-9]{2}-[0-9]{2}$`)
}

// notes returns the text of the release of the version: the lines of its
// section in the journal, from the line after its heading to the next
// heading of the first or the second level, with no empty lines at the
// start and at the end, each ended by \n. ok is false when the journal has
// no section of the version.
func notes(journal string, v version) (text string, ok bool) {
	heading := sectionHeading(v)
	lines := strings.Split(strings.ReplaceAll(journal, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if heading.MatchString(l) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	section := lines[start:end]
	for len(section) > 0 && strings.TrimSpace(section[0]) == "" {
		section = section[1:]
	}
	for len(section) > 0 && strings.TrimSpace(section[len(section)-1]) == "" {
		section = section[:len(section)-1]
	}
	var b strings.Builder
	for _, l := range section {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String(), true
}
