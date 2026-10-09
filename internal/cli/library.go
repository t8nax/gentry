package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

func runLibraryDiff(args []string, env Env) int {
	f := newFlags("library diff")
	if code, done := f.parse(args, env); done {
		return code
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := pull(r)
	if bad == nil {
		bad = push(r, s)
	}
	if bad != nil {
		return fail(env, *bad)
	}
	synced := syncDone(env, r, s, false)
	res, err := flow.LibraryDiff(r)
	if err != nil {
		return fail(env, libraryFailure(err))
	}
	out := contract.LibraryDiffOutput{Dir: r.KindDir(process.Library), Applied: appliedJSON(res.Applied), Changes: []contract.LibraryChange{}, Sync: syncJSON(s, synced)}
	for _, c := range res.Changes {
		out.Changes = append(out.Changes, contract.LibraryChange{Id: c.ID, Change: contract.LibraryDiffOutputChangesElemChange(c.Change)})
	}
	return emit(env, out, libraryDiffText)
}

// libraryDiffText prints when the library was applied and the changes of its
// draft.
func libraryDiffText(p *page, out contract.LibraryDiffOutput) {
	fmt.Fprintln(p, msg.Text(msg.LibraryAppliedAt, appliedText(out.Applied)))
	p.WriteString("\n")
	var lines []string
	for _, c := range out.Changes {
		lines = append(lines, msg.Text(msg.FlowChange, msg.Text(msg.FlowObjAgent, c.Id), changeWord(flow.Change{Object: flow.ObjectAgent, Change: string(c.Change)})))
	}
	writeList(&p.Builder, msg.Text(msg.FlowChanges), lines)
	p.hints(hintOf(msg.HintLibraryApply))
}

func runLibraryApply(args []string, env Env) int {
	f := newFlags("library apply")
	if code, done := f.parse(args, env); done {
		return code
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	s, bad := pull(r)
	if bad != nil {
		return fail(env, *bad)
	}
	if code, refused := refuseOnConflict(env, r, s, process.Library); refused {
		return code
	}
	applied, err := flow.LibraryApply(r)
	if err != nil {
		syncDone(env, r, s, false)
		return fail(env, libraryFailure(err))
	}
	if bad := push(r, s); bad != nil {
		layoutRefused(env, r, s, slices.Collect(maps.Keys(libraryProjects(r)))...)
		return fail(env, *bad)
	}
	record(event{typ: flow.EventLibraryApplied, data: contract.LibraryAppliedData{Commit: applied.Commit}})
	synced := syncDone(env, r, s, true)
	layout, laidPool := layoutFree(r, libraryProjects(r))
	sent := s.Remote && !s.Unavailable
	out := contract.LibraryApplyOutput{Applied: *appliedJSON(&applied), Sent: sent, Sync: syncJSON(s, synced), Agents: layout.json(laidPool)}
	return emit(env, out, func(p *page, out contract.LibraryApplyOutput) {
		fmt.Fprintln(p, msg.Text(msg.LibraryApplied))
		fmt.Fprintln(p, msg.Text(msg.LibraryAppliedAt, localTime(out.Applied.Time)))
		writeLayout(p, out.Agents, false, "")
	})
}

func runLibraryDiscard(args []string, env Env) int {
	f := newFlags("library discard")
	if code, done := f.parse(args, env); done {
		return code
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	if err := flow.LibraryDiscard(r); err != nil {
		return fail(env, libraryFailure(err))
	}
	record(event{typ: flow.EventLibraryDraftDiscarded})
	return emit(env, contract.LibraryDiscardOutput{Dir: r.KindDir(process.Library)}, func(p *page, out contract.LibraryDiscardOutput) {
		fmt.Fprintln(p, msg.Text(msg.LibraryDiscarded))
	})
}

// libraryFailure turns an error of a library command into a failure.
func libraryFailure(err error) failure {
	var (
		noDraft *flow.NoLibraryDraftError
		invalid *flow.LibraryInvalidError
	)
	switch {
	case errors.As(err, &noDraft):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeLibraryDraftNotFound,
			message: msg.Text(msg.ErrLibraryDraftNotFound),
			more:    msg.Text(msg.LibraryDir, noDraft.Dir) + "\n",
			details: map[string]any{"dir": noDraft.Dir},
		}
	case errors.As(err, &invalid):
		var more strings.Builder
		fmt.Fprintln(&more, msg.Text(msg.LibraryDir, invalid.Dir))
		cps := []contract.FlowProblem{}
		if len(invalid.Problems) > 0 {
			more.WriteString("\n")
			cps = writeProblems(&more, invalid.Problems)
		}
		details := map[string]any{"dir": invalid.Dir, "problems": cps}
		message := msg.Text(msg.ErrLibraryDraftInvalid)
		if len(invalid.Flows) > 0 {
			message = msg.Text(msg.ErrLibraryBreaksFlows)
			var flows []map[string]any
			for _, f := range invalid.Flows {
				more.WriteString("\n")
				flows = append(flows, map[string]any{"project": f.Project, "problems": writeProblemsAs(&more, msg.Text(msg.LibraryFlowProblems, f.Project), f.Problems)})
			}
			details["flows"] = flows
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeLibraryDraftInvalid,
			message: message,
			more:    more.String(),
			details: details,
		}
	}
	return flowFailure(err, flow.Places{})
}
