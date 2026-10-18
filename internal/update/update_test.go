package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"0.1.0": true, "10.20.30": true,
		"0.1": false, "v0.1.0": false, "01.0.0": false, "0.3.0+dirty": false, "0.0.0-dev": false,
		"0.1.1-0.20261018105010-5314442d7894": false, "": false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0}, {"0.1.0", "0.2.0", -1}, {"0.10.0", "0.9.0", 1}, {"1.0.0", "0.99.99", 1}, {"0.3.1", "0.3.10", -1},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAsset(t *testing.T) {
	if got := Asset("linux", "arm64"); got != "gentry-linux-arm64" {
		t.Errorf("Asset(linux, arm64) = %s", got)
	}
	if got := Asset("windows", "amd64"); got != "gentry-windows-amd64.exe" {
		t.Errorf("Asset(windows, amd64) = %s", got)
	}
}

func TestReleases(t *testing.T) {
	t.Setenv(ReleasesEnv, "")
	if got := Releases(); got != GitHub {
		t.Errorf("Releases() = %s, want %s", got, GitHub)
	}
	t.Setenv(ReleasesEnv, "http://127.0.0.1:8080/releases/")
	if got := Releases(); got != "http://127.0.0.1:8080/releases" {
		t.Errorf("Releases() with %s = %s", ReleasesEnv, got)
	}
	c := &Client{Releases: GitHub}
	if got, want := c.ChangelogURL("0.3.0"), "https://github.com/t8nax/gentry/raw/v0.3.0/CHANGELOG.md"; got != want {
		t.Errorf("ChangelogURL = %s, want %s", got, want)
	}
	if got, want := Page("0.3.0"), "https://t8nax.github.io/gentry/0.3.0/"; got != want {
		t.Errorf("Page = %s, want %s", got, want)
	}
	// The commands of the scripts of installation name GitHub whatever
	// ReleasesEnv is.
	want := "curl -fsSL https://github.com/t8nax/gentry/releases/latest/download/install.sh | sh"
	if runtime.GOOS == "windows" {
		want = "irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex"
	}
	if got := InstallCommand(); got != want {
		t.Errorf("InstallCommand() = %s, want %s", got, want)
	}
}

// A release is a mock of the releases at the paths of GitHub.
type release struct {
	files    map[string][]byte // by v<version>/<file>
	latest   string            // "" answers 404 to /latest
	journals map[string]string // by version
}

