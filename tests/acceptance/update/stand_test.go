package update_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/tests/testkit"
)

// The releases of the stand. The mock serves 0.1.0, 0.2.0 and 0.3.0, and
// /releases/latest names 0.3.0; 0.4.0 is a program newer than the latest
// release, which the mock does not serve.
const (
	v010   = "0.1.0"
	v020   = "0.2.0"
	v030   = "0.3.0"
	v040   = "0.4.0"
	latest = v030
)

// mockVersions are the releases the mock serves.
var mockVersions = []string{v010, v020, v030}

// releasesEnv names the address of the releases gentry update downloads
// from, as it does for the scripts of installation.
const releasesEnv = "GENTRY_RELEASES_URL"

// The variables the fake Claude Code reads: the file of its state and the
// command it fails, such as update for claude plugin update.
const (
	fakeClaudeStateEnv = "FAKECLAUDE_STATE"
	fakeClaudeFailEnv  = "FAKECLAUDE_FAIL"
)

// githubReleases is the page of the releases on GitHub the texts name;
// GENTRY_RELEASES_URL changes only where the program downloads from.
const githubReleases = "https://github.com/t8nax/gentry/releases"

// installCommand is the command of installation of the platform of the
// stand, as the texts name it.
var installCommand = map[bool]string{
	false: "curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh",
	true:  "irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex",
}[runtime.GOOS == "windows"]

// exe is the suffix of a program on the platform of the stand.
var exe = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// platforms are the platforms of a release.
var platforms = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}

// assetOf returns the name of the program of a platform os/arch in a release.
func assetOf(platform string) string {
	os, arch, _ := strings.Cut(platform, "/")
	name := "gentry-" + os + "-" + arch
	if os == "windows" {
		name += ".exe"
	}
	return name
}

// hostAsset is the program of the platform the programs of the stand are
// built for.
var hostAsset = assetOf(runtime.GOOS + "/" + runtime.GOARCH)

// The texts of the update the tests compare with, as the specification
// gives them.
const (
	newSessions = "Изменения вступят в силу в новых сессиях ИИ-инструментов."
	pageURL     = "https://t8nax.github.io/gentry/"
)

// updated is the first line of an update from the version to the latest.
func updated(from string) string { return "Gentry обновлён: " + from + " → " + latest }

// pageLine is the line of the page of the release of the latest version.
var pageLine = "Страница выпуска: " + pageURL + latest + "/"

// commandTimeout limits one run of a program of the stand.
const commandTimeout = 5 * time.Minute

// builds holds the programs of the stand, built once for the package: the
// programs of the releases and a build of a commit without a tag.
var builds struct {
	once          sync.Once
	err           error
	dir           string
	module        string            // the root of the module under test
	programs      map[string]string // the program of each release, by version
	content       map[string][]byte // the bytes of the program of each release of the mock
	source        string            // a build of a commit with no tag
	sourceVersion string            // the version it names
	dev           string            // a build with no information of the version: 0.0.0-dev
	dirty         string            // a build on the tag v0.3.0 with uncommitted changes: 0.3.0+dirty
}

// The versions the builds with no information of the version and with
// uncommitted changes name.
const (
	devVersion   = "0.0.0-dev"
	dirtyVersion = v030 + "+dirty"
)

// built builds the programs of the stand, once for the package.
func built(t *testing.T) {
	t.Helper()
	root := testkit.ModuleRoot(t)
	builds.once.Do(func() { builds.err = buildAll(root) })
	if builds.err != nil {
		t.Fatal(builds.err)
	}
}

// removeBuilds removes the programs of the stand after the tests.
func removeBuilds() {
	for _, dir := range []string{builds.dir, fakeClaude.dir} {
		if dir != "" {
			os.RemoveAll(dir)
		}
	}
}

