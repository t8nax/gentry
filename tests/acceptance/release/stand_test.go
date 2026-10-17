package release_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/tests/testkit"
)

// programPackage is the program of release, as a package of the module.
const programPackage = "./cmd/release"

// exe is the suffix of a program on the platform of the stand.
var exe = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// program holds the program of release, built once for the package from the
// module under test.
var program struct {
	once sync.Once
	dir  string
	path string
	err  error
}

// releaseProgram returns the path of the program of release.
func releaseProgram(t *testing.T) string {
	t.Helper()
	root := testkit.ModuleRoot(t)
	program.once.Do(func() {
		if program.dir, program.err = os.MkdirTemp("", "gentry-release-"); program.err != nil {
			return
		}
		program.path = filepath.Join(program.dir, "release"+exe)
		cmd := exec.Command("go", "build", "-o", program.path, programPackage)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			program.err = fmt.Errorf("the module has no program of release %s: go build: %v\n%s", programPackage, err, out)
		}
	})
	if program.err != nil {
		t.Fatal(program.err)
	}
	return program.path
}

// removeProgram removes the program of release after the tests.
func removeProgram() {
	if program.dir != "" {
		os.RemoveAll(program.dir)
	}
}

// A result is what a run of the program of release gave.
type result struct {
	code           int
	stdout, stderr string
}

