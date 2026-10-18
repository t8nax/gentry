// Package update_test holds the acceptance tests of the update of Gentry and
// of the commands of installation and maintenance: gentry update, the check
// of the program and the plugin by the session start hook of the installed
// program, and setup and update as commands of the command line only.
//
// The cases of gentry update run on a stand of their own each: a data root
// in a temporary folder with the program of a release in its bin, a fake
// Claude Code with a state of its own, and a mock release served over HTTP
// at the paths of GitHub, which GENTRY_RELEASES_URL points the program at.
// The programs of the releases 0.1.0, 0.2.0, 0.3.0 and 0.4.0 are Gentry of
// the module under test, built once for the package from a copy of the
// module tagged with each version, as the release builds them; so each
// names its version and runs the code under test.
//
// Those cases run the installed program, not the commands of testkit, and
// are skipped on a built gentry, as the area of the scripts of installation
// is; the cases of the help and the tools run the commands of testkit.
package update_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

func TestMain(m *testing.M) {
	// The tests build the programs of the releases and the fake Claude Code
	// with go build, after Main has moved the home.
	if err := testkit.KeepGo(); err != nil {
		panic(err)
	}
	code := testkit.Main(m)
	removeBuilds()
	os.Exit(code)
}
