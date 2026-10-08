package process

import "time"

// Status is the state of the process on this machine, as known without the
// network.
type Status struct {
	Remote    string     // the address of the remote repository; empty if none
	Synced    *time.Time // the last successful synchronization; nil if none
	Drafts    []Kind     // kinds with a draft
	Unsent    []Kind     // kinds changed by commits the remote repository does not have yet
	Conflicts []Kind     // kinds in conflict
}

// Status returns the state of the process. It neither changes the
// repository nor talks to the remote one.
func (r *Repo) Status() (Status, error) {
	var st Status
	var err error
	if st.Remote, err = r.RemoteURL(); err != nil {
		return Status{}, err
	}
	if t, ok, err := r.Synced(); err != nil {
		return Status{}, err
	} else if ok {
		st.Synced = &t
	}
	kinds, err := r.Kinds()
	if err != nil {
		return Status{}, err
	}
	for _, k := range kinds {
		draft, err := r.HasDraft(k)
		if err != nil {
			return Status{}, err
		}
		if draft {
			st.Drafts = append(st.Drafts, k)
		}
		if _, ok, err := r.Conflict(k); err != nil {
			return Status{}, err
		} else if ok {
			st.Conflicts = append(st.Conflicts, k)
		}
	}
	if st.Remote == "" {
		return st, nil
	}
	head, err := r.head()
	if err != nil {
		return Status{}, err
	}
	remote, err := r.rev(remoteRef)
	if err != nil || remote == head {
		return st, err
	}
	before, err := r.treeOf(remote)
	if err != nil {
		return Status{}, err
	}
	after, err := r.treeOf(head)
	if err != nil {
		return Status{}, err
	}
	st.Unsent = changedKinds(before, after)
	return st, nil
}
