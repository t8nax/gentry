package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/t8nax/gentry/contract"
)

// A platform is a platform a release has a program for.
type platform struct{ os, arch string }

// platforms are the platforms of a release.
var platforms = []platform{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// program is the name of the program of the platform in a release:
// gentry-<os>-<arch>, with .exe on Windows.
func (p platform) program() string {
	name := "gentry-" + p.os + "-" + p.arch
	if p.os == "windows" {
		name += ".exe"
	}
	return name
}

// The parts of a release besides the programs.
const (
	checksumsFile = "checksums.txt"
	gentryPackage = "./cmd/gentry"
	scriptsDir    = "install"
)

// scripts are the scripts of installation, in scriptsDir of the repository.
var scripts = []string{"install.sh", "install.ps1"}

// versionMismatchError: the program built names another version than that
// of the release.
type versionMismatchError struct {
	want version
	got  string
}

func (e *versionMismatchError) Error() string {
	return "the program built names the version " + e.got + ", not " + e.want.String()
}

// build builds the files of the release of the version value from the
// repository dir into the folder out: the programs of the platforms, the
// sums of the programs in checksums.txt and the scripts of installation. Go
// stamps the version into the programs from the tag of the commit, so the
// commit must have the tag v<version> and the work tree no change: build
// checks that the program of the platform it runs on names the version. On
// an error it leaves no file in out.
func build(dir, value, out string) (err error) {
	v, ok := parseVersion(value)
	if !ok {
		return &versionFormatError{value: value}
	}
	made, err := makeFolder(out)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			clean(out, made)
		}
	}()
	sums := map[string]string{} // by the name of the program
	for _, p := range platforms {
		file := filepath.Join(out, p.program())
		if err := goBuild(dir, p, file); err != nil {
			return err
		}
		if sums[p.program()], err = fileSum(file); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(out, checksumsFile), checksums(sums), 0o644); err != nil {
		return err
	}
	for _, s := range scripts {
		b, err := os.ReadFile(filepath.Join(dir, scriptsDir, s))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, s), b, 0o644); err != nil {
			return err
		}
	}
	host := platform{runtime.GOOS, runtime.GOARCH}
	if !slices.Contains(platforms, host) {
		return fmt.Errorf("the release has no program of %s/%s, where the build runs, to check its version", host.os, host.arch)
	}
	got, err := programVersion(filepath.Join(out, host.program()))
	if err != nil {
		return err
	}
	if got != v.String() {
		return &versionMismatchError{want: v, got: got}
	}
	return nil
}

// makeFolder makes the folder of the release, or takes an empty one, and
// reports whether it made it.
func makeFolder(out string) (made bool, err error) {
	entries, err := os.ReadDir(out)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, os.MkdirAll(out, 0o755)
	case err != nil:
		return false, err
	case len(entries) > 0:
		return false, fmt.Errorf("the folder of the release %s is not empty", out)
	}
	return false, nil
}

// clean removes the files of the release from the folder out, and the
// folder if the build made it.
func clean(out string, made bool) {
	if made {
		os.RemoveAll(out)
		return
	}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(out, e.Name()))
	}
}

// goBuild builds gentry of the repository dir for the platform into the
// file, as CI builds it: a static program without the paths of the machine
// and without the tables of debugging.
func goBuild(dir string, p platform, file string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", file, gentryPackage)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+p.os, "GOARCH="+p.arch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build for %s/%s: %v\n%s", p.os, p.arch, err, bytes.TrimSpace(out))
	}
	return nil
}

// fileSum returns the SHA-256 of the file in lowercase hex, as sha256sum
// prints it.
func fileSum(file string) (string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// checksums returns checksums.txt of the sums by the name of the file: a
// line «<sum>  <name>» per file, by name, as sha256sum prints them.
func checksums(sums map[string]string) []byte {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(sums)) {
		b.WriteString(sums[name] + "  " + name + "\n")
	}
	return []byte(b.String())
}

// programVersion returns the version the program names on version --json.
func programVersion(program string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(program, "version", "--json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s version --json: %v\n%s", program, err, bytes.TrimSpace(stderr.Bytes()))
	}
	var out contract.VersionOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return "", fmt.Errorf("%s version --json: %v", program, err)
	}
	return out.Gentry, nil
}
