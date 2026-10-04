package claude

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/integration"
)

// fake builds the fake claude into a temporary directory, points its state at
// a fresh file and returns the program path and a function reading its log.
func fake(t *testing.T) (program string, log func() []string) {
	t.Helper()
	dir := t.TempDir()
	program = filepath.Join(dir, "fakeclaude")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", program, "./testdata/fakeclaude").CombinedOutput(); err != nil {
		t.Fatalf("build fake claude: %v\n%s", err, out)
	}
	state := filepath.Join(dir, "state.json")
	t.Setenv("FAKECLAUDE_STATE", state)
	return program, func() []string {
		var s struct{ Log []string }
		b, _ := os.ReadFile(state)
		json.Unmarshal(b, &s)
		return s.Log
	}
}

// plugin writes a plugin built for gentryVersion into dir and returns its version.
func plugin(t *testing.T, dir, gentryVersion string) string {
	t.Helper()
	p, err := Build(integration.Gentry(testExe, "Gentry"), gentryVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, p); err != nil {
		t.Fatal(err)
	}
	return p.Version
}

func register(t *testing.T, program, dir, version string) Result {
	t.Helper()
	r, err := Register(program, dir, version)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegisterLifecycle(t *testing.T) {
	program, log := fake(t)
	dir := filepath.Join(t.TempDir(), "claude")

	v1 := plugin(t, dir, "0.1.0")
	if r := register(t, program, dir, v1); r != (Result{Installed, true}) {
		t.Errorf("first run: %+v", r)
	}
	want := []string{
		"plugin marketplace list --json",
		"plugin marketplace add " + dir + " --json",
		"plugin list --json",
		"plugin install gentry@gentry --scope user --json",
	}
	if got := log(); !slices.Equal(got, want) {
		t.Errorf("first run commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if r := register(t, program, dir, v1); r != (Result{Unchanged, true}) {
		t.Errorf("same version: %+v", r)
	}
	for _, c := range log()[4:] {
		if strings.Contains(c, "install") || strings.Contains(c, "plugin update") {
			t.Errorf("same version must not install or update, ran %q", c)
		}
	}

	v2 := plugin(t, dir, "0.2.0")
	if r := register(t, program, dir, v2); r != (Result{Updated, true}) {
		t.Errorf("new version: %+v", r)
	}
	if got := log(); got[len(got)-1] != "plugin update gentry@gentry --json" {
		t.Errorf("new version must update, last command %q", got[len(got)-1])
	}
}

func TestRegisterKeepsDisabled(t *testing.T) {
	program, _ := fake(t)
	dir := filepath.Join(t.TempDir(), "claude")
	register(t, program, dir, plugin(t, dir, "0.1.0"))
	if out, err := exec.Command(program, "plugin", "disable", "gentry@gentry").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if r := register(t, program, dir, plugin(t, dir, "0.2.0")); r != (Result{Updated, false}) {
		t.Errorf("disabled plugin: %+v; want updated and still disabled", r)
	}
}

func TestRegisterMovedMarketplace(t *testing.T) {
	program, log := fake(t)
	old := filepath.Join(t.TempDir(), "claude")
	register(t, program, old, plugin(t, old, "0.1.0"))

	moved := filepath.Join(t.TempDir(), "claude")
	if r := register(t, program, moved, plugin(t, moved, "0.1.0")); r.Action != Installed {
		t.Errorf("moved marketplace: %+v", r)
	}
	got := strings.Join(log(), "\n")
	for _, c := range []string{"plugin marketplace remove gentry --json", "plugin marketplace add " + moved + " --json"} {
		if !strings.Contains(got, c) {
			t.Errorf("missing %q in:\n%s", c, got)
		}
	}
}

func TestRegisterFailure(t *testing.T) {
	program, _ := fake(t)
	dir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("FAKECLAUDE_FAIL", "install")
	_, err := Register(program, dir, plugin(t, dir, "0.1.0"))
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("want a CommandError, got %v", err)
	}
	if ce.Command != "claude plugin install gentry@gentry --scope user --json" || ce.Output != "simulated failure" {
		t.Errorf("unexpected error %+v", ce)
	}
}

func TestProgram(t *testing.T) {
	t.Setenv(ProgramEnv, filepath.Join(t.TempDir(), "missing"))
	if _, err := Program(); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing program: %v, want ErrNotFound", err)
	}
	exe, _ := os.Executable()
	t.Setenv(ProgramEnv, exe)
	if p, err := Program(); err != nil || p != exe {
		t.Errorf("Program() = %q, %v; want %q", p, err, exe)
	}
}
