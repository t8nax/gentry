// Package release_test holds the acceptance tests of the release: the checks
// of the button Release, the build of a release into a folder, the build of
// the site of the pages of the releases and the build of the message about
// a release to the group in Telegram, made by the program of release
// of the module, which the workflows run, on a temporary git repository; the
// build of the workflows; and the journal of changes of the module.
//
// The program of release is ./cmd/release of the module, run in the root of
// the repository of the release:
//
//	release check <version>          checks that the version can be released
//	                                 and prints the text of the release
//	release build <version> <folder> builds the files of the release into
//	                                 the folder
//	release pages <folder>           checks that the pages can be published
//	                                 and builds the site of the pages of the
//	                                 released versions from the head of the
//	                                 repository into the folder: index.html,
//	                                 the list, and <version>/index.html
//	release notice <version>         prints the message about the release
//	                                 to the group in Telegram, HTML of
//	                                 Telegram
//
// A refusal exits with code 1 and its text on the standard error.
package release_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

func TestMain(m *testing.M) {
	// The tests build the program of release, and it builds Gentry, after
	// Main has moved the home.
	if err := testkit.KeepGo(); err != nil {
		panic(err)
	}
	code := testkit.Main(m)
	removeProgram()
	os.Exit(code)
}
