package msg

// Keys of operator-facing texts. Every key must have a text in every catalog;
// TestCatalogsComplete enforces this.
const (
	HelpIntro           Key = "help.intro"
	HelpUsage           Key = "help.usage"
	HelpCommands        Key = "help.commands"
	CmdHelpSummary      Key = "cmd.help.summary"
	CmdVersionSummary   Key = "cmd.version.summary"
	ErrUnknownCommand   Key = "err.unknown_command"
	HintUnknownCommand  Key = "hint.unknown_command"
	ErrInternal         Key = "err.internal"
	ErrUnexpectedArgs   Key = "err.unexpected_args"
	ErrExtraArgs        Key = "err.extra_args"
	ErrUnknownFlag      Key = "err.unknown_flag"
	ErrFlagValue        Key = "err.flag_value"
	VersionContract     Key = "version.contract"
	ErrHookEventMissing Key = "err.hook_event_missing"
	ErrHookEventUnknown Key = "err.hook_event_unknown"
	HintHookEvents      Key = "hint.hook_events"
)
