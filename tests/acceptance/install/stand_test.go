package install_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// The scripts of installation, as paths from the root of the module: the
// mock release serves them, and the build of a release puts them into it.
const (
	shScript  = "install/install.sh"
	ps1Script = "install/install.ps1"
)

// githubReleases is the page of the releases on GitHub the scripts name to
// the user; GENTRY_RELEASES_URL changes only where they download from.
const githubReleases = "https://github.com/t8nax/gentry/releases"

// releasesEnv names the address of the releases the scripts download from.
const releasesEnv = "GENTRY_RELEASES_URL"

// The releases of the mock: latest is the one /releases/latest names.
const (
	older  = "0.1.0"
	latest = "0.2.0"
)

// platforms are the platforms of a release, in the order of the message on
// an unsupported one.
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

// hostAsset is the program of the platform of the stand.
var hostAsset = assetOf(runtime.GOOS + "/" + runtime.GOARCH)

// exe is the suffix of a program on the platform of the stand.
var exe = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// fakeBuilds holds the programs of the mock releases for the platform of the
// stand, built once for the package: each prints its version.
var fakeBuilds struct {
	once sync.Once
	dir  string
	err  error
}

// fakes returns the directory with the program of each release of the mock
// at <version>/<hostAsset>.
func fakes(t *testing.T) string {
	t.Helper()
	root := testkit.ModuleRoot(t)
	fakeBuilds.once.Do(func() {
		fakeBuilds.dir, fakeBuilds.err = os.MkdirTemp("", "gentry-fakes-")
		if fakeBuilds.err != nil {
			return
		}
		for _, v := range []string{older, latest} {
			cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+v,
				"-o", filepath.Join(fakeBuilds.dir, v, hostAsset), "./tests/acceptance/install/testdata/fakegentry")
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				fakeBuilds.err = fmt.Errorf("go build fakegentry %s: %v\n%s", v, err, out)
				return
			}
		}
	})
	if fakeBuilds.err != nil {
		t.Fatal(fakeBuilds.err)
	}
	return fakeBuilds.dir
}

// removeFakes removes the programs of the mock releases after the tests.
func removeFakes() {
	if fakeBuilds.dir != "" {
		os.RemoveAll(fakeBuilds.dir)
	}
}

// A mock is the mock release: a local HTTP server with the files of the
// releases 0.1.0 and 0.2.0 at the paths of GitHub. As on GitHub,
// /releases/latest/download/<file> redirects to the download of the latest
// release, and a download redirects to the storage of the file, which serves
// it with no charset.
type mock struct {
	srv *httptest.Server

	mu    sync.Mutex
	files map[string][]byte // by <version>/<file>
}

// newMock serves the mock release for the test: the program of the platform
// of the stand prints its version, the programs of the other platforms are
// text; each release has its checksums.txt and the scripts of the module.
func newMock(t *testing.T) *mock {
	t.Helper()
	dir := fakes(t)
	root := testkit.ModuleRoot(t)
	m := &mock{files: map[string][]byte{}}
	for _, v := range []string{older, latest} {
		var sums []string
		for _, p := range platforms {
			name := assetOf(p)
			content := []byte(name + " " + v + "\n")
			if name == hostAsset {
				var err error
				if content, err = os.ReadFile(filepath.Join(dir, v, hostAsset)); err != nil {
					t.Fatal(err)
				}
			}
			m.files[v+"/"+name] = content
			sums = append(sums, sum(content)+"  "+name+"\n")
		}
		sort.Strings(sums)
		m.files[v+"/checksums.txt"] = []byte(strings.Join(sums, ""))
		for _, script := range []string{shScript, ps1Script} {
			if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(script))); err == nil {
				m.files[v+"/"+filepath.Base(script)] = b
			}
		}
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/releases/latest/download/"):
		http.Redirect(w, r, "/releases/download/v"+latest+"/"+strings.TrimPrefix(p, "/releases/latest/download/"), http.StatusFound)
	case strings.HasPrefix(p, "/releases/download/v"):
		file := strings.TrimPrefix(p, "/releases/download/v")
		if _, ok := m.file(file); !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/storage/"+file, http.StatusFound)
	case strings.HasPrefix(p, "/storage/"):
		b, ok := m.file(strings.TrimPrefix(p, "/storage/"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.Write(b)
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

// releases is the address of the releases of the mock, the value of
// GENTRY_RELEASES_URL.
func (m *mock) releases() string { return m.srv.URL + "/releases" }

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
	b, _ := m.file(v + "/" + hostAsset)
	return sum(b)
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// script returns the script of installation the mock serves as the latest
// release, fetched as curl -fsSL does: following the redirects.
func (m *mock) script(t *testing.T, name string) []byte {
	t.Helper()
	resp, err := http.Get(m.srv.URL + "/releases/latest/download/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the mock release has no %s: the module has no script of installation %s", name, filepath.Join("install", name))
	}
	return b
}

// shells are the shells the scripts run in: install.sh in sh, install.ps1 in
// Windows PowerShell 5.1 and in PowerShell 7.
const (
	sh         = "sh"
	powershell = "powershell"
	pwsh       = "pwsh"
)

var (
	allShells = []string{sh, powershell, pwsh}
	shOnly    = []string{sh}
	ps1Only   = []string{powershell, pwsh}
)

// A stand is a machine of a case: a home of its own, a mock release and the
// shell the script runs in.
type stand struct {
	t     *testing.T
	shell string
	home  string // HOME and USERPROFILE
	data  string // the data root: GENTRY_HOME, or <home>/.gentry
	mock  *mock

	gentryHome string // GENTRY_HOME; "" unset
	releaseURL string // GENTRY_RELEASES_URL
	path       string // PATH; "" inherited
}

// eachShell runs f in a subtest per shell of names, on a clean machine of
// its own: the cases of install.sh run on Linux and macOS, those of
// install.ps1 on Windows.
func eachShell(t *testing.T, names []string, f func(t *testing.T, s *stand)) {
	t.Helper()
	testkit.SkipOnBinary(t)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f(t, cleanMachine(t, name))
		})
	}
}

