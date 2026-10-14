package flow

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// processShop is the process repository of testdata with the flows of the
// shop and of the crm, both naming the shared subagent reviewer, applied.
type processShop struct {
	t    *testing.T
	r    *process.Repo
	shop Places
	crm  Places
}

// newProcessShop opens a process repository in a temporary directory with
// the shared subagents and the flow of the shop of testdata, and the same
// flow for the crm, applied by one commit.
func newProcessShop(t *testing.T) processShop {
	t.Helper()
	r, err := process.Open(filepath.Join(t.TempDir(), "process"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	s := processShop{t: t, r: r, shop: PlacesOf(r, "shop"), crm: PlacesOf(r, "crm")}
	for dir, from := range map[string]string{s.shop.Shared: "agents", s.shop.Dir: "shop/flow", s.crm.Dir: "shop/flow"} {
		if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "process", filepath.FromSlash(from)))); err != nil {
			t.Fatal(err)
		}
	}
	var kinds []process.KindFiles
	for _, k := range []process.Kind{process.Shared, process.FlowOf("shop"), process.FlowOf("crm")} {
		files, err := r.Working(k)
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, process.KindFiles{Kind: k, Files: files})
	}
	if _, err := r.ApplyKinds(kinds, "Apply", "test apply"); err != nil {
		t.Fatal(err)
	}
	return s
}

