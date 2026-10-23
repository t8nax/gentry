// Package architecture holds only tests: they check the direction of the
// dependencies between the packages of the module and the packages of
// internal/ the acceptance tests import.
package architecture

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// tableFile is the file to edit when the layers of the module or the imports
// allowed to the acceptance tests change.
const tableFile = "tests/architecture/layers_test.go"

// A layer is a row of the table of layers: its packages as paths from the
// root of the module.
type layer struct {
	name     string
	packages []string
}

// An exception lets a package import a package of a higher layer for a
// reason. Each import it lets through carries a comment that holds the
// reason.
type exception struct {
	from, to string
	reason   string
}

// table holds the layers from the top down. A package may import packages
// of its own layer and of the layers below it.
var table = []layer{
	{"entry", []string{"cmd/gentry", "internal/cli", "cmd/release"}},
	{"adapters", []string{"internal/adapter/claude", "internal/adapter/codex", "internal/integration", "internal/hook", "internal/agenttext", "internal/mcp"}},
	{"scenarios", []string{"internal/task", "internal/project", "internal/flow", "internal/agents", "internal/update"}},
	{"storage", []string{"internal/state", "internal/process", "internal/git", "internal/paths", "internal/filelock", "internal/home", "internal/caller", "internal/buildinfo", "internal/checks", "internal/releases", "internal/changelog"}},
	{"common", []string{"contract", "internal/msg"}},
}

// helpers are the test helpers with their subpackages: only tests import
// them, so they have no layer and their imports are not checked.
var helpers = []string{"tests/testkit", "internal/gittest"}

// exceptions are the imports of a higher layer the code keeps.
var exceptions = []exception{
	{"internal/state", "internal/flow", "the move to schema 6 reads the stage of each task under way from the snapshot of its flow (fillPath, stageOf)"},
}

// acceptanceDir holds the acceptance tests: they check gentry through its
// command line.
const acceptanceDir = "tests/acceptance"

// An allowance lets the acceptance tests import a package of internal/ for a
// reason.
type allowance struct {
	pkg, reason string
}

// allowed are the packages of internal/ the acceptance tests may import;
// contract and the test helpers of tests/testkit they import freely. The
// names of the environment variables of Gentry come through testkit. The
// imports of tests/testkit itself are not checked against this list, nor by
// the direction of the layers: testkit is a test helper, which has no layer.
var allowed = []allowance{
	{"internal/msg", "the expected texts are built from the catalog of messages, so a wording is a literal in one test only"},
	{"internal/cli", "the hints in the form of the channel (HintText, HintFor) are built by the functions the output uses"},
	{"internal/gittest", "the test helper makes the repositories and worktrees of the shop"},
	{"internal/state", "a test makes a state the commands cannot make, such as a newer schema, and finds whether a command created the state store"},
	{"internal/flow", "testkit gives the places of the flow of the shop as flow.Places, and the tests of flow apply and flow discard check the types of the events the flow writes"},
	{"internal/adapter/claude", "the test of a stale plugin builds the plugin by the adapter, and the tests of the hook name its tool and folder"},
	{"internal/integration", "the test of a stale plugin builds the plugin of the integration of Gentry"},
	{"internal/buildinfo", "the test of a stale plugin builds the plugin of the version of Gentry"},
}

