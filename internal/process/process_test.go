package process

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/paths"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gentry-test-")
	if err != nil {
		panic(err)
	}
	if err := gittest.Isolate(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// check is a checker for tests: a file with BAD in it is a problem, as is a
// flow that names a subagent missing from the library by a line "uses: id".
func check(k Kind, files, library Files) ([]string, error) {
	var problems []string
	for _, p := range slices.Sorted(mapKeys(files)) {
		text := files[p]
		if strings.Contains(text, "BAD") {
			problems = append(problems, p+": bad")
		}
		if id, ok := strings.CutPrefix(strings.TrimSpace(text), "uses: "); ok && !k.IsLibrary() {
			if _, found := library[id+".yaml"]; !found {
				problems = append(problems, p+": no "+id)
			}
		}
	}
	return problems, nil
}

var shop = FlowOf("shop")

// machine is a process repository of a machine in a test.
type machine struct {
	t *testing.T
	*Repo
}

// open opens the process repository in directory dir.
func open(t *testing.T, dir string) machine {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return machine{t, r}
}

// write writes files of kind k in its directory; an empty text deletes the
// file.
func (m machine) write(k Kind, files map[string]string) {
	m.t.Helper()
	for rel, text := range files {
		p := filepath.Join(m.KindDir(k), filepath.FromSlash(rel))
		if text == "" {
			if err := os.Remove(p); err != nil {
				m.t.Fatal(err)
			}
			continue
		}
		if err := writeFile(p, text); err != nil {
			m.t.Fatal(err)
		}
	}
}

// apply commits the working files of kind k.
func (m machine) apply(k Kind) {
	m.t.Helper()
	files, err := m.Working(k)
	if err != nil {
		m.t.Fatal(err)
	}
	if _, err := m.Apply(k, files, "Apply", "test apply"); err != nil {
		m.t.Fatal(err)
	}
}

// sync pulls and pushes.
func (m machine) sync() *Sync {
	m.t.Helper()
	s, err := m.Pull(check)
	if err != nil {
		m.t.Fatal(err)
	}
	if err := m.Push(check, s); err != nil {
		m.t.Fatal(err)
	}
	return s
}

func (m machine) active(k Kind) Files {
	m.t.Helper()
	f, err := m.Active(k)
	if err != nil {
		m.t.Fatal(err)
	}
	return f
}

func (m machine) working(k Kind) Files {
	m.t.Helper()
	f, err := m.Working(k)
	if err != nil {
		m.t.Fatal(err)
	}
	return f
}

// pair makes two machines with the process of a in the remote repository and
// connected on b too.
func pair(t *testing.T) (a, b machine, remote string) {
	t.Helper()
	root, err := paths.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote = filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)
	a = open(t, filepath.Join(root, "a"))
	a.write(shop, map[string]string{"flow.yaml": "a\n", "stages/review.yaml": "review\n"})
	a.apply(shop)
	if res, _, err := a.SetRemote(remote, check); err != nil || res != RemoteSent {
		t.Fatalf("SetRemote on a: %q, %v", res, err)
	}
	b = open(t, filepath.Join(root, "b"))
	if res, s, err := b.SetRemote(remote, check); err != nil || res != RemoteReceived || !slices.Equal(s.Received, []Kind{shop}) {
		t.Fatalf("SetRemote on b: %q, %+v, %v", res, s, err)
	}
	return a, b, remote
}

func TestOpen(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "process")
	m := open(t, dir)
	if b, err := os.ReadFile(filepath.Join(dir, "process.yaml")); err != nil || string(b) != "format: 1\n" {
		t.Errorf("process.yaml: %q, %v", b, err)
	}
	if log := gittest.Run(t, dir, "log", "--format=%s|%b"); log != "Create the process repository|Gentry: init\n\n" {
		t.Errorf("log: %q", log)
	}
	if status := gittest.Run(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("status: %q", status)
	}
	m.Close()
	open(t, dir).Close()

	foreign := gittest.Repo(t, filepath.Join(t.TempDir(), "shop"))
	var ie *InvalidError
	if _, err := Open(foreign); !errors.As(err, &ie) || ie.Reason != ReasonNotProcess {
		t.Errorf("foreign repository: %v", err)
	}
	newer := filepath.Join(t.TempDir(), "newer")
	open(t, newer).Close()
	os.WriteFile(filepath.Join(newer, "process.yaml"), []byte("format: 2\n"), 0o644)
	gittest.Run(t, newer, "commit", "--quiet", "-am", "newer")
	if _, err := Open(newer); !errors.As(err, &ie) || ie.Reason != ReasonNewerFormat {
		t.Errorf("newer format: %v", err)
	}
}

func TestLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "process")
	open(t, dir)
	defer func(w time.Duration) { LockWait = w }(LockWait)
	LockWait = 100 * time.Millisecond
	var be *BusyError
	if _, err := Open(dir); !errors.As(err, &be) {
		t.Errorf("second open: %v", err)
	}
}

// TestLockWaits checks that a command waits for another to release the
// repository: the agent and the operator do not interleave their git.
func TestLockWaits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "process")
	first := open(t, dir)
	released := make(chan time.Time, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		released <- time.Now()
		first.Close()
	}()
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if at := <-released; time.Now().Before(at) {
		t.Error("the second command did not wait")
	}
}

func TestDraft(t *testing.T) {
	t.Parallel()
	m := open(t, filepath.Join(t.TempDir(), "process"))
	if _, ok, err := m.Applied(shop); ok || err != nil {
		t.Errorf("applied without a flow: %v, %v", ok, err)
	}
	m.write(shop, map[string]string{"flow.yaml": "a\r\n", ".hidden": "x"})
	if draft, _ := m.HasDraft(shop); !draft {
		t.Error("no draft after a change")
	}
	m.apply(shop)
	if draft, _ := m.HasDraft(shop); draft {
		t.Error("a draft after apply: line ends must not count")
	}
	if got := m.active(shop); !got.Equal(Files{"flow.yaml": "a\n"}) {
		t.Errorf("active: %v", got)
	}
	a, ok, err := m.Applied(shop)
	if !ok || err != nil || len(a.Commit) != 40 || time.Since(a.Time) > time.Minute {
		t.Errorf("applied: %+v, %v, %v", a, ok, err)
	}
	m.write(shop, map[string]string{"flow.yaml": "b\n", "stages/x.yaml": "x\n"})
	if err := m.Discard(shop); err != nil {
		t.Fatal(err)
	}
	if got := m.working(shop); !got.Equal(Files{"flow.yaml": "a\n"}) {
		t.Errorf("working after discard: %v", got)
	}
	if kinds, _ := m.Kinds(); !slices.Equal(kinds, []Kind{shop}) {
		t.Errorf("kinds: %v", kinds)
	}
}

func TestSyncReceiveAndSend(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	if got := b.working(shop); !got.Equal(Files{"flow.yaml": "a\n", "stages/review.yaml": "review\n"}) {
		t.Errorf("b got: %v", got)
	}
	b.write(shop, map[string]string{"flow.yaml": "b\n"})
	b.apply(shop)
	if s := b.sync(); !slices.Equal(s.Sent, []Kind{shop}) || len(s.Received) != 0 {
		t.Errorf("b sync: %+v", s)
	}
	// A draft of a in another file stays over what it receives.
	a.write(shop, map[string]string{"stages/review.yaml": "draft\n"})
	if s := a.sync(); !slices.Equal(s.Received, []Kind{shop}) || len(s.Conflicts) != 0 {
		t.Errorf("a sync: %+v", s)
	}
	if got := a.active(shop)["flow.yaml"]; got != "b\n" {
		t.Errorf("a active: %q", got)
	}
	if got := a.working(shop); !got.Equal(Files{"flow.yaml": "b\n", "stages/review.yaml": "draft\n"}) {
		t.Errorf("a working: %v", got)
	}
	if st, _ := a.Status(); !slices.Equal(st.Drafts, []Kind{shop}) || st.Synced == nil || len(st.Unsent) != 0 {
		t.Errorf("a status: %+v", st)
	}
}

func TestSyncMergesFiles(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	a.write(shop, map[string]string{"stages/plan.yaml": "plan\n"})
	a.apply(shop)
	a.sync()
	b.write(shop, map[string]string{"stages/review.yaml": "review 2\n"})
	b.apply(shop)
	s := b.sync()
	if len(s.Conflicts) != 0 || !slices.Equal(s.Sent, []Kind{shop}) || !slices.Equal(s.Received, []Kind{shop}) {
		t.Errorf("b sync: %+v", s)
	}
	want := Files{"flow.yaml": "a\n", "stages/plan.yaml": "plan\n", "stages/review.yaml": "review 2\n"}
	if got := b.active(shop); !got.Equal(want) {
		t.Errorf("b active: %v", got)
	}
	if draft, _ := b.HasDraft(shop); draft {
		t.Error("b has a draft")
	}
	a.sync()
	if got := a.working(shop); !got.Equal(want) {
		t.Errorf("a working: %v", got)
	}
	// History is linear and keeps the commits of both.
	if log := gittest.Run(t, b.Dir, "log", "--format=%s", "--merges"); log != "" {
		t.Errorf("merge commits: %q", log)
	}
}

