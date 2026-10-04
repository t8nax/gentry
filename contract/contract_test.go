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
	if err := json.Unmarshal([]byte(`{"gentry":"0.1.0","contract":0,"state_schema":1,"future_field":1}`), &v); err != nil {
		t.Errorf("unknown fields must be accepted: %v", err)
	}
	err := json.Unmarshal([]byte(`{"contract":0,"state_schema":1}`), &v)
	if err == nil || !strings.Contains(err.Error(), "gentry") {
		t.Errorf("missing gentry must be rejected, got %v", err)
	}
	err = json.Unmarshal([]byte(`{"gentry":"0.1.0","contract":0}`), &v)
	if err == nil || !strings.Contains(err.Error(), "state_schema") {
		t.Errorf("missing state_schema must be rejected, got %v", err)
	}
}

func TestEventRoundTrip(t *testing.T) {
	line := `{"seq":42,"time":"2026-10-04T18:22:52.123Z","type":"task.taken","project":"shop","task":"SHOP-12","data":{}}`
	var e Event
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatal(err)
	}
	if e.Seq != 42 || e.Type != "task.taken" || e.Task == nil || *e.Task != "SHOP-12" || e.Time.Nanosecond() != 123000000 {
		t.Errorf("unexpected event: %+v", e)
	}
	e = Event{}
	if err := json.Unmarshal([]byte(`{"seq":1,"time":"2026-10-04T18:22:52.123Z","type":"settings.changed","data":{}}`), &e); err != nil {
		t.Errorf("event without project must be accepted: %v", err)
	}
	if e.Project != nil || e.Task != nil {
		t.Errorf("unexpected project or task: %+v", e)
	}
	if err := json.Unmarshal([]byte(`{"seq":1,"time":"2026-10-04T18:22:52.123Z","type":"task.taken"}`), &e); err == nil {
		t.Error("event without data must be rejected")
	}
}