// TestImportDirection checks the non-test files of the packages of the
// module against the table of layers: each package but the test helpers has
// a layer; it imports no package of a higher layer but by an exception and
// no test helper; each package of the table is there once and is not a test
// helper; each package of the table and each test helper is a package of the
// module; each exception has a reason and lets through an import of the
// code; each import it lets through has a comment that holds the reason.
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
// check reports, in each file that has it, an import of a higher layer, an
// import of a test helper and an import an exception lets through with no
// comment or with a comment that does not hold the reason; a package with no
// layer, a package of the table and a test helper that are not in the code,
// an exception that lets no import through, an exception with no reason, a
// package in the table twice and a package in the table that is a test
// helper; and nothing else: not an import of a lower layer, an import an
// exception lets through with the reason in the comment before it, in
// parentheses or not, or after it, across lines, the imports of a test helper
// and its subpackages. It checks that the reading skips test files, files
// with //go:build ignore, files and folders named with . or _, testdata,
// vendor and nested modules.
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
	write("internal/order/ship.go", imports("order", "internal/web"))
	write("internal/db/db.go", "package db\n\nimport (\n"+
		"\t// An exception: the\n\t// reason, kept.\n"+
		"\t_ \""+module+"/internal/order\"\n"+
		"\t_ \""+module+"/internal/dbtest\"\n)\n")
	write("internal/db/cache.go", "package db\n\n// Kept for the reason.\nimport _ \""+module+"/internal/web\"\n")
	write("internal/db/queue.go", "package db\n\nimport _ \""+module+"/internal/web\" // the reason\n")
	write("internal/db/mail.go", imports("db", "internal/web"))
	write("internal/db/stock.go", "package db\n\n// Another reason.\nimport _ \""+module+"/internal/web\"\n")
	write("internal/db/seed.go", imports("db", "internal/dbtest"))
	write("internal/dbtest/dbtest.go", imports("dbtest", "internal/web"))
	write("internal/dbtest/fake/fake.go", imports("fake", "internal/web"))
	write("internal/text/text.go", imports("text", "internal/order"))
	write("internal/text/text_test.go", imports("text", "internal/web"))
	write("internal/text/gen.go", "//go:build ignore\n\n"+imports("main", "internal/web"))
	write("internal/text/_x.go", imports("text", "internal/web"))
	write("internal/text/.x.go", imports("text", "internal/web"))
	write("internal/draft/_draft.go", imports("draft", "internal/web"))
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
		{"storage", []string{"internal/db", "internal/stock", "internal/dbtest"}},
		{"common", []string{"internal/text", "internal/order"}},
	}
	helpers := []string{"internal/dbtest", "internal/mailtest"}
	exceptions := []exception{
		{"internal/db", "internal/order", "the reason"},
		{"internal/db", "internal/web", "the reason"},
		{"internal/text", "internal/order", ""},
		{"internal/text", "internal/web", "the reason"},
		{"internal/order", "internal/db", "the reason"},
	}
	got := check(pkgs, layers, helpers, exceptions)
	want := []string{
		"internal/cart has no layer: add it to the layer table in tests/architecture/layers_test.go",
		"internal/db/db.go imports test helper internal/dbtest: only tests may import it",
		"internal/db/seed.go imports test helper internal/dbtest: only tests may import it",
		`internal/db/mail.go imports internal/web without the reason of its exception: write "the reason" in the comment on the import or fix the reason in tests/architecture/layers_test.go`,
		`internal/db/stock.go imports internal/web without the reason of its exception: write "the reason" in the comment on the import or fix the reason in tests/architecture/layers_test.go`,
		"internal/order/order.go imports internal/web: layer 2 (scenarios) must not import layer 1 (entry)",
		"internal/order/ship.go imports internal/web: layer 2 (scenarios) must not import layer 1 (entry)",
		"internal/stock is in the layer table but is not a package: remove it from tests/architecture/layers_test.go",
		"internal/dbtest is in the layer table and is a test helper: remove it from one of them in tests/architecture/layers_test.go",
		"internal/order is in the layer table twice: keep it in one layer in tests/architecture/layers_test.go",
		"test helper internal/mailtest is not a package: remove it from tests/architecture/layers_test.go",
		"exception internal/text imports internal/order has no reason: write it in tests/architecture/layers_test.go",
		"exception internal/text imports internal/web lets no import through: remove it from tests/architecture/layers_test.go",
		"exception internal/order imports internal/db lets no import through: remove it from tests/architecture/layers_test.go",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check reports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestAcceptanceImports checks the files of the acceptance tests, test files
// or not, against the imports allowed to them: they import of internal/ only
// the packages allowed; each allowance has a reason and lets through an
// import of the tests.
func TestAcceptanceImports(t *testing.T) {
	root, module := moduleRoot(t)
	pkgs, err := testImports(root, module, acceptanceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("no acceptance test found in %s", filepath.Join(root, filepath.FromSlash(acceptanceDir)))
	}
	for _, p := range checkAcceptance(pkgs, allowed) {
		t.Error(p)
	}
}

// TestAcceptanceImportsCatches runs the reading of imports of the tests and
// the check of the acceptance tests on a module made up for the test with
// allowances of its own. It checks that the check reports, in each file that
// has it, an import of internal/ no allowance lets through, in test files and
// in other files; an allowance with no reason and an allowance that lets no
// import through; and nothing else: not an allowed import, an import an
// allowance with no reason lets through, an import of the module out of
// internal/ and an import of the standard library. It checks that the
// reading reads only the folder of the acceptance tests, files of all
// systems, and skips files with //go:build ignore, files and folders named
// with . or _, testdata, vendor and nested modules.
func TestAcceptanceImportsCatches(t *testing.T) {
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
		fmt.Fprintf(&b, "package %s\n", pkg)
		for _, imp := range imps {
			fmt.Fprintf(&b, "import _ %q\n", module+"/"+imp)
		}
		return b.String()
	}
	write("go.mod", "module "+module+"\n")
	write("tests/acceptance/cart/cart_test.go", imports("cart_test", "internal/text", "internal/db", "tests/kit", "contract")+"import _ \"strings\"\n")
	write("tests/acceptance/cart/total_test.go", imports("cart_test", "internal/db", "internal/web"))
	write("tests/acceptance/cart/shop.go", imports("cart", "internal/web"))
	write("tests/acceptance/order/order_unix_test.go", "//go:build unix\n\n"+imports("order_test", "internal/order", "internal/text"))
	write("tests/acceptance/order/gen_test.go", "//go:build ignore\n\n"+imports("main", "internal/db"))
	write("tests/acceptance/order/_x_test.go", imports("order_test", "internal/db"))
	write("tests/acceptance/order/.x_test.go", imports("order_test", "internal/db"))
	write("tests/acceptance/order/testdata/x_test.go", imports("x", "internal/db"))
	write("tests/acceptance/order/vendor/x/x_test.go", imports("x", "internal/db"))
	write("tests/acceptance/order/.cache/x_test.go", imports("x", "internal/db"))
	write("tests/acceptance/order/_old/x_test.go", imports("x", "internal/db"))
	write("tests/acceptance/order/tool/go.mod", "module example.com/tool\n")
	write("tests/acceptance/order/tool/x_test.go", imports("x", "internal/db"))
	write("tests/e2e/e2e_test.go", imports("e2e", "internal/db"))
	write("internal/db/db_test.go", imports("db", "internal/web"))

	pkgs, err := testImports(root, module, "tests/acceptance")
	if err != nil {
		t.Fatal(err)
	}
	allowed := []allowance{
		{"internal/text", "the reason"},
		{"internal/order", ""},
		{"internal/mail", "the reason"},
	}
	got := checkAcceptance(pkgs, allowed)
	want := []string{
		"tests/acceptance/cart/cart_test.go imports internal/db: an acceptance test imports of internal/ only the packages allowed in tests/architecture/layers_test.go",
		"tests/acceptance/cart/total_test.go imports internal/db: an acceptance test imports of internal/ only the packages allowed in tests/architecture/layers_test.go",
		"tests/acceptance/cart/shop.go imports internal/web: an acceptance test imports of internal/ only the packages allowed in tests/architecture/layers_test.go",
		"tests/acceptance/cart/total_test.go imports internal/web: an acceptance test imports of internal/ only the packages allowed in tests/architecture/layers_test.go",
		"allowed import internal/order has no reason: write it in tests/architecture/layers_test.go",
		"allowed import internal/mail lets no import through: remove it from tests/architecture/layers_test.go",
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

// A use is an import in a file: the file as a path from the root of the
// module and the comment on the import, before it and after it, with its
// spaces collapsed.
type use struct {
	file, comment string
}

// moduleImports returns each package of the module under root with the
// packages of the module its files import, both as paths from root, and the
// uses of each import in the files of the package in the order of files. A
// package is a folder with a Go file that is not a test file, a file named
// with . or _, which the go tool ignores, nor a file with //go:build ignore;
// such files of all systems count, whatever their other build constraints.
// Folders testdata and vendor, folders named with . or _ and folders with a
// go.mod of their own are skipped with all they hold.
func moduleImports(root, module string) (map[string]map[string][]use, error) {
	return readImports(root, root, module, false)
}

// testImports returns, as moduleImports does, the packages under dir, a path
// from root, with their imports, reading the test files as well: a package is
// a folder with a Go file, test file or not.
func testImports(root, module, dir string) (map[string]map[string][]use, error) {
	return readImports(root, filepath.Join(root, filepath.FromSlash(dir)), module, true)
}

// readImports returns the packages under the folder top of the module under
// root with their imports and their uses, both as paths from root, as
// moduleImports describes; with tests, test files count as well.
func readImports(root, top, module string, tests bool) (map[string]map[string][]use, error) {
	fset := token.NewFileSet()
	pkgs := map[string]map[string][]use{}
	err := filepath.WalkDir(top, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p == top {
				return nil
			}
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || !tests && strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		ignored, err := ignoredGoFile(f)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if ignored {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		file := filepath.ToSlash(rel)
		pkg := path.Dir(file)
		if pkgs[pkg] == nil {
			pkgs[pkg] = map[string][]use{}
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.IMPORT {
				continue
			}
			for _, s := range gen.Specs {
				spec := s.(*ast.ImportSpec)
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return fmt.Errorf("%s: %w", p, err)
				}
				rest, ok := strings.CutPrefix(imp, module+"/")
				if !ok {
					continue
				}
				doc := spec.Doc.Text()
				if !gen.Lparen.IsValid() {
					// The comment before an import with no parentheses
					// belongs to the declaration.
					doc = gen.Doc.Text()
				}
				comment := collapse(doc + " " + spec.Comment.Text())
				pkgs[pkg][rest] = append(pkgs[pkg][rest], use{file, comment})
			}
		}
		return nil
	})
	return pkgs, err
}

// ignoredGoFile reports the standalone ignore constraint used by source
// generators: other constraints count on every platform in these checks.
func ignoredGoFile(f *ast.File) (bool, error) {
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
				return false, err
			}
			if tag, ok := expr.(*constraint.TagExpr); ok && tag.Tag == "ignore" {
				return true, nil
			}
		}
	}
	return false, nil
}

