// Package layers holds only tests: they check the direction of the
// dependencies between the packages of the module.
package layers

import (
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// tableFile is the file to edit when the layers of the module change.
const tableFile = "internal/layers/layers_test.go"

// A layer is a row of the table of layers: its packages as paths from the
// root of the module.
type layer struct {
	name     string
	packages []string
}

// An exception lets a package import a package of a higher layer. The
// import carries a comment with the same reason.
type exception struct {
	from, to string
	reason   string
}

// table holds the layers from the top down. A package may import packages
// of its own layer and of the layers below it.
var table = []layer{
	{"entry", []string{"cmd/gentry", "internal/cli"}},
	{"adapters", []string{"internal/adapter/claude", "internal/integration", "internal/hook", "internal/agenttext", "internal/mcp"}},
	{"scenarios", []string{"internal/task", "internal/project", "internal/flow", "internal/agents"}},
	{"storage", []string{"internal/state", "internal/process", "internal/git", "internal/paths", "internal/filelock", "internal/home", "internal/caller", "internal/buildinfo"}},
	{"common", []string{"contract", "internal/msg"}},
}

// helpers are the test helpers with their subpackages: only tests import
// them, so they have no layer and their imports are not checked.
var helpers = []string{"internal/cli/clitest", "internal/gittest"}

// exceptions are the imports of a higher layer the code keeps.
var exceptions = []exception{
	{"internal/state", "internal/flow", "the move to schema 6 reads the stage of each task under way from the snapshot of its flow (fillPath, stageOf)"},
}

// TestImportDirection checks the non-test files of the packages of the
// module against the table of layers: each package but the test helpers has
// a layer; it imports no package of a higher layer but by an exception and
// no test helper; each package of the table and each test helper is a
// package of the module; each exception lets through an import of the code.
func TestImportDirection(t *testing.T) {
	root, module := moduleRoot(t)
	pkgs, err := moduleImports(root, module)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("no package found in %s", root)
	}
	for _, p := range check(pkgs, table, helpers, exceptions) {
		t.Error(p)
	}
}

// TestImportDirectionCatches runs the reading of imports and the check on a
// module made up for the test with a table of its own. It checks that the
// check reports an import of a higher layer, a package with no layer, an
// import of a test helper, a package of the table and a test helper that are
// not in the code and an exception that lets no import through, and nothing
// else: not an import of a lower layer, an import an exception lets through,
// the imports of a test helper and its subpackages. It checks that the
// reading skips test files, files with //go:build ignore, testdata, vendor,
// folders named with . or _ and nested modules.
func TestImportDirectionCatches(t *testing.T) {
	const module = "example.com/shop"
	root := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	imports := func(pkg string, imps ...string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "package %s\n", filepath.Base(pkg))
		for _, imp := range imps {
			fmt.Fprintf(&b, "import _ %q\n", module+"/"+imp)
		}
		return b.String()
	}
	write("go.mod", "module "+module+"\n")
	write("internal/web/web.go", imports("web", "internal/order", "internal/cart"))
	write("internal/cart/cart.go", imports("cart", "internal/web"))
	write("internal/order/order.go", imports("order", "internal/web", "internal/db"))
	write("internal/db/db.go", imports("db", "internal/order", "internal/dbtest"))
	write("internal/dbtest/dbtest.go", imports("dbtest", "internal/web"))
	write("internal/dbtest/fake/fake.go", imports("fake", "internal/web"))
	write("internal/text/text.go", imports("text"))
	write("internal/text/text_test.go", imports("text", "internal/web"))
	write("internal/text/gen.go", "//go:build ignore\n\n"+imports("main", "internal/web"))
	write("internal/text/testdata/x.go", imports("x", "internal/web"))
	write("internal/text/vendor/x/x.go", imports("x", "internal/web"))
	write("internal/text/.cache/x.go", imports("x", "internal/web"))
	write("internal/text/_old/x.go", imports("x", "internal/web"))
	write("internal/text/tool/go.mod", "module example.com/tool\n")
	write("internal/text/tool/x.go", imports("x", "internal/web"))

	pkgs, err := moduleImports(root, module)
	if err != nil {
		t.Fatal(err)
	}
	layers := []layer{
		{"entry", []string{"internal/web"}},
		{"scenarios", []string{"internal/order"}},
		{"storage", []string{"internal/db", "internal/stock"}},
		{"common", []string{"internal/text"}},
	}
	helpers := []string{"internal/dbtest", "internal/mailtest"}
	exceptions := []exception{
		{"internal/db", "internal/order", "the reason"},
		{"internal/text", "internal/web", "the reason"},
		{"internal/order", "internal/db", "the reason"},
	}
	got := check(pkgs, layers, helpers, exceptions)
	want := []string{
		"internal/cart has no layer: add it to the layer table in internal/layers/layers_test.go",
		"internal/db imports test helper internal/dbtest: only tests may import it",
		"internal/order imports internal/web: layer 2 (scenarios) must not import layer 1 (entry)",
		"internal/stock is in the layer table but is not a package: remove it from internal/layers/layers_test.go",
		"test helper internal/mailtest is not a package: remove it from internal/layers/layers_test.go",
		"exception internal/text imports internal/web lets no import through: remove it from internal/layers/layers_test.go",
		"exception internal/order imports internal/db lets no import through: remove it from internal/layers/layers_test.go",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check reports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// moduleRoot returns the folder of go.mod above the working directory and
// the module path of its module line.
func moduleRoot(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for line := range strings.Lines(string(data)) {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
					module, err := strconv.Unquote(f[1])
					if err != nil {
						module = f[1]
					}
					return dir, module
				}
			}
			t.Fatalf("%s has no module line", filepath.Join(dir, "go.mod"))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// moduleImports returns each package of the module under root with the
// packages of the module its files import, both as paths from root. A
// package is a folder with a Go file that is not a test file nor a file with
// //go:build ignore; such files of all systems count, whatever their other
// build constraints. Folders testdata and vendor, folders named with . or _
// and folders with a go.mod of their own are skipped with all they hold.
func moduleImports(root, module string) (map[string][]string, error) {
	fset := token.NewFileSet()
	found := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		for _, group := range f.Comments {
			if group.Pos() > f.Package {
				break
			}
			for _, c := range group.List {
				if !constraint.IsGoBuild(c.Text) {
					continue
				}
				expr, err := constraint.Parse(c.Text)
				if err != nil {
					return fmt.Errorf("%s: %w", p, err)
				}
				if tag, ok := expr.(*constraint.TagExpr); ok && tag.Tag == "ignore" {
					return nil
				}
			}
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		if found[pkg] == nil {
			found[pkg] = map[string]bool{}
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			if rest, ok := strings.CutPrefix(imp, module+"/"); ok {
				found[pkg][rest] = true
			}
		}
		return nil
	})
	pkgs := map[string][]string{}
	for pkg, imps := range found {
		pkgs[pkg] = slices.Sorted(maps.Keys(imps))
	}
	return pkgs, err
}

