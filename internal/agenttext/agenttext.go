// Package agenttext holds the texts Gentry gives the agent, such as the texts
// of skills: they are built into gentry, so they always match its commands.
// Every supported language has its own set of texts.
package agenttext

import (
	"embed"
	"path/filepath"
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
)

// Skill returns the text of the skill name for the gentry at exe. The path
// has forward slashes, which every shell on Windows accepts.
func Skill(name, exe string) string {
	b, err := files.ReadFile(language + "/" + name + ".md")
	if err != nil {
		panic("agenttext: no skill " + name)
	}
	return strings.ReplaceAll(string(b), program, filepath.ToSlash(exe))
}
