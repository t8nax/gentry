package update_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/tests/testkit"
)

// GEN-70, 2. Claude Code и Codex на одной машине; 9. Скиллы и справочник в
// Codex; 10. Тексты, называвшие Claude Code.

// dirSum returns a sum of the files of the folder dir, to tell whether they
// changed; "" if there is no folder.
func dirSum(t *testing.T, dir string) string {
	t.Helper()
	if !testkit.FileExists(dir) {
		return ""
	}
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestSetupIndependent checks that the setup of one AI tool does not change
// the integration of the other.
func TestSetupIndependent(t *testing.T) {
	parallel(t)
	t.Run("codex_after_claude", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, true)
		integration := s.integrationSum()
		st := s.claude()
		settings := fileState(t, filepath.Join(s.claudeConfig, "settings.json"))
		s.useCodex()
		wantCode(t, s.gentry("setup", "codex"), 0)
		if s.integrationSum() != integration {
			t.Errorf("setup codex changed the folder %s", s.integration())
		}
		if after := s.claude(); !testkit.JSONEqual(after.Marketplaces, st.Marketplaces) || !testkit.JSONEqual(after.Plugins, st.Plugins) {
			t.Errorf("setup codex changed the plugin in Claude Code: %+v, was %+v", after, st)
		}
		wantFileState(t, filepath.Join(s.claudeConfig, "settings.json"), settings)
		for _, d := range []string{s.integration(), s.codexIntegration()} {
			if !testkit.FileExists(d) {
				t.Errorf("no folder %s", d)
			}
		}
	})
	t.Run("claude_after_codex", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		integration := dirSum(t, s.codexIntegration())
		config, hooks := fileState(t, s.codexConfigFile()), fileState(t, s.codexHooksFile())
		wantCode(t, s.gentry("setup", "claude"), 0)
		if dirSum(t, s.codexIntegration()) != integration {
			t.Errorf("setup claude changed the folder %s", s.codexIntegration())
		}
		wantFileState(t, s.codexConfigFile(), config)
		wantFileState(t, s.codexHooksFile(), hooks)
	})
}

// TestSetupElsewhereOtherTool checks that the plugin of Claude Code of
// another data root does not stop the setup of Codex.
func TestSetupElsewhereOtherTool(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	other := s.otherData(latest)
	s.gentryOf(other, "setup", "claude")
	s.useCodex()
	wantCode(t, s.gentry("setup", "codex"), 0)
	if p, ok := s.codexPlugin(); !ok || !p.Installed || !samePath(s.codexMarketplace(), s.codexIntegration()) {
		t.Errorf("Codex is not connected: the plugin %+v, the marketplace %s", p, s.codexMarketplace())
	}
}

// TestCodexSkills checks the texts of the skills of the plugin of Codex: the
// names of the skills and the path of the guide of the flow.
func TestCodexSkills(t *testing.T) {
	parallel(t)
	t.Run("skill_reference", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		dir := s.codexPluginDir()
		text := readFile(t, filepath.Join(dir, "skills", "editing-flow", "SKILL.md"))
		if !strings.Contains(text, "скилл `gentry:working-on-task`") {
			t.Errorf("skills/editing-flow/SKILL.md of the plugin of Codex does not name «скилл `gentry:working-on-task`»:\n%s", text)
		}
		for _, skill := range codexSkills {
			if text := readFile(t, filepath.Join(dir, "skills", skill, "SKILL.md")); strings.Contains(text, "<skill:") {
				t.Errorf("skills/%s/SKILL.md of the plugin of Codex has a placeholder <skill:", skill)
			}
		}
	})
	t.Run("flow_guide_path", func(t *testing.T) {
		parallel(t)
		s := installedStand(t, latest, false)
		s.connectCodex()
		guide := filepath.Join(s.data, "integrations", "codex", "gentry", "skills", "editing-flow", "references", "flow-guide.md")
		text := readFile(t, filepath.Join(s.codexPluginDir(), "skills", "editing-flow", "SKILL.md"))
		// The step names the path as a path of the text, in backquotes or
		// without them.
		want := []string{
			"2. Перед первой правкой прочитать справочник по формату флоу `" + guide + "` и писать файлы по нему.",
			"2. Перед первой правкой прочитать справочник по формату флоу " + guide + " и писать файлы по нему.",
		}
		if !slices.ContainsFunc(lines(text), func(l string) bool { return slices.Contains(want, l) }) {
			t.Errorf("skills/editing-flow/SKILL.md of the plugin of Codex has no step 2:\n%s\nwant:\n%s", text, want[0])
		}
		if !testkit.FileExists(guide) {
			t.Errorf("no file %s", guide)
		}
		if strings.Contains(text, "<skill-dir>") {
			t.Errorf("skills/editing-flow/SKILL.md of the plugin of Codex has the placeholder <skill-dir>")
		}
	})
}

// codexGuideParagraph is the paragraph of the guide of the flow about
// Codex.
const codexGuideParagraph = "В Codex возможности передаются субагенту текстом в начале его инструкции и не запрещают остальное; уровень рассуждения субагента — тот, что выбран в основной сессии."

// TestFlowGuideCodex checks that the guide of the flow of the plugins of
// both AI tools has the paragraph about Codex right after the list of the
// fields of a subagent.
func TestFlowGuideCodex(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, true)
	s.connectCodex()
	for _, tool := range []string{"claude", "codex"} {
		p := filepath.Join(s.data, "integrations", tool, "gentry", "skills", "editing-flow", "references", "flow-guide.md")
		ls := lines(readFile(t, p))
		i := slices.Index(ls, "Поля субагента:")
		if i < 0 {
			t.Errorf("%s has no line «Поля субагента:»", p)
			continue
		}
		// The list of the fields, after an empty line: its items and their
		// nested items.
		j := i + 1
		for j < len(ls) && ls[j] == "" {
			j++
		}
		for j < len(ls) && (strings.HasPrefix(ls[j], "- ") || strings.HasPrefix(ls[j], "  ")) {
			j++
		}
		if j+1 >= len(ls) || ls[j] != "" || ls[j+1] != codexGuideParagraph {
			t.Errorf("%s: after the list of the fields of a subagent comes %q, want an empty line and the paragraph «%s»", p, ls[j:min(j+2, len(ls))], codexGuideParagraph)
		}
	}
}

// TestSetupClaudeTexts checks that the texts of setup claude stay as they
// were: the first connection with no project.
func TestSetupClaudeTexts(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	r := s.gentry("setup", "claude")
	wantCode(t, r, 0)
	want := testkit.Lines(
		"Gentry подключён к Claude Code.",
		"Плагин начнёт работать со следующей сессии.",
		"Инструменты Gentry разрешены в настройках Claude Code.",
		"Настройки: "+filepath.Join(s.claudeConfig, "settings.json"))
	if r.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
}
