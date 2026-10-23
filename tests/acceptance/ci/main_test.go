// Package ci_test checks the commands the CI workflow runs for acceptance.
package ci_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

func TestMain(m *testing.M) {
	// The go shim reuses this executable as a command, before testkit moves
	// HOME or KeepGo asks the real go command for its settings.
	if os.Getenv("GENTRY_CI_GO_COMMAND") == "1" {
		os.Exit(goCommand())
	}
	if err := testkit.KeepGo(); err != nil {
		panic(err)
	}
	os.Exit(testkit.Main(m))
}
