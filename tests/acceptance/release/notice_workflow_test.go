package release_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// The build of the sending of the message about a release to the group in
// Telegram, as the tests read it:
//
//   - the job that sends the message is a job with a step whose run has the
//     address of the Bot API, api.telegram.org;
//   - the message is built by a step with release notice in its run;
//   - a secret of the bot is an expression secrets.TELEGRAM_<name>; a step
//     gets it through its environment when the expression is a value of the
//     env of the step.

// telegramAPI is the address of the Bot API of Telegram.
const telegramAPI = "api.telegram.org"

// botSecret is an expression of a secret of the bot.
var botSecret = regexp.MustCompile(`secrets\.TELEGRAM_[A-Za-z0-9_]+`)

// tracing is a command or a flag anywhere in the run of a step that prints
// the commands of the shell or the request of curl, with the address of the
// Bot API and its token: set -x, also among other flags as set -ex, set -o
// xtrace, a flag -v, also among other short flags as -sv, --verbose and
// --trace.
var tracing = regexp.MustCompile(`\bset\s+-[A-Za-z]*x|\bset\s+-o\s+xtrace\b|(?:^|\s)-[A-Za-z]*v[A-Za-z]*(?:\s|$)|--verbose\b|--trace`)

// tracingShell is a shell of a step that traces its commands: a flag -x,
// also among other short flags, or the option xtrace.
var tracingShell = regexp.MustCompile(`(?:^|\s)-[A-Za-z]*x|\bxtrace\b`)

// senderJobs returns the jobs of the workflow that send the message to the
// group.
func senderJobs(w workflow) []string {
	return jobsWith(w, runs(telegramAPI))
}

// workflowText returns the text of a workflow of the module.
func workflowText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testkit.ModuleRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("the module has no workflow %s: %v", name, err)
	}
	return string(b)
}

// TestNoticeAfterPages checks the job of the workflow Release that sends the
// message to the group: one job, after the publication of the pages and the
// build of the message, with no condition that runs it when they fail and
// no continue-on-error, so that its failure fails the run and «Re-run failed
// jobs» sends the message again; with no checkout of the code and no local
// action, with its own permissions, none or none of them write, and not in
// the group of concurrency of the publication of the pages.
func TestNoticeAfterPages(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, releaseWorkflow)
	senders := senderJobs(w)
	builders := jobsWith(w, runs("release notice"))
	deploys := deployJobs(w)
	if len(senders) != 1 {
		t.Fatalf("%s has %d jobs that send the message to the group (%s), want one: %v", releaseWorkflow, len(senders), telegramAPI, senders)
	}
	if len(builders) == 0 {
		t.Fatalf("%s has no step that builds the message to the group: release notice", releaseWorkflow)
	}
	if len(deploys) == 0 {
		t.Fatalf("%s has no job that publishes the pages: actions/deploy-pages", releaseWorkflow)
	}
	name := senders[0]
	j := w.Jobs[name]
	before := w.ancestors(name)
	for _, d := range deploys {
		if !slices.Contains(before, d) {
			t.Errorf("%s: the job %s sends the message to the group without needing the job %s that publishes the pages", releaseWorkflow, name, d)
		}
	}
	if !anyOf(before, builders) {
		t.Errorf("%s: the job %s sends the message to the group without needing the job of its build %v", releaseWorkflow, name, builders)
	}
	if bypass(j.If) {
		t.Errorf("%s: the job %s, which sends the message to the group, runs when a job it needs fails: if: %v", releaseWorkflow, name, j.If)
	}
	if truthy(j.ContinueOnError) {
		t.Errorf("%s: the failure of the job %s, which sends the message to the group, is let through: continue-on-error", releaseWorkflow, name)
	}
	for i, s := range j.Steps {
		if bypass(s.If) {
			t.Errorf("%s: the step %d of the job %s runs when a step before it fails: if: %v", releaseWorkflow, i+1, name, s.If)
		}
		if truthy(s.ContinueOnError) {
			t.Errorf("%s: the failure of the step %d of the job %s is let through: continue-on-error", releaseWorkflow, i+1, name)
		}
	}
	if runsCode(j) {
		t.Errorf("%s: the job %s, which has the secrets of the bot, gets the code of the commit: a checkout or a local action", releaseWorkflow, name)
	}
	// The job needs no permission: an empty map gives none, and with no
	// permissions it would have those of the workflow or those by default.
	switch perms := j.Permissions.(type) {
	case nil:
		t.Errorf("%s: the job %s, which sends the message to the group, has no permissions of its own: want permissions: {}", releaseWorkflow, name)
	case string:
		if p := strings.TrimSpace(perms); p != "read-all" {
			t.Errorf("%s: the job %s, which sends the message to the group, may write: permissions: %s", releaseWorkflow, name, p)
		}
	case map[string]any:
		for scope, level := range perms {
			if l := fmt.Sprint(level); l != "read" && l != "none" {
				t.Errorf("%s: the job %s, which sends the message to the group, may write: %s: %s", releaseWorkflow, name, scope, l)
			}
		}
	default:
		t.Errorf("%s: the permissions of the job %s are neither a map nor a name: %v", releaseWorkflow, name, perms)
	}
	group, _ := queue(j.Concurrency)
	for _, d := range deploys {
		if pages, _ := queue(w.Jobs[d].Concurrency); group != "" && group == pages {
			t.Errorf("%s: the job %s, which sends the message to the group, is in the group of concurrency %q of the publication of the pages", releaseWorkflow, name, group)
		}
	}
}

