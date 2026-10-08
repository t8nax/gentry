// Package agenttext holds the texts Gentry gives the agent, such as the texts
// of skills: they are built into gentry, so they always match its commands.
// Every supported language has its own set of texts.
package agenttext

import (
	"embed"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed ru/*.md
var files embed.FS

// language is the language of the texts. Only Russian exists in the first
// version.
const language = "ru"

// program is the place of the path of gentry in the texts: the program may
// be not in PATH, so the texts name it by its full path.
const program = "<gentry>"

// Skills of the integration, by the name of the skill.
const (
	WorkingOnTask = "working-on-task" // take a task and lead it to its closing
	CancelingTask = "canceling-task"  // cancel a task and sort out its changes
	EditingFlow   = "editing-flow"    // show, write, edit and apply the flow of a project
)

// FlowGuideFile is the name of the guide to the format of the flow beside
// the skill editing-flow, which names it.
const FlowGuideFile = "flow-guide.md"

// Skill returns the text of the skill name for the gentry at exe. The path
// has forward slashes, which every shell on Windows accepts. References to
// skills stay neutral: see SkillRef.
func Skill(name, exe string) string {
	b, err := files.ReadFile(language + "/" + name + ".md")
	if err != nil {
		panic("agenttext: no skill " + name)
	}
	return strings.ReplaceAll(string(b), program, filepath.ToSlash(exe))
}

// FlowGuide returns the guide to the format of the flow, which the agent
// reads before it writes a flow. It names neither the program nor skills, so
// it is the same for every tool.
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
