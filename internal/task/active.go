package task

import (
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// ActiveFlow returns the last change of the active flow of the project of t
// if the active flow changed since t was taken: the flow a task taken now
// would get differs from the flow of t. That flow is the one task take keeps:
// the files of the flow of the project and the library subagents its stages
// name, so a change of a subagent no stage names does not count, nor does a
// change of line ends alone. The last change is the later of the commits
// that applied the flow of the project and the library. It is nil for a
// task closed or cancelled, which follows no flow, for a project with no
// active flow, and for a snapshot that cannot be read, as a later Gentry may
// have written it.
func ActiveFlow(st *state.Store, r *process.Repo, t state.Task) (*process.Applied, error) {
	if t.IsEnded() {
		return nil, nil
	}
	s, ok, err := st.Snapshot(t.Project, t.FlowCommit)
	if err != nil || !ok {
		return nil, err
	}
	own, err := flow.DecodeSnapshot([]byte(s.Content))
	if err != nil {
		return nil, nil
	}
	res, _, ok, err := flow.Active(r, t.Project)
	if err != nil || !ok {
		return nil, err
	}
	if len(flow.Diff(own, res.Snapshot)) == 0 {
		return nil, nil
	}
	last, ok, err := r.LastApplied(process.FlowOf(t.Project), process.Library)
	if err != nil || !ok {
		return nil, err
	}
	return &last, nil
}

// TaskFlow is the flow of a task as flow show gives it in the worktree of
// the task: the task with its flow, which is never nil, and the last change
// of the active flow, nil if it did not change.
type TaskFlow struct {
	View
	ActiveFlow *process.Applied
}

// FlowIn returns the flow of the task that holds the worktree of the pool
// that the canonical directory dir lies in, if the task is of project; ok is
// false if dir holds no task of project. worktrees are the pool; st is nil
// for a store not created yet, which has no tasks. A snapshot that cannot be
// read refuses, as stage show does.
func FlowIn(st *state.Store, r *process.Repo, worktrees []state.Worktree, dir, project string) (tf TaskFlow, ok bool, err error) {
	w, pooled := Pooled(worktrees, dir)
	if st == nil || !pooled || w.Project != project {
		return TaskFlow{}, false, nil
	}
	t, held, err := st.TaskIn(w.Path)
	if err != nil || !held {
		return TaskFlow{}, false, err
	}
	views, err := Views(st, []state.Task{t})
	if err != nil {
		return TaskFlow{}, false, err
	}
	if views[0].Flow == nil {
		return TaskFlow{}, false, errUnreadable
	}
	tf.View = views[0]
	if tf.ActiveFlow, err = ActiveFlow(st, r, t); err != nil {
		return TaskFlow{}, false, err
	}
	return tf, true, nil
}
