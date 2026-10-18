// Package releases knows the releases of Gentry on GitHub as the scripts of
// installation do: it finds the latest release by the redirect of
// /releases/latest, downloads the program of the platform and checksums.txt
// of a release into a folder and checks the SHA-256 of the program, reads
// the journal of changes on the tag of a release, and puts a program in
// place of the program of the data root under the lock of updates. The
// decisions of an update are those of the package update.
//
// The package knows nothing of the words for the operator: its errors are
// types with what the command needs to tell them.
package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GitHub is the address of the releases of Gentry. The texts for the operator
// name it, whatever ReleasesEnv is.
const GitHub = "https://github.com/t8nax/gentry/releases"

// ReleasesEnv replaces the address the files are downloaded from, as for the
// scripts of installation: the tests point it at a mock release.
const ReleasesEnv = "GENTRY_RELEASES_URL"

// Pages is the site of the pages of the releases; the page of a version is
// Pages + "<version>/".
const Pages = "https://t8nax.github.io/gentry/"

// The commands of the scripts of installation of the latest release.
const (
	InstallSh  = "curl -fsSL " + GitHub + "/latest/download/install.sh | sh"
	InstallPs1 = "irm " + GitHub + "/latest/download/install.ps1 | iex"
)

// InstallCommand returns the command of installation of this platform.
func InstallCommand() string {
	if runtime.GOOS == "windows" {
		return InstallPs1
	}
	return InstallSh
}

// Page returns the address of the page of the release of version v.
func Page(v string) string { return Pages + v + "/" }

// Releases returns the address of the releases the files are downloaded
// from: ReleasesEnv, or GitHub.
func Releases() string {
	if r := os.Getenv(ReleasesEnv); r != "" {
		return strings.TrimRight(r, "/")
	}
	return GitHub
}

// Program returns the name of the program of Gentry in the folder bin of the
// data root on this platform: gentry, or gentry.exe on Windows.
func Program() string {
	if runtime.GOOS == "windows" {
		return "gentry.exe"
	}
	return "gentry"
}

// Asset returns the name of the program of the platform goos/goarch in a
// release: gentry-<os>-<arch>, with .exe on Windows.
func Asset(goos, goarch string) string {
	name := "gentry-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// releaseVersion is the version of a release: X.Y.Z, with no leading zeros.
var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// IsRelease reports whether v is the version of a release, X.Y.Z: not a
// pseudo-version of a build from the sources, 0.0.0-dev or a version with
// +dirty.
func IsRelease(v string) bool { return releaseVersion.MatchString(v) }

// Compare compares the versions of releases a and b by semver: -1 if a is
// less, 0 if equal, +1 if greater.
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func parts(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

// NotFoundError means there is no such release: no latest one, or no
// checksums.txt of the version. Version is "" for the latest.
type NotFoundError struct{ Version string }

func (e *NotFoundError) Error() string {
	if e.Version == "" {
		return "no latest release"
	}
	return "no release " + e.Version
}

// DownloadError means a file of the address URL did not download.
type DownloadError struct {
	URL string
	Err error
}

func (e *DownloadError) Error() string { return e.URL + ": " + e.Err.Error() }
func (e *DownloadError) Unwrap() error { return e.Err }

// ChecksumError means the program downloaded does not have the sum
// checksums.txt of the release gives it.
type ChecksumError struct {
	File string // the name of the program in the release
	Want string // the sum of checksums.txt, "" if it names no such file
	Got  string // the sum of the file downloaded
}

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("%s: sha256 %s, want %s", e.File, e.Got, e.Want)
}

// WriteError means the program of the data root could not be written.
type WriteError struct {
	Program string
	Err     error
	// Restore is why the program renamed aside could not get its name
	// back: it is <program>.old; nil if it has its name.
	Restore error
}

func (e *WriteError) Error() string {
	if e.Restore != nil {
		return e.Program + ": " + e.Err.Error() + "; " + e.Restore.Error()
	}
	return e.Program + ": " + e.Err.Error()
}
func (e *WriteError) Unwrap() error { return e.Err }

// A Client downloads the releases of the address Releases.
type Client struct {
	Releases string
	HTTP     *http.Client
}

// New returns a client of the releases of Releases(). Redirects are followed,
// as to the storage of the files of GitHub; a server that does not answer
// fails the request in a minute.
func New() *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = time.Minute
	return &Client{Releases: Releases(), HTTP: &http.Client{Transport: t, Timeout: 30 * time.Minute}}
}

// tagPath is the path of the page of a release the redirect of
// /releases/latest leads to: its group is the version.
var tagPath = regexp.MustCompile(`/tag/v([^/?#]+)$`)

