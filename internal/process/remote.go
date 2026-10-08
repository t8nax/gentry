package process

import (
	"errors"
	"strings"

	"github.com/t8nax/gentry/internal/git"
)

// Results of SetRemote.
const (
	RemoteSent      = "sent"      // the remote repository was empty and got the process of this machine
	RemoteReceived  = "received"  // this machine had no process and got the remote one
	RemoteMerged    = "merged"    // both had a process: they are brought together
	RemoteUnchanged = "unchanged" // both had the same process
)

// RemoteUnavailableError means the remote repository does not answer or
// refuses access.
type RemoteUnavailableError struct{ Remote, Output string }

func (e *RemoteUnavailableError) Error() string { return e.Remote + ": " + e.Output }

// RemoteInvalidError means the remote repository holds something else than a
// process, or a process of a newer format.
type RemoteInvalidError struct{ Remote, Reason string }

func (e *RemoteInvalidError) Error() string { return e.Remote + ": " + e.Reason }

// ErrNoRemote means no remote repository is set.
var ErrNoRemote = errors.New("no remote repository")

// SetRemote sets the address of the remote repository, or replaces it, and
// synchronizes with it. A remote repository that does not answer or holds
// something else is refused, and the address is not kept. An empty one gets
// the process of this machine; if this machine has none, it gets the remote
// one; if both have one, they are brought together: what only this machine
// has is added, and where they differ the remote variant is active and the
// variant of this machine stays as a draft.
func (r *Repo) SetRemote(url string, check Checker) (result string, s *Sync, err error) {
	r.headKnown = false
	out, err := git.Run(r.Dir, git.Opts{Network: true}, "ls-remote", url)
	var ce *git.CommandError
	if errors.As(err, &ce) {
		return "", nil, &RemoteUnavailableError{Remote: url, Output: ce.Output}
	}
	if err != nil {
		return "", nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if hash, ref, ok := strings.Cut(line, "\t"); ok {
			refs[ref] = hash
		}
	}
	main := refs[branchRef]
	if len(refs) > 0 && main == "" {
		return "", nil, &RemoteInvalidError{Remote: url, Reason: ReasonNotProcess}
	}
	if main != "" {
		_, err := git.Run(r.Dir, git.Opts{Network: true}, "fetch", "--quiet", "--no-tags", url, "+"+branchRef+":"+incomingRef)
		if errors.As(err, &ce) {
			return "", nil, &RemoteUnavailableError{Remote: url, Output: ce.Output}
		}
		if err != nil {
			return "", nil, err
		}
		defer r.git("update-ref", "-d", incomingRef)
		format, ok, err := r.formatAt(incomingRef)
		switch {
		case err != nil:
			return "", nil, err
		case !ok:
			return "", nil, &RemoteInvalidError{Remote: url, Reason: ReasonNotProcess}
		case format > Format:
			return "", nil, &RemoteInvalidError{Remote: url, Reason: ReasonNewerFormat}
		}
	}

	previous, err := r.RemoteURL()
	if err != nil {
		return "", nil, err
	}
	if previous == "" {
		err = r.setRemote("add", remoteName, url)
	} else {
		err = r.setRemote("set-url", remoteName, url)
	}
	if err != nil {
		return "", nil, err
	}
	// A push refused leaves the address as it was.
	restore := func() {
		if previous == "" {
			r.setRemote("remove", remoteName)
		} else {
			r.setRemote("set-url", remoteName, previous)
		}
	}

	s = &Sync{Remote: true}
	if err := r.verify(check, s); err != nil {
		return "", nil, err
	}
	if main == "" {
		if _, err := r.git("update-ref", "-d", remoteRef); err != nil {
			return "", nil, err
		}
		result = RemoteSent
	} else {
		if _, err := r.git("update-ref", remoteRef, main); err != nil {
			return "", nil, err
		}
		if result, err = r.join(check, main, s); err != nil {
			return "", nil, err
		}
	}
	if err := r.Push(check, s); err != nil {
		return "", nil, err
	}
	if s.Unavailable {
		restore()
		return "", nil, &RemoteUnavailableError{Remote: url, Output: s.Output}
	}
	return result, s, nil
}

// join brings this machine together with commit remote of the remote
// repository it is connected to, and returns the result.
func (r *Repo) join(check Checker, remote string, s *Sync) (string, error) {
	local, err := r.head()
	if err != nil {
		return "", err
	}
	base, err := r.mergeBase(local, remote)
	if err != nil {
		return "", err
	}
	if base != "" {
		// A machine connected again, or a process that came from the remote.
		if err := r.merge(check, s); err != nil {
			return "", err
		}
		if err := r.verify(check, s); err != nil {
			return "", err
		}
		head, err := r.head()
		if err != nil {
			return "", err
		}
		ahead := head != remote
		switch {
		case len(s.Conflicts) > 0 || (len(s.Received) > 0 && ahead):
			return RemoteMerged, nil
		case len(s.Received) > 0:
			return RemoteReceived, nil
		case ahead:
			return RemoteSent, nil
		}
		return RemoteUnchanged, nil
	}

	// Two processes started apart: each machine made its own repository.
	tLocal, err := r.treeOf(local)
	if err != nil {
		return "", err
	}
	tRemote, err := r.treeOf(remote)
	if err != nil {
		return "", err
	}
	if len(tLocal.kinds()) == 0 {
		s.Received = tRemote.kinds()
		if err := r.advance(local, remote, tLocal, tRemote, nil, s); err != nil {
			return "", err
		}
		return RemoteReceived, r.verify(check, s)
	}
	// What only this machine has is added over the remote process; a kind
	// both have becomes a draft over the remote variant where they differ.
	head, tHead := remote, tree{}
	for p, e := range tRemote {
		tHead[p] = e
	}
	added := map[string]*entry{}
	bases := map[Kind]tree{}
	for _, k := range tLocal.kinds() {
		theirs, ours := tRemote.kind(k), tLocal.kind(k)
		if len(theirs) == 0 {
			for rel, e := range ours {
				added[k.root()+"/"+rel] = &e
				tHead[k.root()+"/"+rel] = e
			}
			continue
		}
		if len(changedPaths(theirs, ours)) > 0 {
			bases[k] = tree{}
		}
	}
	if len(added) > 0 {
		if head, err = r.commit(remote, added, message("Add the process of another machine", "process remote"), nil); err != nil {
			return "", err
		}
	}
	if err := r.advance(local, head, tLocal, tHead, bases, s); err != nil {
		return "", err
	}
	return RemoteMerged, r.verify(check, s)
}