// check returns what breaks the table of layers in the packages of pkgs
// with their imports: a package with no layer, an import of a higher layer
// that no exception lets through, an import of a test helper; a package of
// the table or a test helper that is not in pkgs; an exception that lets no
// import through.
func check(pkgs map[string][]string, layers []layer, helpers []string, exceptions []exception) []string {
	isHelper := func(pkg string) bool {
		return slices.ContainsFunc(helpers, func(h string) bool {
			return pkg == h || strings.HasPrefix(pkg, h+"/")
		})
	}
	level := map[string]int{}
	for i, l := range layers {
		for _, p := range l.packages {
			level[p] = i
		}
	}
	used := make([]bool, len(exceptions))
	var out []string
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		if isHelper(p) {
			continue
		}
		from, ok := level[p]
		if !ok {
			out = append(out, fmt.Sprintf("%s has no layer: add it to the layer table in %s", p, tableFile))
			continue
		}
		for _, imp := range pkgs[p] {
			if isHelper(imp) {
				out = append(out, fmt.Sprintf("%s imports test helper %s: only tests may import it", p, imp))
				continue
			}
			to, ok := level[imp]
			if !ok || to >= from {
				// A package with no layer is reported once, as itself.
				continue
			}
			if i := slices.IndexFunc(exceptions, func(e exception) bool { return e.from == p && e.to == imp }); i >= 0 {
				used[i] = true
				continue
			}
			out = append(out, fmt.Sprintf("%s imports %s: layer %d (%s) must not import layer %d (%s)",
				p, imp, from+1, layers[from].name, to+1, layers[to].name))
		}
	}
	for _, l := range layers {
		for _, p := range l.packages {
			if _, ok := pkgs[p]; !ok {
				out = append(out, fmt.Sprintf("%s is in the layer table but is not a package: remove it from %s", p, tableFile))
			}
		}
	}
	for _, h := range helpers {
		if _, ok := pkgs[h]; !ok {
			out = append(out, fmt.Sprintf("test helper %s is not a package: remove it from %s", h, tableFile))
		}
	}
	for i, e := range exceptions {
		if !used[i] {
			out = append(out, fmt.Sprintf("exception %s imports %s lets no import through: remove it from %s", e.from, e.to, tableFile))
		}
	}
	return out
}
