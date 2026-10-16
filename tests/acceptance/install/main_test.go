// Package install_test holds the acceptance tests of the scripts of
// installation, install/install.sh and install/install.ps1 of the module:
// each test serves a mock release of its own over HTTP, points the scripts at
// it with GENTRY_RELEASES_URL and runs them as the user does, in a home of
// its own. The cases of install.sh run on Linux and macOS, those of
// install.ps1 on Windows, in Windows PowerShell and in PowerShell 7.
package install_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

func TestMain(m *testing.M) {
	// The tests build the programs of the mock release with go build, after
	// Main has moved the home.
	if err := testkit.KeepGo(); err != nil {
		panic(err)
	}
	// The real home, before Main moves it: the tests check after each run of
	// a script that it has not touched it.
	if err := guardRealHome(); err != nil {
		panic(err)
	}
	code := testkit.Main(m)
	removeFakes()
	os.Exit(code)
}
