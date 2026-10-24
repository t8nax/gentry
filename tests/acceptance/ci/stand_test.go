package ci_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/t8nax/gentry/tests/testkit"
)

const fixtureModule = "github.com/t8nax/gentry"

type ciStand struct {
	root, shim, log, script, realGo string
	runner, binary, home, dataHome  string
	binaryExit, processExit         int
	failedArea                      string
}

// acceptanceStand makes a fresh checkout-shaped module. The shim observes
// test invocations only: every other go command runs the actual tool, so
// discovery may use the filesystem, git, go list or another ordinary tool.
func acceptanceStand(t *testing.T, runner string, areas ...string) *ciStand {
	t.Helper()
	testkit.SkipOnBinary(t)
	module := testkit.ModuleRoot(t)
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s := &ciStand{
		root: root, shim: filepath.Join(root, "bin"),
		log: filepath.Join(root, "go-test.jsonl"), script: filepath.Join(root, "acceptance.sh"),
		realGo: realGo, runner: runner,
		binary: filepath.ToSlash(filepath.Join(root, "built gentry")),
		home:   filepath.Join(root, "home"), dataHome: filepath.Join(root, "gentry-home"),
	}
	writeStandFile(t, root, "go.mod", "module "+fixtureModule+"\n\ngo 1.27.0\n")
	gocmd, err := os.ReadFile(filepath.Join(module, "tests", "testkit", "gocmd.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeStandFile(t, root, "tests/testkit/gocmd.go", string(gocmd))
	writeStandFile(t, root, "tests/testkit/binary.go", "package testkit\nconst BinaryEnv = \"GENTRY_TEST_BINARY\"\n")
	for _, area := range areas {
		dir := "tests/acceptance/" + area + "/"
		if area == "ordinary" {
			writeStandFile(t, root, dir+"area_test.go", "package ordinary\nimport \"testing\"\nfunc TestArea(t *testing.T) {}\n")
			continue
		}
		writeStandFile(t, root, dir+"helper.go", fmt.Sprintf(`package %s
import (
    "testing"
    "%s/tests/testkit"
)
func inProcess(t *testing.T) {
    t.Helper()
    testkit.SkipOnBinary(t)
}
`, area, fixtureModule))
		writeStandFile(t, root, dir+"area_test.go", fmt.Sprintf("package %s\nimport \"testing\"\nfunc TestArea(t *testing.T) { inProcess(t) }\n", area))
	}
	// Tracked files let discovery use git as it would in the CI checkout.
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeStandFile(t, root, "bin/go", "#!/usr/bin/env bash\nGENTRY_CI_GO_COMMAND=1 exec "+shellQuote(filepath.ToSlash(exe))+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(s.shim, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStandFile(t, root, "acceptance.sh", acceptanceRun(t, module))
	return s
}

// acceptanceRun reads the actual command block, not a transcription of it.
func acceptanceRun(t *testing.T, module string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(module, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string `yaml:"id"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["test"].Steps {
		if step.ID == "acceptance" && step.Run != "" {
			return step.Run
		}
	}
	t.Fatal("CI has no acceptance run block")
	return ""
}

func (s *ciStand) run(t *testing.T) (int, []goCall) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bashCommand(t), "--noprofile", "--norc", "-e", "-o", "pipefail", "./acceptance.sh")
	cmd.Dir = s.root
	cmd.Env = append(os.Environ(),
		"PATH="+s.shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RUNNER_OS="+s.runner, "ACCEPTANCE_BINARY="+s.binary,
		"HOME="+s.home, "USERPROFILE="+s.home, "GENTRY_HOME="+s.dataHome,
		"GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
		"GENTRY_CI_REAL_GO="+s.realGo, "GENTRY_CI_LOG="+s.log,
		"GENTRY_CI_BINARY_EXIT="+strconv.Itoa(s.binaryExit),
		"GENTRY_CI_PROCESS_EXIT="+strconv.Itoa(s.processExit),
		"GENTRY_CI_FAILED_AREA="+s.failedArea,
		"MSYS2_ENV_CONV_EXCL=GENTRY_CI_*;GENTRY_TEST_BINARY;ACCEPTANCE_BINARY;GENTRY_HOME;HOME;USERPROFILE",
	)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("acceptance run output:\n%s", out)
	}
	if ctx.Err() != nil {
		t.Fatalf("acceptance run timed out: %v", ctx.Err())
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("start acceptance run: %v", err)
		}
		code = exit.ExitCode()
	}
	var calls []goCall
	f, err := os.Open(s.log)
	if os.IsNotExist(err) {
		return code, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	for {
		var call goCall
		if err := decoder.Decode(&call); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("read go test journal: %v", err)
		}
		calls = append(calls, call)
	}
	return code, calls
}

func (s *ciStand) wantPasses(t *testing.T, calls []goCall, wantProcess []string) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatal("CI ran no acceptance tests")
	}
	if got := testPackages(calls[0].Args); !slices.Equal(got, []string{"tests/acceptance/..."}) {
		t.Errorf("first pass packages = %v, want ./tests/acceptance/...", got)
	}
	var process []string
	for i, call := range calls {
		if i == 0 {
			if !call.BinarySet || call.Binary != s.binary {
				t.Errorf("first pass GENTRY_TEST_BINARY = %q (set %v), want %q", call.Binary, call.BinarySet, s.binary)
			}
		} else {
			if call.BinarySet {
				t.Errorf("process pass %d still sets GENTRY_TEST_BINARY = %q", i, call.Binary)
			}
			packages := testPackages(call.Args)
			if len(packages) == 0 {
				t.Errorf("process pass %d ran go test without packages", i)
			}
			for _, p := range packages {
				process = append(process, strings.TrimPrefix(p, "tests/acceptance/"))
			}
		}
		if call.Runner != s.runner || call.AcceptanceBinary != s.binary || call.Home != s.home || call.UserProfile != s.home || call.DataHome != s.dataHome {
			t.Errorf("pass %d changed the job environment: %+v", i+1, call)
		}
	}
	slices.Sort(process)
	if !slices.Equal(process, wantProcess) {
		t.Errorf("process pass areas = %v, want %v; go test calls: %+v", process, wantProcess, calls)
	}
}

func testPackages(args []string) []string {
	var packages []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if slices.Contains([]string{"-count", "-run", "-timeout", "-parallel", "-p", "-tags", "-cpu"}, arg) {
				i++
			}
			continue
		}
		arg = strings.TrimPrefix(arg, fixtureModule+"/")
		packages = append(packages, strings.TrimPrefix(arg, "./"))
	}
	return packages
}

func writeStandFile(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// On Windows the bash on PATH can be the WSL launcher; CI uses Git Bash.
func bashCommand(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		git, err := exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"../bin/bash.exe", "../../bin/bash.exe", "../usr/bin/bash.exe", "../../usr/bin/bash.exe"} {
			candidate := filepath.Clean(filepath.Join(filepath.Dir(git), filepath.FromSlash(rel)))
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		t.Fatal("Git Bash was not found next to git")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	return bash
}

type goCall struct {
	Args                                                  []string
	Binary                                                string
	BinarySet                                             bool
	Runner, AcceptanceBinary, Home, UserProfile, DataHome string
}

// goCommand does not discover areas or choose expected packages. It records
// the workflow's go test commands, preserves their environment, and returns
// a configurable result for each pass. Other go commands are unmodified.
func goCommand() int {
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "test" {
		cmd := exec.Command(os.Getenv("GENTRY_CI_REAL_GO"), args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode()
			}
			fmt.Fprintln(os.Stderr, err)
			return 127
		}
		return 0
	}
	binary, set := os.LookupEnv("GENTRY_TEST_BINARY")
	call := goCall{
		Args: args, Binary: binary, BinarySet: set,
		Runner: os.Getenv("RUNNER_OS"), AcceptanceBinary: os.Getenv("ACCEPTANCE_BINARY"),
		Home: os.Getenv("HOME"), UserProfile: os.Getenv("USERPROFILE"), DataHome: os.Getenv("GENTRY_HOME"),
	}
	f, err := os.OpenFile(os.Getenv("GENTRY_CI_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		err = json.NewEncoder(f).Encode(call)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	name := "GENTRY_CI_PROCESS_EXIT"
	if binary != "" {
		name = "GENTRY_CI_BINARY_EXIT"
	} else if area := os.Getenv("GENTRY_CI_FAILED_AREA"); area != "" {
		packages := testPackages(args)
		if !slices.Contains(packages, "tests/acceptance/"+area) && !slices.Contains(packages, "tests/acceptance/...") {
			return 0
		}
	}
	code, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	return code
}
