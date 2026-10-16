// Command release makes the checks and the build of a release of Gentry for
// the workflow Release, which publishes what it builds. It runs in the root
// of the repository of the release:
//
//	release check <version>          checks that the head of main can be
//	                                 released as the version and prints the
//	                                 text of the release: the section of the
//	                                 version in CHANGELOG.md
//	release build <version> <folder> builds the files of the release into
//	                                 the folder: the programs of the six
//	                                 platforms, checksums.txt and the scripts
//	                                 of installation
//
// A refusal exits with code 1 and its text on the standard error; a wrong
// command line exits with code 2. The texts of the refusals are in the catalog
// of messages of Gentry, internal/msg, as the texts of gentry are.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/t8nax/gentry/internal/msg"
)

const usage = `usage:
  release check <version>
  release build <version> <folder>
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
			_, err = io.WriteString(stdout, text)
		}
	case len(args) == 3 && args[0] == "build":
		err = build(".", args[1], args[2])
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
	}
	return err.Error()
}
