package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/caller"
	"github.com/t8nax/gentry/internal/gittest"
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
	// The tests run outside a session of an AI tool even when they are run
	// from one.
	// The tools of Gentry are allowed in the settings of Claude Code, as by an
	// earlier setup; a test of the permission gives setup settings of its own.
	settings := filepath.Join(dir, "claude-config")
	os.MkdirAll(settings, 0o755)
	os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"permissions":{"allow":["`+claude.PermissionRule+`"]}}`), 0o644)
	os.Setenv(claude.ConfigDirEnv, settings)
	os.Unsetenv(caller.ClaudeCodeEnv)
	os.Unsetenv(caller.SessionEnv)
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}

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
