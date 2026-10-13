package task

import (
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/process"
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
