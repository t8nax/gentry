package process

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/git"
)

// Checker checks the content of a kind: for a flow, library is the library of
// subagents of the same commit; for the library it is nil. It returns the
// messages of the problems found, none if the content is valid.
type Checker func(k Kind, files, library Files) ([]string, error)

// Sync is what a synchronization with the remote repository did.
type Sync struct {
	Remote      bool   // a remote repository is set; without it nothing else happens
	Unavailable bool   // the remote repository did not answer
	Output      string // what git printed when the remote repository did not answer
	Received    []Kind // changed by another machine and taken
	Sent        []Kind // changed by this machine and sent
	Conflicts   []Conflict
	Restored    []Restored
}

// Conflict is a kind changed on both machines in a way that cannot be
// brought together: the variant of the other machine became active, and
// what this machine changed stays as a draft over it.
type Conflict struct {
	Kind Kind
	// Paths inside the kind whose variants of the other machine are in its
	// conflict directory.
	Paths []string
}

// Restored is a kind changed around Gentry with problems: the last checked
// content became active again, and the change stays as a draft.
type Restored struct {
	Kind     Kind
	Problems []string
}

// Pull checks the changes made around Gentry, then takes the changes of the
// remote repository, if one is set, and brings them together with those of
// this machine. A remote repository that does not answer is noted in the
// result: the work goes on with what the machine has.
func (r *Repo) Pull(check Checker) (*Sync, error) {
	// A commit made around Gentry since the repository was opened counts.
	r.headKnown = false
	s := &Sync{}
	if err := r.verify(check, s); err != nil {
		return nil, err
	}
	url, err := r.RemoteURL()
	if err != nil || url == "" {
		return s, err
	}
	s.Remote = true
	if ok, err := r.fetch(s); err != nil || !ok {
		return s, err
	}
	if err := r.merge(check, s); err != nil {
		return nil, err
	}
	return s, r.verify(check, s)
}

// fetch takes the commits of the remote repository; ok is false, and s says
// so, if it did not answer.
func (r *Repo) fetch(s *Sync) (ok bool, err error) {
	_, err = git.Run(r.Dir, git.Opts{Network: true}, "fetch", "--quiet", "--no-tags", "--prune", remoteName)
	var ce *git.CommandError
	if errors.As(err, &ce) {
		s.Unavailable, s.Output = true, ce.Output
		return false, nil
	}
	return err == nil, err
}

// pushAttempts limits the pushes refused because another machine pushed
// between the fetch and the push.
const pushAttempts = 3

// Push sends the commits of this machine to the remote repository, unless
// Pull found it unavailable. A push refused because another machine has just
// pushed takes its commits first.
func (r *Repo) Push(check Checker, s *Sync) error {
	if !s.Remote || s.Unavailable {
		return nil
	}
	for attempt := 1; ; attempt++ {
		head, err := r.head()
		if err != nil {
			return err
		}
		remote, err := r.rev(remoteRef)
		if err != nil {
			return err
		}
		if head == remote {
			return r.markSynced()
		}
		before, err := r.treeOf(remote)
		if err != nil {
			return err
		}
		after, err := r.treeOf(head)
		if err != nil {
			return err
		}
		_, err = git.Run(r.Dir, git.Opts{Network: true}, "push", "--quiet", remoteName, "HEAD:"+branchRef)
		var ce *git.CommandError
		switch {
		case err == nil:
			if _, err := r.git("update-ref", remoteRef, head); err != nil {
				return err
			}
			s.Sent = mergeKinds(s.Sent, changedKinds(before, after))
			return r.markSynced()
		case !errors.As(err, &ce):
			return err
		case !rejected(ce.Output) || attempt == pushAttempts:
			s.Unavailable, s.Output = true, ce.Output
			return nil
		}
		if ok, err := r.fetch(s); err != nil || !ok {
			return err
		}
		if err := r.merge(check, s); err != nil {
			return err
		}
		if err := r.verify(check, s); err != nil {
			return err
		}
	}
}

// rejected reports whether git refused a push because the remote branch has
// commits this machine does not.
func rejected(output string) bool {
	return strings.Contains(output, "[rejected]") || strings.Contains(output, "non-fast-forward") ||
		strings.Contains(output, "fetch first")
}

