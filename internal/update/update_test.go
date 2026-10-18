package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/releases"
)

// A fakeReleases serves the latest release, the program of it and its
// journal of changes.
type fakeReleases struct {
	latest      string
	latestErr   error
	downloadErr error
	journal     string
	journalErr  error
	latests     int // the calls of Latest
	downloads   int
}

func (f *fakeReleases) Latest(context.Context) (string, error) {
	f.latests++
	return f.latest, f.latestErr
}

func (f *fakeReleases) Download(_ context.Context, v, asset, dir string) (string, error) {
	f.downloads++
	if f.downloadErr != nil {
		return "", f.downloadErr
	}
	p := filepath.Join(dir, asset)
	return p, os.WriteFile(p, []byte("gentry "+v), 0o644)
}

func (f *fakeReleases) Changelog(context.Context, string) (string, error) {
	return f.journal, f.journalErr
}

// fakeProjects lays out the projects ids; a project of fail fails.
type fakeProjects struct {
	ids      []string
	listErr  error
	fail     map[string]error
	listed   bool
	deployed []string
}

func (f *fakeProjects) List() ([]string, error) {
	f.listed = true
	return f.ids, f.listErr
}

func (f *fakeProjects) Deploy(id string) (contract.UpdateAgents, error) {
	if err := f.fail[id]; err != nil {
		return contract.UpdateAgents{}, err
	}
	f.deployed = append(f.deployed, id)
	return contract.UpdateAgents{Project: id, Changed: true, Worktrees: []contract.AgentsWorktree{}}, nil
}

// A fakeTool is an AI tool of the stand: what it tells and how it builds the
// integration.
type fakeTool struct {
	registered  string
	checkErr    error
	current     bool
	setupAction string
	setupErr    error
	setups      int
}

// tool returns the tool claude of the stand, with its integration in the
// data root if connected.
func (s *stand) tool(f *fakeTool, connected bool) {
	dir := filepath.Join(s.root, "integrations", "claude")
	if connected {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.t.Fatal(err)
		}
	}
	s.u.Tools = append(s.u.Tools, Tool{
		Name:      "claude",
		Dir:       dir,
		Elsewhere: func() (string, error) { return f.registered, f.checkErr },
		Current: func() (contract.SetupOutput, bool) {
			return contract.SetupOutput{Tool: "claude", Dir: dir, Action: unchanged, Enabled: true}, f.current
		},
		Setup: func() (contract.SetupOutput, error) {
			f.setups++
			if f.setupErr != nil {
				return contract.SetupOutput{}, f.setupErr
			}
			return contract.SetupOutput{Tool: "claude", Dir: dir, Action: f.setupAction, Enabled: true}, nil
		},
	})
}

// A stand is a data root with the program of a version installed, and an
// Updater of fakes.
type stand struct {
	t         *testing.T
	root      string
	installed string
	running   string
	releases  *fakeReleases
	projects  *fakeProjects
	u         *Updater
	versions  map[string]string // the version each program names
}

func newStand(t *testing.T, version string) *stand {
	t.Helper()
	root := t.TempDir()
	installed := Installed(root)
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("gentry "+version), 0o755); err != nil {
		t.Fatal(err)
	}
	running, err := filepath.EvalSymlinks(installed)
	if err != nil {
		t.Fatal(err)
	}
	s := &stand{t: t, root: root, installed: installed, running: running,
		releases: &fakeReleases{latest: "0.3.0", journal: "# Журнал изменений\n"},
		projects: &fakeProjects{}, versions: map[string]string{installed: version}}
	s.u = &Updater{Releases: s.releases, Projects: s.projects, VersionOf: func(p string) (string, bool) {
		v, ok := s.versions[p]
		return v, ok
	}}
	return s
}

func (s *stand) run(version, from string) (*Result, *Handoff, error) {
	s.t.Helper()
	return s.u.Run(context.Background(), Request{Root: s.root, Running: s.running, Version: version, From: from})
}

// wantProgram checks the content of the program installed.
func (s *stand) wantProgram(want string) {
	s.t.Helper()
	if b, _ := os.ReadFile(s.installed); string(b) != want {
		s.t.Errorf("the program installed is %q, want %q", b, want)
	}
}

