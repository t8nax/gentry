package release_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// The build of the workflows of the pages, as the tests read it:
//
//   - the job that publishes the pages is a job with a step of the action
//     actions/deploy-pages;
//   - the checks of the button Release are a step with release check in its
//     run, the build of the pages a step with release pages;
//   - the release is created by a step with gh release create;
//   - a job builds or runs the code of the commit when it gets it: a step of
//     actions/checkout, or a local action or workflow ./…;
//   - the right to write to Pages is pages: write or id-token: write, which
//     the deployment needs, or write-all; a job has its permissions, or
//     those of the workflow when it has none, and with none of either it
//     may write to Pages, as GitHub gives the rights by default;
//   - the group of concurrency of the job that publishes the pages is its
//     own, at the level of the job.

// deployJobs returns the jobs of the workflow that publish the pages.
func deployJobs(w workflow) []string {
	return jobsWith(w, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/deploy-pages@") })
}

// jobsWith returns the jobs of the workflow that have a step that matches,
// sorted.
func jobsWith(w workflow, match func(step) bool) []string {
	var out []string
	for name, j := range w.Jobs {
		if slices.ContainsFunc(j.Steps, match) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// runs matches a step whose run has the text.
func runs(text string) func(step) bool {
	return func(s step) bool { return strings.Contains(s.Run, text) }
}

// createsRelease is the command that creates the release on GitHub.
var createsRelease = regexp.MustCompile(`\bgh release create\b`)

// touchesRelease is a command that changes the release or the tag.
var touchesRelease = regexp.MustCompile(`\bgh release\b|\bgit tag\b`)

// writesPages reports whether the permissions give the right to write to
// Pages.
func writesPages(perms any) bool {
	switch v := perms.(type) {
	case string:
		return strings.TrimSpace(v) == "write-all"
	case map[string]any:
		return fmt.Sprint(v["pages"]) == "write" || fmt.Sprint(v["id-token"]) == "write"
	}
	return false
}

// jobPermissions returns the permissions of the job: its own, or those of
// the workflow when it has none; byDefault is true when neither has any.
func jobPermissions(w workflow, j job) (perms any, byDefault bool) {
	if j.Permissions != nil {
		return j.Permissions, false
	}
	return w.Permissions, w.Permissions == nil
}

// mayWritePages reports whether the job may write to Pages.
func mayWritePages(w workflow, j job) bool {
	perms, byDefault := jobPermissions(w, j)
	return byDefault || writesPages(perms)
}

// runsCode reports whether the job builds or runs the code of the commit.
func runsCode(j job) bool {
	if strings.HasPrefix(j.Uses, "./") {
		return true
	}
	return slices.ContainsFunc(j.Steps, func(s step) bool {
		return strings.HasPrefix(s.Uses, "actions/checkout@") || strings.HasPrefix(s.Uses, "./")
	})
}

// queue returns the group of a concurrency of a job or of a workflow and
// whether it cancels the run in progress.
func queue(c any) (group string, cancel any) {
	switch v := c.(type) {
	case string:
		return v, nil
	case map[string]any:
		if g, ok := v["group"]; ok && g != nil {
			group = fmt.Sprint(g)
		}
		return group, v["cancel-in-progress"]
	}
	return "", nil
}

// anyOf reports whether a job of the names is in the list.
func anyOf(list, names []string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return slices.Contains(list, n) })
}

// TestReleaseChecksBeforeTests checks that the checks of the button Release,
// which build the pages, run in a job that the job of the tests, the job
// that creates the release and the job that publishes the pages need.
func TestReleaseChecksBeforeTests(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, releaseWorkflow)
	checks := jobsWith(w, runs("release check"))
	if len(checks) == 0 {
		t.Fatalf("%s has no job of the checks: a step with release check", releaseWorkflow)
	}
	var tests []string
	for name, j := range w.Jobs {
		if j.Uses == "./"+ciWorkflow {
			tests = append(tests, name)
		}
	}
	creators := jobsWith(w, func(s step) bool { return createsRelease.MatchString(s.Run) })
	deploys := deployJobs(w)
	for what, jobs := range map[string][]string{"runs the tests of CI": tests, "creates the release": creators, "publishes the pages": deploys} {
		if len(jobs) == 0 {
			t.Errorf("%s has no job that %s", releaseWorkflow, what)
		}
		for _, name := range jobs {
			if !anyOf(w.ancestors(name), checks) {
				t.Errorf("%s: the job %s, which %s, does not need the job of the checks %v", releaseWorkflow, name, what, checks)
			}
		}
	}
}

