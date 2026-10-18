package main

import (
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/git"
)

// The refusals of the checks of the button Publish pages.
type (
	// pagesNotMainError: the button runs on another branch than main.
	pagesNotMainError struct{ branch string }
	// noReleasesError: the repository has no release yet.
	noReleasesError struct{}
)

func (e *pagesNotMainError) Error() string {
	return "the pages are published from main, not " + e.branch
}
func (e *noReleasesError) Error() string { return "there is no release yet" }

// listFile is the page of the site: the list of the releases in its root,
// and the page of a release in the folder of its version.
const listFile = "index.html"

// pages runs the checks of the button Publish pages on the repository dir
// and builds the site of the pages of the released versions from its head
// into the folder out, with no publication: the head of main is checked
// out, and there is a release, a tag v<version>. The site is index.html,
// the list of the releases, and <version>/index.html, the page of each. The
// folder must be empty or not there; on an error the build leaves no file
// in it.
func pages(dir, out string) (err error) {
	branch, err := headBranch(dir)
	if err != nil {
		return err
	}
	if branch != releaseBranch {
		return &pagesNotMainError{branch: branch}
	}
	tags, err := tagList(dir)
	if err != nil {
		return err
	}
	versions := releasedVersions(tags)
	if len(versions) == 0 {
		return &noReleasesError{}
	}
	files, err := site(dir, versions)
	if err != nil {
		return err
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
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// site reads the releases of the versions from the head of the repository
// dir and returns the files of their site by their path apart by /: the
// list and the page of each version.
func site(dir string, versions []version) (map[string][]byte, error) {
	releases, err := readReleases(dir, versions)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, r := range releases {
		b, err := renderPage(r)
		if err != nil {
			return nil, err
		}
		files[path.Join(r.version.String(), listFile)] = b
	}
	b, err := renderList(releases)
	if err != nil {
		return nil, err
	}
	files[listFile] = b
	return files, nil
}

// readReleases reads the section in the journal and the content of the page
// of each version from the commit HEAD of the repository dir and checks
// them, new versions first: the journal has the section of the version; a
// version whose section has «Новое» or «Несовместимые изменения» has the
// content of its page, and one with «Новое» has cards of «Новое» in it; the
// content is written right. The releases are returned in that order.
func readReleases(dir string, versions []version) ([]release, error) {
	versions = slices.SortedFunc(slices.Values(versions), func(a, b version) int { return b.compare(a) })
	journal, err := headFile(dir, changelogFile)
	if err != nil {
		return nil, err
	}
	contents, err := headFiles(dir, contentDir)
	if err != nil {
		return nil, err
	}
	var releases []release
	for _, v := range versions {
		s, ok := readSection(journal, v)
		if !ok {
			return nil, &noSectionError{version: v}
		}
		r := release{version: v, section: s}
		file := contentFile(v)
		if !contents[file] {
			if s.has(journalNew) || s.has(journalBreaking) {
				return nil, &contentMissingError{version: v, file: file}
			}
			releases = append(releases, r)
			continue
		}
		text, err := showHead(dir, file)
		if err != nil {
			return nil, err
		}
		if r.content, err = parseContent(file, text); err != nil {
			return nil, err
		}
		if s.has(journalNew) && len(r.content.cards) == 0 {
			return nil, &contentInvalidError{file: file, reason: reasonNoNew}
		}
		releases = append(releases, r)
	}
	return releases, nil
}

// headFile returns the text of the file, a path apart by /, in the commit
// HEAD of the repository dir, and an empty text when the commit has no such
// file. An error of git is returned.
func headFile(dir, name string) (string, error) {
	files, err := headFiles(dir, name)
	if err != nil {
		return "", err
	}
	if !files[name] {
		return "", nil
	}
	return showHead(dir, name)
}

// showHead returns the text of the file, a path apart by /, in the commit
// HEAD of the repository dir; the commit has it.
func showHead(dir, name string) (string, error) {
	return git.Run(dir, git.Opts{}, "show", "HEAD:"+name)
}

// headFiles returns the files at the path at, apart by /, in the commit
// HEAD of the repository dir: the file, or those in the folder and in its
// folders.
func headFiles(dir, at string) (map[string]bool, error) {
	out, err := git.Run(dir, git.Opts{}, "ls-tree", "-r", "--name-only", "-z", "HEAD", "--", at)
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, name := range strings.Split(out, "\x00") {
		if name != "" {
			files[name] = true
		}
	}
	return files, nil
}

// headBranch returns the branch of the work tree of the repository dir:
// HEAD when it is on no branch.
func headBranch(dir string) (string, error) {
	branch, err := git.Branch(dir)
	if err != nil {
		return "", err
	}
	if branch == "" {
		branch = detached
	}
	return branch, nil
}

// tagList returns the tags of the repository dir.
func tagList(dir string) ([]string, error) {
	out, err := git.Run(dir, git.Opts{}, "tag", "--list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
