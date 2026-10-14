package stage_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/t8nax/gentry/tests/testkit"
)

// helperEnv makes the test binary a command of checks: its value names what
// the command does. The command inherits the environment of Gentry, so the
// test sets it.
const helperEnv = "GENTRY_STAGE_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(testkit.Main(m))
	case "fail":
		fmt.Fprintln(os.Stdout, "итог: 1 ошибка")
		fmt.Fprintln(os.Stderr, "cart_test.go:7: Total = 5, want 6")
		os.Exit(3)
	case "sleep":
		time.Sleep(20 * time.Second)
	}
	os.Exit(0)
}