// buildAll builds Gentry of the module under test as each release of the
// stand: a copy of the files of its work tree is committed, tagged
// v<version> and built as the release builds it, so the program names the
// version of the tag. A commit after the last tag gives the build from the
// sources with no tag, which names a pseudo-version.
func buildAll(root string) error {
	dir, err := os.MkdirTemp("", "gentry-update-")
	if err != nil {
		return err
	}
	builds.dir, builds.module = dir, root
	builds.programs, builds.content = map[string]string{}, map[string][]byte{}
	module := filepath.Join(dir, "module")
	if err := copyModule(root, module); err != nil {
		return err
	}
	git := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = module
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	if err := git("init", "--quiet", "--initial-branch=main"); err != nil {
		return err
	}
	if err := git("add", "--all"); err != nil {
		return err
	}
	if err := git("commit", "--quiet", "--message", "release"); err != nil {
		return err
	}
	for i, v := range []string{v010, v020, v030, v040} {
		if i > 0 {
			if err := git("commit", "--quiet", "--allow-empty", "--message", "release "+v); err != nil {
				return err
			}
		}
		if err := git("tag", "v"+v); err != nil {
			return err
		}
		p := filepath.Join(dir, v, "gentry"+exe)
		if err := goBuild(module, "./cmd/gentry", p); err != nil {
			return err
		}
		if got, err := versionOf(p); err != nil || got != v {
			return fmt.Errorf("the build of the module tagged v%s names the version %q (%v): go build must stamp the version of the tag", v, got, err)
		}
		builds.programs[v] = p
		if slices.Contains(mockVersions, v) {
			if builds.content[v], err = os.ReadFile(p); err != nil {
				return err
			}
		}
	}
	if err := git("commit", "--quiet", "--allow-empty", "--message", "after the release"); err != nil {
		return err
	}
	builds.source = filepath.Join(dir, "source", "gentry"+exe)
	if err := goBuild(module, "./cmd/gentry", builds.source); err != nil {
		return err
	}
	if builds.sourceVersion, err = versionOf(builds.source); err != nil {
		return err
	}
	if releaseVersion.MatchString(builds.sourceVersion) {
		return fmt.Errorf("the build of a commit with no tag names the version %s of a release", builds.sourceVersion)
	}
	// With no information of the version control the program names 0.0.0-dev.
	builds.dev = filepath.Join(dir, "dev", "gentry"+exe)
	if err := goBuild(module, "./cmd/gentry", builds.dev, "-buildvcs=false"); err != nil {
		return err
	}
	if got, err := versionOf(builds.dev); err != nil || got != devVersion {
		return fmt.Errorf("the build with -buildvcs=false names the version %q (%v), want %s", got, err, devVersion)
	}
	// On the tag v0.3.0 with an uncommitted change it names 0.3.0+dirty.
	if err := git("checkout", "--quiet", "v"+v030); err != nil {
		return err
	}
	changelog := filepath.Join(module, "CHANGELOG.md")
	b, err := os.ReadFile(changelog)
	if err != nil {
		return err
	}
	if err := os.WriteFile(changelog, append(b, "\nНезакоммиченная правка.\n"...), 0o644); err != nil {
		return err
	}
	builds.dirty = filepath.Join(dir, "dirty", "gentry"+exe)
	if err := goBuild(module, "./cmd/gentry", builds.dirty); err != nil {
		return err
	}
	if got, err := versionOf(builds.dirty); err != nil || got != dirtyVersion {
		return fmt.Errorf("the build on the tag v%s with an uncommitted change names the version %q (%v), want %s", v030, got, err, dirtyVersion)
	}
	return git("checkout", "--quiet", "--force", "main")
}

// fakeClaude holds the fake Claude Code, built once for the package.
var fakeClaude struct {
	once    sync.Once
	dir     string
	program string
	err     error
}

// claudeProgram returns the fake Claude Code of the module under test: it
// keeps its state in the file FAKECLAUDE_STATE names.
func claudeProgram(t *testing.T) string {
	t.Helper()
	root := testkit.ModuleRoot(t)
	fakeClaude.once.Do(func() {
		if fakeClaude.dir, fakeClaude.err = os.MkdirTemp("", "gentry-claude-"); fakeClaude.err != nil {
			return
		}
		fakeClaude.program = filepath.Join(fakeClaude.dir, "claude"+exe)
		fakeClaude.err = goBuild(root, "./internal/adapter/claude/testdata/fakeclaude", fakeClaude.program)
	})
	if fakeClaude.err != nil {
		t.Fatal(fakeClaude.err)
	}
	return fakeClaude.program
}

// releaseVersion is the version of a release: X.Y.Z.
var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// copyModule copies the files of the work tree of the module under test,
// tracked and new, to dir.
func copyModule(root, dir string) error {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git ls-files: %v", err)
	}
	for _, name := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		from := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(from)
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted in the work tree
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		to := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, b, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// goBuild builds the package of the module in dir into the file, as the
// release builds Gentry, with the flags of go build added.
func goBuild(dir, pkg, file string, flags ...string) error {
	args := append([]string{"build", "-trimpath", "-ldflags", "-s -w"}, flags...)
	cmd := exec.Command("go", append(args, "-o", file, pkg)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s in %s: %v\n%s", pkg, dir, err, out)
	}
	return nil
}

// versionOf returns the version the program names on version.
func versionOf(program string) (string, error) {
	out, err := exec.Command(program, "version").Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %v", program, err)
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n")), "Версия Gentry: ")
	if !ok {
		return "", fmt.Errorf("%s version printed %q", program, out)
	}
	return v, nil
}

// The lines of «Несовместимые изменения» of the journal of the mock.
const (
	breaking020 = "Флаг `--all` команды `gentry task list` убран: вне проекта команда показывает задачи всех проектов без него. Скрипты с этим флагом нужно поправить."
	breaking030 = "Команда `gentry task show --path` заменена на `gentry task path`. Скрипты с прежней командой нужно поправить."
)

