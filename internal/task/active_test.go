package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// activeShop is a store with the shop and its worktree, and a process
// repository with the flow and the library of the flow testdata applied;
// the library also has translator, which no stage names.
type activeShop struct {
	t        *testing.T
	st       *state.Store
	r        *process.Repo
	worktree string
}

func newActiveShop(t *testing.T) activeShop {
	t.Helper()
	root, err := paths.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := activeShop{t: t, st: st, worktree: filepath.Join(root, "shop-fix")}
	if err := st.Write(func(tx *state.Tx) error {
		if err := tx.AddProject(state.Project{ID: "shop", Prefix: "SHOP", Knowledge: filepath.Join(root, "knowledge")}); err != nil {
			return err
		}
		return tx.AddWorktree(state.Worktree{Path: s.worktree, Project: "shop"})
	}); err != nil {
		t.Fatal(err)
	}
	if s.r, err = process.Open(filepath.Join(root, "process")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.r.Close)
	testdata := filepath.Join("..", "flow", "testdata", "process")
	for k, dir := range map[process.Kind]string{process.Library: "agents", process.FlowOf("shop"): filepath.Join("shop", "flow")} {
		if err := os.CopyFS(s.r.KindDir(k), os.DirFS(filepath.Join(testdata, dir))); err != nil {
			t.Fatal(err)
		}
	}
	s.write(process.Library, map[string]string{
		"translator.yaml": "purpose: перевод текстов витрины\ncapabilities: [read]\n",
		"translator.md":   "Перевести тексты витрины.\n",
	})
	s.apply(process.FlowOf("shop"))
	return s
}

