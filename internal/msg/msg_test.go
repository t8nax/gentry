package msg

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// declaredKeys returns the values of all Key constants declared in keys.go.
func declaredKeys(t *testing.T) []Key {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var keys []Key
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if ident, ok := vs.Type.(*ast.Ident); !ok || ident.Name != "Key" {
				t.Errorf("constant %s in keys.go is not of type Key", vs.Names[0].Name)
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("key %s is not a string literal", vs.Names[0].Name)
					continue
				}
				s, _ := strconv.Unquote(lit.Value)
				keys = append(keys, Key(s))
			}
		}
	}
	return keys
}

func TestCatalogsComplete(t *testing.T) {
	keys := declaredKeys(t)
	if len(keys) == 0 {
		t.Fatal("no keys found in keys.go")
	}
	declared := map[Key]bool{}
	for _, k := range keys {
		if declared[k] {
			t.Errorf("key %q is declared twice", k)
		}
		declared[k] = true
	}

	langs := make([]string, 0, len(catalogs))
	for lang := range catalogs {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		c := catalogs[lang]
		for _, k := range keys {
			_, isText := c.texts[k]
			_, isPlural := c.plurals[k]
			if isText == isPlural {
				t.Errorf("%s: key %q must be either a text or a plural, exactly once", lang, k)
			}
		}
		for k := range c.texts {
			if !declared[k] {
				t.Errorf("%s: text for undeclared key %q", lang, k)
			}
		}
		for k, forms := range c.plurals {
			if !declared[k] {
				t.Errorf("%s: plural for undeclared key %q", lang, k)
			}
			if len(forms) != c.forms {
				t.Errorf("%s: plural %q has %d forms, want %d", lang, k, len(forms), c.forms)
			}
		}
	}
}

func TestRussianForm(t *testing.T) {
	tests := map[int]int{
		0: 2, 1: 0, 2: 1, 4: 1, 5: 2, 11: 2, 12: 2, 14: 2,
		21: 0, 22: 1, 25: 2, 101: 0, 111: 2, 112: 2, 122: 1, -1: 0,
	}
	for n, want := range tests {
		if got := russianForm(n); got != want {
			t.Errorf("russianForm(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestCatalogFormatting(t *testing.T) {
	c := &catalog{
		texts:   map[Key]string{"plain": "без аргументов", "args": "команда %s"},
		plurals: map[Key][]string{"tasks": {"%d задача в %s", "%d задачи в %s", "%d задач в %s"}},
		form:    russianForm,
		forms:   3,
	}
	tests := []struct{ got, want string }{
		{c.text("plain"), "без аргументов"},
		{c.text("args", "version"), "команда version"},
		{c.text("missing"), "missing"},
		{c.count("tasks", 1, "SHOP"), "1 задача в SHOP"},
		{c.count("tasks", 3, "SHOP"), "3 задачи в SHOP"},
		{c.count("tasks", 11, "SHOP"), "11 задач в SHOP"},
		{c.count("missing", 1), "missing"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
