package contract

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTimesMarshalInTimeFormat marshals every type of the contract with a
// time: the time is written in TimeFormat whatever its zone and precision.
// The milliseconds of the times end in a zero, which the standard marshaling
// of time.Time cuts; the first time is also not in UTC and has nanoseconds,
// as one taken from the clock, which it keeps.
func TestTimesMarshalInTimeFormat(t *testing.T) {
	const want = "2026-10-09T13:16:28.870Z"
	for _, at := range []time.Time{
		time.Date(2026, 10, 9, 16, 16, 28, 870012345, time.FixedZone("", 3*60*60)),
		time.Date(2026, 10, 9, 13, 16, 28, 870000000, time.UTC),
	} {
		marshalsInTimeFormat(t, at, want)
	}
}

// marshalsInTimeFormat checks that each type of the contract with a time
// writes at as want.
func marshalsInTimeFormat(t *testing.T, at time.Time, want string) {
	t.Helper()
	ended := &TaskEnded{Source: "operator", Time: at}
	tests := []struct {
		name  string
		value any
		paths []string
	}{
		{"Applied", Applied{Commit: "abc", Time: at}, []string{"time"}},
		{"ProcessStatusOutput", ProcessStatusOutput{Synced: &at}, []string{"synced"}},
		{"Task", Task{Taken: at, Flow: Applied{Time: at}, Ended: ended}, []string{"taken", "flow.time", "ended.time"}},
		{"TaskListItem", TaskListItem{Taken: at}, []string{"taken"}},
		{"TaskPass", TaskPass{Entered: at, Closed: &at}, []string{"entered", "closed"}},
		{"TaskStep", TaskStep{Added: at, Closed: &at}, []string{"added", "closed"}},
		{"TaskNote", TaskNote{Added: at}, []string{"added"}},
		{"TaskArtifact", TaskArtifact{Saved: at}, []string{"saved"}},
		{"OperatorDecision", OperatorDecision{Recorded: at}, []string{"recorded"}},
		{"TaskEnded", *ended, []string{"time"}},
		{"TaskAttempt", TaskAttempt{Taken: at, Ended: ended}, []string{"taken", "ended.time"}},
		{"Event", Event{Seq: 1, Time: at, Type: "task.taken", Data: EventData{}}, []string{"time"}},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.value)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		for _, path := range tt.paths {
			var v any = doc
			for _, name := range strings.Split(path, ".") {
				m, _ := v.(map[string]any)
				v = m[name]
			}
			if v != want {
				t.Errorf("%s of %s: %s is %v, want %s: %s", tt.name, at, path, v, want, b)
			}
		}
	}
}

// TestTimeFieldsHaveMarshaler checks by the sources of the package that every
// struct with a field of time declares MarshalJSON: without it the standard
// marshaling writes the time out of TimeFormat. It checks only that the
// method is there; that the method writes each time of its type in
// TimeFormat is for TestTimesMarshalInTimeFormat.
func TestTimeFieldsHaveMarshaler(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	withTime := map[string]bool{}
	marshalers := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && d.Name.Name == "MarshalJSON" {
					recv := d.Recv.List[0].Type
					if star, ok := recv.(*ast.StarExpr); ok {
						recv = star.X
					}
					if name, ok := recv.(*ast.Ident); ok {
						marshalers[name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						if isTime(field.Type) {
							withTime[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	if len(withTime) == 0 {
		t.Fatal("no struct with a time found")
	}
	for name := range withTime {
		if !marshalers[name] {
			t.Errorf("%s has a field of time but no MarshalJSON", name)
		}
	}
}

// isTime reports whether a field of type expr holds a time: time.Time or a
// pointer to it.
func isTime(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time" && sel.Sel.Name == "Time"
}
