// Package cli parses the gentry command line and dispatches commands.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/buildinfo"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/project"
	"github.com/t8nax/gentry/internal/state"
)

// Env is the environment a command runs in.
type Env struct {
	Stdin  io.Reader // nil for none
	Stdout io.Writer
	Stderr io.Writer

	json bool // --json is among the arguments: failures are printed as JSON
}

// command is a gentry command. Its arguments and flags are declared here and
// nowhere else: the help is built from them, and newFlags accepts only them.
// A command with actions, such as project, is a group: its actions are
// commands of their own, named "project add".
type command struct {
	name    string
	section msg.Key // heading of the help section; empty for a service command
	summary msg.Key // one line in the command list or in the action list of a group
	desc    msg.Key // first paragraph of the command help
	args    []argSpec
	flags   []flagSpec
	actions []command
	run     func(args []string, env Env) int
}

// hidden reports whether c is a service command: not shown in the help.
func (c command) hidden() bool { return c.section == "" }

// argSpec is a positional argument. The command checks required arguments
// itself, to name them in its own words; flags.parse refuses extra ones.
type argSpec struct {
	name     msg.Key // placeholder, e.g. <ИИ-инструмент>
	desc     func() string
	optional bool
	many     bool // the argument may be given several times; only the last one
}

// flagSpec is a flag. A flag with a value placeholder takes a value. The
// command checks a required flag itself, as a required argument.
type flagSpec struct {
	name     string  // without dashes
	value    msg.Key // placeholder of the value; empty for a boolean flag
	desc     func() string
	required bool
	// group names flags of which at most one may be given; the call line of
	// the help shows them as one choice. The command refuses several itself.
	group string
}

// descText returns a description that is the text of k.
func descText(k msg.Key) func() string {
	return func() string { return msg.Text(k) }
}

var jsonFlag = flagSpec{name: "json", desc: descText(msg.FlagJSON)}