// locked reports whether the lock of updates is held.
func (s *stand) locked() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	l, err := releases.Lock(ctx, s.installed)
	if err != nil {
		return true
	}
	l.Release()
	return false
}

// TestRunReplaces checks the update of an older version: the program of the
// latest release takes its place, and the update is handed over to it with
// the version replaced, under the lock until Done.
func TestRunReplaces(t *testing.T) {
	s := newStand(t, "0.1.0")
	claude := &fakeTool{}
	s.tool(claude, true)
	r, h, err := s.run("0.1.0", "")
	if err != nil || r != nil || h == nil {
		t.Fatalf("Run = %+v, %+v, %v; want a handoff", r, h, err)
	}
	if h.Program != s.installed || !slices.Equal(h.Env, []string{FromEnv + "=0.1.0"}) {
		t.Errorf("the handoff %+v, want the program installed with %s=0.1.0", h, FromEnv)
	}
	s.wantProgram("gentry 0.3.0")
	if claude.setups != 0 || s.projects.listed {
		t.Error("the program before the update built an integration or laid out the subagents")
	}
	if !s.locked() {
		t.Error("the lock is free before the new program has done its part")
	}
	h.Done()
	if s.locked() {
		t.Error("the lock is held after Done")
	}
}

// TestRunHandsOver checks that an update that finds the program installed
// replaced by another update hands the update over to it, with the lock
// free.
func TestRunHandsOver(t *testing.T) {
	s := newStand(t, "0.1.0")
	s.versions[s.installed] = "0.3.0"
	_, h, err := s.run("0.1.0", "")
	if err != nil || h == nil || h.Program != s.installed || len(h.Env) != 0 {
		t.Fatalf("Run = %+v, %v; want a handoff to the program installed, as it is", h, err)
	}
	if s.locked() || s.releases.latests != 0 {
		t.Errorf("the handoff holds the lock or asked the releases (%d)", s.releases.latests)
	}
	h.Done()
}