// TestPagesAfterRelease checks that the button Release publishes the pages
// after it has created the release, with no condition that runs the
// publication when that fails, and that the jobs of the pages neither create
// nor change the release or the tag.
func TestPagesAfterRelease(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, releaseWorkflow)
	creators := jobsWith(w, func(s step) bool { return createsRelease.MatchString(s.Run) })
	deploys := deployJobs(w)
	if len(creators) == 0 {
		t.Fatalf("%s has no job that creates the release: gh release create", releaseWorkflow)
	}
	if len(deploys) == 0 {
		t.Fatalf("%s has no job that publishes the pages: actions/deploy-pages", releaseWorkflow)
	}
	// The jobs of the release: those that create it and those they need.
	release := slices.Clone(creators)
	for _, c := range creators {
		release = append(release, w.ancestors(c)...)
	}
	for _, d := range deploys {
		before := w.ancestors(d)
		if !anyOf(before, creators) {
			t.Errorf("%s: the job %s publishes the pages without needing the job that creates the release %v", releaseWorkflow, d, creators)
		}
		for _, name := range append([]string{d}, before...) {
			j := w.Jobs[name]
			if bypass(j.If) {
				t.Errorf("%s: the job %s, before the publication of the pages, runs when a job it needs fails: if: %v", releaseWorkflow, name, j.If)
			}
			if slices.Contains(release, name) {
				continue
			}
			for i, s := range j.Steps {
				if touchesRelease.MatchString(s.Run) {
					t.Errorf("%s: the job %s of the pages changes the release or the tag in its step %d: %q", releaseWorkflow, name, i+1, strings.TrimSpace(s.Run))
				}
			}
		}
	}
}

// TestPagesPermissions checks that in both workflows only the jobs that
// publish the pages may write to Pages and that they neither build nor run
// the code of the commit; a job with no permissions has those of the
// workflow, and with none of either it may write to Pages.
func TestPagesPermissions(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	for _, name := range []string{releaseWorkflow, pagesWorkflow} {
		t.Run(strings.TrimSuffix(name[strings.LastIndex(name, "/")+1:], ".yml"), func(t *testing.T) {
			w := readWorkflow(t, name)
			deploys := deployJobs(w)
			if len(deploys) == 0 {
				t.Errorf("%s has no job that publishes the pages: actions/deploy-pages", name)
			}
			for _, d := range deploys {
				perms, _ := jobPermissions(w, w.Jobs[d])
				if p, ok := perms.(map[string]any); !ok || fmt.Sprint(p["pages"]) != "write" {
					t.Errorf("%s: the job %s, which publishes the pages, has no pages: write: permissions: %v", name, d, perms)
				}
			}
			for jn, j := range w.Jobs {
				if !mayWritePages(w, j) {
					continue
				}
				perms, byDefault := jobPermissions(w, j)
				given := fmt.Sprintf("permissions: %v", perms)
				if byDefault {
					given = "no permissions in the job and in the workflow, the rights by default"
				}
				if !slices.Contains(deploys, jn) {
					t.Errorf("%s: the job %s may write to Pages and does not publish the pages: %s", name, jn, given)
				}
				if runsCode(j) {
					t.Errorf("%s: the job %s may write to Pages and builds or runs the code of the commit: %s", name, jn, given)
				}
			}
		})
	}
}

// TestPagesConcurrency checks that the jobs that publish the pages in both
// workflows are in their own group of concurrency at the level of the job,
// one and the same, that does not cancel the publication in progress and
// is not the group of either workflow as a whole.
func TestPagesConcurrency(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	groups := map[string]string{}
	var whole []string
	for _, name := range []string{releaseWorkflow, pagesWorkflow} {
		w := readWorkflow(t, name)
		if g, _ := queue(w.Concurrency); g != "" {
			whole = append(whole, g)
		}
		deploys := deployJobs(w)
		if len(deploys) == 0 {
			t.Errorf("%s has no job that publishes the pages: actions/deploy-pages", name)
		}
		for _, d := range deploys {
			group, cancel := queue(w.Jobs[d].Concurrency)
			switch {
			case group == "":
				t.Errorf("%s: the job %s, which publishes the pages, has no group of concurrency of its own: concurrency of the job", name, d)
				continue
			case strings.Contains(group, "${{"):
				t.Errorf("%s: the group of concurrency of the job %s is an expression %q, not one name for both workflows", name, d, group)
			}
			if truthy(cancel) {
				t.Errorf("%s: the group of concurrency of the job %s cancels the publication in progress: cancel-in-progress: %v", name, d, cancel)
			}
			groups[name+": "+d] = group
		}
	}
	var names []string
	for where, g := range groups {
		if !slices.Contains(names, g) {
			names = append(names, g)
		}
		if slices.Contains(whole, g) {
			t.Errorf("%s: the group of concurrency %q of the job that publishes the pages is that of a workflow as a whole: %q", where, g, whole)
		}
	}
	if len(names) > 1 {
		t.Errorf("the jobs that publish the pages are in different groups of concurrency: %v", groups)
	}
}

