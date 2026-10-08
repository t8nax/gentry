// Package project connects projects to Gentry: the project information in
// the knowledge repository (gentry.yaml) and the project record in the state
// store.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/t8nax/gentry/internal/msg"
)

// InfoFile is the file with the project information at the root of the
// knowledge repository.
const InfoFile = "gentry.yaml"

// KnowledgeFormat is the latest format version of the knowledge this build
// supports.
const KnowledgeFormat = 1

// DefaultLanguage is the language of new knowledge. Only Russian templates
// exist so far; English ones come with the knowledge templates.
const DefaultLanguage = "ru"

// Info is the project information recorded in the knowledge.
type Info struct {
	Format   int
	Project  string
	Language string
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// ValidID reports whether id is a valid project identifier: 2-32 lowercase
// Latin letters, digits and hyphens, starting with a letter.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// InvalidInfoError means gentry.yaml cannot be read; Cause is for the
// operator.
type InvalidInfoError struct {
	Path  string // the knowledge directory
	Cause string
}

func (e *InvalidInfoError) Error() string { return e.Path + ": " + e.Cause }

// NewerFormatError means the knowledge was written by a newer Gentry.
type NewerFormatError struct {
	Path      string
	Format    int
	Supported int
}

func (e *NewerFormatError) Error() string {
	return fmt.Sprintf("%s: knowledge format %d is newer than %d", e.Path, e.Format, e.Supported)
}

// ReadInfo reads gentry.yaml of the knowledge in dir. It returns an error
// satisfying errors.Is(err, fs.ErrNotExist) if there is no such file.
func ReadInfo(dir string) (Info, error) {
	b, err := os.ReadFile(filepath.Join(dir, InfoFile))
	if err != nil {
		return Info{}, err
	}
	invalid := func(k msg.Key, args ...any) error {
		return &InvalidInfoError{Path: dir, Cause: msg.Text(k, args...)}
	}
	var fields map[string]any
	if err := yaml.Unmarshal(b, &fields); err != nil || fields == nil {
		return Info{}, invalid(msg.CauseInfoSyntax)
	}

	// The format comes first: a newer format may have keys this Gentry does
	// not know, and the operator should be told to update, not of a typo.
	format, ok := integer(fields["format"])
	if !ok {
		if _, present := fields["format"]; !present {
			return Info{}, invalid(msg.CauseInfoMissing, "format")
		}
		return Info{}, invalid(msg.CauseInfoValue, "format", fields["format"])
	}
	if format > KnowledgeFormat {
		return Info{}, &NewerFormatError{Path: dir, Format: format, Supported: KnowledgeFormat}
	}
	if format < 1 {
		return Info{}, invalid(msg.CauseInfoValue, "format", format)
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k != "format" && k != "project" && k != "language" {
			return Info{}, invalid(msg.CauseInfoUnknown, k)
		}
	}
	info := Info{Format: format}
	for _, f := range []struct {
		key   string
		value *string
		valid func(string) bool
	}{
		{"project", &info.Project, ValidID},
		{"language", &info.Language, func(s string) bool { return slices.Contains(msg.KnowledgeLanguages, s) }},
	} {
		v, present := fields[f.key]
		if !present {
			return Info{}, invalid(msg.CauseInfoMissing, f.key)
		}
		s, ok := v.(string)
		if !ok || !f.valid(s) {
			return Info{}, invalid(msg.CauseInfoValue, f.key, v)
		}
		*f.value = s
	}
	return info, nil
}

// integer returns v as an int if it is a YAML integer.
func integer(v any) (int, bool) {
	switch n := v.(type) {
	case uint64:
		return int(n), n <= 1<<31
	case int64:
		return int(n), n >= -1<<31 && n <= 1<<31
	case int:
		return n, true
	}
	return 0, false
}

// WriteInfo writes gentry.yaml into the knowledge in dir, with comments in the
// language of the knowledge.
func WriteInfo(dir string, info Info) error {
	lines := []struct{ field, comment string }{
		{fmt.Sprintf("format: %d", info.Format), msg.Knowledge(info.Language, msg.YAMLFormat)},
		{"project: " + info.Project, msg.Knowledge(info.Language, msg.YAMLProject)},
		{"language: " + info.Language, msg.Knowledge(info.Language, msg.YAMLLanguage)},
	}
	width := 0
	for _, l := range lines {
		width = max(width, len(l.field))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", msg.Knowledge(info.Language, msg.YAMLHeader))
	for _, l := range lines {
		fmt.Fprintf(&b, "%-*s  # %s\n", width, l.field, l.comment)
	}
	return os.WriteFile(filepath.Join(dir, InfoFile), []byte(b.String()), 0o644)
}

// hasInfo reports whether dir holds gentry.yaml.
func hasInfo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, InfoFile))
	return !errors.Is(err, fs.ErrNotExist)
}
