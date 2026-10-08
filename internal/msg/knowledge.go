package msg

import "fmt"

// KnowledgeKey identifies a text Gentry writes into the project knowledge,
// such as a commit message. Unlike operator texts, these are in the language
// of the knowledge, not of the operator: the whole team reads the knowledge.
type KnowledgeKey string

// Keys of knowledge texts. Every key must have a text in every knowledge
// language; TestKnowledgeComplete enforces this.
const (
	CommitProjectAdded KnowledgeKey = "commit.project_added"
	YAMLHeader         KnowledgeKey = "yaml.header"
	YAMLFormat         KnowledgeKey = "yaml.format"
	YAMLProject        KnowledgeKey = "yaml.project"
	YAMLLanguage       KnowledgeKey = "yaml.language"
)

// KnowledgeLanguages are the languages the knowledge can be written in.
var KnowledgeLanguages = []string{"ru", "en"}

var knowledge = map[string]map[KnowledgeKey]string{
	"ru": {
		CommitProjectAdded: "Подключение проекта %s к Gentry",
		YAMLHeader:         "Служебный файл Gentry. Изменяется командами Gentry.",
		YAMLFormat:         "версия формата знания",
		YAMLProject:        "идентификатор проекта",
		YAMLLanguage:       "язык знания: ru или en",
	},
	"en": {
		CommitProjectAdded: "Connect project %s to Gentry",
		YAMLHeader:         "Gentry service file. Changed by Gentry commands.",
		YAMLFormat:         "format version of the knowledge",
		YAMLProject:        "project identifier",
		YAMLLanguage:       "language of the knowledge: ru or en",
	},
}

// Knowledge returns the knowledge text for k in language lang, formatted with
// args.
func Knowledge(lang string, k KnowledgeKey, args ...any) string {
	t, ok := knowledge[lang][k]
	if !ok {
		return string(k)
	}
	if len(args) == 0 {
		return t
	}
	return fmt.Sprintf(t, args...)
}
