package contract

// Exit codes of gentry.
const (
	ExitOK    = 0 // success
	ExitError = 1 // the command failed while running
	ExitUsage = 2 // the command line is wrong: unknown command, flag or argument
)

// Error codes in ErrorOutput. The list is open: new codes come with new
// commands, and a client shows an unknown code as a generic error.
const (
	CodeUnknownCommand       = "unknown_command"         // details: command
	CodeUnexpectedArgs       = "unexpected_args"         // details: command; args for a command that takes some
	CodeUnknownFlag          = "unknown_flag"            // details: command, flag
	CodeFlagValue            = "flag_value"              // a flag value is given where none is taken, missing or invalid; details: flag, value if given
	CodeMissingArgument      = "missing_argument"        // details: command, argument
	CodeInvalidArgument      = "invalid_argument"        // details: command, argument, value
	CodeHomeUnknown          = "home_unknown"            // the data root cannot be found: no GENTRY_HOME, no user home
	CodeIOError              = "io_error"                // reading or writing files failed; details: path
	CodeToolNotFound         = "tool_not_found"          // the AI tool program is not found; details: tool, program
	CodeToolFailed           = "tool_failed"             // a command of the AI tool failed; details: tool, command, output
	CodeStateNewer           = "state_newer"             // the state store was created by a newer Gentry; details: path, schema, supported
	CodeStateUnavailable     = "state_unavailable"       // the state store cannot be opened or written, even after retries; details: path
	CodeProjectExists        = "project_exists"          // the project is connected with another knowledge or main worktree; details: project, knowledge, main_worktree
	CodePrefixTaken          = "prefix_taken"            // another project of the machine has the prefix; details: prefix, project
	CodeNotGitRepo           = "not_git_repo"            // the directory is not a git worktree; details: path
	CodeKnowledgeInvalid     = "knowledge_invalid"       // the directory cannot hold the project knowledge; details: path, reason (open list: not_empty, foreign_repo, nested, bad_file)
	CodeKnowledgeNewer       = "knowledge_newer"         // the knowledge has a newer format; details: path, format, supported
	CodeKnowledgeMismatch    = "knowledge_mismatch"      // the identifier given differs from the one in the knowledge; details: value, knowledge_value
	CodeWorktreeTaken        = "worktree_taken"          // the worktree is in the pool of another project; details: path, project
	CodeWorktreeInvalid      = "worktree_invalid"        // the directory is the knowledge of a project or lies within it; details: path
	CodeProjectUndetermined  = "project_undetermined"    // no project is named and none is found by the directory; details: dir
	CodeProjectNotFound      = "project_not_found"       // the project named is not connected; details: project
	CodeGitNotFound          = "git_not_found"           // the git program is not found
	CodeGitFailed            = "git_failed"              // a git command failed; details: command, output
	CodeFlowNotFound         = "flow_not_found"          // the project has no active flow; details: project, dir
	CodeFlowInvalid          = "flow_invalid"            // the active flow does not pass the checks of this Gentry; details: FlowInvalidDetails
	CodeFlowObjectNotFound   = "flow_object_not_found"   // the flow or the draft has no object of that identifier; details: project, kind (scenario, stage, agent, part), id, draft
	CodeFlowDraftNotFound    = "flow_draft_not_found"    // the flow directory has no changes; details: project, dir
	CodeFlowDraftInvalid     = "flow_draft_invalid"      // the flow draft has problems; details: FlowDraftInvalidDetails
	CodeConflictingFlags     = "conflicting_flags"       // flags that cannot be given together; details: command, flags
	CodeLibraryDraftNotFound = "library_draft_not_found" // the library directory has no changes; details: dir
	CodeLibraryDraftInvalid  = "library_draft_invalid"   // the library draft has problems; details: LibraryDraftInvalidDetails
	CodeProcessRemoteNotSet  = "process_remote_not_set"  // no remote repository of the process is connected
	CodeRemoteUnavailable    = "remote_unavailable"      // the remote repository does not answer or refuses access; details: remote, output (what git printed)
	CodeRemoteInvalid        = "remote_invalid"          // the remote repository is not a process repository or has a newer format; details: remote, reason (not_process, newer_format)
	CodeProcessInvalid       = "process_invalid"         // the process directory is another git repository or has a newer format; details: path, reason (not_process, newer_format)
	CodeProcessBusy          = "process_busy"            // another command of Gentry holds the process repository too long; details: path
	CodeFlowConflict         = "flow_conflict"           // the synchronization before flow apply found the flow changed on another machine: the draft is not applied; details: project, conflict_dir
	CodeLibraryConflict      = "library_conflict"        // the synchronization before library apply found the library changed on another machine: the draft is not applied; details: conflict_dir
	CodeInternal             = "internal"                // a failure inside Gentry
)

// Kinds of flow problems, the code of FlowProblem. The list is open: new kinds
// come with new flow fields, and a client shows an unknown kind by its message.
const (
	ProblemSyntax                = "syntax"                  // a file is not YAML or not in UTF-8
	ProblemUnknownField          = "unknown_field"           // a field the flow does not have
	ProblemUnsupportedField      = "unsupported_field"       // a field of a later version of Gentry: tracker actions, procedures
	ProblemMissingField          = "missing_field"           // a required field is absent or empty
	ProblemInvalidValue          = "invalid_value"           // a value of the wrong kind, such as a string for a list, or an unknown capability
	ProblemInvalidID             = "invalid_id"              // an invalid identifier of a scenario, stage, part, node or subagent
	ProblemExtraFile             = "extra_file"              // a file or directory that does not belong to the flow
	ProblemMissingInstruction    = "missing_instruction"     // a stage or a subagent has fields but no instruction
	ProblemOrphanInstruction     = "orphan_instruction"      // a stage or a subagent has an instruction but no fields
	ProblemNoScenarios           = "no_scenarios"            // the flow has no scenario
	ProblemUnknownStage          = "unknown_stage"           // a node names a stage that does not exist
	ProblemUnknownPart           = "unknown_part"            // include names a part that does not exist
	ProblemUnknownExecutor       = "unknown_executor"        // executor names a subagent that does not exist
	ProblemUnknownNode           = "unknown_node"            // the start node or a transition target does not exist
	ProblemReservedNode          = "reserved_node"           // a node is named finish
	ProblemUnreachable           = "unreachable"             // a node is not reachable from the start node
	ProblemDeadEnd               = "dead_end"                // the end of the scenario is not reachable from a node
	ProblemUnlimitedLoop         = "unlimited_loop"          // a loop has no transition with a limit of rounds
	ProblemLimitOutsideLoop      = "limit_outside_loop"      // a transition with a limit of rounds closes no loop
	ProblemLimitWithoutCondition = "limit_without_condition" // a transition with a limit of rounds has no condition
	ProblemSeveralDefaults       = "several_defaults"        // a node has more than one transition without a condition
	ProblemDuplicateTransition   = "duplicate_transition"    // a node has two transitions to one node
)
