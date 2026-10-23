package architecture

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

const (
	mainHelperEnv = "GENTRY_TESTKIT_MAIN_HELPER"
	mainMarkerEnv = "GENTRY_TESTKIT_MAIN_MARKER"
	mainFailEnv   = "GENTRY_TESTKIT_MAIN_FAIL"
)

// The child process of TestAcceptanceMain enters the shared acceptance main;
// the architecture tests themselves always run, including in short mode.
func TestMain(m *testing.M) {
	if os.Getenv(mainHelperEnv) == "1" {
		os.Exit(testkit.Main(m))
	}
	os.Exit(m.Run())
}

// TestAcceptanceMain checks the process boundary of the shared entry point:
// short mode skips preparation and test functions; full mode isolates the
// home, runs the tests, cleans up and preserves their exit status.
func TestAcceptanceMain(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		short, fail bool
	}{
		{name: "short", short: true},
		{name: "full"},
		{name: "failure", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "ran")
			args := []string{"-test.run=^TestAcceptanceMainHelper$", "-test.v"}
			if tc.short {
				args = append(args, "-test.short")
			}
			cmd := exec.Command(executable, args...)
			cmd.Env = append(os.Environ(), mainHelperEnv+"=1", mainMarkerEnv+"="+marker,
				mainFailEnv+"="+strconv.FormatBool(tc.fail), testkit.BinaryEnv+"=")
			if tc.short {
				// Neither the temporary home nor the binary is needed when no
				// test runs. Both would fail if Main prepared the full run.
				missing := filepath.Join(dir, "missing")
				cmd.Env = append(cmd.Env, "TMP="+missing, "TEMP="+missing, "TMPDIR="+missing,
					testkit.BinaryEnv+"="+filepath.Join(missing, "gentry"))
			}
			out, err := cmd.CombinedOutput()
			if tc.fail {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("failing test: %v\n%s", err, out)
				}
			} else if err != nil {
				t.Fatalf("test main: %v\n%s", err, out)
			}
			data, err := os.ReadFile(marker)
			if tc.short {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("short mode ran a test: marker %q, error %v\n%s", data, err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("full mode did not run a test: %v\n%s", err, out)
			}
			if _, err := os.Stat(string(data)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("temporary home %q survived Main: %v", data, err)
			}
		})
	}
}

func TestAcceptanceMainHelper(t *testing.T) {
	if os.Getenv(mainHelperEnv) != "1" {
		t.Skip("runs in the child process of TestAcceptanceMain")
	}
	home := os.Getenv(testkit.HomeEnv)
	if home == "" || os.Getenv("HOME") != home || os.Getenv("USERPROFILE") != home {
		t.Fatalf("Main did not isolate the home: GENTRY_HOME=%q, HOME=%q, USERPROFILE=%q",
			home, os.Getenv("HOME"), os.Getenv("USERPROFILE"))
	}
	if err := os.WriteFile(os.Getenv(mainMarkerEnv), []byte(home), 0o644); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(mainFailEnv) == "true" {
		t.Fatal("requested helper failure")
	}
}

// TestAcceptanceMains keeps new areas on the common entry point, which
// isolates the full run and skips all acceptance tests in short mode.
func TestAcceptanceMains(t *testing.T) {
	root, module := moduleRoot(t)
	problems, err := acceptanceMains(root, module)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestAcceptanceMainsCatches(t *testing.T) {
	const module = "example.com/shop"
	root := t.TempDir()
	write := func(area, src string) {
		p := filepath.Join(root, "tests", "acceptance", filepath.FromSlash(area))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := func(imp, body string) string {
		return fmt.Sprintf("package area_test\nimport (\"testing\"; kit %q)\nfunc TestMain(m *testing.M) { %s }\n", imp, body)
	}
	write("shared/main_test.go", main(module+"/tests/testkit", "kit.Main(m)"))
	write("ignored/case_test.go", "package area_test\nimport \"testing\"\nfunc TestCase(t *testing.T) {}\n")
	write("ignored/main_test.go", "//go:build ignore\n\n"+main(module+"/tests/testkit", "kit.Main(m)"))
	write("missing/main_test.go", "package area_test\nimport \"testing\"\nfunc TestCase(t *testing.T) {}\n")
	write("bypass/main_test.go", main(module+"/tests/testkit", "kit.Main(m); m.Run()"))
	write("wrong-import/main_test.go", main("example.com/other/testkit", "kit.Main(m)"))
	write("wrong-argument/main_test.go", main(module+"/tests/testkit", "kit.Main(nil)"))

	got, err := acceptanceMains(root, module)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tests/acceptance/bypass/main_test.go: TestMain calls m.Run directly: use testkit.Main(m)",
		"tests/acceptance/ignored (area_test): no TestMain: call testkit.Main(m) from the common acceptance entry point",
		"tests/acceptance/missing (area_test): no TestMain: call testkit.Main(m) from the common acceptance entry point",
		"tests/acceptance/wrong-argument/main_test.go: TestMain must call testkit.Main(m)",
		"tests/acceptance/wrong-import/main_test.go: TestMain must call testkit.Main(m)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check reports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// acceptanceMains checks TestMain of every test package under acceptance.
// testImports gives the same directory exclusions as the import checks.
func acceptanceMains(root, module string) ([]string, error) {
	dirs, err := testImports(root, module, acceptanceDir)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, filepath.Join(root, filepath.FromSlash(dir)), func(info fs.FileInfo) bool {
			name := info.Name()
			return strings.HasSuffix(name, "_test.go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
		}, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, name := range slices.Sorted(maps.Keys(pkgs)) {
			found := false
			hasTests := false
			for _, file := range slices.Sorted(maps.Keys(pkgs[name].Files)) {
				f := pkgs[name].Files[file]
				ignored, err := ignoredGoFile(f)
				if err != nil {
					return nil, err
				}
				if ignored {
					continue
				}
				hasTests = true
				for _, decl := range f.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" {
						continue
					}
					found = true
					rel, err := filepath.Rel(root, file)
					if err != nil {
						return nil, err
					}
					shared, direct := mainCalls(f, fn, module+"/tests/testkit")
					if !shared {
						problems = append(problems, filepath.ToSlash(rel)+": TestMain must call testkit.Main(m)")
					}
					if direct {
						problems = append(problems, filepath.ToSlash(rel)+": TestMain calls m.Run directly: use testkit.Main(m)")
					}
				}
			}
			if hasTests && !found {
				problems = append(problems, dir+" ("+name+"): no TestMain: call testkit.Main(m) from the common acceptance entry point")
			}
		}
	}
	return problems, nil
}

func mainCalls(f *ast.File, fn *ast.FuncDecl, kitPath string) (shared, direct bool) {
	kit := ""
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err == nil && p == kitPath {
			kit = "testkit"
			if imp.Name != nil {
				kit = imp.Name.Name
			}
		}
	}
	if fn.Type.Params.NumFields() != 1 || len(fn.Type.Params.List[0].Names) != 1 {
		return false, false
	}
	m := fn.Type.Params.List[0].Names[0].Name
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if receiver.Name == m && sel.Sel.Name == "Run" {
			direct = true
		}
		if kit != "" && receiver.Name == kit && sel.Sel.Name == "Main" && len(call.Args) == 1 {
			arg, ok := call.Args[0].(*ast.Ident)
			shared = shared || ok && arg.Name == m
		}
		return true
	})
	return shared, direct
}
