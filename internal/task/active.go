package task

import (
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// ActiveFlow returns the last change of the active flow of the project of
// the task of v if the active flow changed since the task was taken: the flow
// a task taken now would get differs from the flow of the task. That flow is
// the one task take keeps: the files of the flow of the project and the
// library subagents its stages name, so a change of a subagent no stage
// names does not count, nor does a change of line ends alone. The last change
// is the later of the commits that applied the flow of the project and the
// library. It is nil for a task closed or cancelled, which follows no flow,
// for a project with no active flow, and for a snapshot that cannot be
// decoded, as a later Gentry may have written it. The snapshot is the one
// Views read.
func ActiveFlow(r *process.Repo, v View) (*process.Applied, error) {
	if v.IsEnded() || v.Snapshot == nil {
		return nil, nil
	}
	res, ok, err := flow.Active(r, v.Project)
	if err != nil || !ok {
		return nil, err
	}
	if len(flow.Diff(*v.Snapshot, res.Snapshot)) == 0 {
		return nil, nil
	}
	last, ok, err := r.LastApplied(process.FlowOf(v.Project), process.Library)
	if err != nil || !ok {
		return nil, err
	}
	return &last, nil
}

// FlowView is the flow of a task as flow show gives it in the worktree of
// the task: the task with its flow, which is never nil, and the last change
// of the active flow, nil if it did not change.
type FlowView struct {
	View
	ActiveFlow *process.Applied
}

// FlowIn returns the flow of the task that holds the worktree of the pool
// that the canonical directory dir lies in, if the task is of project; ok is
// false if dir holds no task of project. worktrees are the pool; st is nil
// for a store not created yet, which has no tasks. A snapshot that cannot be
// read refuses, as stage show does.
func FlowIn(st *state.Store, r *process.Repo, worktrees []state.Worktree, dir, project string) (tf FlowView, ok bool, err error) {
	w, pooled := Pooled(worktrees, dir)
	if st == nil || !pooled || w.Project != project {
		return FlowView{}, false, nil
	}
	t, held, err := st.TaskIn(w.Path)
	if err != nil || !held {
		return FlowView{}, false, err
	}
	views, err := Views(st, []state.Task{t})
	if err != nil {
		return FlowView{}, false, err
	}
	if views[0].Flow == nil {
		return FlowView{}, false, errUnreadable
	}
	tf.View = views[0]
	if tf.ActiveFlow, err = ActiveFlow(r, tf.View); err != nil {
		return FlowView{}, false, err
	}
	return tf, true, nil
}

// ActiveFlowAt returns ActiveFlow of the task of v by the process repository
// in directory dir, if the repository can be read at once. A repository held
// by another command, without commits, unreadable or missing, or no git,
// gives nil, as if the active flow did not change: a command that shows the
// task neither waits for the process nor refuses for it. The repository is
// not created and not synchronized.
func ActiveFlowAt(dir string, v View) *process.Applied {
	if v.IsEnded() || v.Snapshot == nil {
		return nil
	}
	r, err := process.TryOpen(dir)
	if err != nil {
		return nil
	}
	defer r.Close()
	a, err := ActiveFlow(r, v)
	if err != nil {
		return nil
	}
	return a
}
