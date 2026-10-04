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
	CodeUnknownCommand   = "unknown_command"   // details: command
	CodeUnexpectedArgs   = "unexpected_args"   // details: command; args for a command that takes some
	CodeUnknownFlag      = "unknown_flag"      // details: command, flag
	CodeFlagValue        = "flag_value"        // a flag value is given where none is taken, missing or invalid; details: flag, value if given
	CodeMissingArgument  = "missing_argument"  // details: command, argument
	CodeInvalidArgument  = "invalid_argument"  // details: command, argument, value
	CodeHomeUnknown      = "home_unknown"      // the data root cannot be found: no GENTRY_HOME, no user home
	CodeIOError          = "io_error"          // reading or writing files failed; details: path
	CodeToolNotFound     = "tool_not_found"    // the AI tool program is not found; details: tool, program
	CodeToolFailed       = "tool_failed"       // a command of the AI tool failed; details: tool, command, output
	CodeStateNewer       = "state_newer"       // the state store was created by a newer Gentry; details: path, schema, supported
	CodeStateUnavailable = "state_unavailable" // the state store cannot be opened or written, even after retries; details: path
	CodeInternal         = "internal"          // a failure inside Gentry
)
