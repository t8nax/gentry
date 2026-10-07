package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/integration"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// startSession answers the session start hook: it removes the marks of an
// interrupted session and writes the introduction for the agent.
func startSession(w io.Writer, in hook.Input) error {
	hook.RunSessionStart(in)
	return introduce(w, in)
}

// introCommands are the commands of the agent in the introduction to a task,
// with their purposes.
var introCommands = []struct {
	command string
	purpose msg.Key
}{
	{"gentry task show", msg.IntroTaskShow},
	{"gentry task show --statement", msg.IntroStatement},
	{"gentry stage show", msg.IntroStageShow},
	{"gentry step add <шаг>...", msg.IntroStepAdd},
	{"gentry step done <номер>", msg.IntroStepDone},
	{"gentry step drop <номер>", msg.IntroStepDrop},
	{"gentry stage exit", msg.IntroStageExit},
	{"gentry stage skip", msg.IntroStageSkip},
	{"gentry artifact save <имя>", msg.IntroArtifactSave},
	{"gentry note add <текст>", msg.IntroNoteAdd},
	{"gentry note list", msg.IntroNoteList},
	{"gentry operator record", msg.IntroOperatorRecord},
	{"gentry task close", msg.IntroTaskClose},
	{"gentry task cancel", msg.IntroTaskCancel},
}

// introduce writes the introduction for the agent in a worktree of a pool:
// the project and the worktree, then either how to take a task or the task
// with the commands of the agent. Anywhere else it writes nothing. It only
// reads the state store and never creates it.
func introduce(w io.Writer, in hook.Input) error {
	dir := in.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	dir, err := paths.Canonical(dir)
	if err != nil {
		return err
	}
	path, err := state.Path()
	if err != nil {
		return err
	}
	st, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer st.Close()
	worktrees, err := st.Worktrees()
	if err != nil {
		return err
	}
	wt, ok := task.Pooled(worktrees, dir)
	if !ok {
		return nil
	}
	t, taken, err := st.TaskIn(wt.Path)
	if err != nil {
		return err
	}

	var b strings.Builder
	if pluginStale() {
		fmt.Fprintln(&b, msg.Text(msg.IntroPluginStale))
		fmt.Fprintln(&b, msg.Text(msg.HintIntroPluginStale))
		b.WriteString("\n")
	}
	fmt.Fprintln(&b, msg.Text(msg.TaskProject, wt.Project))
	fmt.Fprintln(&b, msg.Text(msg.TaskWorktree, wt.Path))
	if !taken {
		fmt.Fprintln(&b, msg.Text(msg.IntroNoTask))
		b.WriteString("\n")
		fmt.Fprintln(&b, msg.Text(msg.HintIntroTake, agenttext.WorkingOnTask))
		_, err := io.WriteString(w, b.String())
		return err
	}
	views, err := task.Views(st, []state.Task{t})
	if err != nil {
		return err
	}
	v := views[0]
	fmt.Fprintln(&b, msg.Text(msg.TaskHeading, t.Key(), oneLine(t.Title)))
	fmt.Fprintln(&b, msg.Text(msg.TaskScenario, named(v.ScenarioTitle, t.Scenario)))
	fmt.Fprintln(&b, msg.Text(msg.TaskStage, stageRound(v.StageTitle, v.Stage, v.Round, v.Finished)))
	if !v.Finished {
		fmt.Fprintln(&b, introSteps(v))
	}
	if v.Flow != nil {
		fmt.Fprintln(&b, msg.Text(msg.ProgressLine, progressText(v.Progress)))
	}
	if exe, err := executable(); err == nil {
		fmt.Fprintln(&b, msg.Text(msg.IntroProgram, filepath.ToSlash(exe)))
	}
	b.WriteString("\n")
	rows := [][]string{{msg.Text(msg.ColCommand), msg.Text(msg.ColPurpose)}}
	for _, c := range introCommands {
		rows = append(rows, []string{c.command, msg.Text(c.purpose)})
	}
	writeTable(&b, rows)
	b.WriteString("\n")
	fmt.Fprintln(&b, msg.Text(msg.HintIntroHelp))
	fmt.Fprintln(&b, msg.Text(msg.HintIntroContinue, agenttext.WorkingOnTask))
	_, err = io.WriteString(w, b.String())
	return err
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

// pluginStale reports whether the plugin whose hook runs gentry differs from
// the one this gentry builds, as after an update of gentry without
// `gentry setup`. Outside a hook of the plugin there is nothing to compare.
func pluginStale() bool {
	running, ok := claude.RunningVersion()
	if !ok {
		return false
	}
	exe, err := executable()
	if err != nil {
		return false
	}
	p, err := claude.Build(integration.Gentry(exe), buildinfo.Version())
	return err == nil && p.Version != running
}
