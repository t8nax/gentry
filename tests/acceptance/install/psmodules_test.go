package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// TestPowerShellBuiltinModules checks the launcher with the environment of
// a Go process started from PowerShell 7: its module path contains modules
// of both PowerShell editions. Each child must load its own Get-FileHash
// without changing the environment of the process that launched it.
func TestPowerShellBuiltinModules(t *testing.T) {
	testkit.SkipOnBinary(t)
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 is checked on Windows")
	}

	var modulePaths []string
	for _, shell := range []string{pwsh, powershell} {
		program, err := exec.LookPath(shell)
		if err != nil {
			t.Fatal(err)
		}
		modulePaths = append(modulePaths, filepath.Join(filepath.Dir(program), "Modules"))
	}
	mixed := strings.Join(modulePaths, string(os.PathListSeparator))

	for _, tc := range []struct {
		name string
		key  string // empty: PSModulePath is absent in the parent
	}{
		{name: "mixed", key: "PSModulePath"},
		{name: "mixed_case", key: "pSmOdUlEpAtH"},
		{name: "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Setenv registers restoration even for the absent-variable case.
			// Removing the old entry also lets the case below choose its spelling.
			t.Setenv("PSModulePath", "")
			if err := os.Unsetenv("PSModulePath"); err != nil {
				t.Fatal(err)
			}
			if tc.key != "" {
				t.Setenv(tc.key, mixed)
			}

			for _, shell := range ps1Only {
				t.Run(shell, func(t *testing.T) {
					s := cleanMachine(t, shell)
					writeFile(t, filepath.Join(s.home, "hash-input.bin"), "abc")
					parent := os.Environ()
					command := "$ErrorActionPreference = 'Stop'; [Console]::Out.WriteLine((Get-FileHash -LiteralPath 'hash-input.bin' -Algorithm SHA256).Hash)"
					r := s.runScript(exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", command))
					if !slices.Equal(os.Environ(), parent) {
						t.Error("the PowerShell launcher changed the parent environment")
					}
					wantCode(t, r, 0)
					// SHA256 of the literal bytes abc, independent of the mock release.
					const want = "BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD\n"
					if r.stdout != want || r.stderr != "" {
						t.Errorf("Get-FileHash: %s\nwant stdout %q and empty stderr", r, want)
					}
				})
			}
		})
	}
}
