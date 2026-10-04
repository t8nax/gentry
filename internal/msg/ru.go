package msg

var ru = catalog{
	form:  russianForm,
	forms: 3,
	texts: map[Key]string{
		HelpIntro:         "Gentry ведёт задачи агента по флоу проекта и хранит их состояние.",
		HelpUsage:         "Использование:\n  gentry <команда> [аргументы]",
		HelpCommands:      "Команды:",
		CmdHelpSummary:    "показать эту справку",
		CmdVersionSummary: "показать версию Gentry",
		ErrUnknownCommand: "неизвестная команда «%s». Перечень команд — gentry help.",
		ErrUnexpectedArgs: "команда %s не принимает аргументов.",
		ErrUnknownFlag:    "команда %s не принимает флаг %s.",
		ErrFlagValue:      "флаг %s не принимает значения.",
		VersionContract:   "контракт %d",
	},
	plurals: map[Key][]string{},
}