// The sections of the versions of the journal of the mock.
const (
	section010 = "## 0.1.0 — 2026-10-20\n\nПервый выпуск Gentry.\n\n### Новое\n* Добавлена установка одной командой.\n"
	section020 = "## 0.2.0 — 2026-10-26\n\nСписок задач вне проекта показывает все проекты.\n\n" +
		"### Несовместимые изменения\n* " + breaking020 + "\n\n" +
		"### Новое\n* Список задач вне проекта показывает задачи всех проектов.\n"
	section030 = "## 0.3.0 — 2026-11-02\n\nПуть задачи показывает отдельная команда.\n\n" +
		"### Несовместимые изменения\n* " + breaking030 + "\n"
	// section030Multiline is the section 0.3.0 with its line of
	// «Несовместимые изменения» on three lines of the file.
	section030Multiline = "## 0.3.0 — 2026-11-02\n\nПуть задачи показывает отдельная команда.\n\n" +
		"### Несовместимые изменения\n" +
		"* Команда `gentry task show --path` заменена\n" +
		"  на `gentry task path`. Скрипты с прежней\n" +
		"  командой нужно поправить.\n"
	// section030NoBreaking is the section 0.3.0 with no «Несовместимые
	// изменения».
	section030NoBreaking = "## 0.3.0 — 2026-11-02\n\nПуть задачи показывает отдельная команда.\n\n" +
		"### Новое\n* Добавлена команда `gentry task path`.\n"
)

// journal returns a journal of changes with an empty section of the
// unreleased changes and the sections, as it is on the tag of a release.
func journal(sections ...string) string {
	s := "# Журнал изменений\n\n## Не выпущено\n"
	for _, sec := range sections {
		s += "\n" + sec
	}
	return s
}

// A mock is the mock release: a local HTTP server with the releases 0.1.0,
// 0.2.0 and 0.3.0 at the paths of GitHub. As on GitHub, /releases/latest
// redirects to the page of the latest release, a download to the storage of
// the file, and /raw/v<version>/CHANGELOG.md, the journal of changes on the
// tag of the version, to the storage of raw files. It records the paths of
// the requests.
type mock struct {
	srv *httptest.Server

	mu       sync.Mutex
	files    map[string][]byte // by <version>/<file>
	journals map[string]string // CHANGELOG.md on the tag, by version
	noLatest bool              // /releases/latest answers 404
	slow     time.Duration     // the delay before the program of the latest release is served
	requests []string
}

