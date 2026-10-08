package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// Event types of the process repository.
const (
	eventRemoteSet = "process.remote_set"
	eventSynced    = "process.synced"
)

// openProcess opens the process repository, creating it if needed. The
// caller closes it.
func openProcess() (*process.Repo, *failure) {
	dir, err := home.Process()
	if err != nil {
		f := homeUnknown()
		return nil, &f
	}
	r, err := process.Open(dir)
	if err != nil {
		f := processFailure(err)
		return nil, &f
	}
	return r, nil
}

// pull checks the changes made around Gentry and takes those of the remote
// repository.
func pull(r *process.Repo) (*process.Sync, *failure) {
	s, err := r.Pull(flow.Check)
	if err != nil {
		f := processFailure(err)
		return nil, &f
	}
	return s, nil
}

// push sends the commits of this machine, unless the remote repository did
// not answer to pull.
func push(r *process.Repo, s *process.Sync) *failure {
	if err := r.Push(flow.Check, s); err != nil {
		f := processFailure(err)
		return &f
	}
	return nil
}

// syncDone finishes the synchronization inside a command: it records what it
// did as an event, lays out the free worktrees of the projects whose flow or
// library it changed and, in text, prints both on stderr before the output of
// the command. apply tells a command that commits, for which an unavailable
// remote repository defers the sending. It returns what the layout did, for
// the output in JSON.
func syncDone(env Env, r *process.Repo, s *process.Sync, apply bool) *contract.AgentsLayout {
	if s == nil {
		return nil
	}
	recordSync(s)
	if !env.json {
		writeSync(env.Stderr, r, s, syncText{apply: apply, received: true, conflicts: true})
	}
	o, p := layoutFree(r, syncProjects(r, s))
	if !env.json {
		writeFreeLayout(env.Stderr, o, p)
	}
	return o.json(p)
}

// syncText selects what writeSync prints.
type syncText struct {
	apply     bool // a command that commits: the sending is deferred
	received  bool // what was received
	conflicts bool // the conflicts
}

// writeSync prints what the synchronization did as blocks, each followed by
// an empty line.
func writeSync(w io.Writer, r *process.Repo, s *process.Sync, what syncText) {
	var b strings.Builder
	block := func() { b.WriteString("\n") }
	if s.Unavailable {
		if what.apply {
			fmt.Fprintln(&b, msg.Text(msg.SyncDeferred))
		} else {
			fmt.Fprintln(&b, msg.Text(msg.SyncSkipped))
		}
		block()
	}
	// A kind in conflict is told by its conflict.
	var received []process.Kind
	for _, k := range s.Received {
		if !slices.ContainsFunc(s.Conflicts, func(c process.Conflict) bool { return c.Kind == k }) {
			received = append(received, k)
		}
	}
	if what.received && len(received) > 0 {
		writeList(&b, msg.Text(msg.SyncReceived), kindNames(received))
		block()
	}
	for _, c := range s.Conflicts {
		if !what.conflicts {
			break
		}
		if c.Kind.IsLibrary() {
			fmt.Fprintln(&b, msg.Text(msg.SyncLibraryConflict))
		} else {
			fmt.Fprintln(&b, msg.Text(msg.SyncFlowConflict, c.Kind.Project))
		}
		fmt.Fprintln(&b, msg.Text(msg.SyncConflictDir, r.ConflictDir(c.Kind)))
		var objects []string
		for _, o := range flow.ObjectsOf(c.Kind, c.Paths) {
			objects = append(objects, changeObject(o))
		}
		if len(objects) > 0 {
			b.WriteString("\n")
			writeList(&b, msg.Text(msg.SyncBothChanged), objects)
		}
		fmt.Fprintf(&b, "\n%s\n", diffHint(c.Kind))
		block()
	}
	for _, rs := range s.Restored {
		if rs.Kind.IsLibrary() {
			fmt.Fprintln(&b, msg.Text(msg.SyncLibraryRestored))
		} else {
			fmt.Fprintln(&b, msg.Text(msg.SyncFlowRestored, rs.Kind.Project))
		}
		b.WriteString("\n")
		writeList(&b, msg.Text(msg.FlowProblems), rs.Problems)
		fmt.Fprintf(&b, "\n%s\n", diffHint(rs.Kind))
		block()
	}
	io.WriteString(w, b.String())
}

// takeConflict removes the conflict of kind k from s and returns it; ok is
// false if k is not in conflict. A command that applies k refuses then: the
// operator compares the variants first.
func takeConflict(s *process.Sync, k process.Kind) (c process.Conflict, ok bool) {
	for i, c := range s.Conflicts {
		if c.Kind == k {
			s.Conflicts = append(s.Conflicts[:i:i], s.Conflicts[i+1:]...)
			return c, true
		}
	}
	return process.Conflict{}, false
}

