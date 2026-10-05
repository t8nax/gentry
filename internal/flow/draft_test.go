package flow

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/t8nax/gentry/internal/state"
)

// shopStore returns a state store with the shop connected, and the places of
// its flow in a process directory with the library of testdata and no draft.
func shopStore(t *testing.T) (*state.Store, Places) {
	t.Helper()
	st, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Write(func(tx *state.Tx) error {
		return tx.AddProject(state.Project{ID: "shop", Prefix: "SHOP", Knowledge: "/k/shop"})
	}); err != nil {
		t.Fatal(err)
	}
	p := shop(t, nil)
	if err := os.RemoveAll(p.Draft); err != nil {
		t.Fatal(err)
	}
	return st, p
}

// copyShop puts the flow of testdata into the draft of p.
func copyShop(t *testing.T, p Places) {
	t.Helper()
	if err := os.CopyFS(p.Draft+"-src", os.DirFS("testdata/process/shop/flow-draft")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.Draft); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.Draft+"-src", p.Draft); err != nil {
		t.Fatal(err)
	}
}

// files returns the paths of the files under dir, with forward slashes.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			rel += "/"
		}
		if rel != "./" {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func eventTypes(t *testing.T, st *state.Store) []string {
	t.Helper()
	events, err := st.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.Type+" "+string(e.Data))
	}
	return types
}

func TestDraftCycle(t *testing.T) {
	st, p := shopStore(t)

	// Without a flow, there is nothing to show, compare or apply.
	var noDraft *NoDraftError
	if _, err := Apply(st, p); !errors.As(err, &noDraft) {
		t.Errorf("apply without a draft: %v", err)
	}
	if _, err := DiffDraft(st, p); !errors.As(err, &noDraft) {
		t.Errorf("diff without a draft: %v", err)
	}
	if err := Discard(st, p); !errors.As(err, &noDraft) {
		t.Errorf("discard without a draft: %v", err)
	}

	// The first draft is empty: the directories of the flow only.
	res, err := Edit(st, p)
	if err != nil {
		t.Fatal(err)
	}
	if want := (EditResult{Dir: p.Draft, Created: true}); res != want {
		t.Errorf("edit: %+v, want %+v", res, want)
	}
	if got, want := files(t, p.Draft), []string{"agents/", "parts/", "scenarios/", "stages/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("empty draft: %v", got)
	}
	// Continuing does not lay it out again.
	if err := os.WriteFile(filepath.Join(p.Draft, "flow.yaml"), []byte("# правка\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := Edit(st, p); err != nil || res.Created || res.Base != 0 {
		t.Errorf("edit again: %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(p.Draft, "flow.yaml")); err != nil {
		t.Errorf("the edit was lost: %v", err)
	}

	// A draft with problems is not applied, and stays.
	var invalid *DraftInvalidError
	if _, err := Apply(st, p); !errors.As(err, &invalid) || invalid.Dir != p.Draft || invalid.Problems[0].Message != "Во флоу нет ни одного сценария." {
		t.Errorf("apply an empty draft: %v", err)
	}
	if _, err := os.Stat(p.Draft); err != nil {
		t.Errorf("a refused draft must stay: %v", err)
	}

	copyShop(t, p)
	diff, err := DiffDraft(st, p)
	if err != nil || diff.Version != 0 || len(diff.Changes) != 11 {
		t.Errorf("diff from nothing: %+v, %v", diff, err)
	}
	version, err := Apply(st, p)
	if err != nil || version != 1 {
		t.Fatalf("apply: %d, %v", version, err)
	}
	if _, err := os.Stat(p.Draft); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the draft must be deleted: %v", err)
	}
	if _, open, _ := st.FlowDraft("shop"); open {
		t.Error("the draft record must be deleted")
	}
	v, ok, err := Active(st, "shop")
	if err != nil || !ok || v.Number != 1 || len(v.Snapshot.Library) != 2 {
		t.Fatalf("active: %+v, %v, %v", v, ok, err)
	}

	// The next draft is the active version, without the library.
	if res, err := Edit(st, p); err != nil || !res.Created || res.Base != 1 {
		t.Fatalf("edit version 1: %+v, %v", res, err)
	}
	if got := files(t, p.Draft); len(got) != 20 || got[0] != "agents/" {
		t.Errorf("draft of version 1: %v", got)
	}
	var unchanged *UnchangedError
	if _, err := Apply(st, p); !errors.As(err, &unchanged) || unchanged.Version != 1 {
		t.Errorf("apply unchanged: %v", err)
	}
	if diff, err := DiffDraft(st, p); err != nil || diff.Version != 1 || len(diff.Changes) != 0 {
		t.Errorf("diff unchanged: %+v, %v", diff, err)
	}

	// A draft whose directory is gone is laid out again from its base.
	if err := os.RemoveAll(p.Draft); err != nil {
		t.Fatal(err)
	}
	if res, err := Edit(st, p); err != nil || res.Created || res.Base != 1 {
		t.Errorf("edit a lost draft: %+v, %v", res, err)
	}
	if got := files(t, p.Draft); len(got) != 20 {
		t.Errorf("lost draft laid out: %v", got)
	}

	if err := os.WriteFile(filepath.Join(p.Draft, "stages", "merge.md"), []byte("Влить ветку и удалить её.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if version, err := Apply(st, p); err != nil || version != 2 {
		t.Fatalf("apply version 2: %d, %v", version, err)
	}

	// Discard deletes the draft and keeps the flow.
	if _, err := Edit(st, p); err != nil {
		t.Fatal(err)
	}
	if err := Discard(st, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Draft); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("discarded draft: %v", err)
	}
	if v, _, _ := Active(st, "shop"); v.Number != 2 {
		t.Errorf("active after discard: %d", v.Number)
	}

	want := []string{
		`flow.draft_created {}`,
		`flow.applied {"version":1}`,
		`flow.draft_created {"base_version":1}`,
		`flow.applied {"version":2}`,
		`flow.draft_created {"base_version":2}`,
		`flow.draft_discarded {}`,
	}
	if got := eventTypes(t, st); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n%v\nwant\n%v", got, want)
	}
}

