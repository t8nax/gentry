package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/home"
)

// TestMain points the data root and the user's home at a temporary directory,
// so no test can touch the operator's data, and makes `setup claude` call a
// fake claude instead of the operator's Claude Code.
func TestMain(m *testing.M) {
	os.Exit(isolated(m))
}

func isolated(m *testing.M) int {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv(home.EnvVar, dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)

	fake := filepath.Join(dir, "fakeclaude")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fake, "../adapter/claude/testdata/fakeclaude").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fake claude: %v\n%s", err, out)
		return 1
	}
	os.Setenv(claude.ProgramEnv, fake)
	os.Setenv("FAKECLAUDE_STATE", filepath.Join(dir, "fakeclaude-state.json"))
	return m.Run()
}
