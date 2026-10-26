package update_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkingOnTaskReturnLimitRule checks that setup delivers the agreed
// rule about stage results and exhausted return limits to both AI tools.
func TestWorkingOnTaskReturnLimitRule(t *testing.T) {
	parallel(t)
	want := "Результат этапа и переход определяются только содержанием работы: разбором замечаний, проверкой условий постановки и оценкой условий переходов. Предел возвратов на эту оценку не влияет. Исчерпанный предел не является основанием отклонять замечания, считать условия выполненными или выбирать другой переход.\n\n" +
		"Если по существу нужен возврат, вызвать `task_stage_exit` с этим переходом, даже если предел уже исчерпан. Возврат сверх предела отклоняется. В этом случае спросить оператора, разрешает ли он ещё один возврат, и записать ответ с разрешением: `task_decision_add` с полем `allow_return`."
	for _, tool := range []string{"claude", "codex"} {
		t.Run(tool, func(t *testing.T) {
			parallel(t)
			s := installedStand(t, latest, false)
			if tool == "codex" {
				s.useCodex()
			}
			wantCode(t, s.gentry("setup", tool), 0)
			path := filepath.Join(s.data, "integrations", tool, "gentry", "skills", "working-on-task", "SKILL.md")
			text := strings.ReplaceAll(readFile(t, path), "\r\n", "\n")
			if n := strings.Count(text, "\n"+want+"\n"); n != 1 {
				t.Errorf("%s has %d copies of the agreed return-limit rule, want exactly one:\n%s", path, n, text)
			}
		})
	}
}
