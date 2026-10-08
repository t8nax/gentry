package process_test

import (
	"os"
	"testing"

	"github.com/t8nax/gentry/internal/cli/clitest"
)

func TestMain(m *testing.M) { os.Exit(clitest.Main(m)) }