// cleanMachine returns a clean machine: a new home with no data root and
// GENTRY_HOME unset; on Linux and macOS the home has an empty .profile and
// .bashrc.
func cleanMachine(t *testing.T, shell string) *stand {
	t.Helper()
	switch {
	case shell == sh && runtime.GOOS == "windows":
		t.Skip("install.sh is checked on Linux and macOS")
	case shell != sh && runtime.GOOS != "windows":
		t.Skip("install.ps1 is checked on Windows")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &stand{t: t, shell: shell, home: home, data: filepath.Join(home, ".gentry"), mock: newMock(t)}
	s.releaseURL = s.mock.releases()
	if shell == sh {
		for _, f := range []string{".profile", ".bashrc"} {
			writeFile(t, filepath.Join(home, f), "")
		}
	}
	return s
}

// setGentryHome sets GENTRY_HOME to dir: the data root.
func (s *stand) setGentryHome(dir string) {
	s.gentryHome, s.data = dir, dir
}

// bin is the folder of the program; program is its path.
func (s *stand) bin() string     { return filepath.Join(s.data, "bin") }
func (s *stand) program() string { return filepath.Join(s.bin(), "gentry"+exe) }

// versionArgs names the version v to the script; noPath tells it not to
// change PATH.
func (s *stand) versionArgs(v string) []string {
	if s.shell == sh {
		return []string{"--version", v}
	}
	return []string{"-Version", v}
}

func (s *stand) noPath() string {
	if s.shell == sh {
		return "--no-modify-path"
	}
	return "-NoModifyPath"
}

// keepUserPath returns -NoModifyPath for install.ps1 and nothing for
// install.sh. A case of install.ps1 that is not about the variable Path runs
// with it on every machine: the stand does not isolate the registry, and a
// script that fails before it changes Path gives the same result with it.
func (s *stand) keepUserPath() []string {
	if s.shell == sh {
		return nil
	}
	return []string{"-NoModifyPath"}
}

// environ is the environment of the script: that of the test with the home,
// the data root and the address of the releases of the stand.
func (s *stand) environ() []string {
	drop := []string{"HOME", "USERPROFILE", testkit.HomeEnv, releasesEnv, "XDG_CONFIG_HOME", "ZDOTDIR"}
	if s.path != "" {
		drop = append(drop, "PATH")
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(drop, func(d string) bool { return sameEnvName(name, d) }) {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+s.home, "USERPROFILE="+s.home, releasesEnv+"="+s.releaseURL)
	if s.gentryHome != "" {
		env = append(env, testkit.HomeEnv+"="+s.gentryHome)
	}
	if s.path != "" {
		env = append(env, "PATH="+s.path)
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

// A result is what a run of the script gave: its exit code and output, with
// the lines ended by \n.
type result struct {
	code           int
	stdout, stderr string
}

func (r result) String() string {
	return fmt.Sprintf("exit code %d, stdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// install runs the script of installation of the shell with args as the
// user does: curl -fsSL <mock>/releases/latest/download/install.sh | sh -s --
// <args>; irm <mock>/releases/latest/download/install.ps1 | iex, or with
// args & ([scriptblock]::Create((irm …))) <args>.
func (s *stand) install(args ...string) result {
	s.t.Helper()
	if s.shell == sh {
		script := s.mock.script(s.t, "install.sh")
		return s.runSh(bytes.NewReader(script), append([]string{"-s", "--"}, args...)...)
	}
	return s.runPs1(args...)
}

// runSh runs sh with args and the script on its standard input.
func (s *stand) runSh(stdin io.Reader, args ...string) result {
	s.t.Helper()
	cmd := exec.Command("sh", args...)
	cmd.Stdin = stdin
	cmd.Env = s.environ()
	cmd.Dir = s.home
	return s.run(cmd)
}

// ps1Encoding makes the console write the output of the script in UTF-8:
// the test reads it from a pipe, a terminal shows it as it is.
const ps1Encoding = "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; "

func (s *stand) runPs1(args ...string) result {
	s.t.Helper()
	s.mock.script(s.t, "install.ps1")
	url := s.mock.srv.URL + "/releases/latest/download/install.ps1"
	command := ps1Encoding + "irm '" + url + "' | iex"
	if len(args) > 0 {
		command = ps1Encoding + "& ([scriptblock]::Create((irm '" + url + "'))) " + strings.Join(args, " ")
	}
	cmd := exec.Command(s.shell, "-NoProfile", "-NonInteractive", "-Command", command)
	cmd.Env = s.environ()
	cmd.Dir = s.home
	before := realInstall()
	checkUserPath := guardUserPath(s.t)
	r := s.run(cmd)
	checkUserPath()
	if after := realInstall(); after != before {
		s.t.Errorf("the script changed the folder of the program of the real profile %s:\nbefore:\n%s\nafter:\n%s", realProfile, before, after)
	}
	return r
}

func (s *stand) run(cmd *exec.Cmd) result {
	s.t.Helper()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
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

// crlf ends the lines of a Windows console with \n.
func crlf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// installed makes the stand one where the script has installed the release
// v, as the user installs it: install.sh changes the profiles, install.ps1
// keeps the variable Path.
func (s *stand) installed(v string) {
	s.t.Helper()
	args := s.versionArgs(v)
	if s.shell != sh {
		args = append(args, s.noPath())
	}
	r := s.install(args...)
	if r.code != 0 {
		s.t.Fatalf("stand: install %s: %s", v, r)
	}
	if got := s.version(); got != versionLine(v) {
		s.t.Fatalf("stand: install %s: gentry version printed %q", v, got)
	}
}

// version returns what the installed program prints on version, or the
// failure to run it.
func (s *stand) version() string {
	s.t.Helper()
	out, err := exec.Command(s.program(), "version").Output()
	if err != nil {
		return fmt.Sprintf("%s version: %v", s.program(), err)
	}
	return crlf(string(out))
}

func versionLine(v string) string { return "Версия Gentry: " + v + "\n" }

// realProfile is the profile of the user who runs the tests, before Main
// moves it; "" on Linux and macOS.
var realProfile string

// realInstall describes the folder of the program of the real profile: the
// name, size and time of change of each file. A script that takes the
// profile from the system rather than from USERPROFILE would change it.
func realInstall() string {
	if realProfile == "" {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(realProfile, ".gentry", "bin"))
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			fmt.Fprintf(&b, "%s %d %s\n", e.Name(), info.Size(), info.ModTime())
		}
	}
	return b.String()
}

// onCIRunner reports whether the tests run on a runner of GitHub Actions
// hosted by GitHub: a new machine for each job, whose registry the tests may
// change.
func onCIRunner() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted"
}

// guardUserPath keeps the variable Path of the user of a machine that is not
// a runner of CI: the returned function restores it and fails the test if a
// run of the script has changed it.
func guardUserPath(t *testing.T) func() {
	t.Helper()
	if runtime.GOOS != "windows" || onCIRunner() {
		return func() {}
	}
	before, err := readUserPath()
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		after, err := readUserPath()
		if err != nil {
			t.Fatal(err)
		}
		if after != before {
			if err := writeUserPath(before); err != nil {
				t.Errorf("restore the variable Path of the user: %v", err)
			}
			t.Errorf("the script changed the variable Path of the user of the machine; restored. Before: %+v, after: %+v", before, after)
		}
	}
}

// ciOnly skips a case about the variable Path of the user anywhere but on a
// runner of CI; on the runner the variable is restored after the test.
func ciOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 is checked on Windows")
	}
	if !onCIRunner() {
		t.Skip("the variable Path of the user is checked only on a runner of CI: the stand does not isolate the registry")
	}
	saved, err := readUserPath()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writeUserPath(saved); err != nil {
			t.Errorf("restore the variable Path of the user: %v", err)
		}
	})
}

