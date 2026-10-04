package contract

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSchemasCompile(t *testing.T) {
	names, err := fs.Glob(Schemas, "schemas/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no schemas embedded")
	}
	c := jsonschema.NewCompiler()
	for _, name := range names {
		f, err := Schemas.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := c.AddResource(name, doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := c.Compile(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVersionOutputRequiredFields(t *testing.T) {
	var v VersionOutput
	if err := json.Unmarshal([]byte(`{"gentry":"0.1.0","contract":0,"future_field":1}`), &v); err != nil {
		t.Errorf("unknown fields must be accepted: %v", err)
	}
	err := json.Unmarshal([]byte(`{"contract":0}`), &v)
	if err == nil || !strings.Contains(err.Error(), "gentry") {
		t.Errorf("missing gentry must be rejected, got %v", err)
	}
}
