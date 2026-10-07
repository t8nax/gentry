package main

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
)

// TestMain points the data root and the user's home at a temporary directory,
// so no test can touch the operator's data.
func TestMain(m *testing.M) {
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
	testDir = dir
	os.Setenv(home.EnvVar, dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	return m.Run()
}
