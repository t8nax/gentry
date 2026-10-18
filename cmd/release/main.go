// Command release makes the checks and the build of a release of Gentry and
// the build of the site of the pages of the releases for the workflows
// Release and Publish pages, which publish what it builds. It runs in the
// root of the repository of the release:
//
//	release check <version>          checks that the head of main can be
//	                                 released as the version and prints the
//	                                 text of the release: the section of the
//	                                 version in CHANGELOG.md and the line
//	                                 with the address of the page of the
//	                                 release
//	release build <version> <folder> builds the files of the release into
//	                                 the folder: the programs of the six
//	                                 platforms, checksums.txt and the scripts
//	                                 of installation
//	release pages <folder>           checks that the pages can be published
//	                                 and builds the site of the pages of the
//	                                 released versions from the head of main
//	                                 into the folder: index.html, the list of
//	                                 the releases, and <version>/index.html
//	release notice <version>         prints the message about the release of
//	                                 the version to the group in Telegram,
//	                                 HTML of Telegram, from its section in
//	                                 CHANGELOG.md and the text for the group
//	                                 in the content of its page,
//	                                 releases/<version>.md, of the commit
//
// The checks of a release build the site of the pages too, with the page of
// the new version, and the message to the group, and refuse when a content
// of a page is written wrong or missing.
//
// A refusal exits with code 1 and its text on the standard error; a wrong
// command line exits with code 2. The texts of the refusals and the words of
// the pages are in the catalog of messages of Gentry, internal/msg, as the
// texts of gentry are.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/t8nax/gentry/internal/msg"
)