// A userPath is the variable Path of the user in the registry.
type userPath struct {
	value  string
	expand bool // of type REG_EXPAND_SZ, not REG_SZ
	exists bool
}

// fakeUname puts first in PATH a uname that names the kernel and the machine
// of the case, and the operating system as uname -o does.
func (s *stand) fakeUname(kernel, machine, system string) {
	s.t.Helper()
	dir := s.t.TempDir()
	script := `#!/bin/sh
s= n= r= v= m= p= i= o=
[ $# -eq 0 ] && s=1
for arg do
  case $arg in
    --kernel-name) s=1 ;;
    --nodename) n=1 ;;
    --kernel-release) r=1 ;;
    --kernel-version) v=1 ;;
    --machine) m=1 ;;
    --processor) p=1 ;;
    --hardware-platform) i=1 ;;
    --operating-system) o=1 ;;
    --all) s=1 n=1 r=1 v=1 m=1 o=1 ;;
    -?*)
      flags=${arg#-}
      while [ -n "$flags" ]; do
        f=${flags%"${flags#?}"}
        flags=${flags#?}
        case $f in
          s) s=1 ;; n) n=1 ;; r) r=1 ;; v) v=1 ;; m) m=1 ;; p) p=1 ;; i) i=1 ;; o) o=1 ;;
          a) s=1 n=1 r=1 v=1 m=1 o=1 ;;
          *) echo "uname: invalid option -- '$f'" >&2; exit 1 ;;
        esac
      done ;;
    *) echo "uname: extra operand '$arg'" >&2; exit 1 ;;
  esac
done
out=
[ -n "$s" ] && out="$out KERNEL"
[ -n "$n" ] && out="$out stand"
[ -n "$r" ] && out="$out 1.0"
[ -n "$v" ] && out="$out #1"
[ -n "$m" ] && out="$out MACHINE"
[ -n "$p" ] && out="$out MACHINE"
[ -n "$i" ] && out="$out MACHINE"
[ -n "$o" ] && out="$out SYSTEM"
printf '%s\n' "${out# }"
`
	script = strings.NewReplacer("KERNEL", kernel, "MACHINE", machine, "SYSTEM", system).Replace(script)
	writeExecutable(s.t, filepath.Join(dir, "uname"), script)
	s.path = dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// pathWithout points PATH at a folder with the programs of the PATH of the
// test but those named.
func (s *stand) pathWithout(names ...string) {
	s.t.Helper()
	dir := s.t.TempDir()
	for _, from := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(from)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if slices.Contains(names, name) || testkit.FileExists(filepath.Join(dir, name)) {
				continue
			}
			info, err := os.Stat(filepath.Join(from, name))
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				continue
			}
			if err := os.Symlink(filepath.Join(from, name), filepath.Join(dir, name)); err != nil {
				s.t.Fatal(err)
			}
		}
	}
	s.path = dir
}