// TestPagesWorkflow checks the button Publish pages: the workflow
// «Страницы выпусков», started by hand only and with no fields.
func TestPagesWorkflow(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, pagesWorkflow)
	if w.Name != "Страницы выпусков" {
		t.Errorf("%s is named %q, want «Страницы выпусков»", pagesWorkflow, w.Name)
	}
	if got := events(w.On); !slices.Equal(got, []string{"workflow_dispatch"}) {
		t.Errorf("%s starts on %v, want by hand only: workflow_dispatch", pagesWorkflow, got)
	}
	if on, ok := w.On.(map[string]any); ok {
		if d, ok := on["workflow_dispatch"].(map[string]any); ok {
			if inputs, ok := d["inputs"].(map[string]any); ok && len(inputs) > 0 || !ok && d["inputs"] != nil {
				t.Errorf("%s has fields: %v", pagesWorkflow, d["inputs"])
			}
		}
	}
}

// TestPagesBuiltBeforeDeploy checks that in both workflows the job that
// publishes the pages publishes the site the build of the pages of the
// program of release has made in a job it needs, with no condition that runs
// it when that fails; in the workflow Release the build of the pages runs
// after the release is created, in a job apart from it.
func TestPagesBuiltBeforeDeploy(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	for _, name := range []string{releaseWorkflow, pagesWorkflow} {
		t.Run(strings.TrimSuffix(name[strings.LastIndex(name, "/")+1:], ".yml"), func(t *testing.T) {
			w := readWorkflow(t, name)
			builders := jobsWith(w, runs("release pages"))
			deploys := deployJobs(w)
			if len(builders) == 0 {
				t.Errorf("%s has no step of the build of the pages: release pages", name)
			}
			if len(deploys) == 0 {
				t.Errorf("%s has no job that publishes the pages: actions/deploy-pages", name)
			}
			for _, d := range deploys {
				before := w.ancestors(d)
				if !anyOf(before, builders) {
					t.Errorf("%s: the job %s publishes the pages without needing the job of the build of the pages %v", name, d, builders)
				}
				for _, jn := range append([]string{d}, before...) {
					j := w.Jobs[jn]
					if bypass(j.If) {
						t.Errorf("%s: the job %s, before the publication of the pages, runs when a job it needs fails: if: %v", name, jn, j.If)
					}
					if truthy(j.ContinueOnError) {
						t.Errorf("%s: the job %s, before the publication of the pages, may fail: continue-on-error", name, jn)
					}
				}
				for i, s := range w.Jobs[d].Steps {
					if bypass(s.If) {
						t.Errorf("%s: the step %d of the job %s runs when a step before it fails: if: %v", name, i+1, d, s.If)
					}
				}
			}
			for _, b := range builders {
				for i, s := range w.Jobs[b].Steps {
					if !strings.Contains(s.Run, "release pages") {
						continue
					}
					if truthy(s.ContinueOnError) {
						t.Errorf("%s: the build of the pages (job %s, step %d) may fail: continue-on-error", name, b, i+1)
					}
					for _, l := range strings.Split(s.Run, "\n") {
						if strings.Contains(l, "release pages") && strings.Contains(l, "||") {
							t.Errorf("%s: the failure of the build of the pages (job %s, step %d) is let through: %q", name, b, i+1, strings.TrimSpace(l))
						}
					}
				}
			}
			if name != releaseWorkflow {
				return
			}
			creators := jobsWith(w, func(s step) bool { return createsRelease.MatchString(s.Run) })
			for _, b := range builders {
				if slices.Contains(creators, b) {
					t.Errorf("%s: the job %s builds the pages and creates the release: a failure of the pages would run the creation again", name, b)
				}
				if !anyOf(w.ancestors(b), creators) {
					t.Errorf("%s: the job %s builds the pages without needing the job that creates the release %v", name, b, creators)
				}
			}
		})
	}
}
