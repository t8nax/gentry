package msg

var ru = catalog{
	form:  russianForm,
	forms: 3,
	texts: map[Key]string{
		HelpIntro:           "Gentry ведёт задачи агента по флоу проекта и хранит их состояние.",
		HelpUsage:           "Использование:\n  gentry <команда> [аргументы]",
		HelpCommands:        "Команды:",
		CmdHelpSummary:      "показать эту справку",
		CmdVersionSummary:   "показать версию Gentry",
		ErrUnknownCommand:   "неизвестная команда «%s».",
		HintUnknownCommand:  "Перечень команд — gentry help.",
		ErrInternal:         "внутренний сбой Gentry: %v.",
		ErrUnexpectedArgs:   "команда %s не принимает аргументов.",
		ErrExtraArgs:        "лишние аргументы команды %s: %s.",
		ErrUnknownFlag:      "команда %s не принимает флаг %s.",
		ErrFlagValue:        "флаг %s не принимает значения.",
		VersionContract:     "контракт %d",
		ErrHookEventMissing: "команде hook нужно указать событие хука.",
		ErrHookEventUnknown: "неизвестное событие хука «%s».",
		HintHookEvents:      "Допустимые: %s.",
	},
	plurals: map[Key][]string{},
}