// commands returns the top-level commands in the order of the help.
func commands() []command {
	cmds := []command{
		{
			name: "project", section: msg.HelpSectionProjects, summary: msg.CmdProjectSummary, desc: msg.CmdProjectDesc,
			actions: []command{
				{
					name: "project add", summary: msg.CmdProjectAddSummary, desc: msg.CmdProjectAddDesc,
					args: []argSpec{{name: msg.ArgProjectID, desc: descText(msg.ArgProjectIDDesc), optional: true}},
					flags: []flagSpec{
						{name: "knowledge", value: msg.ArgPath, desc: descText(msg.FlagKnowledgeDesc), required: true},
						{name: "prefix", value: msg.ArgPrefix, desc: descText(msg.FlagPrefixDesc)},
						jsonFlag,
					},
					run: runProjectAdd,
				},
				{
					name: "project list", summary: msg.CmdProjectListSummary, desc: msg.CmdProjectListDesc,
					flags: []flagSpec{jsonFlag},
					run:   runProjectList,
				},
			},
		},
		{
			name: "worktree", section: msg.HelpSectionProjects, summary: msg.CmdWorktreeSummary, desc: msg.CmdWorktreeDesc,
			actions: []command{
				{
					name: "worktree add", summary: msg.CmdWorktreeAddSummary, desc: msg.CmdWorktreeAddDesc,
					args: []argSpec{{name: msg.ArgPath, desc: descText(msg.ArgWorktreePathDesc), optional: true}},
					flags: []flagSpec{
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagWorktreeAddProject)},
						jsonFlag,
					},
					run: runWorktreeAdd,
				},
				{
					name: "worktree list", summary: msg.CmdWorktreeListSummary, desc: msg.CmdWorktreeListDesc,
					flags: []flagSpec{
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagWorktreeListProject)},
						jsonFlag,
					},
					run: runWorktreeList,
				},
			},
		},
		{
			name: "flow", section: msg.HelpSectionProcess, summary: msg.CmdFlowSummary, desc: msg.CmdFlowDesc,
			actions: []command{
				{
					name: "flow show", summary: msg.CmdFlowShowSummary, desc: msg.CmdFlowShowDesc,
					flags: []flagSpec{
						{name: "scenario", value: msg.ArgScenario, desc: descText(msg.FlagShowScenario), group: "object"},
						{name: "stage", value: msg.ArgStage, desc: descText(msg.FlagShowStage), group: "object"},
						{name: "agent", value: msg.ArgAgent, desc: descText(msg.FlagShowAgent), group: "object"},
						{name: "part", value: msg.ArgPart, desc: descText(msg.FlagShowPart), group: "object"},
						{name: "draft", desc: descText(msg.FlagShowDraft)},
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagFlowShowProject)},
						jsonFlag,
					},
					run: runFlowShow,
				},
				{
					name: "flow diff", summary: msg.CmdFlowDiffSummary, desc: msg.CmdFlowDiffDesc,
					flags: []flagSpec{
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagFlowDiffProject)},
						jsonFlag,
					},
					run: runFlowDiff,
				},
				{
					name: "flow apply", summary: msg.CmdFlowApplySummary, desc: msg.CmdFlowApplyDesc,
					flags: []flagSpec{
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagFlowApplyProject)},
						jsonFlag,
					},
					run: runFlowApply,
				},
				{
					name: "flow discard", summary: msg.CmdFlowDiscardSummary, desc: msg.CmdFlowDiscardDesc,
					flags: []flagSpec{
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagFlowDiscardProject)},
						jsonFlag,
					},
					run: runFlowDiscard,
				},
			},
		},
		{
			name: "library", section: msg.HelpSectionProcess, summary: msg.CmdLibrarySummary, desc: msg.CmdLibraryDesc,
			actions: []command{
				{name: "library diff", summary: msg.CmdLibraryDiffSummary, desc: msg.CmdLibraryDiffDesc, flags: []flagSpec{jsonFlag}, run: runLibraryDiff},
				{name: "library apply", summary: msg.CmdLibraryApplySummary, desc: msg.CmdLibraryApplyDesc, flags: []flagSpec{jsonFlag}, run: runLibraryApply},
				{name: "library discard", summary: msg.CmdLibraryDiscardSummary, desc: msg.CmdLibraryDiscardDesc, flags: []flagSpec{jsonFlag}, run: runLibraryDiscard},
			},
		},
		{
			name: "process", section: msg.HelpSectionProcess, summary: msg.CmdProcessSummary, desc: msg.CmdProcessDesc,
			actions: []command{
				{
					name: "process remote", summary: msg.CmdProcessRemoteSummary, desc: msg.CmdProcessRemoteDesc,
					args:  []argSpec{{name: msg.ArgRemote, desc: descText(msg.ArgRemoteDesc)}},
					flags: []flagSpec{jsonFlag},
					run:   runProcessRemote,
				},
				{name: "process sync", summary: msg.CmdProcessSyncSummary, desc: msg.CmdProcessSyncDesc, flags: []flagSpec{jsonFlag}, run: runProcessSync},
				{name: "process status", summary: msg.CmdProcessStatusSummary, desc: msg.CmdProcessStatusDesc, flags: []flagSpec{jsonFlag}, run: runProcessStatus},
			},
		},
		{
			name: "task", section: msg.HelpSectionTasks, summary: msg.CmdTaskSummary, desc: msg.CmdTaskDesc,
			actions: []command{
				{
					name: "task take", summary: msg.CmdTaskTakeSummary, desc: msg.CmdTaskTakeDesc,
					flags: []flagSpec{
						{name: "scenario", value: msg.ArgScenario, desc: descText(msg.FlagTaskScenario)},
						{name: "title", value: msg.ArgTitle, desc: descText(msg.FlagTaskTitle)},
						{name: "statement", value: msg.ArgText, desc: descText(msg.FlagTaskStatement)},
						{name: "worktree", value: msg.ArgPath, desc: descText(msg.FlagTaskWorktree)},
						{name: "input", value: msg.ArgFile, desc: descText(msg.FlagTaskInput)},
						jsonFlag,
					},
					run: runTaskTake,
				},
				{
					name: "task show", summary: msg.CmdTaskShowSummary, desc: msg.CmdTaskShowDesc,
					args: []argSpec{{name: msg.ArgTask, desc: descText(msg.ArgTaskDesc), optional: true}},
					flags: []flagSpec{
						{name: "path", desc: descText(msg.FlagTaskShowPath)},
						jsonFlag,
					},
					run: runTaskShow,
				},
				{
					name: "task list", summary: msg.CmdTaskListSummary, desc: msg.CmdTaskListDesc,
					flags: []flagSpec{
						{name: "all", desc: descText(msg.FlagTaskListAll), group: "filter"},
						{name: "state", value: msg.ArgState, desc: descText(msg.FlagTaskListState), group: "filter"},
						{name: "project", value: msg.ArgProjectID, desc: descText(msg.FlagTaskListProject)},
						jsonFlag,
					},
					run: runTaskList,
				},
			},
		},
		{
			name: "stage", section: msg.HelpSectionTasks, summary: msg.CmdStageSummary, desc: msg.CmdStageDesc,
			actions: []command{
				{
					name: "stage show", summary: msg.CmdStageShowSummary, desc: msg.CmdStageShowDesc,
					flags: []flagSpec{taskFlag, jsonFlag},
					run:   runStageShow,
				},
				{
					name: "stage exit", summary: msg.CmdStageExitSummary, desc: msg.CmdStageExitDesc,
					flags: []flagSpec{
						{name: "kind", value: msg.ArgKind, desc: descText(msg.FlagExitKind)},
						{name: "text", value: msg.ArgText, desc: descText(msg.FlagExitText)},
						{name: "artifact", value: msg.ArgName, desc: descText(msg.FlagExitArtifact)},
						{name: "to", value: msg.ArgNode, desc: descText(msg.FlagExitTo)},
						{name: "reason", value: msg.ArgReason, desc: descText(msg.FlagExitReason)},
						taskFlag,
						inputFlag("kind, text, artifact, to, reason", false),
						jsonFlag,
					},
					run: runStageExit,
				},
				{
					name: "stage skip", summary: msg.CmdStageSkipSummary, desc: msg.CmdStageSkipDesc,
					flags: []flagSpec{
						{name: "reason", value: msg.ArgReason, desc: descText(msg.FlagSkipReason)},
						{name: "to", value: msg.ArgNode, desc: descText(msg.FlagExitTo)},
						taskFlag,
						inputFlag("reason, to", false),
						jsonFlag,
					},
					run: runStageSkip,
				},
			},
		},
		{
			name: "step", section: msg.HelpSectionTasks, summary: msg.CmdStepSummary, desc: msg.CmdStepDesc,
			actions: []command{
				{
					name: "step add", summary: msg.CmdStepAddSummary, desc: msg.CmdStepAddDesc,
					args:  []argSpec{{name: msg.ArgStep, desc: descText(msg.ArgStepDesc), many: true}},
					flags: []flagSpec{taskFlag, inputFlag("steps", true), jsonFlag},
					run:   runStepAdd,
				},
				{
					name: "step done", summary: msg.CmdStepDoneSummary, desc: msg.CmdStepDoneDesc,
					args: []argSpec{{name: msg.ArgNumber, desc: descText(msg.ArgStepNumberDesc)}},
					flags: []flagSpec{
						{name: "check", value: msg.ArgCheck, desc: descText(msg.FlagStepCheck)},
						taskFlag,
						inputFlag("step, check", true),
						jsonFlag,
					},
					run: runStepDone,
				},
				{
					name: "step drop", summary: msg.CmdStepDropSummary, desc: msg.CmdStepDropDesc,
					args: []argSpec{{name: msg.ArgNumber, desc: descText(msg.ArgStepNumberDesc)}},
					flags: []flagSpec{
						{name: "reason", value: msg.ArgReason, desc: descText(msg.FlagStepReason)},
						taskFlag,
						inputFlag("step, reason", true),
						jsonFlag,
					},
					run: runStepDrop,
				},
			},
		},
		{
			name: "note", section: msg.HelpSectionTasks, summary: msg.CmdNoteSummary, desc: msg.CmdNoteDesc,
			actions: []command{
				{
					name: "note add", summary: msg.CmdNoteAddSummary, desc: msg.CmdNoteAddDesc,
					args:  []argSpec{{name: msg.ArgText, desc: descText(msg.ArgNoteDesc)}},
					flags: []flagSpec{taskFlag, inputFlag("text", true), jsonFlag},
					run:   runNoteAdd,
				},
			},
		},
		{
			name: "artifact", section: msg.HelpSectionTasks, summary: msg.CmdArtifactSummary, desc: msg.CmdArtifactDesc,
			actions: []command{
				{
					name: "artifact save", summary: msg.CmdArtifactSaveSummary, desc: msg.CmdArtifactSaveDesc,
					args: []argSpec{{name: msg.ArgName, desc: descText(msg.ArgArtifactNameDesc)}},
					flags: []flagSpec{
						{name: "file", value: msg.ArgPath, desc: descText(msg.FlagArtifactFile), group: "source"},
						{name: "url", value: msg.ArgURL, desc: descText(msg.FlagArtifactURL), group: "source"},
						taskFlag,
						inputFlag("name, file, url", true),
						jsonFlag,
					},
					run: runArtifactSave,
				},
			},
		},
		{
			name: "setup", section: msg.HelpSectionMaint, summary: msg.CmdSetupSummary, desc: msg.CmdSetupDesc,
			args: []argSpec{{name: msg.ArgTool, desc: func() string {
				return msg.Text(msg.ArgToolDesc, strings.Join(tools, ", "))
			}}},
			flags: []flagSpec{jsonFlag},
			run:   runSetup,
		},
		{
			name: "version", section: msg.HelpSectionMaint, summary: msg.CmdVersionSummary, desc: msg.CmdVersionDesc,
			flags: []flagSpec{{name: "json", desc: descText(msg.FlagVersionJSON)}},
			run:   runVersion,
		},
		// help is a service command too: the help names --help, which every
		// command takes, and help stays for those used to git help.
		{name: "help", args: []argSpec{{}, {}}, run: runHelp},
		{name: "hook", args: []argSpec{{}}, run: runHook},
		{name: "events", flags: []flagSpec{{name: "json"}, {name: "after", value: msg.ArgNumber}}, run: runEvents},
	}
	for i := range cmds {
		for j := range cmds[i].actions {
			cmds[i].actions[j].section = cmds[i].section
		}
	}
	return cmds
}

