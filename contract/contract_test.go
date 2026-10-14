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
	events, err := fs.Glob(Schemas, "schemas/events/*.json")
	if err != nil || len(events) == 0 {
		t.Fatalf("no event schemas embedded: %v", err)
	}
	names = append(names, events...)
	if len(names) == 0 {
		t.Fatal("no schemas embedded")
	}
	// All schemas first: one may refer to another, as flow-invalid.json to
	// flow-problem.json.
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
		if err := c.AddResource(schemaURL(name), doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range names {
		if _, err := c.Compile(schemaURL(name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVersionOutputRequiredFields(t *testing.T) {
	var v VersionOutput
	if err := json.Unmarshal([]byte(`{"gentry":"0.1.0","contract":0,"state_schema":1,"knowledge_format":1,"future_field":1}`), &v); err != nil {
		t.Errorf("unknown fields must be accepted: %v", err)
	}
	err := json.Unmarshal([]byte(`{"contract":0,"state_schema":1,"knowledge_format":1}`), &v)
	if err == nil || !strings.Contains(err.Error(), "gentry") {
		t.Errorf("missing gentry must be rejected, got %v", err)
	}
	err = json.Unmarshal([]byte(`{"gentry":"0.1.0","contract":0,"knowledge_format":1}`), &v)
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

// schemaURL is the $id of the embedded schema name: schemas refer to each
// other by it.
func schemaURL(name string) string { return "https://github.com/t8nax/gentry/contract/" + name }

// TestEarlierEvents checks that the events earlier versions of Gentry wrote
// to the journal are read: flow.applied without projects, process.synced
// with the kind library, library.applied and library.draft_discarded.
func TestEarlierEvents(t *testing.T) {
	var applied FlowAppliedData
	if err := json.Unmarshal([]byte(`{"commit":"c0ffee"}`), &applied); err != nil || applied.Commit != "c0ffee" ||
		applied.Projects == nil || len(applied.Projects) != 0 {
		t.Errorf("flow.applied without projects: %+v, %v", applied, err)
	}
	var synced ProcessSyncedData
	if err := json.Unmarshal([]byte(`{"received":[{"kind":"library"}],"sent":[],"conflicts":[],"restored":[]}`), &synced); err != nil ||
		len(synced.Received) != 1 || synced.Received[0].Kind != ProcessItemKindLibrary {
		t.Errorf("process.synced with the kind library: %+v, %v", synced, err)
	}
	var library LibraryAppliedData
	if err := json.Unmarshal([]byte(`{"commit":"c0ffee"}`), &library); err != nil || library.Commit != "c0ffee" {
		t.Errorf("library.applied: %+v, %v", library, err)
	}
	var discarded LibraryDraftDiscardedData
	if err := json.Unmarshal([]byte(`{}`), &discarded); err != nil {
		t.Errorf("library.draft_discarded: %v", err)
	}
	// This version writes the projects of every flow.applied event.
	for _, d := range []FlowAppliedData{{Commit: "c0ffee"}, {Commit: "c0ffee", Projects: []string{"crm"}}} {
		b, err := json.Marshal(d)
		want := `{"commit":"c0ffee","projects":[]}`
		if len(d.Projects) > 0 {
			want = `{"commit":"c0ffee","projects":["crm"]}`
		}
		if err != nil || string(b) != want {
			t.Errorf("flow.applied written: %s, %v; want %s", b, err, want)
		}
	}
}
