package agents_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

func TestMain(m *testing.M) {
	// The tests build the fake Codex with go build, after Main has moved the
	// home.
	if err := testkit.KeepGo(); err != nil {
		panic(err)
	}
	os.Exit(testkit.Main(m))
}
