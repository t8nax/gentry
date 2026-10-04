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
	},
	plurals: map[Key][]string{},
}