const usage = `usage:
  release check <version>
  release build <version> <folder>
  release pages <folder>
  release notice <version>
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run runs the command line args in the current directory and returns the
// exit code.
func run(args []string, stdout, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 2 && args[0] == "check":
		var text string
		if text, err = check(".", args[1]); err == nil {
			v, _ := parseVersion(args[1]) // checked by check
			_, err = io.WriteString(stdout, releaseText(text, v))
		}
	case len(args) == 3 && args[0] == "build":
		err = build(".", args[1], args[2])
	case len(args) == 2 && args[0] == "pages":
		err = pages(".", args[1])
	case len(args) == 2 && args[0] == "notice":
		var text string
		if text, err = notice(".", args[1]); err == nil {
			_, err = io.WriteString(stdout, text)
		}
	default:
		io.WriteString(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, refusal(err))
		return 1
	}
	return 0
}

// releaseText returns the text of the release of the version on GitHub:
// its section in the journal, then an empty line and the line with the
// address of its page.
func releaseText(section string, v version) string {
	if section != "" {
		section += "\n"
	}
	return section + msg.Text(msg.ReleasePageLink, pageURL(v)) + "\n"
}

// refusal returns the text that tells the operator of the error: a refusal
// in the words of the button from the catalog of messages, any other error
// as it is.
func refusal(err error) string {
	var (
		format     *versionFormatError
		notMain    *notMainError
		released   *releasedError
		notGreater *notGreaterError
		noChanges  *noChangesError
		noSection  *noSectionError
		mismatch   *versionMismatchError
		missing    *contentMissingError
		invalid    *contentInvalidError
		pagesMain  *pagesNotMainError
		none       *noReleasesError
	)
	switch {
	case errors.As(err, &format):
		return msg.Text(msg.ReleaseVersionInvalid, format.value)
	case errors.As(err, &notMain):
		return msg.Text(msg.ReleaseNotMain, notMain.branch)
	case errors.As(err, &released):
		return msg.Text(msg.ReleaseExists, released.version)
	case errors.As(err, &notGreater):
		return msg.Text(msg.ReleaseNotGreater, notGreater.version, notGreater.latest)
	case errors.As(err, &noChanges):
		return msg.Text(msg.ReleaseNoChanges, noChanges.latest)
	case errors.As(err, &noSection):
		return msg.Text(msg.ReleaseNoSection, noSection.version)
	case errors.As(err, &mismatch):
		return msg.Text(msg.ReleaseVersionMismatch, mismatch.want, mismatch.got)
	case errors.As(err, &missing):
		return msg.Text(msg.ReleaseContentMissing, missing.version, missing.file)
	case errors.As(err, &invalid):
		return contentRefusal(invalid)
	case errors.As(err, &pagesMain):
		return msg.Text(msg.ReleasePagesNotMain, pagesMain.branch)
	case errors.As(err, &none):
		return msg.Text(msg.ReleasePagesNone)
	}
	return err.Error()
}

// contentRefusal returns the text of the refusal of a content of a page
// written wrong: the file, the line when the error is of one, the length and
// the limit of a message to the group that is too long, the error and, for
// an unknown section, subheading or subsection of the text for the group,
// the ones a content may have, for text out of an item, the rule of an
// item.
func contentRefusal(e *contentInvalidError) string {
	lines := []string{msg.Text(msg.ReleaseContentInvalid, e.file)}
	if e.line > 0 {
		lines = append(lines, msg.Text(msg.ReleaseContentLine, e.line))
	}
	reason, allowed := "", ""
	switch e.reason {
	case reasonUnknownSection:
		reason, allowed = msg.Text(msg.ReleaseContentUnknownSection, e.subject), msg.Text(msg.ReleaseContentSections)
	case reasonRepeated:
		reason = msg.Text(msg.ReleaseContentRepeated, e.subject)
	case reasonOutside:
		reason = msg.Text(msg.ReleaseContentOutside)
	case reasonOutsideCard:
		reason = msg.Text(msg.ReleaseContentOutsideCard)
	case reasonCardOutside:
		reason = msg.Text(msg.ReleaseContentCardOutside)
	case reasonNoExplanation:
		reason = msg.Text(msg.ReleaseContentNoExplanation, e.subject)
	case reasonUnknownSubheading:
		reason, allowed = msg.Text(msg.ReleaseContentUnknownSubheading, e.subject), msg.Text(msg.ReleaseContentSubheadings)
	case reasonNoExample:
		reason = msg.Text(msg.ReleaseContentNoExample, e.subject)
	case reasonNoPair:
		reason = msg.Text(msg.ReleaseContentNoPair, e.subject)
	case reasonUnclosed:
		reason = msg.Text(msg.ReleaseContentUnclosed)
	case reasonTableColumns:
		reason = msg.Text(msg.ReleaseContentTableColumns)
	case reasonNoNew:
		reason = msg.Text(msg.ReleaseContentNoNew)
	case reasonNoticeUnknown:
		reason, allowed = msg.Text(msg.ReleaseContentNoticeUnknown, e.subject), msg.Text(msg.ReleaseContentNoticeSubsections)
	case reasonNoticeRepeated:
		reason = msg.Text(msg.ReleaseContentNoticeRepeated, e.subject)
	case reasonNoticeOutside:
		reason, allowed = msg.Text(msg.ReleaseContentNoticeOutside), msg.Text(msg.ReleaseContentNoticeItemRule)
	case reasonNoNotice:
		reason = msg.Text(msg.ReleaseContentNoNotice)
	case reasonNoticeExtra:
		reason = msg.Text(msg.ReleaseContentNoticeExtra, e.subject)
	case reasonNoticeMissing:
		reason = msg.Text(msg.ReleaseContentNoticeMissing, e.subject)
	case reasonNoticeEmpty:
		reason = msg.Text(msg.ReleaseContentNoticeEmpty, e.subject)
	case reasonNoticeEmptyItem:
		reason = msg.Text(msg.ReleaseContentNoticeEmptyItem)
	case reasonNoticeLong:
		reason = msg.Text(msg.ReleaseContentNoticeLong)
		lines = append(lines, msg.Text(msg.ReleaseContentLength, e.length), msg.Text(msg.ReleaseContentLimit, noticeLimit))
	}
	lines = append(lines, msg.Text(msg.ReleaseContentError, reason))
	if allowed != "" {
		lines = append(lines, allowed)
	}
	return strings.Join(lines, "\n")
}