// write writes files of kind k by path inside its directory.
func (s activeShop) write(k process.Kind, files map[string]string) {
	s.t.Helper()
	for rel, text := range files {
		if err := os.WriteFile(filepath.Join(s.r.KindDir(k), filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
}

// apply applies the working files of the library, unless they are applied
// already, and then those of kind k, and returns the commit of k.
func (s activeShop) apply(k process.Kind) process.Applied {
	s.t.Helper()
	var applied process.Applied
	kinds := []process.Kind{process.Library, k}
	if k.IsLibrary() {
		kinds = kinds[1:]
	}
	for _, kind := range kinds {
		files, err := s.r.Working(kind)
		if err != nil {
			s.t.Fatal(err)
		}
		active, err := s.r.Active(kind)
		if err != nil {
			s.t.Fatal(err)
		}
		if kind != k && files.Equal(active) {
			continue
		}
		if applied, err = s.r.Apply(kind, files, "Apply", "test apply"); err != nil {
			s.t.Fatal(err)
		}
	}
	return applied
}

// take takes a task of the feature scenario in the worktree of the shop by
// the active flow.
func (s activeShop) take() state.Task {
	s.t.Helper()
	res, _, err := flow.Active(s.r, "shop")
	if err != nil || res.Flow == nil {
		s.t.Fatalf("active flow: %v, %+v", err, res)
	}
	applied, _, err := s.r.Applied(process.FlowOf("shop"))
	if err != nil {
		s.t.Fatal(err)
	}
	library, _, err := s.r.Applied(process.Library)
	if err != nil {
		s.t.Fatal(err)
	}
	sc, _ := res.Flow.Scenario("feature")
	t, err := Take(s.st, TakeRequest{
		Project: "shop", Worktree: s.worktree, Scenario: sc, Title: "Частичный возврат", Statement: "Вернуть часть заказа.",
		Source: state.SourceOperator, Snapshot: res.Snapshot, Flow: applied, Library: library,
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return t
}

// active returns the last change of the active flow for t, viewed with its
// snapshot as Views reads it.
func (s activeShop) active(t state.Task) *process.Applied {
	s.t.Helper()
	views, err := Views(s.st, []state.Task{t})
	if err != nil {
		s.t.Fatal(err)
	}
	a, err := ActiveFlow(s.r, views[0])
	if err != nil {
		s.t.Fatal(err)
	}
	return a
}

func TestActiveFlow(t *testing.T) {
	flowKind := process.FlowOf("shop")
	tests := []struct {
		name    string
		change  func(s activeShop) *process.Applied // the change after the take and the last change it makes; nil if the flow does not change
		changed bool
	}{
		{"nothing applied", func(s activeShop) *process.Applied { return nil }, false},
		{"a stage", func(s activeShop) *process.Applied {
			s.write(flowKind, map[string]string{"stages/branch.md": "Создать ветку задачи от main и отправить её на сервер.\n"})
			a := s.apply(flowKind)
			return &a
		}, true},
		{"a subagent of a stage", func(s activeShop) *process.Applied {
			s.write(process.Library, map[string]string{"reviewer.md": "Проверить изменения задачи и логи.\n"})
			a := s.apply(process.Library)
			return &a
		}, true},
		{"a subagent no stage names", func(s activeShop) *process.Applied {
			s.write(process.Library, map[string]string{"translator.md": "Перевести тексты витрины и писем.\n"})
			s.apply(process.Library)
			return nil
		}, false},
		{"a subagent no stage names after the flow", func(s activeShop) *process.Applied {
			s.write(flowKind, map[string]string{"stages/branch.md": "Создать ветку.\n"})
			s.apply(flowKind)
			s.write(process.Library, map[string]string{"translator.md": "Перевести тексты витрины и писем.\n"})
			a := s.apply(process.Library)
			return &a
		}, true},
		{"line ends alone", func(s activeShop) *process.Applied {
			s.write(flowKind, map[string]string{"stages/branch.md": "Создать ветку задачи от main.\r\n"})
			s.apply(flowKind)
			return nil
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newActiveShop(t)
			task := s.take()
			want := tt.change(s)
			got := s.active(task)
			switch {
			case !tt.changed && got != nil:
				t.Errorf("changed: %+v", *got)
			case tt.changed && (got == nil || got.Commit != want.Commit || !got.Time.Equal(want.Time)):
				t.Errorf("%+v, want %+v", got, *want)
			}
			task.State = state.TaskCancelled
			if got := s.active(task); got != nil {
				t.Errorf("a cancelled task: %+v", *got)
			}
		})
	}
}

func TestFlowIn(t *testing.T) {
	t.Parallel()
	s := newActiveShop(t)
	worktrees, err := s.st.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(s.worktree, "src")
	if _, ok, err := FlowIn(s.st, s.r, worktrees, src, "shop"); ok || err != nil {
		t.Errorf("a free worktree: %v, %v", ok, err)
	}
	task := s.take()
	s.write(process.FlowOf("shop"), map[string]string{"stages/plan-feature.md": "Написать план.\n"})
	applied := s.apply(process.FlowOf("shop"))
	tf, ok, err := FlowIn(s.st, s.r, worktrees, src, "shop")
	if !ok || err != nil || tf.ID != task.ID || tf.ActiveFlow == nil || tf.ActiveFlow.Commit != applied.Commit {
		t.Fatalf("the worktree of the task: %+v, %v, %v", tf, ok, err)
	}
	if st, _ := tf.Flow.Stage("plan-feature"); st.Instruction == "Написать план.\n" {
		t.Errorf("the flow of the task is the active one: %q", st.Instruction)
	}
	for _, tt := range []struct{ name, dir, project string }{
		{"another project", src, "cart"},
		{"outside the pool", filepath.Dir(s.worktree), "shop"},
	} {
		if _, ok, err := FlowIn(s.st, s.r, worktrees, tt.dir, tt.project); ok || err != nil {
			t.Errorf("%s: %v, %v", tt.name, ok, err)
		}
	}
	if _, ok, err := FlowIn(nil, s.r, worktrees, src, "shop"); ok || err != nil {
		t.Errorf("no store: %v, %v", ok, err)
	}
}

// TestActiveFlowAt checks that a task is shown without the change of the
// active flow while the process repository cannot be read at once: another
// command holds it, or there is none, which is not created.
func TestActiveFlowAt(t *testing.T) {
	t.Parallel()
	s := newActiveShop(t)
	task := s.take()
	s.write(process.FlowOf("shop"), map[string]string{"stages/merge.md": "Влить ветку после ревью.\n"})
	applied := s.apply(process.FlowOf("shop"))
	views, err := Views(s.st, []state.Task{task})
	if err != nil {
		t.Fatal(err)
	}
	// s holds the repository as a command of Gentry does.
	start := time.Now()
	if a := ActiveFlowAt(s.r.Dir, views[0]); a != nil {
		t.Errorf("held by another command: %+v", *a)
	}
	if waited := time.Since(start); waited > process.LockWait/2 {
		t.Errorf("waited %s for the repository", waited)
	}
	s.r.Close()
	if a := ActiveFlowAt(s.r.Dir, views[0]); a == nil || a.Commit != applied.Commit {
		t.Errorf("free: %+v, want %s", a, applied.Commit)
	}
	missing := filepath.Join(t.TempDir(), "process")
	if a := ActiveFlowAt(missing, views[0]); a != nil {
		t.Errorf("no repository: %+v", *a)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the repository is created: %v", err)
	}
}