// conflictFailure is the refusal of apply when the synchronization before it
// found the kind changed on another machine: the draft stays, with the
// variants of the other machine to compare.
func conflictFailure(r *process.Repo, c process.Conflict) failure {
	dir := r.ConflictDir(c.Kind)
	var more strings.Builder
	fmt.Fprintln(&more, msg.Text(msg.SyncConflictDir, dir))
	var objects []string
	for _, o := range flow.ObjectsOf(c.Kind, c.Paths) {
		objects = append(objects, changeObject(o))
	}
	if len(objects) > 0 {
		more.WriteString("\n")
		writeList(&more, msg.Text(msg.SyncBothChanged), objects)
	}
	f := failure{
		exit:    contract.ExitError,
		code:    contract.CodeLibraryConflict,
		message: msg.Text(msg.SyncLibraryConflict),
		more:    more.String(),
		hint:    diffHint(c.Kind),
		details: map[string]any{"conflict_dir": dir},
	}
	if !c.Kind.IsLibrary() {
		f.code, f.message = contract.CodeFlowConflict, msg.Text(msg.SyncFlowConflict, c.Kind.Project)
		f.details["project"] = c.Kind.Project
	}
	return f
}

// refuseOnConflict refuses an apply of kind k if the synchronization before
// it found k in conflict: what was synchronized is sent and told, and the
// conflict is the refusal.
func refuseOnConflict(env Env, r *process.Repo, s *process.Sync, k process.Kind) (int, bool) {
	c, ok := takeConflict(s, k)
	if !ok {
		return 0, false
	}
	bad := push(r, s)
	s.Conflicts = append(s.Conflicts, c)
	recordSync(s)
	if bad != nil {
		return fail(env, *bad), true
	}
	s.Conflicts = s.Conflicts[:len(s.Conflicts)-1]
	if !env.json {
		rest := *s
		rest.Received = slices.DeleteFunc(slices.Clone(s.Received), func(x process.Kind) bool { return x == k })
		writeSync(env.Stderr, r, &rest, syncText{received: true, conflicts: true})
	}
	return fail(env, conflictFailure(r, c)), true
}

// diffHint is the hint to compare the draft of kind k with its active
// variant.
func diffHint(k process.Kind) string {
	if k.IsLibrary() {
		return msg.Text(msg.HintLibraryDiff)
	}
	return msg.Text(msg.HintFlowDiffProject, k.Project)
}

// kindName names kind k for the operator.
func kindName(k process.Kind) string {
	if k.IsLibrary() {
		return msg.Text(msg.KindLibrary)
	}
	return msg.Text(msg.KindFlow, k.Project)
}

func kindNames(kinds []process.Kind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = kindName(k)
	}
	return out
}

// items returns kinds as the contract names them, an empty array for none.
func items(kinds []process.Kind) []contract.ProcessItem {
	out := []contract.ProcessItem{}
	for _, k := range kinds {
		if k.IsLibrary() {
			out = append(out, contract.ProcessItem{Kind: contract.ProcessItemKindLibrary})
			continue
		}
		project := k.Project
		out = append(out, contract.ProcessItem{Kind: contract.ProcessItemKindFlow, Project: &project})
	}
	return out
}

// conflictKinds returns the kinds of conflicts.
func conflictKinds(cs []process.Conflict) []process.Kind {
	out := make([]process.Kind, len(cs))
	for i, c := range cs {
		out[i] = c.Kind
	}
	return out
}

// restoredKinds returns the kinds of restored.
func restoredKinds(rs []process.Restored) []process.Kind {
	out := make([]process.Kind, len(rs))
	for i, r := range rs {
		out[i] = r.Kind
	}
	return out
}

// syncJSON returns what the synchronization did as the contract has it; nil
// if there is nothing to tell.
func syncJSON(s *process.Sync, agents *contract.AgentsLayout) *contract.Sync {
	if s == nil {
		return nil
	}
	var out contract.Sync
	empty := true
	if agents != nil {
		out.Agents, empty = agents, false
	}
	if len(s.Received) > 0 {
		out.Received, empty = items(s.Received), false
	}
	if len(s.Sent) > 0 {
		out.Sent, empty = items(s.Sent), false
	}
	if len(s.Conflicts) > 0 {
		out.Conflicts, empty = items(conflictKinds(s.Conflicts)), false
	}
	if len(s.Restored) > 0 {
		out.Restored, empty = items(restoredKinds(s.Restored)), false
	}
	if s.Unavailable {
		yes := true
		out.Unavailable, empty = &yes, false
	}
	if empty {
		return nil
	}
	return &out
}

