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
	CodeUnknownCommand  = "unknown_command"  // details: command
	CodeUnexpectedArgs  = "unexpected_args"  // details: command; args for a command that takes some
	CodeUnknownFlag     = "unknown_flag"     // details: command, flag
	CodeFlagValue       = "flag_value"       // details: flag
	CodeMissingArgument = "missing_argument" // details: command, argument
	CodeInvalidArgument = "invalid_argument" // details: command, argument, value
	CodeHomeUnknown     = "home_unknown"     // the data root cannot be found: no GENTRY_HOME, no user home
	CodeIOError         = "io_error"         // reading or writing files failed; details: path
	CodeInternal        = "internal"         // a failure inside Gentry
)
