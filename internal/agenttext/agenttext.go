// Package agenttext holds the texts Gentry gives the agent, such as the texts
// of skills: they are built into gentry, so they always match its commands.
// Every supported language has its own set of texts.
package agenttext

import (
	"embed"
	"regexp"
)

//go:embed ru/*.md
var files embed.FS

// language is the language of the texts. Only Russian exists in the first
// version.
const language = "ru"

// Skills of the integration, by the name of the skill.
const (
	WorkingOnTask = "working-on-task" // take a task and lead it to its closing
	CancelingTask = "canceling-task"  // cancel a task and sort out its changes
	EditingFlow   = "editing-flow"    // show, write, edit and apply the flow of a project
)

// FlowGuideFile is the name of the guide to the format of the flow.
const FlowGuideFile = "flow-guide.md"

// FlowGuidePath is where the guide lies in the directory of the skill
// editing-flow, which names it: reference material of a skill goes to
// references/, as the Agent Skills specification has it.
const FlowGuidePath = "references/" + FlowGuideFile

// Skill returns the text of the skill name. The skills name the tools of
// Gentry, not its program. References to skills stay neutral: see SkillRef.
func Skill(name string) string {
	b, err := files.ReadFile(language + "/" + name + ".md")
	if err != nil {
		panic("agenttext: no skill " + name)
	}
	return string(b)
}

// FlowGuide returns the guide to the format of the flow, which the agent
// reads before it writes a flow. It names no skills, so it is the same for
// every tool.
func FlowGuide() string {
	b, err := files.ReadFile(language + "/" + FlowGuideFile)
	if err != nil {
		panic("agenttext: no guide to the flow")
	}
	return string(b)
}

// SkillDir is the place of the directory of the skill in its text: the files
// of the skill lie there, and each tool names the directory its own way, such
// as ${CLAUDE_SKILL_DIR} in Claude Code.
const SkillDir = "<skill-dir>"

// skillRef matches a neutral reference to a skill: each tool names skills its
// own way, such as gentry:working-on-task in Claude Code.
var skillRef = regexp.MustCompile(`<skill:([a-z0-9-]+)>`)

// SkillRef returns the neutral reference to the skill name.
func SkillRef(name string) string { return "<skill:" + name + ">" }

// ResolveSkills replaces the neutral references to skills in text with the
// names ref gives them in a tool.
func ResolveSkills(text string, ref func(name string) string) string {
	return skillRef.ReplaceAllStringFunc(text, func(m string) string {
		return ref(skillRef.FindStringSubmatch(m)[1])
	})
}
