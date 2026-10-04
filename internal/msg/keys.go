package msg

// Keys of operator-facing texts. Every key must have a text in every catalog;
// TestCatalogsComplete enforces this.
const (
	HelpIntro         Key = "help.intro"
	HelpUsage         Key = "help.usage"
	HelpCommands      Key = "help.commands"
	CmdHelpSummary    Key = "cmd.help.summary"
	CmdVersionSummary Key = "cmd.version.summary"
	ErrUnknownCommand Key = "err.unknown_command"
	ErrUnexpectedArgs Key = "err.unexpected_args"
	ErrUnknownFlag    Key = "err.unknown_flag"
	ErrFlagValue      Key = "err.flag_value"
	VersionContract   Key = "version.contract"
)