// collapse returns s with each run of spaces and line breaks made one space
// and none at the ends.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// check returns what breaks the table of layers in the packages of pkgs
// with their imports: a package with no layer; a use of an import of a
// higher layer that no exception lets through, of an import of a test helper
// and of an import an exception lets through with no comment that holds the
// reason, each with the file of the use; a package in the table twice, of
// which the first layer counts; a package of the table that is a test
// helper; a package of the table or a test helper that is not in pkgs; an
// exception with no reason; an exception that lets no import through.
func check(pkgs map[string]map[string][]use, layers []layer, helpers []string, exceptions []exception) []string {
	isHelper := func(pkg string) bool {
		return slices.ContainsFunc(helpers, func(h string) bool {
			return pkg == h || strings.HasPrefix(pkg, h+"/")
		})
	}
	level := map[string]int{}
	for i, l := range layers {
		for _, p := range l.packages {
			if _, ok := level[p]; !ok {
				level[p] = i
			}
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
		for _, imp := range slices.Sorted(maps.Keys(pkgs[p])) {
			if isHelper(imp) {
				for _, u := range pkgs[p][imp] {
					out = append(out, fmt.Sprintf("%s imports test helper %s: only tests may import it", u.file, imp))
				}
				continue
			}
			to, ok := level[imp]
			if !ok || to >= from {
				// A package with no layer is reported once, as itself.
				continue
			}
			if i := slices.IndexFunc(exceptions, func(e exception) bool { return e.from == p && e.to == imp }); i >= 0 {
				used[i] = true
				reason := collapse(exceptions[i].reason)
				if reason == "" {
					// An exception with no reason is reported once, as itself.
					continue
				}
				for _, u := range pkgs[p][imp] {
					if !strings.Contains(u.comment, reason) {
						out = append(out, fmt.Sprintf("%s imports %s without the reason of its exception: write %q in the comment on the import or fix the reason in %s",
							u.file, imp, reason, tableFile))
					}
				}
				continue
			}
			for _, u := range pkgs[p][imp] {
				out = append(out, fmt.Sprintf("%s imports %s: layer %d (%s) must not import layer %d (%s)",
					u.file, imp, from+1, layers[from].name, to+1, layers[to].name))
			}
		}
	}
	seen := map[string]bool{}
	for _, l := range layers {
		for _, p := range l.packages {
			if seen[p] {
				out = append(out, fmt.Sprintf("%s is in the layer table twice: keep it in one layer in %s", p, tableFile))
				continue
			}
			seen[p] = true
			if isHelper(p) {
				out = append(out, fmt.Sprintf("%s is in the layer table and is a test helper: remove it from one of them in %s", p, tableFile))
			}
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
	for _, e := range exceptions {
		if collapse(e.reason) == "" {
			out = append(out, fmt.Sprintf("exception %s imports %s has no reason: write it in %s", e.from, e.to, tableFile))
		}
	}
	for i, e := range exceptions {
		if !used[i] {
			out = append(out, fmt.Sprintf("exception %s imports %s lets no import through: remove it from %s", e.from, e.to, tableFile))
		}
	}
	return out
}

// checkAcceptance returns what breaks the allowances in the packages of the
// acceptance tests of pkgs with their imports: a use of an import of
// internal/ no allowance lets through, with the file of the use; an
// allowance with no reason; an allowance that lets no import through.
func checkAcceptance(pkgs map[string]map[string][]use, allowed []allowance) []string {
	used := make([]bool, len(allowed))
	var out []string
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		for _, imp := range slices.Sorted(maps.Keys(pkgs[p])) {
			if !strings.HasPrefix(imp, "internal/") {
				continue
			}
			if i := slices.IndexFunc(allowed, func(a allowance) bool { return a.pkg == imp }); i >= 0 {
				used[i] = true
				continue
			}
			for _, u := range pkgs[p][imp] {
				out = append(out, fmt.Sprintf("%s imports %s: an acceptance test imports of internal/ only the packages allowed in %s", u.file, imp, tableFile))
			}
		}
	}
	for _, a := range allowed {
		if collapse(a.reason) == "" {
			out = append(out, fmt.Sprintf("allowed import %s has no reason: write it in %s", a.pkg, tableFile))
		}
	}
	for i, a := range allowed {
		if !used[i] {
			out = append(out, fmt.Sprintf("allowed import %s lets no import through: remove it from %s", a.pkg, tableFile))
		}
	}
	return out
}