// merge brings the commits of the remote branch together with those of this
// machine: a fast-forward if this machine has none of its own, otherwise its
// commits are carried over the remote ones one by one, kind by kind. A kind
// changed on both machines in the same file, or with problems once brought
// together, is in conflict: the remote variant becomes active, and the
// changes of this machine stay in its directory as a draft.
func (r *Repo) merge(check Checker, s *Sync) error {
	local, err := r.head()
	if err != nil {
		return err
	}
	remote, err := r.rev(remoteRef)
	if err != nil || remote == "" || remote == local {
		return err
	}
	base, err := r.mergeBase(local, remote)
	if err != nil || base == remote {
		return err
	}
	tLocal, err := r.treeOf(local)
	if err != nil {
		return err
	}
	tRemote, err := r.treeOf(remote)
	if err != nil {
		return err
	}
	if base == local {
		s.Received = mergeKinds(s.Received, changedKinds(tLocal, tRemote))
		return r.advance(local, remote, tLocal, tRemote, nil, s)
	}
	tBase, err := r.treeOf(base)
	if err != nil {
		return err
	}
	conflicts, err := r.conflicts(check, tBase, tLocal, tRemote)
	if err != nil {
		return err
	}
	head, tHead, err := r.replay(base, local, remote, tBase, tRemote, conflicts)
	if err != nil {
		return err
	}
	bases := map[Kind]tree{}
	for k := range conflicts {
		bases[k] = tBase
	}
	s.Received = mergeKinds(s.Received, changedKinds(tBase, tRemote))
	return r.advance(local, head, tLocal, tHead, bases, s)
}

// conflicts returns the kinds that cannot be brought together: changed on
// both machines in the same file, or with problems once brought together. A
// kind with changes of this machine is checked even if only this machine
// changed it: a flow may name a subagent the other machine removed from the
// library. The library goes first, as flows are checked with it.
func (r *Repo) conflicts(check Checker, tBase, tLocal, tRemote tree) (map[Kind]bool, error) {
	seen := map[Kind]bool{}
	for _, t := range []tree{tBase, tLocal, tRemote} {
		for _, k := range t.kinds() {
			seen[k] = true
		}
	}
	delete(seen, Library)
	flows := sortKinds(seen)
	out := map[Kind]bool{}

	// result returns the files of kind k brought together and whether they
	// have changes of this machine; ok is false if both changed one file.
	result := func(k Kind) (files map[string]entry, local, ok bool) {
		b, l, rm := tBase.kind(k), tLocal.kind(k), tRemote.kind(k)
		ours, theirs := changedPaths(b, l), changedPaths(b, rm)
		switch {
		case len(ours) == 0:
			return rm, false, true
		case len(theirs) == 0:
			return l, true, true
		}
		merged := map[string]entry{}
		for p, e := range rm {
			merged[p] = e
		}
		for _, p := range ours {
			e, inLocal := l[p]
			if slices.Contains(theirs, p) && rm[p] != e {
				return nil, true, false
			}
			if inLocal {
				merged[p] = e
			} else {
				delete(merged, p)
			}
		}
		return merged, true, true
	}
	checkKind := func(k Kind, files map[string]entry, library Files) (bool, error) {
		texts, err := r.contents(files)
		if err != nil {
			return false, err
		}
		if k.IsLibrary() {
			library = nil
		}
		problems, err := check(k, texts, library)
		return len(problems) == 0, err
	}

	libFiles, libLocal, ok := result(Library)
	if ok && libLocal {
		valid, err := checkKind(Library, libFiles, nil)
		if err != nil {
			return nil, err
		}
		ok = valid
	}
	library, err := r.contents(libFiles)
	if err != nil {
		return nil, err
	}
	// The library of this machine must not break a flow it has not changed.
	if ok && libLocal {
		for _, k := range flows {
			files, local, fine := result(k)
			if !fine || local || len(files) == 0 {
				continue
			}
			valid, err := checkKind(k, files, library)
			if err != nil {
				return nil, err
			}
			if !valid {
				ok = false
				break
			}
		}
	}
	if !ok {
		out[Library] = true
		if library, err = r.contents(tRemote.kind(Library)); err != nil {
			return nil, err
		}
	}
	for _, k := range flows {
		files, local, fine := result(k)
		switch {
		case !fine:
			out[k] = true
		case local && len(files) > 0:
			valid, err := checkKind(k, files, library)
			if err != nil {
				return nil, err
			}
			if !valid {
				out[k] = true
			}
		}
	}
	return out, nil
}