// TestRunRefusals checks the refusals before anything is replaced: the
// program installed stays and nothing is downloaded.
func TestRunRefusals(t *testing.T) {
	notFound := &releases.NotFoundError{}
	down := &releases.DownloadError{URL: "http://127.0.0.1:1/releases/latest", Err: errors.New("refused")}
	for _, c := range []struct {
		name    string
		version string
		prepare func(s *stand)
		check   func(t *testing.T, s *stand, err error)
	}{
		{"elsewhere", "0.1.0", func(s *stand) { s.running = filepath.Join(s.root, "elsewhere", "gentry") }, func(t *testing.T, s *stand, err error) {
			var e *ElsewhereError
			if !errors.As(err, &e) || e.Installed != s.installed {
				t.Errorf("%v, want ElsewhereError naming %s", err, s.installed)
			}
		}},
		{"no program installed", "0.1.0", func(s *stand) { os.Remove(s.installed) }, func(t *testing.T, s *stand, err error) {
			var e *ElsewhereError
			if !errors.As(err, &e) || e.Installed != "" {
				t.Errorf("%v, want ElsewhereError naming no program", err)
			}
		}},
		{"no release", "0.0.0-dev", nil, func(t *testing.T, s *stand, err error) {
			var e *NotReleaseError
			if !errors.As(err, &e) || e.Version != "0.0.0-dev" || s.releases.latests != 0 {
				t.Errorf("%v, want NotReleaseError and no request", err)
			}
			// Refused before the lock, whose file is left once taken.
			if _, err := os.Stat(filepath.Join(filepath.Dir(s.installed), ".update.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the lock was taken: %v", err)
			}
		}},
		{"newer", "0.4.0", nil, func(t *testing.T, s *stand, err error) {
			var e *NewerError
			if !errors.As(err, &e) || e.Latest != "0.3.0" {
				t.Errorf("%v, want NewerError of 0.3.0", err)
			}
		}},
		{"no latest release", "0.1.0", func(s *stand) { s.releases.latestErr = notFound }, func(t *testing.T, s *stand, err error) {
			if !errors.Is(err, notFound) {
				t.Errorf("%v, want %v", err, notFound)
			}
		}},
		{"download failed", "0.1.0", func(s *stand) { s.releases.downloadErr = down }, func(t *testing.T, s *stand, err error) {
			if !errors.Is(err, down) {
				t.Errorf("%v, want %v", err, down)
			}
		}},
		{"plugin elsewhere", "0.1.0", func(s *stand) { s.tool(&fakeTool{registered: "/home/anna/.gentry-old/integrations/claude"}, false) }, func(t *testing.T, s *stand, err error) {
			var e *PluginElsewhereError
			if !errors.As(err, &e) || e.Registered != "/home/anna/.gentry-old/integrations/claude" || s.releases.latests != 0 {
				t.Errorf("%v, want PluginElsewhereError and no request", err)
			}
		}},
		{"no program of the tool, connected", "0.1.0", func(s *stand) { s.tool(&fakeTool{checkErr: ErrToolNotFound}, true) }, func(t *testing.T, s *stand, err error) {
			var e *ToolNotFoundError
			if !errors.As(err, &e) || e.Tool != "claude" || s.releases.latests != 0 {
				t.Errorf("%v, want ToolNotFoundError and no request", err)
			}
		}},
		{"tool failed, connected", "0.1.0", func(s *stand) { s.tool(&fakeTool{checkErr: errors.New("claude failed")}, true) }, func(t *testing.T, s *stand, err error) {
			var e *CheckError
			if !errors.As(err, &e) || e.Tool != "claude" || s.releases.latests != 0 {
				t.Errorf("%v, want CheckError and no request", err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStand(t, c.version)
			if c.prepare != nil {
				c.prepare(s)
			}
			r, h, err := s.run(c.version, "")
			if r != nil || h != nil {
				t.Errorf("Run = %+v, %+v; want a refusal", r, h)
			}
			c.check(t, s, err)
			if b, _ := os.ReadFile(s.installed); len(b) > 0 && string(b) != "gentry "+c.version {
				t.Errorf("the program installed changed: %q", b)
			}
			if s.locked() {
				t.Error("the lock is held after the refusal")
			}
		})
	}
}

// TestRunToolUnconnected checks that a tool whose integration is not
// connected and that fails to tell, or has no program, does not stop the
// update.
func TestRunToolUnconnected(t *testing.T) {
	for _, err := range []error{errors.New("claude failed"), ErrToolNotFound} {
		s := newStand(t, "0.1.0")
		s.tool(&fakeTool{checkErr: err}, false)
		_, h, runErr := s.run("0.1.0", "")
		if runErr != nil || h == nil {
			t.Errorf("with %v: %v, want the update", err, runErr)
			continue
		}
		h.Done()
	}
}

// TestRunCurrent checks the latest release installed: nothing is
// downloaded; an integration that matches is left as it is, another is
// built anew, and then the subagents are laid out.
func TestRunCurrent(t *testing.T) {
	for _, c := range []struct {
		name      string
		tool      *fakeTool
		connected bool
		setups    int
		laid      bool
	}{
		{"plugin matches", &fakeTool{current: true}, true, 0, false},
		{"plugin built anew", &fakeTool{setupAction: "updated"}, true, 1, true},
		{"setup changed nothing", &fakeTool{setupAction: unchanged}, true, 1, false},
		{"no integration", &fakeTool{}, false, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStand(t, "0.3.0")
			s.projects.ids = []string{"shop"}
			s.tool(c.tool, c.connected)
			r, h, err := s.run("0.3.0", "")
			if err != nil || h != nil || r == nil {
				t.Fatalf("Run = %+v, %+v, %v; want a result", r, h, err)
			}
			if r.Action != Current || r.Previous != "" || r.Connected != c.connected || len(r.Integrations) != map[bool]int{true: 1}[c.connected] {
				t.Errorf("the result %+v", r)
			}
			if c.tool.setups != c.setups || s.projects.listed != c.laid || (r.Agents != nil) != c.laid {
				t.Errorf("setups %d, layout %v (%+v); want %d, %v", c.tool.setups, s.projects.listed, r.Agents, c.setups, c.laid)
			}
			if s.releases.downloads != 0 {
				t.Error("the latest release was downloaded")
			}
			s.wantProgram("gentry 0.3.0")
		})
	}
}

// TestRunFinish checks the new program that finishes the update: it builds
// anew every integration connected, lays out the subagents of each project
// in the order given and reads the incompatible changes of the versions
// skipped; a step that fails does not stop the others.
func TestRunFinish(t *testing.T) {
	s := newStand(t, "0.3.0")
	claude := &fakeTool{setupAction: "updated"}
	s.tool(claude, true)
	s.projects.ids = []string{"shop", "admin", "blog"}
	deployErr := errors.New("git does not know the worktree")
	s.projects.fail = map[string]error{"admin": deployErr}
	s.releases.journal = "## 0.3.0 — 2026-11-02\n\n### Несовместимые изменения\n* Поле `price` заменено на `total`.\n\n## 0.2.0 — 2026-10-26\n\n### Новое\n* Корзина.\n"
	r, h, err := s.run("0.3.0", "0.1.0")
	if err != nil || h != nil || r == nil {
		t.Fatalf("Run = %+v, %+v, %v; want a result", r, h, err)
	}
	if r.Action != Updated || r.Previous != "0.1.0" || r.Version != "0.3.0" || r.Program != s.installed || claude.setups != 1 || len(r.Integrations) != 1 {
		t.Errorf("the result %+v", r)
	}
	if !slices.Equal(s.projects.deployed, []string{"shop", "blog"}) || len(r.Agents) != 2 || len(r.Projects) != 1 || r.Projects[0].Project != "admin" || !errors.Is(r.Projects[0].Err, deployErr) {
		t.Errorf("the layout: deployed %q, agents %+v, failures %+v", s.projects.deployed, r.Agents, r.Projects)
	}
	if len(r.Breaking) != 1 || r.Breaking[0].Version != "0.3.0" || r.ChangelogErr != nil {
		t.Errorf("the incompatible changes %+v, %v", r.Breaking, r.ChangelogErr)
	}
	if s.releases.latests != 0 || !r.Failed() {
		t.Errorf("the new program asked for the latest release (%d), or the failure is not told", s.releases.latests)
	}

	// The new program is that of the data root.
	s = newStand(t, "0.3.0")
	s.running = filepath.Join(s.root, "elsewhere", "gentry")
	var e *ElsewhereError
	if _, _, err := s.run("0.3.0", "0.1.0"); !errors.As(err, &e) {
		t.Errorf("the update finished by a program elsewhere: %v, want ElsewhereError", err)
	}

	// An integration that fails, the projects that cannot be listed and the
	// journal that cannot be read.
	s = newStand(t, "0.3.0")
	setupErr, listErr, journalErr := errors.New("claude refused"), errors.New("state unavailable"), errors.New("HTTP 404")
	s.tool(&fakeTool{setupErr: setupErr}, true)
	s.projects.listErr, s.releases.journalErr = listErr, journalErr
	r, _, _ = s.run("0.3.0", "0.2.0")
	if len(r.Tools) != 1 || !errors.Is(r.Tools[0].Err, setupErr) || !r.Connected || len(r.Integrations) != 0 {
		t.Errorf("the integration that failed: %+v", r)
	}
	if !errors.Is(r.ListErr, listErr) || !errors.Is(r.ChangelogErr, journalErr) || r.Breaking != nil {
		t.Errorf("the layout and the journal: %v, %v, %+v", r.ListErr, r.ChangelogErr, r.Breaking)
	}
}

// TestRunRemovesOld checks that on Windows the update removes a free .old
// of an earlier update with the latest release installed too.
func TestRunRemovesOld(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a program is renamed to .old only on Windows")
	}
	s := newStand(t, "0.3.0")
	old := releases.Old(s.installed)
	if err := os.WriteFile(old, []byte("gentry 0.2.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.run("0.3.0", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the free %s is still there: %v", old, err)
	}
}