// write writes files by path inside dir; an empty text removes the file.
func (s processShop) write(dir string, files map[string]string) {
	s.t.Helper()
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if text == "" {
			if err := os.Remove(p); err != nil {
				s.t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
}

// drafts reports which of the flow of the shop, the flow of the crm and the
// shared subagents have a draft.
func (s processShop) drafts() []bool {
	s.t.Helper()
	var out []bool
	for _, k := range []process.Kind{process.FlowOf("shop"), process.FlowOf("crm"), process.Shared} {
		d, err := s.r.HasDraft(k)
		if err != nil {
			s.t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

// TestDraftShared checks that the draft of the flow of a project holds the
// changes of the shared subagents: with them alone the flow of the project
// is read with them, a stage of the draft may name a shared subagent there
// is only in the draft, and their problems are problems of the draft.
func TestDraftShared(t *testing.T) {
	t.Parallel()
	s := newProcessShop(t)
	if _, ok, err := Draft(s.r, s.shop); ok || err != nil {
		t.Fatalf("no draft: %v, %v", ok, err)
	}

	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить изменения и логи.\n"})
	res, ok, err := Draft(s.r, s.shop)
	if !ok || err != nil || res.Flow == nil {
		t.Fatalf("a draft of the shared subagents alone: %v, %v, %+v", ok, err, res)
	}
	if a, _ := res.Flow.Agent("reviewer"); !a.Shared || a.Instruction != "Проверить изменения и логи.\n" {
		t.Errorf("reviewer of the draft: %+v", a)
	}

	// A new shared subagent named by a stage of the draft.
	s.write(s.shop.Shared, map[string]string{
		"auditor.yaml": "purpose: аудит изменений\ncapabilities: [read]\n",
		"auditor.md":   "Проверить риски.\n",
	})
	s.write(s.shop.Dir, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: auditor\n"})
	if res, ok, err = Draft(s.r, s.shop); !ok || err != nil || res.Flow == nil {
		t.Fatalf("a stage of a new shared subagent: %v, %v, %+v", ok, err, res)
	}

	// A problem of a shared subagent a stage names is told once; one of a
	// subagent no stage names is a problem of the draft too, without a file.
	s.write(s.shop.Dir, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: reviewer\n"})
	s.write(s.shop.Shared, map[string]string{
		"reviewer.yaml": "capabilities: [read, search]\n",
		"linter.yaml":   "purpose: проверка стиля\n",
		"linter.md":     "Проверить стиль.\n",
	})
	res, _, err = Draft(s.r, s.shop)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjSharedAgent, "reviewer"), "purpose")
	linter := msg.Text(msg.ProblemMissingField, msg.Text(msg.FlowObjSharedAgent, "linter"), "capabilities")
	var got []string
	for _, p := range res.Problems {
		got = append(got, p.Message)
		if p.File != "" && strings.HasPrefix(p.Message, msg.Text(msg.FlowObjSharedAgent, "")) {
			t.Errorf("a problem of a shared subagent has a file: %+v", p)
		}
	}
	if res.Flow != nil || !slices.Equal(got, []string{reviewer, linter}) {
		t.Errorf("problems: %q, want %q, %q", got, reviewer, linter)
	}
}

// TestApplyShared checks that the draft of a project is applied with its
// changes of the shared subagents by one commit, which names the other
// projects whose flows name the shared subagents changed; and that changes
// of the shared subagents alone are applied from a project that names none.
func TestApplyShared(t *testing.T) {
	t.Parallel()
	s := newProcessShop(t)
	if _, err := Apply(s.r, s.shop); !errors.As(err, new(*NoDraftError)) {
		t.Errorf("no draft: %v", err)
	}

	s.write(s.shop.Dir, map[string]string{"stages/merge.md": "Влить ветку после ревью.\n"})
	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить изменения и логи.\n"})
	applied, err := Apply(s.r, s.shop)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied.Projects, []string{"crm"}) {
		t.Errorf("projects: %v", applied.Projects)
	}
	for _, k := range []process.Kind{process.FlowOf("shop"), process.Shared} {
		if a, _, err := s.r.Applied(k); err != nil || a.Commit != applied.Commit {
			t.Errorf("%v applied by %s, not %s: %v", k, a.Commit, applied.Commit, err)
		}
	}
	if d := s.drafts(); slices.Contains(d, true) {
		t.Errorf("drafts after apply: %v", d)
	}

	// The flow of the project alone concerns no other project.
	s.write(s.shop.Dir, map[string]string{"stages/merge.md": "Влить ветку.\n"})
	if applied, err = Apply(s.r, s.shop); err != nil || applied.Projects == nil || len(applied.Projects) != 0 {
		t.Errorf("the flow alone: %v, %v", applied.Projects, err)
	}

	// A new project without a flow applies the shared subagents too.
	blog := PlacesOf(s.r, "blog")
	if err := s.r.EnsureFlowDir("blog"); err != nil {
		t.Fatal(err)
	}
	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить изменения.\n"})
	if applied, err = Apply(s.r, blog); err != nil || !slices.Equal(applied.Projects, []string{"crm", "shop"}) {
		t.Errorf("from a project without a flow: %v, %v", applied.Projects, err)
	}
	if d := s.drafts(); slices.Contains(d, true) {
		t.Errorf("drafts after apply from blog: %v", d)
	}
}

// TestApplyBreaksOther checks that a change of the shared subagents that
// brings problems into the active flow of another project is refused with
// the problems of that flow, and nothing is applied.
func TestApplyBreaksOther(t *testing.T) {
	t.Parallel()
	s := newProcessShop(t)
	s.write(s.shop.Dir, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: orchestrator\n"})
	s.write(s.shop.Shared, map[string]string{"reviewer.yaml": "", "reviewer.md": ""})
	_, err := Apply(s.r, s.shop)
	var invalid *DraftInvalidError
	if !errors.As(err, &invalid) || len(invalid.Problems) != 0 || len(invalid.Flows) != 1 || invalid.Flows[0].Project != "crm" ||
		len(invalid.Flows[0].Problems) == 0 {
		t.Fatalf("refusal: %v, %+v", err, invalid)
	}
	if d := s.drafts(); !slices.Equal(d, []bool{true, false, true}) {
		t.Errorf("drafts after the refusal: %v", d)
	}
}

// TestDiscardShared checks that discarding the draft of a project brings
// back its flow and the shared subagents and leaves the draft of another
// project.
func TestDiscardShared(t *testing.T) {
	t.Parallel()
	s := newProcessShop(t)
	if err := Discard(s.r, s.shop); !errors.As(err, new(*NoDraftError)) {
		t.Errorf("no draft: %v", err)
	}
	s.write(s.shop.Dir, map[string]string{"stages/merge.md": "Влить ветку после ревью.\n"})
	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить изменения и логи.\n"})
	s.write(s.crm.Dir, map[string]string{"stages/merge.md": "Влить ветку в main.\n"})
	if err := Discard(s.r, s.shop); err != nil {
		t.Fatal(err)
	}
	if d := s.drafts(); !slices.Equal(d, []bool{false, true, false}) {
		t.Errorf("drafts after discard: %v", d)
	}
	// The shared subagents alone are a draft of the shop too.
	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить логи.\n"})
	if err := Discard(s.r, s.shop); err != nil {
		t.Fatal(err)
	}
	if d := s.drafts(); !slices.Equal(d, []bool{false, true, false}) {
		t.Errorf("drafts after discard of the shared subagents: %v", d)
	}
}

// TestDiffShared checks the diff of a draft of the shared subagents alone:
// the flow has no changes, the shared subagent names the projects it
// concerns.
func TestDiffShared(t *testing.T) {
	t.Parallel()
	s := newProcessShop(t)
	if _, err := DiffDraft(s.r, s.shop); !errors.As(err, new(*NoDraftError)) {
		t.Errorf("no draft: %v", err)
	}
	s.write(s.shop.Shared, map[string]string{"reviewer.md": "Проверить изменения и логи.\n"})
	res, err := DiffDraft(s.r, s.shop)
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft || len(res.Changes) != 0 || len(res.Problems) != 0 || res.Shared == nil || len(res.Shared.Changes) != 1 ||
		res.Shared.Changes[0].ID != "reviewer" || !slices.Equal(res.Shared.Changes[0].Projects, []string{"crm", "shop"}) {
		t.Errorf("diff: %+v, shared %+v", res, res.Shared)
	}
}