// syncedData returns the data of the event process.synced, or nil if the
// synchronization did nothing worth it.
func syncedData(s *process.Sync) *contract.ProcessSyncedData {
	if s == nil || len(s.Received)+len(s.Sent)+len(s.Conflicts)+len(s.Restored) == 0 {
		return nil
	}
	return &contract.ProcessSyncedData{
		Received: items(s.Received), Sent: items(s.Sent),
		Conflicts: items(conflictKinds(s.Conflicts)), Restored: items(restoredKinds(s.Restored)),
	}
}

// event is an event to record.
type event struct {
	typ, project string
	data         any
}

// record writes events to the state store after a change in the process
// repository. The change is made already, and git is its source of truth: a
// store that cannot be written loses the events, not the change.
func record(events ...event) {
	if len(events) == 0 {
		return
	}
	path, err := state.Path()
	if err != nil {
		return
	}
	st, err := state.Open(path)
	if err != nil {
		return
	}
	defer st.Close()
	st.Write(func(tx *state.Tx) error {
		for _, e := range events {
			if _, err := tx.AddEvent(e.typ, e.project, "", e.data); err != nil {
				return err
			}
		}
		return nil
	})
}

// recordSync records the event of a synchronization that did something.
func recordSync(s *process.Sync) {
	if data := syncedData(s); data != nil {
		record(event{typ: eventSynced, data: data})
	}
}

// localTime is a time as the operator reads it: local, to the minute.
func localTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

// appliedText is the time a kind was applied, or a dash if it never was.
func appliedText(a *process.Applied) string {
	if a == nil {
		return msg.Text(msg.ValueNone)
	}
	return localTime(a.Time)
}

// appliedJSON returns a as the contract has it; nil for nil.
func appliedJSON(a *process.Applied) *contract.Applied {
	if a == nil {
		return nil
	}
	return &contract.Applied{Commit: a.Commit, Time: a.Time}
}

// gitFailure turns an error of running git into a failure; ok is false for
// other errors.
func gitFailure(err error) (failure, bool) {
	var ce *git.CommandError
	switch {
	case errors.Is(err, git.ErrNotFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeGitNotFound,
			message: msg.Text(msg.ErrToolNotFound, "git"),
			hint:    msg.Text(msg.HintGitNotFound),
		}, true
	case errors.As(err, &ce):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeGitFailed,
			message: msg.Text(msg.ErrGitFailed, ce.Command, ce.Output),
			details: map[string]any{"command": ce.Command, "output": ce.Output},
		}, true
	}
	return failure{}, false
}

// processFailure turns an error of the process repository into a failure.
func processFailure(err error) failure {
	var (
		invalid     *process.InvalidError
		busy        *process.BusyError
		unavailable *process.RemoteUnavailableError
		remote      *process.RemoteInvalidError
		pathErr     *fs.PathError
	)
	switch {
	case errors.As(err, &invalid):
		k := msg.ErrProcessNotProcess
		if invalid.Reason == process.ReasonNewerFormat {
			k = msg.ErrProcessNewer
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProcessInvalid,
			message: msg.Text(k, invalid.Path),
			details: map[string]any{"path": invalid.Path, "reason": invalid.Reason},
		}
	case errors.As(err, &busy):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProcessBusy,
			message: msg.Text(msg.ErrProcessBusy),
			details: map[string]any{"path": busy.Path},
		}
	case errors.As(err, &unavailable):
		return remoteUnavailable(unavailable.Remote, unavailable.Output)
	case errors.As(err, &remote):
		k := msg.ErrRemoteNotProcess
		if remote.Reason == process.ReasonNewerFormat {
			k = msg.ErrRemoteNewer
		}
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeRemoteInvalid,
			message: msg.Text(k, remote.Remote),
			details: map[string]any{"remote": remote.Remote, "reason": remote.Reason},
		}
	case errors.Is(err, process.ErrNoRemote):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeProcessRemoteNotSet,
			message: msg.Text(msg.ErrProcessRemoteNotSet),
			hint:    msg.Text(msg.HintProcessRemote),
		}
	case errors.As(err, &pathErr):
		return ioError(pathErr.Path, pathErr.Err)
	}
	if f, ok := gitFailure(err); ok {
		return f
	}
	return stateFailure(err)
}

func remoteUnavailable(remote, output string) failure {
	return failure{
		exit:    contract.ExitError,
		code:    contract.CodeRemoteUnavailable,
		message: msg.Text(msg.ErrRemoteUnavailable, remote),
		hint:    msg.Text(msg.HintRemoteAccess, remote),
		details: map[string]any{"remote": remote, "output": output},
	}
}