// TestNoticeSecrets checks that the secrets of the bot are used in the job
// that sends the message to the group only, through the environment of its
// steps only, not in the env of the workflow; that the job of the build of
// the message has no secrets, nor a job that calls a workflow of the
// repository; and that the steps of the job that sends do not trace their
// commands or the request, in their run or by their shell.
func TestNoticeSecrets(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, releaseWorkflow)
	senders := senderJobs(w)
	if len(senders) != 1 {
		t.Fatalf("%s has %d jobs that send the message to the group (%s), want one: %v", releaseWorkflow, len(senders), telegramAPI, senders)
	}
	sender := w.Jobs[senders[0]]
	// The secrets of the bot in the environments of the steps of the job
	// that sends: the only ones of the file.
	inEnv := 0
	for _, s := range sender.Steps {
		inEnv += len(botSecret.FindAllString(fmt.Sprint(s.Env), -1))
	}
	if inEnv < 2 {
		t.Errorf("%s: the steps of the job %s get %d secrets of the bot through their env, want the token and the group: TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID", releaseWorkflow, senders[0], inEnv)
	}
	if all := botSecret.FindAllString(workflowText(t, releaseWorkflow), -1); len(all) != inEnv {
		t.Errorf("%s uses the secrets of the bot %d times, %d of them out of the env of the steps of the job %s: %v", releaseWorkflow, len(all), len(all)-inEnv, senders[0], all)
	}
	if m := botSecret.FindString(fmt.Sprint(w.Env)); m != "" {
		t.Errorf("%s: the env of the workflow has the secret of the bot %s: every job would get it", releaseWorkflow, m)
	}
	for i, s := range sender.Steps {
		if m := tracing.FindString(s.Run); m != "" {
			t.Errorf("%s: the step %d of the job %s prints its commands or the request with the address of the Bot API: %q", releaseWorkflow, i+1, senders[0], strings.TrimSpace(m))
		}
		if m := tracingShell.FindString(s.Shell); m != "" {
			t.Errorf("%s: the shell of the step %d of the job %s traces its commands: shell: %s", releaseWorkflow, i+1, senders[0], s.Shell)
		}
	}
	// A job that calls a workflow of the repository, uses: ./…, runs the
	// code of the commit: it gets no secrets.
	for name, j := range w.Jobs {
		if strings.HasPrefix(j.Uses, "./") && j.Secrets != nil {
			t.Errorf("%s: the job %s calls %s, which runs the code of the commit, with secrets: %v", releaseWorkflow, name, j.Uses, j.Secrets)
		}
	}
	builders := jobsWith(w, runs("release notice"))
	if len(builders) == 0 {
		t.Fatalf("%s has no step that builds the message to the group: release notice", releaseWorkflow)
	}
	for _, b := range builders {
		j := w.Jobs[b]
		parts := []any{j.Env, j.With}
		for _, s := range j.Steps {
			parts = append(parts, s.Env, s.With, s.Run, s.If)
		}
		if text := fmt.Sprint(parts...); strings.Contains(text, "secrets.") {
			t.Errorf("%s: the job %s, which builds the message to the group and runs the code of the commit, has secrets", releaseWorkflow, b)
		}
		if slices.Contains(senders, b) {
			t.Errorf("%s: the job %s builds the message to the group and sends it", releaseWorkflow, b)
		}
	}
}