// Latest returns the version of the latest release: /releases/latest
// redirects to the page of its tag, /releases/tag/v<version>.
func (c *Client) Latest(ctx context.Context) (string, error) {
	addr := c.Releases + "/latest"
	resp, err := c.get(ctx, addr)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", &NotFoundError{}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &DownloadError{URL: addr, Err: statusError(resp)}
	}
	m := tagPath.FindStringSubmatch(resp.Request.URL.Path)
	if m == nil || !IsRelease(m[1]) {
		return "", &NotFoundError{}
	}
	return m[1], nil
}

// Download downloads checksums.txt and the program asset of the release v
// into dir, checks the sum of the program and returns its path.
func (c *Client) Download(ctx context.Context, v, asset, dir string) (string, error) {
	base := c.Releases + "/download/v" + v
	checksums, err := c.fetch(ctx, base+"/checksums.txt", filepath.Join(dir, "checksums.txt"))
	var nf *httpNotFound
	if errors.As(err, &nf) {
		return "", &NotFoundError{Version: v}
	}
	if err != nil {
		return "", err
	}
	program := filepath.Join(dir, asset)
	got, err := c.fetch(ctx, base+"/"+asset, program)
	if errors.As(err, &nf) {
		return "", &DownloadError{URL: nf.url, Err: nf.err}
	}
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(checksums.path)
	if err != nil {
		return "", err
	}
	want := checksumOf(string(b), asset)
	if want != got.sum {
		return "", &ChecksumError{File: asset, Want: want, Got: got.sum}
	}
	return program, nil
}

// checksumLine is a line of checksums.txt: the sum and the name of a file,
// apart by a space and a space or *, as sha256sum writes them.
var checksumLine = regexp.MustCompile(`^([0-9a-fA-F]{64}) [ *](.+)$`)

// checksumOf returns the sum checksums.txt gives the file name, in lower
// case; "" if it gives none.
func checksumOf(checksums, name string) string {
	for _, l := range strings.Split(strings.ReplaceAll(checksums, "\r\n", "\n"), "\n") {
		if m := checksumLine.FindStringSubmatch(l); m != nil && m[2] == name {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

// maxChangelog limits the journal of changes read.
const maxChangelog = 4 << 20

// ChangelogURL returns the address of the journal of changes on the tag of
// the version v in the repository: the address of the releases without
// /releases, then /raw/v<version>/CHANGELOG.md.
func (c *Client) ChangelogURL(v string) string {
	return strings.TrimSuffix(c.Releases, "/releases") + "/raw/v" + v + "/CHANGELOG.md"
}

// Changelog returns the journal of changes on the tag of the version v.
func (c *Client) Changelog(ctx context.Context, v string) (string, error) {
	addr := c.ChangelogURL(v)
	resp, err := c.get(ctx, addr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &DownloadError{URL: addr, Err: statusError(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxChangelog))
	if err != nil {
		return "", &DownloadError{URL: addr, Err: reason(err)}
	}
	return string(b), nil
}

// get requests the address; a request that gets no answer is a
// DownloadError.
func (c *Client) get(ctx context.Context, addr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, &DownloadError{URL: addr, Err: err}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &DownloadError{URL: addr, Err: reason(err)}
	}
	return resp, nil
}

// fetched is a file downloaded: its path and its sum.
type fetched struct {
	path, sum string
}

// httpNotFound is a file the server has not: it answered 404.
type httpNotFound struct {
	url string
	err error
}

func (e *httpNotFound) Error() string { return e.url + ": " + e.err.Error() }

// fetch downloads the address into the file and returns its SHA-256.
func (c *Client) fetch(ctx context.Context, addr, file string) (fetched, error) {
	resp, err := c.get(ctx, addr)
	if err != nil {
		return fetched{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fetched{}, &httpNotFound{url: addr, err: statusError(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		return fetched{}, &DownloadError{URL: addr, Err: statusError(resp)}
	}
	f, err := os.Create(file)
	if err != nil {
		return fetched{}, err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil && cerr != nil {
		return fetched{}, cerr
	}
	if err != nil {
		return fetched{}, &DownloadError{URL: addr, Err: reason(err)}
	}
	return fetched{path: file, sum: hex.EncodeToString(h.Sum(nil))}, nil
}

// statusError is the answer of a server that did not give the file.
func statusError(resp *http.Response) error { return errors.New("HTTP " + resp.Status) }

// reason is what went wrong with a request, without the address the
// DownloadError names apart.
func reason(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
