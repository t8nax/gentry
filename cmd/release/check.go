package main

import (
	"errors"
	"strings"

	"github.com/t8nax/gentry/internal/git"
)

// releaseBranch is the branch a release is made from: its head.
const releaseBranch = "main"

// The refusals of the checks of the button. Each holds what the operator
// needs to see to correct the run; main.go turns them into words.
type (
	// versionFormatError: the version is not of the form X.Y.Z.
	versionFormatError struct{ value string }
	// notMainError: the button runs on another branch than main.
	notMainError struct{ branch string }
	// releasedError: the tag of the version is there already.
	releasedError struct{ version version }
	// notGreaterError: the version is not greater than the latest release.
	notGreaterError struct{ version, latest version }
	// noChangesError: main has changed nothing but the journal since the
	// latest release.
	noChangesError struct{ latest version }
	// noSectionError: the journal has no section of the version.
	noSectionError struct{ version version }
)

func (e *versionFormatError) Error() string { return "the version is not X.Y.Z: " + e.value }
func (e *notMainError) Error() string       { return "the release is made from main, not " + e.branch }
func (e *releasedError) Error() string      { return "the release " + e.version.String() + " exists" }
func (e *notGreaterError) Error() string {
	return "the version " + e.version.String() + " is not greater than the latest release " + e.latest.String()
}
func (e *noChangesError) Error() string {
	return "main has no change but the journal since the release " + e.latest.String()
}
func (e *noSectionError) Error() string {
	return "the journal has no section of the version " + e.version.String()
}

// detached names the branch of a work tree that is on no branch, as git
// does.
const detached = "HEAD"

// check runs the checks of the button on the repository dir for the version
// value and returns the text of the release: the version has the form X.Y.Z;
// the head of main is checked out; there is no tag of the version; the
// version is greater than the latest release; main has changed more than the
// journal since it; the journal of the commit of the release has the
// section of the version.
func check(dir, value string) (string, error) {
	v, ok := parseVersion(value)
	if !ok {
		return "", &versionFormatError{value: value}
	}
	branch, err := git.Branch(dir)
	if err != nil {
		return "", err
	}
	if branch == "" {
		branch = detached
	}
	if branch != releaseBranch {
		return "", &notMainError{branch: branch}
	}
	out, err := git.Run(dir, git.Opts{}, "tag", "--list")
	if err != nil {
		return "", err
	}
	tags := strings.Fields(out)
	for _, tag := range tags {
		if tag == tagPrefix+v.String() {
			return "", &releasedError{version: v}
		}
	}
	if latest, ok := latestRelease(tags); ok {
		if v.compare(latest) <= 0 {
			return "", &notGreaterError{version: v, latest: latest}
		}
		changed, err := changedSince(dir, tagPrefix+latest.String())
		if err != nil {
			return "", err
		}
		if !changed {
			return "", &noChangesError{latest: latest}
		}
	}
	journal, err := git.Run(dir, git.Opts{}, "show", "HEAD:"+changelogFile)
	var ce *git.CommandError
	if errors.As(err, &ce) {
		journal = "" // no journal in the commit: no section either
	} else if err != nil {
		return "", err
	}
	text, ok := notes(journal, v)
	if !ok {
		return "", &noSectionError{version: v}
	}
	return text, nil
}

// changedSince reports whether HEAD changes a file other than the journal
// against the commit of the tag.
func changedSince(dir, tag string) (bool, error) {
	out, err := git.Run(dir, git.Opts{}, "diff", "--name-only", "--no-renames", tag, "HEAD", "--")
	if err != nil {
		return false, err
	}
	for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
		if name != "" && name != changelogFile {
			return true, nil
		}
	}
	return false, nil
}
