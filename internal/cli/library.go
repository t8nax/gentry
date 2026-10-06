package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
)

func runLibraryDiff(args []string, env Env) int {
	f := newFlags("library diff")
	asJSON := f.Bool("json")
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
	syncDone(env, r, s, false)
	res, err := flow.LibraryDiff(r)
	if err != nil {
		return fail(env, libraryFailure(err))
	}
	if *asJSON {
		out := contract.LibraryDiffOutput{Dir: r.KindDir(process.Library), Applied: appliedJSON(res.Applied), Changes: []contract.LibraryChange{}, Sync: syncJSON(s)}
		for _, c := range res.Changes {
			out.Changes = append(out.Changes, contract.LibraryChange{Id: c.ID, Change: contract.LibraryDiffOutputChangesElemChange(c.Change)})
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.LibraryAppliedAt, appliedText(res.Applied)))
	b.WriteString("\n")
	var lines []string
	for _, c := range res.Changes {
		lines = append(lines, msg.Text(msg.FlowChange, msg.Text(msg.FlowObjAgent, c.ID), changeWord(flow.Change{Object: flow.ObjectAgent, Change: c.Change})))
	}
	writeList(&b, msg.Text(msg.FlowChanges), lines)
	fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintLibraryApply))
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

func runLibraryApply(args []string, env Env) int {
	f := newFlags("library apply")
	asJSON := f.Bool("json")
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
		return fail(env, *bad)
	}
	record(event{typ: flow.EventLibraryApplied, data: contract.LibraryAppliedData{Commit: applied.Commit}})
	syncDone(env, r, s, true)
	sent := s.Remote && !s.Unavailable
	if *asJSON {
		out := contract.LibraryApplyOutput{Applied: *appliedJSON(&applied), Sent: sent, Sync: syncJSON(s)}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(msg.LibraryApplied))
	fmt.Fprintln(env.Stdout, msg.Text(msg.LibraryAppliedAt, localTime(applied.Time)))
	return contract.ExitOK
}

func runLibraryDiscard(args []string, env Env) int {
	f := newFlags("library discard")
	asJSON := f.Bool("json")
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
	if *asJSON {
		if err := writeJSON(env, contract.LibraryDiscardOutput{Dir: r.KindDir(process.Library)}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	fmt.Fprintln(env.Stdout, msg.Text(msg.LibraryDiscarded))
	return contract.ExitOK
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
		more.WriteString("\n")
		cps := writeProblems(&more, invalid.Problems)
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeLibraryDraftInvalid,
			message: msg.Text(msg.ErrLibraryDraftInvalid),
			more:    more.String(),
			details: map[string]any{"dir": invalid.Dir, "problems": cps},
		}
	}
	return flowFailure(err, flow.Places{})
}