func runProcessRemote(args []string, env Env) int {
	f := newFlags("process remote")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if len(f.args) == 0 || f.args[0] == "" {
		return fail(env, missingArgument("process remote", "remote", msg.Text(msg.ErrRemoteMissing), msg.Text(msg.HintCommandHelp, "process remote")))
	}
	url := f.args[0]
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	result, s, err := r.SetRemote(url, flow.Check)
	if err != nil {
		return fail(env, processFailure(err))
	}
	events := []event{{typ: eventRemoteSet, data: contract.ProcessRemoteSetData{Remote: url, Result: contract.ProcessRemoteSetDataResult(result)}}}
	if data := syncedData(s); data != nil {
		events = append(events, event{typ: eventSynced, data: data})
	}
	record(events...)
	drafts := conflictKinds(s.Conflicts)
	if *asJSON {
		out := contract.ProcessRemoteOutput{Remote: url, Result: contract.ProcessRemoteOutputResult(result), Drafts: items(drafts)}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	writeSync(env.Stderr, r, s, syncText{})
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.ProcessRemoteSet, url))
	k := map[string]msg.Key{
		process.RemoteSent: msg.ProcessResultSent, process.RemoteReceived: msg.ProcessResultReceived,
		process.RemoteMerged: msg.ProcessResultMerged, process.RemoteUnchanged: msg.ProcessResultUnchanged,
	}[result]
	fmt.Fprintln(&b, msg.Text(k))
	if len(drafts) > 0 {
		b.WriteString("\n")
		writeList(&b, msg.Text(msg.ProcessDrafts), kindNames(drafts))
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintProcessStatus))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

func runProcessSync(args []string, env Env) int {
	f := newFlags("process sync")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	if url, err := r.RemoteURL(); err != nil {
		return fail(env, processFailure(err))
	} else if url == "" {
		return fail(env, processFailure(process.ErrNoRemote))
	}
	s, bad := pull(r)
	if bad == nil {
		bad = push(r, s)
	}
	if bad != nil {
		return fail(env, *bad)
	}
	recordSync(s)
	layout, laidPool := layoutFree(r, syncProjects(r, s))
	if s.Unavailable {
		url, _ := r.RemoteURL()
		if !env.json {
			writeSync(env.Stderr, r, &process.Sync{Conflicts: s.Conflicts, Restored: s.Restored}, syncText{conflicts: true})
		}
		return fail(env, remoteUnavailable(url, s.Output))
	}
	if *asJSON {
		out := contract.ProcessSyncOutput{
			Received: items(s.Received), Sent: items(s.Sent),
			Conflicts: items(conflictKinds(s.Conflicts)), Restored: items(restoredKinds(s.Restored)),
			Agents: layout.json(laidPool),
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	writeSync(env.Stderr, r, s, syncText{conflicts: true})
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.ProcessSynced))
	if len(s.Received) > 0 {
		b.WriteString("\n")
		writeList(&b, msg.Text(msg.ProcessReceived), kindNames(s.Received))
	}
	if len(s.Sent) > 0 {
		b.WriteString("\n")
		writeList(&b, msg.Text(msg.ProcessSent), kindNames(s.Sent))
	}
	fmt.Fprint(env.Stdout, b.String())
	writeFreeLayout(env.Stdout, layout, laidPool)
	return contract.ExitOK
}

func runProcessStatus(args []string, env Env) int {
	f := newFlags("process status")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	st, err := r.Status()
	if err != nil {
		return fail(env, processFailure(err))
	}
	if *asJSON {
		out := contract.ProcessStatusOutput{Drafts: items(st.Drafts), Unsent: items(st.Unsent), Conflicts: items(st.Conflicts), Synced: st.Synced}
		if st.Remote != "" {
			out.Remote = &st.Remote
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	if st.Remote == "" {
		fmt.Fprintln(&b, msg.Text(msg.ProcessNoRemote))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.ProcessRemote, st.Remote))
		synced := msg.Text(msg.ValueNone)
		if st.Synced != nil {
			synced = localTime(*st.Synced)
		}
		fmt.Fprintln(&b, msg.Text(msg.ProcessSyncedAt, synced))
	}
	for _, l := range []struct {
		heading msg.Key
		kinds   []process.Kind
	}{{msg.ProcessDrafts, st.Drafts}, {msg.ProcessUnsent, st.Unsent}, {msg.ProcessConflicts, st.Conflicts}} {
		if len(l.kinds) > 0 {
			b.WriteString("\n")
			writeList(&b, msg.Text(l.heading), kindNames(l.kinds))
		}
	}
	switch {
	case st.Remote == "":
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintProcessRemote))
	case len(st.Unsent) > 0:
		fmt.Fprintf(&b, "\n%s\n", msg.Text(msg.HintProcessSync))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}
