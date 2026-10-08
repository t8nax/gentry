// Package integration describes, independently of any tool, what Gentry
// installs into the tool the agent works in. Tool adapters turn this
// description into the tool's own format, such as a Claude Code plugin.
package integration

import (
	"github.com/t8nax/gentry/internal/agenttext"
	"github.com/t8nax/gentry/internal/hook"
	"github.com/t8nax/gentry/internal/msg"
)

// Name is the name of the integration in every tool.
const Name = "gentry"

// Description is the neutral description of the integration.
type Description struct {
	Name        string
	Description string // operator-facing, in the operator's language
	Hooks       []Hook
	Skills      []Skill
}

// Hook is a command the tool runs on a hook event.
type Hook struct {
	Event   string   // a neutral event from package hook, such as hook.SessionStart
	Command []string // program and arguments
}

// Skill is an entry point of the agent for the words of the operator.
type Skill struct {
	Name        string
	Description string // when to use the skill; the tool shows it to the agent and the operator
	Text        string // what the agent does, in markdown
	// Files lie beside the text, by name, such as a guide the text names:
	// the agent reads them when it needs them.
	Files map[string]string
}

// skills are the skills of Gentry with the keys of their descriptions.
var skills = []struct {
	name        string
	description msg.Key
	files       func() map[string]string
}{
	{agenttext.WorkingOnTask, msg.SkillWorkingOnTask, nil},
	{agenttext.CancelingTask, msg.SkillCancelingTask, nil},
	{agenttext.EditingFlow, msg.SkillEditingFlow, func() map[string]string {
		return map[string]string{agenttext.FlowGuideFile: agenttext.FlowGuide()}
	}},
}

// Gentry returns the integration description for the gentry binary at exe.
func Gentry(exe string) Description {
	d := Description{
		Name:        Name,
		Description: msg.Text(msg.HelpIntro),
		Hooks: []Hook{
			{Event: hook.SessionStart, Command: []string{exe, "hook", hook.SessionStart}},
			{Event: hook.PreTool, Command: []string{exe, "hook", hook.PreTool}},
			{Event: hook.PostTool, Command: []string{exe, "hook", hook.PostTool}},
			{Event: hook.Stop, Command: []string{exe, "hook", hook.Stop}},
		},
	}
	for _, s := range skills {
		sk := Skill{Name: s.name, Description: msg.Text(s.description), Text: agenttext.Skill(s.name, exe)}
		if s.files != nil {
			sk.Files = s.files()
		}
		d.Skills = append(d.Skills, sk)
	}
	return d
}