func (r *release) serve(w http.ResponseWriter, req *http.Request) {
	p := req.URL.Path
	switch {
	case p == "/releases/latest":
		if r.latest == "" {
			http.NotFound(w, req)
			return
		}
		http.Redirect(w, req, "/releases/tag/v"+r.latest, http.StatusFound)
	case p == "/releases/tag/elsewhere":
		w.Write([]byte("elsewhere"))
	case strings.HasPrefix(p, "/releases/tag/"):
		w.Write([]byte("release"))
	case strings.HasPrefix(p, "/releases/download/"):
		b, ok := r.files[strings.TrimPrefix(p, "/releases/download/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(b)
	case strings.HasPrefix(p, "/raw/v"):
		j, ok := r.journals[strings.TrimSuffix(strings.TrimPrefix(p, "/raw/v"), "/CHANGELOG.md")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write([]byte(j))
	default:
		http.NotFound(w, req)
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// mock serves a release 0.3.0 of the program gentry-linux-amd64.
func mock(t *testing.T) (*release, *Client) {
	t.Helper()
	program := []byte("gentry 0.3.0\n")
	r := &release{latest: "0.3.0", files: map[string][]byte{
		"v0.3.0/gentry-linux-amd64": program,
		"v0.3.0/checksums.txt":      []byte(sum([]byte("other")) + "  gentry-darwin-arm64\n" + sum(program) + "  gentry-linux-amd64\n"),
	}, journals: map[string]string{"0.3.0": "# Журнал изменений\n"}}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(srv.Close)
	return r, &Client{Releases: srv.URL + "/releases", HTTP: srv.Client()}
}

func TestLatest(t *testing.T) {
	r, c := mock(t)
	ctx := context.Background()
	if v, err := c.Latest(ctx); err != nil || v != "0.3.0" {
		t.Errorf("Latest = %q, %v; want 0.3.0", v, err)
	}
	r.latest = ""
	var nf *NotFoundError
	if _, err := c.Latest(ctx); !errors.As(err, &nf) || nf.Version != "" {
		t.Errorf("Latest with no release: %v, want NotFoundError of the latest", err)
	}
	// /latest that leads to no page of a tag of a release, as on GitHub with
	// no release.
	r.latest = "x"
	if _, err := c.Latest(ctx); !errors.As(err, &nf) {
		t.Errorf("Latest led to a tag that is no version: %v, want NotFoundError", err)
	}

	down := &Client{Releases: "http://127.0.0.1:1/releases", HTTP: http.DefaultClient}
	var de *DownloadError
	if _, err := down.Latest(ctx); !errors.As(err, &de) || de.URL != "http://127.0.0.1:1/releases/latest" || de.Err == nil || strings.Contains(de.Err.Error(), "127.0.0.1:1/releases") {
		t.Errorf("Latest with no server: %v, want DownloadError of the address with the reason apart", err)
	}
}

func TestDownload(t *testing.T) {
	r, c := mock(t)
	ctx := context.Background()
	dir := t.TempDir()
	p, err := c.Download(ctx, "0.3.0", "gentry-linux-amd64", dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "gentry 0.3.0\n" || filepath.Dir(p) != dir {
		t.Errorf("Download put %q at %s", b, p)
	}

	// No checksums.txt: no such release.
	var nf *NotFoundError
	if _, err := c.Download(ctx, "0.2.0", "gentry-linux-amd64", t.TempDir()); !errors.As(err, &nf) || nf.Version != "0.2.0" {
		t.Errorf("Download of no release: %v, want NotFoundError of 0.2.0", err)
	}
	// No program of the platform: it did not download.
	var de *DownloadError
	if _, err := c.Download(ctx, "0.3.0", "gentry-linux-arm64", t.TempDir()); !errors.As(err, &de) || !strings.HasSuffix(de.URL, "/download/v0.3.0/gentry-linux-arm64") {
		t.Errorf("Download of no program: %v, want DownloadError of its address", err)
	}

	want := sum(r.files["v0.3.0/gentry-linux-amd64"])
	r.files["v0.3.0/gentry-linux-amd64"] = []byte("corrupted\n")
	var ce *ChecksumError
	if _, err := c.Download(ctx, "0.3.0", "gentry-linux-amd64", t.TempDir()); !errors.As(err, &ce) ||
		ce.File != "gentry-linux-amd64" || ce.Want != want || ce.Got != sum([]byte("corrupted\n")) {
		t.Errorf("Download of a corrupted program: %v, want ChecksumError", err)
	}
}

func TestChangelog(t *testing.T) {
	r, c := mock(t)
	ctx := context.Background()
	if j, err := c.Changelog(ctx, "0.3.0"); err != nil || j != r.journals["0.3.0"] {
		t.Errorf("Changelog = %q, %v", j, err)
	}
	var de *DownloadError
	if _, err := c.Changelog(ctx, "0.2.0"); !errors.As(err, &de) || !strings.Contains(err.Error(), "404") {
		t.Errorf("Changelog of no tag: %v, want DownloadError with 404", err)
	}
}

func TestBreakingOf(t *testing.T) {
	journal := strings.Join([]string{
		"# Журнал изменений",
		"",
		"## Не выпущено",
		"",
		"### Несовместимые изменения",
		"* Не выпущенное изменение.",
		"",
		"## 0.4.0 — 2026-11-09",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.4.0.",
		"",
		"## 0.3.0 — 2026-11-02",
		"",
		"Путь задачи показывает отдельная команда.",
		"",
		"### Несовместимые изменения",
		"* Команда `gentry task show --path` заменена",
		"  на `gentry task path`. Скрипты с прежней",
		"  командой нужно поправить.",
		"* Второе изменение.",
		"",
		"### Новое",
		"* Новое 0.3.0.",
		"",
		"## 0.2.1 — 2026-10-30",
		"",
		"### Исправлено",
		"* Исправление.",
		"",
		"## 0.2.0 — 2026-10-26",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.2.0.",
		"",
		"## 0.1.0 — 2026-10-20",
		"",
		"### Несовместимые изменения",
		"* Изменение 0.1.0.",
	}, "\r\n")
	got := BreakingOf(journal, "0.1.0", "0.3.0")
	want := []Breaking{
		{"0.2.0", []string{"Изменение 0.2.0."}},
		{"0.3.0", []string{"Команда `gentry task show --path` заменена на `gentry task path`. Скрипты с прежней командой нужно поправить.", "Второе изменение."}},
	}
	if !slices.EqualFunc(got, want, func(a, b Breaking) bool { return a.Version == b.Version && slices.Equal(a.Lines, b.Lines) }) {
		t.Errorf("BreakingOf 0.1.0 → 0.3.0:\n%q\nwant:\n%q", got, want)
	}
	if got := BreakingOf(journal, "0.2.0", "0.2.1"); len(got) != 0 {
		t.Errorf("BreakingOf 0.2.0 → 0.2.1 = %q, want none", got)
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, Program())
	downloaded := filepath.Join(t.TempDir(), "gentry-new")
	os.WriteFile(program, []byte("old"), 0o755)
	os.WriteFile(downloaded, []byte("new"), 0o644)
	if err := Replace(downloaded, program); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(program)
	fi, _ := os.Stat(program)
	if string(b) != "new" || runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("the program after Replace: %q, mode %v", b, fi.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder of the program after Replace: %v, want the program alone", entries)
	}

	var we *WriteError
	if err := Replace(filepath.Join(dir, "missing"), program); !errors.As(err, &we) || we.Program != program {
		t.Errorf("Replace of no file: %v, want WriteError of the program", err)
	}
	if b, _ := os.ReadFile(program); string(b) != "new" {
		t.Errorf("a failed Replace changed the program: %q", b)
	}
}

func TestRemoveOld(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a running program is renamed to .old only on Windows")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, Program())
	for _, n := range []string{Program(), Program() + ".old", Program() + ".1a2b3c4d.old", "other.old"} {
		os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644)
	}
	RemoveOld(program)
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{Program(), "other.old"}; !slices.Equal(names, want) {
		t.Errorf("after RemoveOld: %q, want %q", names, want)
	}
}

func TestLock(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, Program())
	l, err := Lock(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	var we *WriteError
	if _, err := Lock(context.Background(), filepath.Join(dir, "missing", Program())); !errors.As(err, &we) {
		t.Errorf("Lock in no folder: %v, want WriteError", err)
	}
}
