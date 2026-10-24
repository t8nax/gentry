// Package e2e holds only tests: they run the gentry binary, built once for
// the package, in processes of its own. They check what only the process
// boundary shows: commands of several processes at once, the MCP server, the
// hooks and their budget, the startup time and the end of the commands of
// checks of a stage. Short mode skips the whole package before it builds
// gentry: CI runs it in full, in a job of its own.
package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/tests/testkit"
)

// TestMain points the data root and the user's home at a temporary directory,
// so no test can touch the operator's data. With helperEnv set, the test
// binary is a command of checks of the tests instead. Short mode runs no
// test.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(checkHelper(mode))
	}
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}
	os.Exit(isolated(m))
}

// testDir is the temporary directory of the package.
var testDir string

func isolated(m *testing.M) int {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	defer testkit.RemoveFakeCodex()
	testDir = dir
	os.Setenv(home.EnvVar, dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	return m.Run()
}
