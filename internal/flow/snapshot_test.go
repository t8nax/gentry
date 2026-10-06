package flow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotRoundTrip(t *testing.T) {
	res := readShop(t, shop(t, auditor))
	b, err := res.Snapshot.Encode()
	if err != nil {
		t.Fatal(err)
	}
	snap, err := DecodeSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	// The snapshot reads the same flow without the directories.
	again, err := ReadSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Problems) > 0 || !reflect.DeepEqual(again.Flow, res.Flow) || !reflect.DeepEqual(again.Snapshot, res.Snapshot) {
		t.Errorf("snapshot reads differently:\n%+v\nwant\n%+v\n%s", again.Flow, res.Flow, dump(again.Problems))
	}
	if len(Diff(res.Snapshot, snap)) != 0 {
		t.Errorf("a snapshot differs from itself: %+v", Diff(res.Snapshot, snap))
	}
	// A snapshot without fields is empty, not nil.
	if s, err := DecodeSnapshot([]byte(`{}`)); err != nil || s.Files == nil || s.Library == nil {
		t.Errorf("empty snapshot: %+v, %v", s, err)
	}
}

func TestDiff(t *testing.T) {
	old := readShop(t, shop(t, nil)).Snapshot
	p := shop(t, with(auditor, map[string]string{
		"flow.yaml":            "# общие правила\n",
		"stages/review.md":     "Проверить изменения по новому списку.\n",
		"parts/plan-format.md": "",
		// Line ends of Windows are no change.
		"stages/branch.md": "Создать ветку задачи от main.\r\n",
	}))
	if err := os.WriteFile(filepath.Join(p.Library, "reviewer.md"), []byte("Новая инструкция.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "stages", "plan-bug.yaml"), []byte("title: План бага\nexit: x\nexecutor: orchestrator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur := readShop(t, p).Snapshot
	got := Diff(old, cur)
	want := []Change{
		{Object: ObjectCommon, Change: Added},
		{Object: ObjectStage, ID: "plan-bug", Change: Modified},
		{Object: ObjectStage, ID: "review", Change: Modified},
		{Object: ObjectStage, ID: "security", Change: Added},
		{Object: ObjectPart, ID: "plan-format", Change: Removed},
		{Object: ObjectAgent, ID: "auditor", Change: Added},
		{Object: ObjectAgent, ID: "reviewer", Library: true, Change: Modified},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diff:\n%+v\nwant\n%+v", got, want)
	}
	// From no flow, everything is added.
	if got := Diff(Snapshot{}, old); len(got) != 11 || got[0].Change != Added {
		t.Errorf("from nothing: %+v", got)
	}
}