func (r result) String() string {
	return fmt.Sprintf("exit code %d, stdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// release runs the program of release with args in the repository dir.
func release(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(releaseProgram(t), args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		t.Fatalf("%s: %v", cmd, err)
	}
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

// A repo is a temporary git repository of the button, on main.
type repo struct {
	t   *testing.T
	dir string
}

// newRepo makes an empty repository on main for the test.
func newRepo(t *testing.T) *repo {
	t.Helper()
	testkit.SkipOnBinary(t)
	t.Parallel()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "--quiet", "--initial-branch=main")
	return &repo{t: t, dir: dir}
}

// commit writes the files and commits all the changes of the work tree.
func (r *repo) commit(files map[string]string) {
	r.t.Helper()
	testkit.WriteFiles(r.t, r.dir, files)
	gittest.Run(r.t, r.dir, "add", "--all")
	gittest.Run(r.t, r.dir, "commit", "--quiet", "--message", "change")
}

// tag tags the commit of the work tree with v<version>.
func (r *repo) tag(version string) {
	r.t.Helper()
	gittest.Run(r.t, r.dir, "tag", "v"+version)
}

// branch moves the work tree to a new branch.
func (r *repo) branch(name string) {
	r.t.Helper()
	gittest.Run(r.t, r.dir, "checkout", "--quiet", "-b", name)
}

// check runs the checks of the button for the version.
func (r *repo) check(version string) result {
	r.t.Helper()
	return release(r.t, r.dir, "check", version)
}

// wantTags checks that the repository has the tags and no other.
func (r *repo) wantTags(tags ...string) {
	r.t.Helper()
	got := gittest.Run(r.t, r.dir, "tag", "--list")
	want := ""
	if len(tags) > 0 {
		want = testkit.Lines(tags...)
	}
	if got != want {
		r.t.Errorf("tags:\n%s\nwant:\n%s", got, want)
	}
}

// wantRefusal checks that the run refused with the lines on the standard
// error.
func wantRefusal(t *testing.T, r result, want ...string) {
	t.Helper()
	if r.code != 1 || r.stderr != testkit.Lines(want...) {
		t.Errorf("%s\nwant exit code 1, stderr:\n%s", r, testkit.Lines(want...))
	}
}

// The journal of changes of the shop.
const (
	changelogFile = "CHANGELOG.md"
	journalTitle  = "# Журнал изменений\n\n## Не выпущено\n"
)

// changelog returns a journal of changes with an empty section of the
// unreleased changes and the sections; an empty section is none.
func changelog(sections ...string) string {
	s := journalTitle
	for _, sec := range sections {
		if sec != "" {
			s += "\n" + sec
		}
	}
	return s
}

// section returns the section of a version: its heading, the phrase about
// the main thing and the section «Новое» with the entries.
func section(version, date, phrase string, entries ...string) string {
	s := "## " + version + " — " + date + "\n\n" + phrase + "\n\n### Новое\n"
	for _, e := range entries {
		s += "* " + e + "\n"
	}
	return s
}

// fixes returns the section of a version with only the section
// «Исправлено»: such a release needs no content of its page.
func fixes(version, date, phrase string, entries ...string) string {
	s := "## " + version + " — " + date + "\n\n" + phrase + "\n\n### Исправлено\n"
	for _, e := range entries {
		s += "* " + e + "\n"
	}
	return s
}

// The sections of the versions of the shop.
var (
	section010 = section("0.1.0", "2026-10-17", "Первый выпуск магазина.", "Добавлен каталог.")
	section015 = section("0.1.5", "2026-10-21", "Каталог открывается быстрее.", "Добавлен поиск по каталогу.")
	section020 = section("0.2.0", "2026-10-20", "Корзина появилась.", "Добавлена корзина.")
	section021 = fixes("0.2.1", "2026-10-22", "Корзина считает скидку.", "Исправлена скидка в корзине.")
	section030 = section("0.3.0", "2026-11-02", "Корзина сохраняется после входа.", "Добавлен список желаний.")
)

// contentDir is the folder of the contents of the pages of the releases,
// in the root of the repository: releases/<version>.md.
const contentDir = "releases/"

// content is a right content of the page of a release: the section «Новое»
// with one card of one example.
const content = "# Новое\n\n## Каталог\n\nКаталог открывается быстрее.\n\n```\n$ shop catalog\n+ Товаров: 120\n```\n"

// sectionVersion matches the heading of the section of a version in the
// journal and gives the version.
var sectionVersion = regexp.MustCompile(`(?m)^## (\d+\.\d+\.\d+) — \d{4}-\d{2}-\d{2}$`)

// withContents adds to the files a right content of the page of each
// version whose section in the journal of the files has «Новое» or
// «Несовместимые изменения» and that the files give no content of. A
// release needs the content of its page then, and the checks of the button
// check the pages of all the released versions.
func withContents(files map[string]string) map[string]string {
	journal := files[changelogFile]
	out := map[string]string{}
	for name, text := range files {
		out[name] = text
	}
	at := sectionVersion.FindAllStringSubmatchIndex(journal, -1)
	for i, m := range at {
		end := len(journal)
		if i+1 < len(at) {
			end = at[i+1][0]
		}
		body := journal[m[1]:end]
		if !strings.Contains(body, "\n### Новое\n") && !strings.Contains(body, "\n### Несовместимые изменения\n") {
			continue
		}
		name := contentDir + journal[m[2]:m[3]] + ".md"
		if _, ok := out[name]; !ok {
			out[name] = content
		}
	}
	return out
}

// released makes a repository on main with the release last tagged and a
// change of the code after it; the journal of the work tree has the
// sections of the versions next and last, and the versions have the
// contents of their pages they need.
func released(t *testing.T, last, lastSection, nextSection string) *repo {
	t.Helper()
	r := newRepo(t)
	r.commit(withContents(map[string]string{"cart.go": "package cart\n", changelogFile: changelog(lastSection)}))
	r.tag(last)
	r.commit(withContents(map[string]string{"cart.go": "package cart\n\nconst Limit = 10\n", changelogFile: changelog(nextSection, lastSection)}))
	return r
}

// moduleCopy makes a repository of the files of the module under test, as
// they are in its work tree, committed on main and tagged v<version>.
func moduleCopy(t *testing.T, version string) *repo {
	t.Helper()
	r := newRepo(t)
	root := testkit.ModuleRoot(t)
	files := gittest.Run(t, root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	for _, name := range strings.Split(strings.TrimSuffix(files, "\x00"), "\x00") {
		from := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(from)
		if errors.Is(err, os.ErrNotExist) {
			continue // deleted in the work tree
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		copyFile(t, from, filepath.Join(r.dir, filepath.FromSlash(name)), info.Mode().Perm())
	}
	gittest.Run(t, r.dir, "add", "--all")
	gittest.Run(t, r.dir, "commit", "--quiet", "--message", "release")
	r.tag(version)
	return r
}

func copyFile(t *testing.T, from, to string, perm os.FileMode) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// build runs the build of the release of the version into a new folder and
// returns the folder.
func (r *repo) build(version string) (string, result) {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "release")
	return dir, release(r.t, r.dir, "build", version, dir)
}

// pages runs the checks of the button Publish pages and the build of the
// site of the pages from the head of the repository into a new folder, with
// no publication, and returns the folder.
func (r *repo) pages() (string, result) {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "site")
	return dir, release(r.t, r.dir, "pages", dir)
}