// deadReleases points the scripts at an address where nothing listens.
func (s *stand) deadReleases() {
	s.t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	s.releaseURL = "http://" + addr + "/releases"
}

// start runs the installed program, or another file, with mcp, as the tool
// server of a session of an agent does, and returns the input that ends it
// and the channel of its end.
func start(t *testing.T, program string) (io.WriteCloser, <-chan error) {
	t.Helper()
	cmd := exec.Command(program, "mcp")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		in.Close()
		<-done
	})
	return in, done
}

// lines returns the lines of an output.
func lines(out string) []string {
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

// wantCode fails the test unless the script exited with code.
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

// wantFirst checks that the output begins with the lines.
func wantFirst(t *testing.T, out string, want ...string) {
	t.Helper()
	ls := lines(out)
	if len(ls) < len(want) || !slices.Equal(ls[:len(want)], want) {
		t.Errorf("output begins otherwise:\n%s\nwant first lines:\n%s", out, testkit.Lines(want...))
	}
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

// wantNo checks that the path does not exist.
func wantNo(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists, want none", p)
	}
}

// wantVersion checks that the installed program prints the version v.
func (s *stand) wantVersion(v string) {
	s.t.Helper()
	if got := s.version(); got != versionLine(v) {
		s.t.Errorf("gentry version printed %q, want %q", got, versionLine(v))
	}
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

func writeExecutable(t *testing.T, p, content string) {
	t.Helper()
	writeFile(t, p, content)
	if err := os.Chmod(p, 0o755); err != nil {
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

// copyFile copies the file from to the file to.
func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// skipAsRoot skips a case about a file closed for writing: root writes it.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes a file closed for writing")
	}
}