// allCommands returns every command that runs on its own: top-level commands
// without actions and the actions of groups.
func allCommands() []command {
	var all []command
	for _, c := range commands() {
		if c.actions == nil {
			all = append(all, c)
		}
		all = append(all, c.actions...)
	}
	return all
}

// topLevel returns the top-level command named name.
func topLevel(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// lookup returns the command or action named name, such as "project add".
func lookup(name string) (command, bool) {
	for _, c := range allCommands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// action returns the action named a of group g.
func (g command) action(a string) (command, bool) {
	for _, c := range g.actions {
		if c.name == g.name+" "+a {
			return c, true
		}
	}
	return command{}, false
}

// actionNames returns the action names of group g, such as add, list.
func (g command) actionNames() []string {
	names := make([]string, len(g.actions))
	for i, c := range g.actions {
		names[i] = strings.TrimPrefix(c.name, g.name+" ")
	}
	return names
}

// Run executes the command given by args (without the program name)
// and returns the exit code.
func Run(args []string, env Env) int {
	env.json = hasJSONFlag(args)
	if len(args) == 0 {
		return runHelp(nil, env)
	}
	name := args[0]
	if name == "-h" || name == "--help" {
		return runHelp(nil, env)
	}
	c, ok := topLevel(name)
	if !ok {
		// An unknown command fails in JSON when asked: a client newer than this
		// gentry learns from the code that the command does not exist yet.
		return fail(env, unknownCommand(name))
	}
	args = args[1:]
	if c.actions != nil {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			return commandHelp(c, env)
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return fail(env, missingAction(c))
		}
		a, ok := c.action(args[0])
		if !ok {
			// As with an unknown command, in JSON when asked.
			return fail(env, unknownAction(c, args[0]))
		}
		c, args = a, args[1:]
	}
	// A command without --json refuses it in text: JSON output would suggest
	// the flag is supported.
	env.json = env.json && c.acceptsJSON()
	return c.run(args, env)
}

// acceptsJSON reports whether c declares the --json flag.
func (c command) acceptsJSON() bool {
	for _, f := range c.flags {
		if f.name == "json" {
			return true
		}
	}
	return false
}

// hasJSONFlag reports whether --json is among the flags, so that even a
// command line that cannot be parsed fails in JSON.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--json" {
			return true
		}
	}
	return false
}

func runVersion(args []string, env Env) int {
	f := newFlags("version")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	out := contract.VersionOutput{
		Gentry:          buildinfo.Version(),
		Contract:        contract.Version,
		StateSchema:     state.SchemaVersion(),
		KnowledgeFormat: project.KnowledgeFormat,
	}
	if *asJSON {
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	// The contract and format versions are for programs: only --json has them.
	fmt.Fprintln(env.Stdout, msg.Text(msg.VersionGentry, out.Gentry))
	return contract.ExitOK
}

// writeJSON prints v as the single JSON object of a --json command.
func writeJSON(env Env, v any) error {
	enc := json.NewEncoder(env.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