// newMock serves the mock release for the test: the program of the platform
// of the stand is Gentry of the release, the programs of the other platforms
// are text; each release has its checksums.txt and its journal of changes.
func newMock(t *testing.T) *mock {
	t.Helper()
	m := &mock{files: map[string][]byte{}, journals: map[string]string{
		v010: journal(section010),
		v020: journal(section020, section010),
		v030: journal(section030, section020, section010),
	}}
	for _, v := range mockVersions {
		var sums []string
		for _, p := range platforms {
			name := assetOf(p)
			content := []byte(name + " " + v + "\n")
			if name == hostAsset {
				content = builds.content[v]
			}
			m.files[v+"/"+name] = content
			sums = append(sums, sum(content)+"  "+name+"\n")
		}
		sort.Strings(sums)
		m.files[v+"/checksums.txt"] = []byte(strings.Join(sums, ""))
		m.files[v+"/install.sh"] = []byte("#!/bin/sh\n")
		m.files[v+"/install.ps1"] = []byte("# install\n")
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	m.mu.Lock()
	m.requests = append(m.requests, p)
	noLatest := m.noLatest
	m.mu.Unlock()
	switch {
	case p == "/releases/latest":
		if noLatest {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/releases/tag/v"+latest, http.StatusFound)
	case strings.HasPrefix(p, "/releases/tag/v"):
		v := strings.TrimPrefix(p, "/releases/tag/v")
		if _, ok := m.file(v + "/checksums.txt"); !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!DOCTYPE html>\n<title>Release Gentry %s</title>\n", v)
	case strings.HasPrefix(p, "/releases/latest/download/"):
		if noLatest {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/releases/download/v"+latest+"/"+strings.TrimPrefix(p, "/releases/latest/download/"), http.StatusFound)
	case strings.HasPrefix(p, "/releases/download/v"):
		file := strings.TrimPrefix(p, "/releases/download/v")
		if _, ok := m.file(file); !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/storage/"+file, http.StatusFound)
	case strings.HasPrefix(p, "/storage/"):
		name := strings.TrimPrefix(p, "/storage/")
		b, ok := m.file(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if name == latest+"/"+hostAsset {
			m.mu.Lock()
			slow := m.slow
			m.mu.Unlock()
			time.Sleep(slow)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.Write(b)
	case strings.HasPrefix(p, "/raw/v") && strings.HasSuffix(p, "/CHANGELOG.md"):
		if _, ok := m.journal(strings.TrimSuffix(strings.TrimPrefix(p, "/raw/v"), "/CHANGELOG.md")); !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/raw-storage/"+strings.TrimPrefix(p, "/raw/"), http.StatusFound)
	case strings.HasPrefix(p, "/raw-storage/v") && strings.HasSuffix(p, "/CHANGELOG.md"):
		text, ok := m.journal(strings.TrimSuffix(strings.TrimPrefix(p, "/raw-storage/v"), "/CHANGELOG.md"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, text)
	default:
		http.NotFound(w, r)
	}
}

func (m *mock) file(name string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[name]
	return b, ok
}

func (m *mock) journal(v string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	text, ok := m.journals[v]
	return text, ok
}

// releases is the address of the releases of the mock, the value of
// GENTRY_RELEASES_URL.
func (m *mock) releases() string { return m.srv.URL + "/releases" }

// setJournal sets the journal of changes on the tag of the version; ""
// makes the mock answer 404 to it.
func (m *mock) setJournal(v, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if text == "" {
		delete(m.journals, v)
		return
	}
	m.journals[v] = text
}

// dropLatest makes the mock answer 404 to /releases/latest: there is no
// release.
func (m *mock) dropLatest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.noLatest = true
}

// slowProgram makes the mock wait d before it serves the program of the
// platform of the stand of the latest release.
func (m *mock) slowProgram(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slow = d
}

// served returns how many times the mock has served the file <version>/<file>
// from its storage, wherever the request was redirected from.
func (m *mock) served(name string) int {
	n := 0
	for _, p := range m.requested() {
		if p == "/storage/"+name {
			n++
		}
	}
	return n
}

// corrupt changes the program of the platform of the stand in the release v
// without changing its checksums.txt, and returns the sum the file has now.
func (m *mock) corrupt(v string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := append(slices.Clone(m.files[v+"/"+hostAsset]), "corrupted\n"...)
	m.files[v+"/"+hostAsset] = b
	return sum(b)
}

// checksum returns the sum checksums.txt of the release v gives the program
// of the platform of the stand.
func (m *mock) checksum(v string) string {
	b, _ := m.file(v + "/checksums.txt")
	for _, line := range strings.Split(string(b), "\n") {
		if s, name, ok := strings.Cut(line, "  "); ok && name == hostAsset {
			return s
		}
	}
	return ""
}

// requested returns the paths of the requests the mock has had.
func (m *mock) requested() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

// programRequests returns the paths of the requests of the programs of a
// release: the files gentry-<os>-<arch>.
func (m *mock) programRequests() []string {
	var out []string
	for _, p := range m.requested() {
		if strings.HasPrefix(p[strings.LastIndex(p, "/")+1:], "gentry-") {
			out = append(out, p)
		}
	}
	return out
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// A stand is the machine of a case: a home and a data root of its own, a
// fake Claude Code with a state of its own and a mock release.
type stand struct {
	t             *testing.T
	root          string // the temporary folder of the stand, with symbolic links resolved
	home          string // HOME and USERPROFILE
	data          string // the data root, GENTRY_HOME
	claudeState   string // the state of the fake Claude Code
	claudeConfig  string // CLAUDE_CONFIG_DIR
	claudeProgram string // the fake Claude Code, GENTRY_CLAUDE
	mock          *mock
	releaseURL    string            // GENTRY_RELEASES_URL
	extra         map[string]string // variables the case sets for the programs
}

// parallel skips the case on a built gentry, as the cases of the stand run
// the programs of the releases, and runs it in parallel with the other cases
// of the stand: each has a machine of its own and sets no variable of the
// process of the test.
func parallel(t *testing.T) {
	t.Helper()
	testkit.SkipOnBinary(t)
	t.Parallel()
}

// newStand returns a machine with nothing installed: an empty data root.
func newStand(t *testing.T) *stand {
	t.Helper()
	testkit.SkipOnBinary(t)
	built(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &stand{
		t:             t,
		root:          root,
		home:          filepath.Join(root, "home"),
		data:          filepath.Join(root, "data"),
		claudeState:   filepath.Join(root, "claude-state.json"),
		claudeConfig:  filepath.Join(root, "claude-config"),
		claudeProgram: claudeProgram(t),
		mock:          newMock(t),
		extra:         map[string]string{},
	}
	s.releaseURL = s.mock.releases()
	for _, d := range []string{s.home, s.data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// installedStand returns a machine with the release v installed and, if
// claude is set, Claude Code connected by the installed program.
func installedStand(t *testing.T, v string, claude bool) *stand {
	t.Helper()
	s := newStand(t)
	s.installed(v)
	if claude {
		s.connectClaude()
	}
	return s
}

// dataIn moves the data root of a stand with nothing installed to the folder
// name of the stand, such as a folder with a space in its name.
func (s *stand) dataIn(name string) {
	s.t.Helper()
	s.data = filepath.Join(s.root, name)
	if err := os.MkdirAll(s.data, 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// bin is the folder of the program; program is its path.
func (s *stand) bin() string     { return filepath.Join(s.data, "bin") }
func (s *stand) program() string { return filepath.Join(s.bin(), "gentry"+exe) }

// installed puts the program of the release v into the bin of the data root.
func (s *stand) installed(v string) {
	s.t.Helper()
	s.installedFile(builds.programs[v])
}

// installedFile puts the program from into the bin of the data root.
func (s *stand) installedFile(from string) {
	s.t.Helper()
	copyFile(s.t, from, s.program())
}

// set sets a variable of the environment of the programs the stand runs; ""
// removes it.
func (s *stand) set(name, value string) { s.extra[name] = value }

// environ is the environment of the programs of the stand: that of the test
// with the home, the data root, the address of the releases and the fake
// Claude Code of the stand, and the variables the case sets.
func (s *stand) environ(over map[string]string) []string {
	vars := map[string]string{
		"HOME":                   s.home,
		"USERPROFILE":            s.home,
		testkit.HomeEnv:          s.data,
		releasesEnv:              s.releaseURL,
		testkit.ClaudeProgramEnv: s.claudeProgram,
		fakeClaudeStateEnv:       s.claudeState,
		testkit.ClaudeConfigEnv:  s.claudeConfig,
		testkit.PluginRootEnv:    "",
		testkit.ClaudeCodeEnv:    "",
		testkit.SessionEnv:       "",
		fakeClaudeFailEnv:        "",
		"XDG_CONFIG_HOME":        "",
		testkit.BinaryEnv:        "",
	}
	for k, v := range s.extra {
		vars[k] = v
	}
	for k, v := range over {
		vars[k] = v
	}
	names := slices.Collect(maps.Keys(vars))
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(names, func(d string) bool { return sameEnvName(name, d) }) {
			env = append(env, kv)
		}
	}
	for k, v := range vars {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// sameEnvName reports whether a and b name the same environment variable:
// on Windows the names do not depend on the case.
func sameEnvName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// A result is what a run of a program gave: its exit code and output, with
// the lines ended by \n.
type result struct {
	code           int
	stdout, stderr string
}

func (r result) String() string {
	return fmt.Sprintf("exit code %d, stdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// runEnv runs the program with args in dir, in the environment of the stand
// with the variables over, and stdin as its standard input: nil for the null
// device.
func (s *stand) runEnv(program, dir string, stdin io.Reader, over map[string]string, args ...string) result {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, s.environ(over), stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		s.t.Fatalf("%s %s did not end in %s: it waits for something", program, strings.Join(args, " "), commandTimeout)
	}
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		s.t.Fatalf("%s: %v", cmd, err)
	}
	return result{code: code, stdout: crlf(out.String()), stderr: crlf(errOut.String())}
}

// start starts the installed program with args in the folder of the stand
// and returns the function that waits for it to end and gives what it gave.
func (s *stand) start(args ...string) func() result {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	cmd := exec.CommandContext(ctx, s.program(), args...)
	cmd.Dir, cmd.Env = s.root, s.environ(nil)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		cancel()
		s.t.Fatal(err)
	}
	return func() result {
		s.t.Helper()
		defer cancel()
		err := cmd.Wait()
		if ctx.Err() != nil {
			s.t.Fatalf("gentry %s did not end in %s: it waits for something", strings.Join(args, " "), commandTimeout)
		}
		code := 0
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			code = exit.ExitCode()
		case err != nil:
			s.t.Fatalf("%s: %v", cmd, err)
		}
		return result{code: code, stdout: crlf(out.String()), stderr: crlf(errOut.String())}
	}
}

// crlf ends the lines of a Windows console with \n.
func crlf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// runIn runs the program with args in dir.
func (s *stand) runIn(program, dir string, args ...string) result {
	s.t.Helper()
	return s.runEnv(program, dir, nil, nil, args...)
}

// gentry runs the installed program with args in the folder of the stand.
func (s *stand) gentry(args ...string) result {
	s.t.Helper()
	return s.runIn(s.program(), s.root, args...)
}

// update runs gentry update of the installed program with the flags.
func (s *stand) update(flags ...string) result {
	s.t.Helper()
	return s.gentry(append([]string{"update"}, flags...)...)
}

// mustIn runs the installed program with args in dir and fails the test
// unless it succeeds.
func (s *stand) mustIn(dir string, args ...string) string {
	s.t.Helper()
	r := s.runIn(s.program(), dir, args...)
	if r.code != 0 {
		s.t.Fatalf("stand: gentry %s: %s", strings.Join(args, " "), r)
	}
	return r.stdout
}

// connectClaude connects Claude Code by gentry setup claude of the installed
// program.
func (s *stand) connectClaude() {
	s.t.Helper()
	s.mustIn(s.root, "setup", "claude")
	if !testkit.FileExists(s.integration()) {
		s.t.Fatalf("stand: setup claude made no folder %s", s.integration())
	}
}

// integration is the folder of the integration of Claude Code in the data
// root.
func (s *stand) integration() string { return filepath.Join(s.data, "integrations", "claude") }

// wantVersion checks that the installed program names the version v.
func (s *stand) wantVersion(v string) {
	s.t.Helper()
	if r := s.gentry("version"); r.code != 0 || r.stdout != "Версия Gentry: "+v+"\n" {
		s.t.Errorf("gentry version: %s\nwant «Версия Gentry: %s»", r, v)
	}
}

// programSum returns the sum of the installed program, to tell whether it
// changed.
func (s *stand) programSum() string {
	s.t.Helper()
	b, err := os.ReadFile(s.program())
	if err != nil {
		s.t.Fatal(err)
	}
	return sum(b)
}

// wantProgram checks that the installed program has the sum.
func (s *stand) wantProgram(want string) {
	s.t.Helper()
	if got := s.programSum(); got != want {
		s.t.Errorf("the installed program changed: its sum is %s, was %s", got, want)
	}
}

// wantNoRequests checks that the mock has had no request.
func (s *stand) wantNoRequests() {
	s.t.Helper()
	if r := s.mock.requested(); len(r) > 0 {
		s.t.Errorf("the mock release had requests %q, want none", r)
	}
}

// flowTestdata is the process directory of the flow testdata of the module.
func flowTestdata() string {
	return filepath.Join(builds.module, "internal", "flow", "testdata", "process")
}

// project connects a project of its own repository by the installed program
// and applies the flow of the flow testdata to it, which lays out its
// subagent reviewer; each of the worktrees named is added to its pool. It
// returns the main worktree, <root>/<name>; a worktree w is
// <root>/<name>-<w>.
func (s *stand) project(name string, worktrees ...string) string {
	s.t.Helper()
	dir := gittest.Repo(s.t, filepath.Join(s.root, name))
	for _, w := range worktrees {
		gittest.Worktree(s.t, dir, s.worktree(name, w), w)
	}
	s.mustIn(dir, "project", "add", name, "--knowledge", filepath.Join(s.root, name+"-knowledge"))
	process := filepath.Join(s.data, "process")
	if !testkit.FileExists(filepath.Join(process, "agents", "reviewer.md")) {
		if err := os.CopyFS(filepath.Join(process, "agents"), os.DirFS(filepath.Join(flowTestdata(), "agents"))); err != nil {
			s.t.Fatal(err)
		}
	}
	if err := os.CopyFS(filepath.Join(process, name, "flow"), os.DirFS(filepath.Join(flowTestdata(), "shop", "flow"))); err != nil {
		s.t.Fatal(err)
	}
	s.mustIn(dir, "flow", "apply")
	pool := []string{dir}
	for _, w := range worktrees {
		s.mustIn(dir, "worktree", "add", s.worktree(name, w))
		pool = append(pool, s.worktree(name, w))
	}
	for _, w := range pool {
		if !testkit.FileExists(reviewer(w)) {
			s.t.Fatalf("stand: no subagent reviewer in %s", w)
		}
	}
	return dir
}

// worktree is the worktree w of the project name.
func (s *stand) worktree(name, w string) string { return filepath.Join(s.root, name+"-"+w) }

// reviewer is the file of the subagent reviewer in the worktree.
func reviewer(worktree string) string {
	return filepath.Join(worktree, ".claude", "agents", "reviewer.md")
}

// handEdit is the line a test adds to a file of a subagent Gentry laid out,
// as an edit by hand: the mark of Gentry stays at its start.
const handEdit = "Правка вручную."

// editByHand adds handEdit to the end of the file.
func editByHand(t *testing.T, p string) {
	t.Helper()
	b := readFile(t, p)
	writeFile(t, p, b+"\n"+handEdit+"\n")
}

// projectOrder returns the projects in the order of gentry project list.
func (s *stand) projectOrder() []string {
	s.t.Helper()
	var list struct {
		Projects []struct {
			Project string `json:"project"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(s.mustIn(s.root, "project", "list", "--json")), &list); err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for _, p := range list.Projects {
		out = append(out, p.Project)
	}
	return out
}

// agentsInPlace is the first line of gentry agents deploy when the
// subagents of the project match its flow.
func agentsInPlace(project string) string {
	return "Субагенты в рабочих копиях проекта " + project + " соответствуют флоу."
}

// wantAgentsInPlace checks that gentry agents deploy --project of the
// installed program finds the subagents of the project in place.
func (s *stand) wantAgentsInPlace(project string) {
	s.t.Helper()
	r := s.gentry("agents", "deploy", "--project", project)
	if ls := lines(r.stdout); r.code != 0 || len(ls) == 0 || ls[0] != agentsInPlace(project) {
		s.t.Errorf("agents deploy --project %s after the update: %s\nwant the first line «%s»", project, r, agentsInPlace(project))
	}
}

// The state of the fake Claude Code, as it keeps it.
type claudeState struct {
	Marketplaces []claudeMarketplace `json:"marketplaces"`
	Plugins      []claudePlugin      `json:"plugins"`
	Log          []string            `json:"log"`
}

type claudeMarketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
}

type claudePlugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
}

// pluginID is the plugin of Gentry in Claude Code.
const pluginID = "gentry@gentry"

// claude returns the state of the fake Claude Code.
func (s *stand) claude() claudeState {
	s.t.Helper()
	var st claudeState
	b, err := os.ReadFile(s.claudeState)
	if errors.Is(err, fs.ErrNotExist) {
		return st
	}
	if err != nil {
		s.t.Fatal(err)
	}
	if err := json.Unmarshal(b, &st); err != nil {
		s.t.Fatal(err)
	}
	return st
}

// editPlugin changes the plugin of Gentry in the state of the fake Claude
// Code.
func (s *stand) editPlugin(edit func(p *claudePlugin)) {
	s.t.Helper()
	st := s.claude()
	i := slices.IndexFunc(st.Plugins, func(p claudePlugin) bool { return p.ID == pluginID })
	if i < 0 {
		s.t.Fatalf("stand: the fake Claude Code has no plugin %s", pluginID)
	}
	edit(&st.Plugins[i])
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		s.t.Fatal(err)
	}
	writeFile(s.t, s.claudeState, string(b))
}

// plugin returns the plugin of Gentry in the fake Claude Code.
func (s *stand) plugin() claudePlugin {
	s.t.Helper()
	st := s.claude()
	i := slices.IndexFunc(st.Plugins, func(p claudePlugin) bool { return p.ID == pluginID })
	if i < 0 {
		s.t.Fatalf("the fake Claude Code has no plugin %s: %+v", pluginID, st)
	}
	return st.Plugins[i]
}

// claudeCalls returns the commands of the fake Claude Code run after the
// first n.
func (s *stand) claudeCalls(n int) []string {
	s.t.Helper()
	log := s.claude().Log
	if n > len(log) {
		return nil
	}
	return log[n:]
}

// integrationSum returns a sum of the files of the integration of Claude
// Code in the data root, to tell whether they changed.
func (s *stand) integrationSum() string {
	s.t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(s.integration(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.integration(), p)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// otherData makes a second data root of the machine with the program of the
// release v in its bin and returns it.
func (s *stand) otherData(v string) string {
	s.t.Helper()
	dir := filepath.Join(s.root, "other-data")
	copyFile(s.t, builds.programs[v], filepath.Join(dir, "bin", "gentry"+exe))
	return dir
}

// gentryOf runs the program of the data root dir with args, with
// GENTRY_HOME pointing at dir, and fails the test unless it succeeds.
func (s *stand) gentryOf(dir string, args ...string) {
	s.t.Helper()
	r := s.runEnv(filepath.Join(dir, "bin", "gentry"+exe), s.root, nil, map[string]string{testkit.HomeEnv: dir}, args...)
	if r.code != 0 {
		s.t.Fatalf("stand: gentry %s of %s: %s", strings.Join(args, " "), dir, r)
	}
}

// stateStore is the file of the state store of the data root.
func (s *stand) stateStore() string { return filepath.Join(s.data, "state", "state.db") }

// The output of gentry update --json, as the specification names its
// fields.
type updateOutput struct {
	Action         string              `json:"action"`
	Previous       *string             `json:"previous"`
	Version        string              `json:"version"`
	Program        string              `json:"program"`
	Integrations   []updateIntegration `json:"integrations"`
	Breaking       []updateBreaking    `json:"breaking"`
	ChangelogError *string             `json:"changelog_error"`
	Page           string              `json:"page"`
	Agents         []updateAgents      `json:"agents"`
}

type updateIntegration struct {
	Tool          string `json:"tool"`
	Dir           string `json:"dir"`
	PluginVersion string `json:"plugin_version"`
	Action        string `json:"action"`
	Enabled       bool   `json:"enabled"`
}

type updateBreaking struct {
	Version string   `json:"version"`
	Lines   []string `json:"lines"`
}

type updateAgents struct {
	Project   string                 `json:"project"`
	Changed   *bool                  `json:"changed"`
	Worktrees []updateAgentsWorktree `json:"worktrees"`
}

type updateAgentsWorktree struct {
	Path    string   `json:"path"`
	Updated []string `json:"updated"`
}

// updateSchema is the schema of the output of gentry update --json in the
// contract.
const updateSchema = "schemas/update.json"

// updateJSON runs gentry update --json, checks that it succeeds and that its
// output matches the schema of the contract, and returns the output and its
// fields.
func (s *stand) updateJSON() (string, updateOutput) {
	s.t.Helper()
	r := s.update("--json")
	if r.code != 0 {
		s.t.Fatalf("update --json: %s", r)
	}
	testkit.Validate(s.t, updateSchema, r.stdout)
	var out updateOutput
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		s.t.Fatalf("update --json: %v\n%s", err, r.stdout)
	}
	return r.stdout, out
}

// wantError runs gentry update --json of the installed program and checks
// that it fails with the exit code 1 and the code of the error.
func (s *stand) wantError(code string) {
	s.t.Helper()
	if got := s.errorOf(s.program()); got != code {
		s.t.Errorf("update --json: the error %q, want %s", got, code)
	}
}

// errorOf runs gentry update --json of the program, checks that it fails
// with the exit code 1 and an output of the schema of errors, and returns
// the code of the error.
func (s *stand) errorOf(program string) string {
	s.t.Helper()
	r := s.runIn(program, s.root, "update", "--json")
	if r.code != 1 {
		s.t.Errorf("update --json: %s\nwant exit code 1", r)
	}
	testkit.Validate(s.t, "schemas/error.json", r.stdout)
	var e contract.ErrorOutput
	json.Unmarshal([]byte(r.stdout), &e)
	return e.Error.Code
}

// stalePlugin points CLAUDE_PLUGIN_ROOT of the programs of the stand at a
// plugin of the version 0.0.0+00000000, which no program builds.
func (s *stand) stalePlugin() {
	s.t.Helper()
	dir := filepath.Join(s.root, "stale-plugin")
	writeFile(s.t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"gentry","version":"0.0.0+00000000"}`)
	s.set(testkit.PluginRootEnv, dir)
}

// hookIn runs the session start hook of the installed program for a session
// in dir, as the plugin of Claude Code runs it.
func (s *stand) hookIn(dir string) result {
	s.t.Helper()
	in, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	return s.runEnv(s.program(), dir, bytes.NewReader(in), nil, "hook", "session-start", "--tool", "claude")
}

// pluginStaleHint is what to do in the hint of the hook about a plugin that
// does not match the program.
const pluginStaleHint = "Сообщить оператору и по его слову обновить плагин: "

// lines returns the lines of an output.
func lines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

// wantCode fails the test unless the run exited with code.
func wantCode(t *testing.T, r result, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("want exit code %d; %s", code, r)
	}
}

// wantStderr checks that the error output is the lines.
func wantStderr(t *testing.T, r result, want ...string) {
	t.Helper()
	if r.stderr != testkit.Lines(want...) {
		t.Errorf("stderr:\n%s\nwant:\n%s", r.stderr, testkit.Lines(want...))
	}
}

// wantLine checks that out has the line.
func wantLine(t *testing.T, out, line string) {
	t.Helper()
	if !slices.Contains(lines(out), line) {
		t.Errorf("output has no line %q:\n%s", line, out)
	}
}

// wantNoLineWith checks that no line of out has the text.
func wantNoLineWith(t *testing.T, out, text string) {
	t.Helper()
	for _, l := range lines(out) {
		if strings.Contains(l, text) {
			t.Errorf("output has the line %q with «%s»:\n%s", l, text, out)
		}
	}
}

// wantInOrder checks that out has the lines one after another.
func wantInOrder(t *testing.T, out string, want ...string) {
	t.Helper()
	ls := lines(out)
	for i := 0; i+len(want) <= len(ls); i++ {
		if slices.Equal(ls[i:i+len(want)], want) {
			return
		}
	}
	t.Errorf("output has no lines one after another:\n%s\noutput:\n%s", testkit.Lines(want...), out)
}

// wantLastBlock checks that the last block of the output, after an empty
// line, is the lines.
func wantLastBlock(t *testing.T, out string, want ...string) {
	t.Helper()
	ls := lines(out)
	block := append([]string{""}, want...)
	if len(ls) < len(block) || !slices.Equal(ls[len(ls)-len(block):], block) {
		t.Errorf("the last block of the output is otherwise:\n%s\nwant an empty line and:\n%s", out, testkit.Lines(want...))
	}
}

// wantReason checks that the line starts with the label and names a reason
// after it.
func wantReason(t *testing.T, line, label string) {
	t.Helper()
	if !strings.HasPrefix(line, label) || strings.TrimSpace(strings.TrimPrefix(line, label)) == "" {
		t.Errorf("the line %q is not «%s<причина>»", line, label)
	}
}

// samePath reports whether two paths are the same: on Windows the case does
// not matter.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// copyFile copies the program from to the file to.
func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// skipAsRoot skips a case about a folder closed for writing: root writes it.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes a folder closed for writing")
	}
}

// updateClosedInput runs gentry update of the installed program with its
// standard input closed: the pipe of the input is closed once the program
// starts.
func (s *stand) updateClosedInput() result {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.program(), "update")
	cmd.Dir, cmd.Env = s.root, s.environ(nil)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	in, err := cmd.StdinPipe()
	if err != nil {
		s.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	in.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		s.t.Fatalf("update with the input closed did not end in %s", commandTimeout)
	}
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		s.t.Fatalf("%s: %v", cmd, err)
	}
	return result{code: code, stdout: crlf(out.String()), stderr: crlf(errOut.String())}
}

// An mcpServer is the installed program running gentry mcp, as the tool
// server of a session of an agent does.
type mcpServer struct {
	t     *testing.T
	in    io.WriteCloser
	lines chan string
	done  chan error
	id    int
}

// startMCP runs gentry mcp of the installed program and waits for its answer
// to a ping. The test ends it by closing its input.
func (s *stand) startMCP() *mcpServer {
	s.t.Helper()
	cmd := exec.Command(s.program(), "mcp")
	cmd.Dir, cmd.Env = s.root, s.environ(nil)
	in, err := cmd.StdinPipe()
	if err != nil {
		s.t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		s.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	m := &mcpServer{t: s.t, in: in, lines: make(chan string, 16), done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			m.lines <- sc.Text()
		}
		close(m.lines)
	}()
	go func() { m.done <- cmd.Wait() }()
	s.t.Cleanup(func() {
		in.Close()
		<-m.done
	})
	m.ping()
	return m
}

// ping sends a ping to the server and fails the test unless it answers.
func (m *mcpServer) ping() {
	m.t.Helper()
	m.id++
	if _, err := fmt.Fprintf(m.in, `{"jsonrpc":"2.0","id":%d,"method":"ping"}`+"\n", m.id); err != nil {
		m.t.Fatalf("gentry mcp takes no input: %v", err)
	}
	select {
	case line, ok := <-m.lines:
		if !ok || !strings.Contains(line, fmt.Sprintf(`"id":%d`, m.id)) {
			m.t.Fatalf("gentry mcp answered the ping %d with %q", m.id, line)
		}
	case <-time.After(30 * time.Second):
		m.t.Fatalf("gentry mcp did not answer the ping %d", m.id)
	}
}

// wantRunning checks that the server still runs and answers.
func (m *mcpServer) wantRunning() {
	m.t.Helper()
	select {
	case err := <-m.done:
		m.done <- err
		m.t.Fatalf("gentry mcp ended: %v", err)
	default:
	}
	m.ping()
}