// replay carries the commits of this machine since base over remote one by
// one, leaving out the kinds in conflict and the other files the remote
// changed. It returns the last commit and its files.
func (r *Repo) replay(base, local, remote string, tBase, tRemote tree, conflicts map[Kind]bool) (string, tree, error) {
	args := []string{"rev-list", "--reverse", "--first-parent", local}
	if base != "" {
		args = append(args, "^"+base)
	}
	out, err := r.git(args...)
	if err != nil {
		return "", nil, err
	}
	head, tHead := remote, tree{}
	for p, e := range tRemote {
		tHead[p] = e
	}
	for _, c := range strings.Fields(out) {
		parent, err := r.rev(c + "^")
		if err != nil {
			return "", nil, err
		}
		tc, err := r.treeOf(c)
		if err != nil {
			return "", nil, err
		}
		tp, err := r.treeOf(parent)
		if err != nil {
			return "", nil, err
		}
		changes := map[string]*entry{}
		for _, p := range changedPaths(tp, tc) {
			if k, _, ok := kindOf(p); ok && conflicts[k] {
				continue
			} else if !ok && tBase[p] != tRemote[p] {
				continue
			}
			e, inCommit := tc[p]
			cur, inHead := tHead[p]
			if inCommit == inHead && e == cur {
				continue
			}
			if inCommit {
				changes[p] = &e
			} else {
				changes[p] = nil
			}
		}
		if len(changes) == 0 {
			continue
		}
		meta, err := r.git("log", "-1", "--format=%an%x00%ae%x00%aI%x00%B", c)
		if err != nil {
			return "", nil, err
		}
		f := strings.SplitN(meta, "\x00", 4)
		if len(f) != 4 {
			continue
		}
		env := []string{"GIT_AUTHOR_NAME=" + f[0], "GIT_AUTHOR_EMAIL=" + f[1], "GIT_AUTHOR_DATE=" + f[2]}
		if head, err = r.commit(head, changes, f[3], env); err != nil {
			return "", nil, err
		}
		for p, e := range changes {
			if e == nil {
				delete(tHead, p)
			} else {
				tHead[p] = *e
			}
		}
	}
	return head, tHead, nil
}

// advance moves HEAD from commit old, whose files are from, to commit c,
// whose files are to, and brings the working directory along: a file the
// working directory has as in from takes its content in to; a file changed
// in the working directory stays as it is, a draft. bases gives, for a kind
// in conflict, the files its working content is to be taken over instead of
// from: the common ancestor, so that all changes of this machine stay.
// A file changed in the working directory and in to differently is disputed:
// the variant of to goes to the conflict directory of its kind.
func (r *Repo) advance(old, c string, from, to tree, bases map[Kind]tree, s *Sync) error {
	if err := r.moveHead(c, old); err != nil {
		return err
	}
	seen := map[Kind]bool{}
	for _, t := range []tree{from, to} {
		for _, k := range t.kinds() {
			seen[k] = true
		}
	}
	for k := range bases {
		seen[k] = true
	}
	for _, k := range sortKinds(seen) {
		f := from
		base, inConflict := bases[k]
		if inConflict {
			f = base
		}
		fk, err := r.contents(f.kind(k))
		if err != nil {
			return err
		}
		tk, err := r.contents(to.kind(k))
		if err != nil {
			return err
		}
		disputed, err := moveWorking(r.KindDir(k), fk, tk)
		if err != nil {
			return err
		}
		if len(disputed) == 0 && !inConflict {
			continue
		}
		// Without a disputed file the conflict is in the problems of the
		// files brought together: the operator compares what the other
		// machine changed.
		shown := disputed
		if len(shown) == 0 {
			shown = changedPaths(fk, tk)
		}
		if err := writeConflict(r.ConflictDir(k), tk, shown); err != nil {
			return err
		}
		s.Conflicts = append(s.Conflicts, Conflict{Kind: k, Paths: shown})
	}
	// Other files, such as process.yaml, follow the remote variant unless
	// changed in the working directory.
	ff, err := r.contents(others(from))
	if err != nil {
		return err
	}
	tf, err := r.contents(others(to))
	if err != nil {
		return err
	}
	_, err = moveWorking(r.Dir, ff, tf)
	return err
}

// others returns the files of t that belong to no kind.
func others(t tree) map[string]entry {
	out := map[string]entry{}
	for p, e := range t {
		if _, _, ok := kindOf(p); !ok {
			out[p] = e
		}
	}
	return out
}

// moveWorking brings the files in directory dir from content from to content
// to, except those changed in dir: these stay. It returns the paths changed
// in dir and in to differently.
func moveWorking(dir string, from, to Files) (disputed []string, err error) {
	for _, p := range changedPaths(from, to) {
		fv, inFrom := from[p]
		tv, inTo := to[p]
		full := filepath.Join(dir, filepath.FromSlash(p))
		wv, inWorking, err := readFile(full)
		if err != nil {
			return nil, err
		}
		switch {
		case inWorking == inFrom && wv == fv:
			if inTo {
				err = writeFile(full, tv)
			} else {
				err = os.Remove(full)
			}
			if err != nil {
				return nil, err
			}
		case inWorking == inTo && wv == tv:
		default:
			disputed = append(disputed, p)
		}
	}
	return disputed, nil
}