func TestSyncConflict(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	a.write(shop, map[string]string{"stages/review.yaml": "review a\n", "parts/a.md": "a\n"})
	a.apply(shop)
	a.sync()
	b.write(shop, map[string]string{"stages/review.yaml": "review b\n", "parts/b.md": "b\n"})
	b.apply(shop)
	s := b.sync()
	if len(s.Conflicts) != 1 || s.Conflicts[0].Kind != shop || !slices.Equal(s.Conflicts[0].Paths, []string{"stages/review.yaml"}) {
		t.Fatalf("b sync: %+v", s)
	}
	// The variant of a is active; b keeps all its changes as a draft and the
	// variant of a of the shared file in conflict/.
	if got := b.active(shop); !got.Equal(Files{"flow.yaml": "a\n", "stages/review.yaml": "review a\n", "parts/a.md": "a\n"}) {
		t.Errorf("b active: %v", got)
	}
	if got := b.working(shop); !got.Equal(Files{"flow.yaml": "a\n", "stages/review.yaml": "review b\n", "parts/a.md": "a\n", "parts/b.md": "b\n"}) {
		t.Errorf("b working: %v", got)
	}
	if got, ok, _ := b.Conflict(shop); !ok || !got.Equal(Files{"stages/review.yaml": "review a\n"}) {
		t.Errorf("b conflict: %v", got)
	}
	if st, _ := b.Status(); !slices.Equal(st.Conflicts, []Kind{shop}) || !slices.Equal(st.Drafts, []Kind{shop}) || len(st.Unsent) != 0 {
		t.Errorf("b status: %+v", st)
	}
	if status := gittest.Run(t, b.Dir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "conflict") {
		t.Errorf("conflict/ is not ignored: %s", status)
	}
	b.apply(shop)
	if _, ok, _ := b.Conflict(shop); ok {
		t.Error("conflict/ after apply")
	}
	b.sync()
	a.sync()
	if got := a.working(shop)["stages/review.yaml"]; got != "review b\n" {
		t.Errorf("a after the resolution: %q", got)
	}
}

func TestSyncConflictWithDraft(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	a.write(shop, map[string]string{"flow.yaml": "a 2\n"})
	a.apply(shop)
	a.sync()
	b.write(shop, map[string]string{"flow.yaml": "b draft\n"})
	s := b.sync()
	if len(s.Conflicts) != 1 || !slices.Equal(s.Received, []Kind{shop}) {
		t.Fatalf("b sync: %+v", s)
	}
	if got := b.working(shop)["flow.yaml"]; got != "b draft\n" {
		t.Errorf("b working: %q", got)
	}
	if got, _, _ := b.Conflict(shop); got["flow.yaml"] != "a 2\n" {
		t.Errorf("b conflict: %v", got)
	}
}

func TestSyncConflictByCheck(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	a.write(Library, map[string]string{"reviewer.yaml": "r\n"})
	a.apply(Library)
	a.sync()
	b.sync()
	// a removes the subagent, b starts to use it: each is valid alone.
	a.write(Library, map[string]string{"reviewer.yaml": ""})
	a.apply(Library)
	a.sync()
	b.write(shop, map[string]string{"stages/check.yaml": "uses: reviewer\n"})
	b.apply(shop)
	s := b.sync()
	if len(s.Conflicts) != 1 || s.Conflicts[0].Kind != shop {
		t.Fatalf("b sync: %+v", s)
	}
	if _, ok := b.active(shop)["stages/check.yaml"]; ok {
		t.Error("the flow with problems is active")
	}
	if len(b.active(Library)) != 0 {
		t.Error("the library change of a is not taken")
	}
	if got := b.working(shop)["stages/check.yaml"]; got != "uses: reviewer\n" {
		t.Errorf("b draft: %q", got)
	}
}

