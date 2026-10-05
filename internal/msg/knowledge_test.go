package msg

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestKnowledgeComplete(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "knowledge.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var keys []KnowledgeKey
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := vs.Type.(*ast.Ident); ok && ident.Name == "KnowledgeKey" {
			for _, v := range vs.Values {
				s, _ := strconv.Unquote(v.(*ast.BasicLit).Value)
				keys = append(keys, KnowledgeKey(s))
			}
		}
		return true
	})
	if len(keys) == 0 {
		t.Fatal("no knowledge keys found")
	}
	if len(knowledge) != len(KnowledgeLanguages) {
		t.Errorf("catalogs for %d languages, want %v", len(knowledge), KnowledgeLanguages)
	}
	for _, lang := range KnowledgeLanguages {
		texts := knowledge[lang]
		for _, k := range keys {
			if texts[k] == "" {
				t.Errorf("%s: no text for %q", lang, k)
			}
		}
		if len(texts) != len(keys) {
			t.Errorf("%s: %d texts for %d keys", lang, len(texts), len(keys))
		}
	}
	if got := Knowledge("en", CommitProjectAdded, "shop"); got != "Connect project shop to Gentry" {
		t.Errorf("got %q", got)
	}
}