// readFile returns the normalized text of file p; ok is false if there is no
// such file. A directory in its place reads as a file of no known text.
func readFile(p string) (text string, ok bool, err error) {
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		return string(Normalize(b)), true, nil
	case os.IsNotExist(err):
		return "", false, nil
	}
	if fi, statErr := os.Stat(p); statErr == nil && fi.IsDir() {
		return "\x00", true, nil
	}
	return "", false, err
}

// writeConflict replaces the conflict directory dir with the variants in to
// of the files at paths; a file to has not is left out.
func writeConflict(dir string, to Files, paths []string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, p := range paths {
		if text, ok := to[p]; ok {
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(p)), text); err != nil {
				return err
			}
		}
	}
	return nil
}

// verify checks the changes made around Gentry since the last check: the
// kinds changed by a commit without the service line of Gentry. A kind with
// problems gets its content of the last check back by a commit of Gentry;
// the working files stay, so the change becomes a draft.
func (r *Repo) verify(check Checker, s *Sync) error {
	head, err := r.head()
	if err != nil {
		return err
	}
	checked, err := r.rev(checkedRef)
	if err != nil || checked == head {
		return err
	}
	if checked == "" {
		return r.setChecked(head)
	}
	out, err := r.git("log", "--format=%H%x1f%P%x1f%B%x1e", head, "^"+checked)
	if err != nil {
		return err
	}
	bypass := map[Kind]bool{}
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 3)
		if len(f) != 3 || byGentry(f[2]) {
			continue
		}
		parent, _, _ := strings.Cut(f[1], " ")
		tc, err := r.treeOf(f[0])
		if err != nil {
			return err
		}
		tp, err := r.treeOf(parent)
		if err != nil {
			return err
		}
		for _, k := range changedKinds(tp, tc) {
			bypass[k] = true
		}
	}
	if len(bypass) > 0 {
		if head, err = r.checkBypass(check, head, checked, bypass, s); err != nil {
			return err
		}
	}
	return r.setChecked(head)
}

// checkBypass checks the kinds changed around Gentry at commit head and
// restores those with problems to their content at commit checked. It
// returns the new HEAD.
func (r *Repo) checkBypass(check Checker, head, checked string, bypass map[Kind]bool, s *Sync) (string, error) {
	tHead, err := r.treeOf(head)
	if err != nil {
		return "", err
	}
	tChecked, err := r.treeOf(checked)
	if err != nil {
		return "", err
	}
	library, err := r.contents(tHead.kind(Library))
	if err != nil {
		return "", err
	}
	kinds := sortKinds(bypass)
	// The library goes first: flows are checked with it.
	if bypass[Library] {
		kinds = append([]Kind{Library}, kinds[:len(kinds)-1]...)
	}
	for _, k := range kinds {
		files, err := r.contents(tHead.kind(k))
		if err != nil {
			return "", err
		}
		lib := library
		if k.IsLibrary() {
			lib = nil
		}
		problems, err := check(k, files, lib)
		if err != nil {
			return "", err
		}
		if len(problems) == 0 {
			continue
		}
		changes := map[string]*entry{}
		good := tChecked.kind(k)
		for rel := range tHead.kind(k) {
			if _, ok := good[rel]; !ok {
				changes[k.root()+"/"+rel] = nil
			}
		}
		for rel, e := range good {
			changes[k.root()+"/"+rel] = &e
		}
		subject, action := "Restore the flow of "+k.Project, "restore flow "+k.Project
		if k.IsLibrary() {
			subject, action = "Restore the library of subagents", "restore library"
		}
		c, err := r.commit(head, changes, message(subject, action), nil)
		if err != nil {
			return "", err
		}
		if err := r.moveHead(c, head); err != nil {
			return "", err
		}
		head = c
		if k.IsLibrary() {
			if library, err = r.contents(good); err != nil {
				return "", err
			}
		}
		s.Restored = append(s.Restored, Restored{Kind: k, Problems: problems})
	}
	return head, nil
}

// mergeKinds returns the kinds of a and b, each once, in order.
func mergeKinds(a, b []Kind) []Kind {
	seen := map[Kind]bool{}
	for _, k := range a {
		seen[k] = true
	}
	for _, k := range b {
		seen[k] = true
	}
	return sortKinds(seen)
}
