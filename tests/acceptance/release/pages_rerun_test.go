package release_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/t8nax/gentry/tests/testkit"
)

// The steps of a workflow with the inputs of their actions, with:, which
// the tests of the build of the pages from main and of the keeping of the
// site read.
type (
	withWorkflow struct {
		Jobs map[string]struct {
			Steps []withStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	withStep struct {
		Uses string         `yaml:"uses"`
		Run  string         `yaml:"run"`
		With map[string]any `yaml:"with"`
	}
)

// readWithWorkflow reads the steps of a workflow of the module with their
// inputs.
func readWithWorkflow(t *testing.T, name string) withWorkflow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testkit.ModuleRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("the module has no workflow %s: %v", name, err)
	}
	var w withWorkflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return w
}

// input returns an input of the action of a step as text, "" when there is
// none.
func input(s withStep, name string) string {
	v, ok := s.With[name]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// TestPagesBuildFromMain checks that in the workflow Release the job of the
// build of the pages gets the code of the branch main with all the tags,
// not the commit the run is pinned to: a checkout with ref: main and the
// whole history (fetch-depth: 0) or the tags (fetch-tags: true) before the
// step release pages.
func TestPagesBuildFromMain(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWithWorkflow(t, releaseWorkflow)
	builds := 0
	for name, j := range w.Jobs {
		for i, s := range j.Steps {
			if !strings.Contains(s.Run, "release pages") {
				continue
			}
			builds++
			var checkout *withStep
			for k := i - 1; k >= 0; k-- {
				if strings.HasPrefix(j.Steps[k].Uses, "actions/checkout@") {
					checkout = &j.Steps[k]
					break
				}
			}
			switch {
			case checkout == nil:
				t.Errorf("%s: the job %s builds the pages with no checkout of the code before it", releaseWorkflow, name)
			case input(*checkout, "ref") != "main":
				t.Errorf("%s: the job %s builds the pages from %q, want the branch main: ref: main", releaseWorkflow, name, input(*checkout, "ref"))
			case input(*checkout, "fetch-depth") != "0" && input(*checkout, "fetch-tags") != "true":
				t.Errorf("%s: the checkout of the job %s gets not all the tags: want fetch-depth: 0 or fetch-tags: true, with: %v", releaseWorkflow, name, checkout.With)
			}
		}
	}
	if builds == 0 {
		t.Errorf("%s has no step of the build of the pages: release pages", releaseWorkflow)
	}
}

// TestPagesArtifactRetention checks that in both workflows the site built
// for the publication is kept for 7 days: retention-days: 7 of the step of
// actions/upload-pages-artifact.
func TestPagesArtifactRetention(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	for _, name := range []string{releaseWorkflow, pagesWorkflow} {
		t.Run(strings.TrimSuffix(name[strings.LastIndex(name, "/")+1:], ".yml"), func(t *testing.T) {
			w := readWithWorkflow(t, name)
			uploads := 0
			for jn, j := range w.Jobs {
				for i, s := range j.Steps {
					if !strings.HasPrefix(s.Uses, "actions/upload-pages-artifact@") {
						continue
					}
					uploads++
					if got := input(s, "retention-days"); got != "7" {
						t.Errorf("%s: the step %d of the job %s keeps the site %q days, want retention-days: 7", name, i+1, jn, got)
					}
				}
			}
			if uploads == 0 {
				t.Errorf("%s has no step that keeps the site for the publication: actions/upload-pages-artifact", name)
			}
		})
	}
}
