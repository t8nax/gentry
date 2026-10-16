package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goSettings are the settings of the go command whose defaults lie in the
// user's home: Main moves the home to a temporary directory, and the go
// command would then build without its cache and download the modules anew.
var goSettings = []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}

// KeepGo pins the settings of the go command the tests run to their values in
// the environment of the test, before Main moves the home. Call it from
// TestMain before Main in the packages whose tests run go build.
func KeepGo() error {
	out, err := exec.Command("go", append([]string{"env", "-json"}, goSettings...)...).Output()
	if err != nil {
		return err
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return err
	}
	for _, name := range goSettings {
		if v := env[name]; v != "" {
			os.Setenv(name, v)
		}
	}
	return nil
}

// ModuleRoot returns the root directory of the module under test: the
// directory of its go.mod.
func ModuleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("go env GOMOD: the tests run outside the module")
	}
	return filepath.Dir(gomod)
}

// SkipOnBinary skips the test when the commands run on a built gentry: the
// tests of an area that does not run gentry commands check the same on both,
// so the run on the binary skips them.
func SkipOnBinary(t *testing.T) {
	t.Helper()
	if os.Getenv(BinaryEnv) != "" {
		t.Skipf("%s is set: the area does not run gentry commands, the run in the process of the test checks it", BinaryEnv)
	}
}