// TestDraftLeftover checks that a draft directory without its record, left
// when it could not be deleted, is replaced by the next edit.
func TestDraftLeftover(t *testing.T) {
	st, p := shopStore(t)
	copyShop(t, p)
	if res, err := Edit(st, p); err != nil || !res.Created {
		t.Fatalf("edit: %+v, %v", res, err)
	}
	if got, want := files(t, p.Draft), []string{"agents/", "parts/", "scenarios/", "stages/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("leftover not replaced: %v", got)
	}
	// No temporary directory is left beside the draft.
	entries, err := os.ReadDir(filepath.Dir(p.Draft))
	if err != nil || len(entries) != 1 {
		t.Errorf("beside the draft: %v, %v", entries, err)
	}
}

// TestDraftAsCurrentDir checks the draft directory as the current directory
// of a terminal, where the operator edits the flow: Windows does not let
// delete it, so it is emptied and filled again.
func TestDraftAsCurrentDir(t *testing.T) {
	st, p := shopStore(t)
	if _, err := Edit(st, p); err != nil {
		t.Fatal(err)
	}
	copyShop(t, p)
	t.Chdir(p.Draft)
	if _, err := Apply(st, p); err != nil {
		t.Fatal(err)
	}
	if res, err := Edit(st, p); err != nil || !res.Created {
		t.Fatalf("edit in the draft directory: %+v, %v", res, err)
	}
	if got := files(t, p.Draft); len(got) != 20 {
		t.Errorf("draft laid out: %v", got)
	}
	if err := Discard(st, p); err != nil {
		t.Fatal(err)
	}
	if _, open, _ := st.FlowDraft("shop"); open {
		t.Error("draft still open")
	}
	if got := files(t, filepath.Dir(p.Draft)); len(got) > 1 {
		t.Errorf("left beside: %v", got)
	}
}
