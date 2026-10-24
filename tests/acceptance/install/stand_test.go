package install_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// hostAsset is the program of the platform of the stand, as the script takes
// it: under Rosetta on macOS, that of arm64.
var hostAsset = assetOf(runtime.GOOS + "/" + hostArch())

// hostArch returns the architecture of the machine of the stand: a test built
// for amd64 runs under Rosetta on a Mac with arm64.
func hostArch() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		out, err := exec.Command("sysctl", "-n", "sysctl.proc_translated").Output()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return "arm64"
		}
	}
	return runtime.GOARCH
}

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
// /releases/latest redirects to the page of the latest release,
// /releases/latest/download/<file> to its download, and a download to the
// storage of the file, which serves it with no charset.
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
	case p == "/releases/latest":
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
	loginShell string // SHELL

	process map[string]string // variables of the process set by the case; "" removed
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
// .bashrc, and the login shell is /bin/sh.
func cleanMachine(t *testing.T, shell string) *stand {
	t.Helper()
	switch {
	case shell == sh && runtime.GOOS == "windows":
		t.Skip("install.sh is checked on Linux and macOS")
	case shell != sh && runtime.GOOS != "windows":
		t.Skip("install.ps1 is checked on Windows")
	}
	return machine(t, shell, "")
}

// machine returns a clean machine whatever the system, its home the path sub
// inside a new temporary folder, or the folder itself for "".
func machine(t *testing.T, shell, sub string) *stand {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, sub)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &stand{t: t, shell: shell, home: home, data: filepath.Join(home, ".gentry"), mock: newMock(t), loginShell: "/bin/sh"}
	s.releaseURL = s.mock.releases()
	if runtime.GOOS != "windows" {
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
// the data root, the login shell and the address of the releases of the
// stand, and the variables of the process the case sets or removes.
func (s *stand) environ() []string {
	drop := []string{"HOME", "USERPROFILE", testkit.HomeEnv, releasesEnv, "XDG_CONFIG_HOME", "ZDOTDIR", "SHELL"}
	if s.shell != sh {
		// Let each PowerShell build its own module path: a path inherited
		// from PowerShell 7 hides built-in modules of Windows PowerShell
		// when it is started through an intermediate process such as Go.
		drop = append(drop, "PSModulePath")
	}
	if s.path != "" {
		drop = append(drop, "PATH")
	}
	for name := range s.process {
		drop = append(drop, name)
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(drop, func(d string) bool { return sameEnvName(name, d) }) {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+s.home, "USERPROFILE="+s.home, releasesEnv+"="+s.releaseURL, "SHELL="+s.loginShell)
	if s.gentryHome != "" {
		env = append(env, testkit.HomeEnv+"="+s.gentryHome)
	}
	if s.path != "" {
		env = append(env, "PATH="+s.path)
	}
	for name, value := range s.process {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// setProcess sets a variable of the environment of the process of the
// script; an empty value removes it.
func (s *stand) setProcess(name, value string) {
	if s.process == nil {
		s.process = map[string]string{}
	}
	s.process[name] = value
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
	return s.runScript(cmd)
}

// ps1Encoding makes the console write the output of the script in UTF-8:
// the test reads it from a pipe, a terminal shows it as it is.
const ps1Encoding = "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; "

// sessionGoesOn is what the command after install.ps1 prints.
const sessionGoesOn = "gentry-test: the session goes on"

// runPs1 runs install.ps1 in a session of PowerShell as the user does, then a
// command that prints sessionGoesOn and ends the session with the code in
// $LASTEXITCODE: the exit code of the script is the code it leaves there,
// and the window of the user goes on after the script, so the command after
// it runs. A script that closes the window or stops with an error fails the
// test.
func (s *stand) runPs1(args ...string) result {
	s.t.Helper()
	s.mock.script(s.t, "install.ps1")
	url := s.mock.srv.URL + "/releases/latest/download/install.ps1"
	call := "irm '" + url + "' | iex"
	if len(args) > 0 {
		call = "& ([scriptblock]::Create((irm '" + url + "'))) " + strings.Join(args, " ")
	}
	command := ps1Encoding + call + "; [Console]::Out.WriteLine('" + sessionGoesOn + "'); exit $LASTEXITCODE"
	r := s.runScript(exec.Command(s.shell, "-NoProfile", "-NonInteractive", "-Command", command))
	rest, ok := strings.CutSuffix(r.stdout, sessionGoesOn+"\n")
	if !ok {
		s.t.Errorf("the session of PowerShell did not run the command after the script: the script closed the window or stopped with an error; %s", r)
		return r
	}
	r.stdout = rest
	return r
}

// runScript runs a script of installation in the environment and the home of
// the stand, under the guard of the real home.
func (s *stand) runScript(cmd *exec.Cmd) result {
	s.t.Helper()
	stopIfBroken(s.t)
	cmd.Env = s.environ()
	cmd.Dir = s.home
	r := s.run(cmd)
	checkRealHome(s.t)
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

// onCIRunner reports whether the tests run on a runner of GitHub Actions
// hosted by GitHub: a new machine for each job, whose registry the tests may
// change.
func onCIRunner() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted"
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
	s.prependPath(dir)
}

// fakeRosetta puts first in PATH a sysctl that tells the script it runs
// under Rosetta on a Mac with arm64: sysctl.proc_translated and
// hw.optional.arm64 are 1; it asks the real sysctl for the other names.
func (s *stand) fakeRosetta() {
	s.t.Helper()
	dir := s.t.TempDir()
	writeExecutable(s.t, filepath.Join(dir, "sysctl"), `#!/bin/sh
n= name= value=
for arg do
  case $arg in
    -*) case $arg in *n*) n=1 ;; esac ;;
    sysctl.proc_translated|hw.optional.arm64) name=$arg value=1 ;;
    *) exec /usr/sbin/sysctl "$@" ;;
  esac
done
if [ -n "$n" ]; then echo "$value"; else echo "$name: $value"; fi
`)
	s.prependPath(dir)
}

// prependPath puts the folder first in the PATH of the script.
func (s *stand) prependPath(dir string) {
	path := s.path
	if path == "" {
		path = os.Getenv("PATH")
	}
	s.path = dir + string(os.PathListSeparator) + path
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

// deadReleases points the scripts at an address where nothing listens: port
// 1 of the loopback, which no server of the tests takes.
func (s *stand) deadReleases() {
	s.releaseURL = "http://127.0.0.1:1/releases"
}

// untouched returns a check that the script has changed no profile of a
// shell and not the variable Path of the user: on Linux and macOS .profile
// and .bashrc stay empty and no .zshrc or config.fish appears; on Windows
// the variable Path of the user is what it is now.
func (s *stand) untouched() func() {
	s.t.Helper()
	if s.shell != sh {
		before, err := readUserPath()
		if err != nil {
			s.t.Fatal(err)
		}
		return func() {
			s.t.Helper()
			if after, err := readUserPath(); err != nil || after != before {
				s.t.Errorf("the variable Path of the user: %+v (%v), want it unchanged: %+v", after, err, before)
			}
		}
	}
	return func() {
		s.t.Helper()
		for _, f := range []string{".profile", ".bashrc"} {
			if got := readFile(s.t, filepath.Join(s.home, f)); got != "" {
				s.t.Errorf("~/%s: %q, want empty", f, got)
			}
		}
		wantNo(s.t, filepath.Join(s.home, ".zshrc"))
		wantNo(s.t, filepath.Join(s.home, ".config", "fish"))
	}
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
