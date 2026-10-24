package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/adapter/codex"
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// startSession answers the session start hook: it writes the introduction
// for the agent.
func startSession(w io.Writer, in hook.Input) error {
	return introduce(w, in)
}

// introduce writes the introduction for the agent in a worktree of a pool:
// the project and the worktree, then either how to take a task or the task
// with its state. A plugin that does not match the program is warned of
// before, and anywhere: in any session the plugin is loaded in, its skills
// may describe the commands of another version. Anywhere else it writes
// nothing more. It only reads the state store and never creates it.
func introduce(w io.Writer, in hook.Input) error {
	var stale strings.Builder
	var exe string
	var ok bool
	switch in.Tool {
	case claude.Tool:
		exe, ok = pluginStale()
	case codex.Tool:
		exe, ok = codexPluginStale()
	}
	if ok {
		fmt.Fprintln(&stale, msg.Text(msg.IntroPluginStale))
		fmt.Fprintln(&stale, introHint(hintOf(msg.HintIntroPluginStale).set("tool", in.Tool).by(exe, in.Tool)))
	}
	// The warning does not depend on the state store: it stays when the
	// introduction cannot be read.
	intro, wt, err := introduction(in)
	if err != nil && stale.Len() == 0 {
		return err
	}
	// In Codex the subagents of the flow are read only from a worktree it
	// trusts: the agent tells the operator, who alone can trust it, as the
	// sandbox keeps the settings of Codex from the agent.
	if in.Tool == codex.Tool && intro != "" && !codexTrusted(wt) {
		fmt.Fprintln(&stale, msg.Text(msg.IntroCodexUntrusted))
	}
	if stale.Len() > 0 && intro != "" {
		stale.WriteString("\n")
	}
	_, err = io.WriteString(w, stale.String()+intro)
	return err
}

// codexTrusted reports whether the folder of a session of Codex is trusted
// in Codex, or the settings of Codex cannot be read: then nothing is told
// of the trust.
func codexTrusted(dir string) bool {
	home, err := codex.Home()
	if err != nil {
		return true
	}
	yes, ok := codex.Trusted(codex.ConfigPath(home), dir)
	return yes || !ok
}

// codexPluginStale reports whether the plugin of Gentry Codex has differs
// from the one this gentry builds, or Codex has none, and returns the path
// of this gentry, which the hint names. The files of Codex are read, not
// its program, which would not answer in the time of the hook.
func codexPluginStale() (string, bool) {
	exe, err := executable()
	if err != nil {
		return "", false
	}
	dir, err := home.Integration(codex.Tool)
	if err != nil {
		return "", false
	}
	return exe, !codex.PluginMatches(dir, exe, buildinfo.Version())
}

// introduction is the introduction for the agent in a worktree of a pool;
// empty anywhere else. It returns the folder of the session too.
func introduction(in hook.Input) (string, string, error) {
	dir := in.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		dir = wd
	}
	dir, err := paths.Canonical(dir)
	if err != nil {
		return "", "", err
	}
	intro, err := introductionIn(dir, in.Tool)
	return intro, dir, err
}

// introductionIn is the introduction for the agent in the folder dir of a
// session of tool.
func introductionIn(dir, tool string) (string, error) {
	path, err := state.Path()
	if err != nil {
		return "", err
	}
	st, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer st.Close()
	worktrees, err := st.Worktrees()
	if err != nil {
		return "", err
	}
	wt, ok := task.Pooled(worktrees, dir)
	if !ok {
		return "", nil
	}
	t, taken, err := st.TaskIn(wt.Path)
	if err != nil {
		return "", err
	}
	in := hook.Input{Tool: tool}

	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.TaskProject, wt.Project))
	fmt.Fprintln(&b, msg.Text(msg.TaskWorktree, wt.Path))
	if !taken {
		fmt.Fprintln(&b, msg.Text(msg.IntroNoTask))
		b.WriteString("\n")
		fmt.Fprintln(&b, introHint(hintOf(msg.HintIntroTake, skillName(in.Tool, agenttext.WorkingOnTask))))
		return b.String(), nil
	}
	views, err := task.Views(st, []state.Task{t})
	if err != nil {
		return "", err
	}
	v := views[0]
	fmt.Fprintln(&b, msg.Text(msg.TaskHeading, t.Key(), oneLine(t.Title)))
	fmt.Fprintln(&b, msg.Text(msg.TaskScenario, named(v.ScenarioTitle, t.Scenario)))
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)))
	if !v.Finished {
		fmt.Fprintln(&b, introSteps(v))
	}
	if v.Flow != nil {
		fmt.Fprintln(&b, msg.Text(msg.ProgressLine, progressLine(progressJSON(v.Progress))))
	}
	b.WriteString("\n")
	fmt.Fprintln(&b, introHint(hintOf(msg.HintIntroContinue, skillName(in.Tool, agenttext.WorkingOnTask))))
	return b.String(), nil
}

// introHint is h for the agent: the introduction is the agent's.
func introHint(h hint) string {
	line, _ := h.render(agentChannel)
	return line
}

// skillRefs name skills the way of each tool.
var skillRefs = map[string]func(string) string{claude.Tool: claude.SkillRef, codex.Tool: codex.SkillRef}

// skillName is the name of the skill name in tool; a tool not known, or not
// named, gets the name alone.
func skillName(tool, name string) string {
	if ref, ok := skillRefs[tool]; ok {
		return ref(name)
	}
	return name
}

// introSteps is the line of the steps of the current stage of v: done of
// all but the dropped ones.
func introSteps(v task.View) string {
	n := len(v.Path)
	if n == 0 || !v.Path[n-1].Current() {
		return msg.Text(msg.IntroNoSteps)
	}
	done, all := 0, 0
	for _, s := range v.Path[n-1].Steps {
		switch s.State {
		case state.StepDone:
			done++
			all++
		case state.StepPlanned:
			all++
		}
	}
	if all == 0 {
		return msg.Text(msg.IntroNoSteps)
	}
	return msg.Text(msg.IntroSteps, done, all)
}

// pluginStale reports whether the Claude Code plugin whose hook runs gentry
// differs from the one this gentry builds, as after an update of gentry
// without `gentry setup`, and returns the path of this gentry, which the
// hint names. Outside a hook of the plugin there is nothing to compare.
func pluginStale() (string, bool) {
	running, ok := claude.RunningVersion()
	if !ok {
		return "", false
	}
	exe, err := executable()
	if err != nil {
		return "", false
	}
	p, err := claude.Build(integration.Gentry(exe), buildinfo.Version())
	return exe, err == nil && p.Version != running
}
