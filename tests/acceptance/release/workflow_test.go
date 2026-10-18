package release_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/t8nax/gentry/tests/testkit"
)

// The workflows of the module: the button Release, CI, whose tests the
// button runs as a reusable workflow, and the button Publish pages.
const (
	releaseWorkflow = ".github/workflows/release.yml"
	ciWorkflow      = ".github/workflows/ci.yml"
	pagesWorkflow   = ".github/workflows/pages.yml"
)

// A workflow is what the test reads of a workflow of GitHub Actions.
type workflow struct {
	Name        string         `yaml:"name"`
	On          any            `yaml:"on"`
	Permissions any            `yaml:"permissions"`
	Concurrency any            `yaml:"concurrency"`
	Env         any            `yaml:"env"`
	Jobs        map[string]job `yaml:"jobs"`
}

type job struct {
	Uses            string `yaml:"uses"`
	Needs           any    `yaml:"needs"`
	If              any    `yaml:"if"`
	ContinueOnError any    `yaml:"continue-on-error"`
	Permissions     any    `yaml:"permissions"`
	Concurrency     any    `yaml:"concurrency"`
	Env             any    `yaml:"env"`
	With            any    `yaml:"with"`
	Steps           []step `yaml:"steps"`
}

type step struct {
	Name            string `yaml:"name"`
	Uses            string `yaml:"uses"`
	Run             string `yaml:"run"`
	If              any    `yaml:"if"`
	ContinueOnError any    `yaml:"continue-on-error"`
	Env             any    `yaml:"env"`
	With            any    `yaml:"with"`
}

// readWorkflow reads a workflow of the module.
func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testkit.ModuleRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("the module has no workflow %s: %v", name, err)
	}
	var w workflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return w
}

// events returns the events that start a workflow: on is a name, a list of
// names or a map by name.
func events(on any) []string {
	switch v := on.(type) {
	case string:
		return []string{v}
	case []any:
		var names []string
		for _, n := range v {
			names = append(names, fmt.Sprint(n))
		}
		return names
	case map[string]any:
		var names []string
		for n := range v {
			names = append(names, n)
		}
		return names
	}
	return nil
}

// needs returns the jobs a job needs directly: needs is a name or a list.
func needs(j job) []string {
	switch v := j.Needs.(type) {
	case string:
		return []string{v}
	case []any:
		var names []string
		for _, n := range v {
			names = append(names, fmt.Sprint(n))
		}
		return names
	}
	return nil
}

// ancestors returns the jobs a job needs, directly or through other jobs.
func (w workflow) ancestors(name string) []string {
	var out []string
	queue := needs(w.Jobs[name])
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
		queue = append(queue, needs(w.Jobs[n])...)
	}
	return out
}

// truthy reports whether continue-on-error may be true: an expression may.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		return s != "" && s != "false"
	}
}

// bypass reports whether a condition runs a job or a step when a job it
// needs or a step before it has failed.
func bypass(cond any) bool {
	s := fmt.Sprint(cond)
	return strings.Contains(s, "always()") || strings.Contains(s, "failure()") || strings.Contains(s, "cancelled()")
}

// publication is a command that creates the tag or the release on GitHub.
var publication = regexp.MustCompile(`\bgh release (create|upload|edit)\b|\bgit push\b|\bgit tag\b`)

// A place is a step of a job of the workflow.
type place struct {
	job  string
	step int
}

func (p place) String() string { return fmt.Sprintf("job %s, step %d", p.job, p.step+1) }

// TestReleaseFailedTests checks the build of the workflow Release that keeps
// a release from failed tests and refused checks: it runs the tests of CI as
// the reusable workflow ci.yml; each step that creates the tag or publishes
// the release runs only after the job of the tests and after the checks of
// the program of release, with no condition that runs it when they fail; and
// the checks are not let fail.
func TestReleaseFailedTests(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	ci := readWorkflow(t, ciWorkflow)
	if !slices.Contains(events(ci.On), "workflow_call") {
		t.Errorf("%s does not run as a reusable workflow: its on has no workflow_call: %v", ciWorkflow, events(ci.On))
	}
	w := readWorkflow(t, releaseWorkflow)
	var tests []string
	var checks, publications []place
	for name, j := range w.Jobs {
		if j.Uses == "./"+ciWorkflow {
			tests = append(tests, name)
		}
		for i, s := range j.Steps {
			if strings.Contains(s.Run, "release check") {
				checks = append(checks, place{name, i})
			}
			if publication.MatchString(s.Run) || strings.Contains(s.Uses, "release") {
				publications = append(publications, place{name, i})
			}
		}
	}
	if len(tests) == 0 {
		t.Errorf("%s has no job that runs the tests of CI: uses: ./%s", releaseWorkflow, ciWorkflow)
	}
	if len(checks) == 0 {
		t.Errorf("%s has no step that runs the checks of the program of release: release check", releaseWorkflow)
	}
	if len(publications) == 0 {
		t.Errorf("%s has no step that creates the tag or publishes the release", releaseWorkflow)
	}
	for _, c := range checks {
		j := w.Jobs[c.job]
		s := j.Steps[c.step]
		if truthy(s.ContinueOnError) || truthy(j.ContinueOnError) {
			t.Errorf("%s: the checks of the program of release (%s) may fail: continue-on-error", releaseWorkflow, c)
		}
		for _, line := range strings.Split(s.Run, "\n") {
			if strings.Contains(line, "release check") && strings.Contains(line, "||") {
				t.Errorf("%s: the failure of the checks of the program of release (%s) is let through: %q", releaseWorkflow, c, strings.TrimSpace(line))
			}
		}
	}
	for _, p := range publications {
		before := w.ancestors(p.job)
		for _, name := range tests {
			if !slices.Contains(before, name) {
				t.Errorf("%s: %s creates the tag or publishes the release without needing the job of the tests %s", releaseWorkflow, p, name)
			}
		}
		if !slices.ContainsFunc(checks, func(c place) bool {
			return slices.Contains(before, c.job) || c.job == p.job && c.step < p.step
		}) {
			t.Errorf("%s: %s creates the tag or publishes the release before the checks of the program of release", releaseWorkflow, p)
		}
		for _, name := range append([]string{p.job}, before...) {
			j := w.Jobs[name]
			if bypass(j.If) {
				t.Errorf("%s: the job %s, before %s, runs when a job it needs fails: if: %v", releaseWorkflow, name, p, j.If)
			}
			if truthy(j.ContinueOnError) {
				t.Errorf("%s: the job %s, before %s, may fail: continue-on-error", releaseWorkflow, name, p)
			}
		}
		if s := w.Jobs[p.job].Steps[p.step]; bypass(s.If) {
			t.Errorf("%s: %s runs when a step before it fails: if: %v", releaseWorkflow, p, s.If)
		}
	}
}