// TestPagesNoNotice checks that the button Publish pages sends no message
// to the group: its workflow has neither the secrets of the bot nor the
// address of the Bot API.
func TestPagesNoNotice(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	text := workflowText(t, pagesWorkflow)
	for _, word := range []string{"TELEGRAM_", telegramAPI} {
		if strings.Contains(text, word) {
			t.Errorf("%s has %q: the button Publish pages sends no message to the group", pagesWorkflow, word)
		}
	}
}

// TestNoticeArtifact checks how the message goes from the job that builds it
// to the job that sends it: the build, release notice, runs in the job that
// creates the release, before gh release create, so its failure leaves no
// release; the job keeps the message with actions/upload-artifact for 30
// days, as long as GitHub lets a run be rerun, and with overwrite: true, so
// that a rerun of the job uploads it again; the job that sends downloads it
// with actions/download-artifact by the same name.
func TestNoticeArtifact(t *testing.T) {
	testkit.SkipOnBinary(t)
	t.Parallel()
	w := readWorkflow(t, releaseWorkflow)
	ws := readWithWorkflow(t, releaseWorkflow)
	senders := senderJobs(w)
	if len(senders) != 1 {
		t.Fatalf("%s has %d jobs that send the message to the group (%s), want one: %v", releaseWorkflow, len(senders), telegramAPI, senders)
	}
	creators := jobsWith(w, func(s step) bool { return createsRelease.MatchString(s.Run) })
	builders := jobsWith(w, runs("release notice"))
	if len(creators) != 1 || !slices.Equal(builders, creators) {
		t.Fatalf("%s: the message to the group is built in the jobs %v, want the job that creates the release %v", releaseWorkflow, builders, creators)
	}
	steps := ws.Jobs[creators[0]].Steps
	build := slices.IndexFunc(steps, func(s withStep) bool { return strings.Contains(s.Run, "release notice") })
	create := slices.IndexFunc(steps, func(s withStep) bool { return createsRelease.MatchString(s.Run) })
	if build > create {
		t.Errorf("%s: the job %s builds the message to the group in its step %d, after it creates the release in its step %d", releaseWorkflow, creators[0], build+1, create+1)
	}
	upload := slices.IndexFunc(steps, func(s withStep) bool { return strings.HasPrefix(s.Uses, "actions/upload-artifact@") })
	if upload < 0 {
		t.Fatalf("%s: the job %s does not keep the message to the group: no step of actions/upload-artifact", releaseWorkflow, creators[0])
	}
	up := steps[upload]
	if upload < build {
		t.Errorf("%s: the job %s keeps the message to the group in its step %d, before it builds it in its step %d", releaseWorkflow, creators[0], upload+1, build+1)
	}
	if got := input(up, "retention-days"); got != "30" {
		t.Errorf("%s: the job %s keeps the message to the group %q days, want retention-days: 30", releaseWorkflow, creators[0], got)
	}
	if got := input(up, "overwrite"); got != "true" {
		t.Errorf("%s: a rerun of the job %s may not upload the message to the group again: overwrite: %q, want true", releaseWorkflow, creators[0], got)
	}
	name := input(up, "name")
	if name == "" {
		t.Errorf("%s: the job %s keeps the message to the group with no name of the artifact", releaseWorkflow, creators[0])
	}
	downloads := 0
	for _, s := range ws.Jobs[senders[0]].Steps {
		if !strings.HasPrefix(s.Uses, "actions/download-artifact@") {
			continue
		}
		downloads++
		if got := input(s, "name"); got != name {
			t.Errorf("%s: the job %s downloads the artifact %q, want the message to the group %q", releaseWorkflow, senders[0], got, name)
		}
	}
	if downloads == 0 {
		t.Errorf("%s: the job %s does not download the message to the group: no step of actions/download-artifact", releaseWorkflow, senders[0])
	}
}