func TestBypass(t *testing.T) {
	t.Parallel()
	a, b, _ := pair(t)
	// A valid change around Gentry is taken as it is.
	a.write(shop, map[string]string{"flow.yaml": "by hand\n"})
	gittest.Run(t, a.Dir, "commit", "--quiet", "-am", "by hand")
	if s := a.sync(); len(s.Restored) != 0 || !slices.Equal(s.Sent, []Kind{shop}) {
		t.Errorf("valid bypass: %+v", s)
	}
	// A change with problems is undone and stays a draft, here and on the
	// machine that receives it.
	a.write(shop, map[string]string{"flow.yaml": "BAD\n"})
	gittest.Run(t, a.Dir, "commit", "--quiet", "-am", "bad")
	s := a.sync()
	if len(s.Restored) != 1 || s.Restored[0].Kind != shop || !slices.Equal(s.Restored[0].Problems, []string{"flow.yaml: bad"}) {
		t.Fatalf("bypass with problems: %+v", s)
	}
	if got := a.active(shop)["flow.yaml"]; got != "by hand\n" {
		t.Errorf("a active: %q", got)
	}
	if got := a.working(shop)["flow.yaml"]; got != "BAD\n" {
		t.Errorf("a draft: %q", got)
	}
	if log := gittest.Run(t, a.Dir, "log", "-1", "--format=%s|%b"); log != "Restore the flow of shop|Gentry: restore flow shop\n\n" {
		t.Errorf("restore commit: %q", log)
	}
	if s := b.sync(); len(s.Restored) != 0 || b.active(shop)["flow.yaml"] != "by hand\n" {
		t.Errorf("b: %+v, %v", s, b.active(shop))
	}
}

func TestUnavailable(t *testing.T) {
	t.Parallel()
	a, _, remote := pair(t)
	if err := os.Rename(remote, remote+"-away"); err != nil {
		t.Fatal(err)
	}
	a.write(shop, map[string]string{"flow.yaml": "offline\n"})
	a.apply(shop)
	s := a.sync()
	if !s.Unavailable || s.Output == "" || len(s.Sent) != 0 {
		t.Errorf("sync: %+v", s)
	}
	if st, _ := a.Status(); !slices.Equal(st.Unsent, []Kind{shop}) {
		t.Errorf("status: %+v", st)
	}
	os.Rename(remote+"-away", remote)
	if s := a.sync(); s.Unavailable || !slices.Equal(s.Sent, []Kind{shop}) {
		t.Errorf("sync back online: %+v", s)
	}

	var ue *RemoteUnavailableError
	if _, _, err := a.SetRemote(filepath.Join(t.TempDir(), "nothing.git"), check); !errors.As(err, &ue) {
		t.Errorf("unavailable remote: %v", err)
	}
	if url, _ := a.RemoteURL(); url != remote {
		t.Errorf("the address changed: %s", url)
	}
}

func TestRemoteForeign(t *testing.T) {
	t.Parallel()
	m := open(t, filepath.Join(t.TempDir(), "process"))
	code := gittest.Repo(t, filepath.Join(t.TempDir(), "shop"))
	var ri *RemoteInvalidError
	if _, _, err := m.SetRemote(code, check); !errors.As(err, &ri) || ri.Reason != ReasonNotProcess {
		t.Errorf("code repository: %v", err)
	}
	if url, _ := m.RemoteURL(); url != "" {
		t.Errorf("the address is kept: %s", url)
	}
}

func TestRemoteJoin(t *testing.T) {
	t.Parallel()
	root, _ := paths.Canonical(t.TempDir())
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)
	a := open(t, filepath.Join(root, "a"))
	a.write(shop, map[string]string{"flow.yaml": "a\n"})
	a.apply(shop)
	a.SetRemote(remote, check)

	// b made its own process before it connected: what only b has is added,
	// the flow both have stays active as a has it, b's is a draft.
	b := open(t, filepath.Join(root, "b"))
	cart := FlowOf("cart")
	b.write(shop, map[string]string{"flow.yaml": "b\n"})
	b.apply(shop)
	b.write(cart, map[string]string{"flow.yaml": "cart\n"})
	b.apply(cart)
	res, s, err := b.SetRemote(remote, check)
	if err != nil || res != RemoteMerged || len(s.Conflicts) != 1 || s.Conflicts[0].Kind != shop {
		t.Fatalf("join: %q, %+v, %v", res, s, err)
	}
	if b.active(shop)["flow.yaml"] != "a\n" || b.working(shop)["flow.yaml"] != "b\n" || b.active(cart)["flow.yaml"] != "cart\n" {
		t.Errorf("b: %v %v %v", b.active(shop), b.working(shop), b.active(cart))
	}
	if s := a.sync(); !slices.Equal(s.Received, []Kind{cart}) {
		t.Errorf("a: %+v", s)
	}
	// Connecting again changes nothing.
	if res, _, err := a.SetRemote(remote, check); err != nil || res != RemoteUnchanged {
		t.Errorf("again: %q, %v", res, err)
	}
}
