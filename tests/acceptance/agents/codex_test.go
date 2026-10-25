package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// GEN-70, 5. Субагенты в формате Codex: the layout in the format of each AI
// tool connected. Claude Code is connected by the folder of its integration,
// Codex by gentry setup codex with the fake Codex, before the shop is
// connected.

// codexFile is the file of the subagent of Codex in worktree w.
func codexFile(w, agent string) string { return filepath.Join(w, ".codex", "agents", agent+".toml") }

// The subagent reviewer of the specification: the shorthand «субагент
// reviewer».
const (
	reviewerYAML        = "purpose: ревью изменений заказа — корректность и тесты\ncapabilities: [read, search, task]\nmodel: strong\n"
	reviewerInstruction = "Проверить изменения задачи и вернуть замечания с местом в коде.\n"
)

// The lines of the capabilities of reviewer in the instruction of its file
// of Codex.
const (
	reviewerCan    = "Возможности: читать файлы, искать по коду, смотреть задачу инструментами Gentry."
	reviewerCannot = "Не делать: править файлы, запускать команды в оболочке, обращаться к сети, вести ход работы задачи (шаги, заметки, артефакты)."
)

// specReviewer makes the shared reviewer of the shop that of the
// specification and applies it.
func specReviewer(t *testing.T) {
	t.Helper()
	testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{"reviewer.yaml": reviewerYAML, "reviewer.md": reviewerInstruction})
	testkit.MustRun(t, "flow", "apply")
}

// str returns v as a string; "" if it is none.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// readTOML returns the file of TOML p.
func readTOML(t *testing.T, p string) map[string]any {
	t.Helper()
	text := read(t, p)
	doc, err := toml.Parse(text)
	if err != nil {
		t.Fatalf("%s is no TOML: %v\n%s", p, err, text)
	}
	return doc
}

// gitShows reports whether git status of the worktree w shows the file rel.
func gitShows(t *testing.T, w, rel string) bool {
	t.Helper()
	status := gittest.Run(t, w, "status", "--porcelain", "--untracked-files=all")
	return strings.Contains(status, rel)
}

// TestAgentsFormats checks that the subagents are laid out in the format of
// each AI tool connected and in no other.
func TestAgentsFormats(t *testing.T) {
	t.Run("codex_only", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		if !testkit.FileExists(codexFile(a, "reviewer")) {
			t.Errorf("no %s", codexFile(a, "reviewer"))
		}
		if testkit.FileExists(reviewerFile(a)) {
			t.Errorf("%s is laid out, Claude Code is not connected", reviewerFile(a))
		}
	})
	t.Run("claude_only", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, false)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		if !testkit.FileExists(reviewerFile(a)) {
			t.Errorf("no %s", reviewerFile(a))
		}
		if p := filepath.Join(a, ".codex", "agents"); testkit.FileExists(p) {
			t.Errorf("%s is there, Codex is not connected", p)
		}
	})
	t.Run("both", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		for _, p := range []string{reviewerFile(a), codexFile(a, "reviewer")} {
			if !testkit.FileExists(p) {
				t.Errorf("no %s", p)
			}
		}
	})
	t.Run("task_take", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		files := []string{reviewerFile(a), codexFile(a, "reviewer")}
		for _, p := range files {
			os.Remove(p)
		}
		testkit.MustRun(t, testkit.TakeArgs()...)
		taken := map[string]string{}
		for _, p := range files {
			if !testkit.FileExists(p) {
				t.Fatalf("no %s after task take", p)
			}
			taken[p] = read(t, p)
			if !strings.Contains(taken[p], "Проверить изменения задачи и вернуть замечания") {
				t.Errorf("%s is not the reviewer of the flow:\n%s", p, taken[p])
			}
		}
		// The worktree of the task keeps the subagents of its snapshot.
		testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{"reviewer.md": "Проверить заказ.\n"})
		testkit.MustRun(t, "flow", "apply")
		for _, p := range files {
			if read(t, p) != taken[p] {
				t.Errorf("%s of the worktree of the task changed after flow apply", p)
			}
		}
	})
}

// TestAgentsDeployTools checks the field tools of the JSON of agents deploy.
func TestAgentsDeployTools(t *testing.T) {
	testkit.ToolsShopFlow(t, true, true)
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/agents-deploy.json", "agents", "deploy", "--project", "shop")
	var res struct {
		Tools []string `json:"tools"`
	}
	json.Unmarshal([]byte(out), &res)
	if strings.Join(res.Tools, ",") != "claude,codex" {
		t.Errorf("agents deploy --json: %s\nwant tools [\"claude\", \"codex\"]", out)
	}
}

// TestAgentsUnconnectedFormat checks that the files of Gentry of the format
// of an AI tool no longer connected are left as they are and stay hidden
// from git.
func TestAgentsUnconnectedFormat(t *testing.T) {
	testkit.ToolsShopFlow(t, true, true)
	specReviewer(t)
	a := testkit.MustWd(t)
	testkit.DisconnectClaude(t)
	before := read(t, reviewerFile(a))
	if !strings.Contains(before, "# Gentry:") {
		t.Fatalf("stand: %s has no mark of Gentry:\n%s", reviewerFile(a), before)
	}
	// A new instruction of reviewer: a file laid out anew would have it.
	const changed = "Проверить изменения заказа и тексты для оператора.\n"
	testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{"reviewer.md": changed})
	testkit.MustRun(t, "flow", "apply")
	testkit.MustRun(t, "agents", "deploy", "--project", "shop")
	if !testkit.FileExists(reviewerFile(a)) || read(t, reviewerFile(a)) != before {
		t.Errorf("%s changed", reviewerFile(a))
	}
	if doc := readTOML(t, codexFile(a, "reviewer")); !strings.HasSuffix(str(doc["developer_instructions"]), "\n\n"+changed) {
		t.Errorf("%s has not the new instruction of reviewer: %q", codexFile(a, "reviewer"), doc["developer_instructions"])
	}
	for _, rel := range []string{".claude/agents/reviewer.md", ".codex/agents/reviewer.toml"} {
		if gitShows(t, a, rel) {
			t.Errorf("git status of %s shows %s", a, rel)
		}
	}
}

// TestAgentsDeployNoTools checks agents deploy with no AI tool connected.
func TestAgentsDeployNoTools(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, false)
		a := testkit.MustWd(t)
		code, stdout, _ := testkit.Run("agents", "deploy", "--project", "shop")
		want := testkit.Lines("ИИ-инструменты не подключены: субагентов некуда раскладывать.", "", "Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>")
		if code != contract.ExitOK || stdout != want {
			t.Errorf("agents deploy: exit code %d, stdout:\n%s\nwant 0 and:\n%s", code, stdout, want)
		}
		for _, d := range []string{filepath.Join(a, ".claude", "agents"), filepath.Join(a, ".codex", "agents")} {
			if testkit.FileExists(d) {
				t.Errorf("%s is there", d)
			}
		}
	})
	t.Run("unknown_project", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, false)
		if code, c := testkit.ErrorCode(t, "agents", "deploy", "--project", "nope"); code != contract.ExitError || c != contract.CodeProjectNotFound {
			t.Errorf("agents deploy --project nope: exit code %d, the error %s, want 1 and project_not_found", code, c)
		}
	})
	t.Run("json", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, false)
		out := testkit.WantJSON(t, contract.ExitOK, "schemas/agents-deploy.json", "agents", "deploy", "--project", "shop")
		var fields map[string]json.RawMessage
		json.Unmarshal([]byte(out), &fields)
		if string(fields["project"]) != `"shop"` || string(fields["changed"]) != "false" || string(fields["worktrees"]) != "[]" || string(fields["tools"]) != "[]" {
			t.Errorf("agents deploy --json: %s\nwant project shop, changed false, worktrees [], tools []", out)
		}
	})
}

// TestTakeNoTools checks that task take with no AI tool connected skips the
// layout silently. The worktree has no files of subagents, as a layout
// would add them.
func TestTakeNoTools(t *testing.T) {
	testkit.ToolsShopFlow(t, false, false)
	a := testkit.MustWd(t)
	dirs := []string{filepath.Join(a, ".claude"), filepath.Join(a, ".codex")}
	for _, d := range dirs {
		os.RemoveAll(d)
	}
	out := testkit.WantJSON(t, contract.ExitOK, "schemas/task-take.json", "task", "take", "--scenario", "feature", "--title", "Заказ", "--statement", "Оформить заказ")
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(out), &fields)
	if _, ok := fields["agents"]; ok {
		t.Errorf("task take --json: %s\nwant no field agents", out)
	}
	for _, d := range dirs {
		if testkit.FileExists(d) {
			t.Errorf("task take laid out the subagents in %s", d)
		}
	}
	if _, stdout, _ := testkit.Run("task", "list"); !strings.Contains(stdout, "SHOP-1") {
		t.Errorf("the task is not taken:\n%s", stdout)
	}
}

// TestCodexAgentFile checks the file of a subagent of Codex.
func TestCodexAgentFile(t *testing.T) {
	t.Run("reviewer", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		p := codexFile(a, "reviewer")
		if first, _, _ := strings.Cut(read(t, p), "\n"); strings.TrimSuffix(first, "\r") != "# Gentry: файл разложен из флоу проекта и перезаписывается при раскладке субагентов." {
			t.Errorf("the first line of %s: %q, want the mark of Gentry", p, first)
		}
		doc := readTOML(t, p)
		if keys, err := toml.TopKeys(read(t, p)); err != nil || strings.Join(keys, ",") != "name,description,model,developer_instructions" || len(doc) != 4 {
			t.Errorf("%s: the keys %q (%v), want name, description, model, developer_instructions, in this order, and no other", p, keys, err)
		}
		want := map[string]any{
			"name":                   "reviewer",
			"description":            "ревью изменений заказа — корректность и тесты",
			"model":                  "gpt-6-astra",
			"developer_instructions": reviewerCan + "\n" + reviewerCannot + "\n\n" + reviewerInstruction,
		}
		for k, v := range want {
			if doc[k] != v {
				t.Errorf("%s: %s = %q, want %q", p, k, doc[k], v)
			}
		}
		for _, k := range []string{"model_reasoning_effort", "features"} {
			if _, ok := doc[k]; ok {
				t.Errorf("%s has the key %s", p, k)
			}
		}
	})
	t.Run("special_characters", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		// The lines of the capabilities of reviewer are stated by the case
		// reviewer: here they are those of its file before the change.
		specReviewer(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		lines, ok := strings.CutSuffix(str(readTOML(t, codexFile(testkit.MustWd(t), "reviewer"))["developer_instructions"]), reviewerInstruction)
		if !ok {
			t.Fatalf("stand: the instruction of reviewer does not end with its instruction")
		}
		const purpose = `ревью "заказа" и путей C:\shop\cart`
		const instruction = "Первый абзац с \"\"\" и ''' внутри.\n\nВторой абзац: \\n — не перевод строки.\n\nТретий абзац.\n"
		testkit.WriteFiles(t, testkit.ShopPlaces().Shared, map[string]string{
			"reviewer.yaml": "purpose: '" + purpose + "'\ncapabilities: [read, search, task]\n",
			"reviewer.md":   instruction,
		})
		testkit.MustRun(t, "flow", "apply")
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		doc := readTOML(t, codexFile(a, "reviewer"))
		if doc["description"] != purpose {
			t.Errorf("description = %q, want %q", doc["description"], purpose)
		}
		if want := lines + instruction; doc["developer_instructions"] != want {
			t.Errorf("developer_instructions = %q, want %q", doc["developer_instructions"], want)
		}
	})
}

// codexAgents lays out the shared subagents of the files named, which the
// stage plan-feature names, and returns their files of Codex by
// identifier. The subagents have the instruction agentInstruction.
func codexAgents(t *testing.T, yamls map[string]string) map[string]map[string]any {
	t.Helper()
	testkit.ToolsShopFlow(t, false, true)
	p := testkit.ShopPlaces()
	files := map[string]string{}
	var ids []string
	for id, y := range yamls {
		files[id+".yaml"] = y
		files[id+".md"] = agentInstruction
		ids = append(ids, id)
	}
	testkit.WriteFiles(t, p.Shared, files)
	testkit.WriteDraft(t, p, map[string]string{
		"stages/plan-feature.yaml": "title: План фичи\nexit: план согласован с оператором\nexecutor: orchestrator\ninclude: [plan-format]\nagents: [" + strings.Join(ids, ", ") + "]\n",
	})
	testkit.MustRun(t, "flow", "apply")
	testkit.MustRun(t, "agents", "deploy", "--project", "shop")
	a := testkit.MustWd(t)
	out := map[string]map[string]any{}
	for _, id := range ids {
		out[id] = readTOML(t, codexFile(a, id))
	}
	return out
}

// agentInstruction is the instruction of the subagents of codexAgents.
const agentInstruction = "Найти места изменения и описать их.\n"

// TestCodexAgentModels checks that the model of a subagent is turned into a
// model of Codex.
func TestCodexAgentModels(t *testing.T) {
	agents := codexAgents(t, map[string]string{
		"scout":   "purpose: разведка\ncapabilities: [read]\nmodel: light\n",
		"coder":   "purpose: правка\ncapabilities: [read, edit]\nmodel: standard\n",
		"auditor": "purpose: аудит\ncapabilities: [read]\nmodel: strong\n",
		"helper":  "purpose: помощь\ncapabilities: [read]\n",
	})
	for id, want := range map[string]string{"scout": "gpt-6-luna", "coder": "gpt-6.1-sol", "auditor": "gpt-6-astra"} {
		if got := agents[id]["model"]; got != want {
			t.Errorf("%s: model = %v, want %s", id, got, want)
		}
	}
	if got, ok := agents["helper"]["model"]; ok {
		t.Errorf("helper without a model: model = %v, want no key", got)
	}
}

// instructionOf returns developer_instructions of the subagent scout of
// codexAgents with the capabilities.
func instructionOf(t *testing.T, capabilities string) (string, map[string]any) {
	t.Helper()
	doc := codexAgents(t, map[string]string{"scout": "purpose: разведка\ncapabilities: " + capabilities + "\n"})["scout"]
	s, _ := doc["developer_instructions"].(string)
	return s, doc
}

// TestCodexAgentCapabilities checks the line of the capabilities of a
// subagent of Codex.
func TestCodexAgentCapabilities(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		got, _ := instructionOf(t, "[read, search, edit, run, web, task, progress]")
		want := "Возможности: читать файлы, искать по коду, править файлы, запускать команды в оболочке, обращаться к сети, смотреть задачу и вести ход работы инструментами Gentry (шаги, заметки, артефакты).\n\n" + agentInstruction
		if got != want {
			t.Errorf("developer_instructions = %q, want %q", got, want)
		}
	})
	t.Run("none", func(t *testing.T) {
		got, _ := instructionOf(t, "[]")
		want := "Не делать: читать файлы, искать по коду, править файлы, запускать команды в оболочке, обращаться к сети, вызывать инструменты Gentry.\n\n" + agentInstruction
		if got != want {
			t.Errorf("developer_instructions = %q, want %q", got, want)
		}
	})
}

// TestCodexAgentRestrictions checks the line of what a subagent of Codex
// does not do.
func TestCodexAgentRestrictions(t *testing.T) {
	t.Run("no_gentry_tools", func(t *testing.T) {
		got, _ := instructionOf(t, "[read, search, run]")
		want := "Возможности: читать файлы, искать по коду, запускать команды в оболочке.\nНе делать: править файлы, обращаться к сети, вызывать инструменты Gentry.\n\n" + agentInstruction
		if got != want {
			t.Errorf("developer_instructions = %q, want %q", got, want)
		}
	})
	t.Run("no_shell", func(t *testing.T) {
		got, doc := instructionOf(t, "[read]")
		var cannot string
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(l, "Не делать:") {
				cannot = l
			}
		}
		if !strings.Contains(cannot, "запускать команды в оболочке") {
			t.Errorf("developer_instructions = %q, want the line «Не делать:» with «запускать команды в оболочке»", got)
		}
		if v, ok := toml.At(doc, "features", "shell_tool"); ok {
			t.Errorf("the file has features.shell_tool = %v", v)
		}
	})
}

// TestAgentsHidden checks that the files of both formats are hidden from
// git of the project.
func TestAgentsHidden(t *testing.T) {
	t.Run("both_formats", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		for _, rel := range []string{".claude/agents/reviewer.md", ".codex/agents/reviewer.toml"} {
			if !testkit.FileExists(filepath.Join(a, filepath.FromSlash(rel))) || gitShows(t, a, rel) {
				t.Errorf("%s is not laid out in %s or git status shows it", rel, a)
			}
		}
	})
	t.Run("other_worktree", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		b := gittest.Worktree(t, a, filepath.Join(filepath.Dir(a), "shop-b"), "b")
		testkit.MustRun(t, "worktree", "add", b)
		for _, rel := range []string{".claude/agents/reviewer.md", ".codex/agents/reviewer.toml"} {
			if gitShows(t, a, rel) {
				t.Errorf("git status of %s shows %s after worktree add %s", a, rel, b)
			}
		}
	})
}

// TestCodexAgentConflict checks the conflict and the removal of the files of
// Codex.
func TestCodexAgentConflict(t *testing.T) {
	t.Run("foreign", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		const own = "name = \"reviewer\"\ndescription = \"свой\"\ndeveloper_instructions = \"Свой ревьюер.\"\n"
		testkit.WriteFiles(t, a, map[string]string{".codex/agents/reviewer.toml": own})
		out := testkit.WantJSON(t, contract.ExitError, "schemas/error.json", "agents", "deploy", "--project", "shop")
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Details struct {
					Conflicts []contract.AgentsConflict `json:"conflicts"`
				} `json:"details"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(out), &e)
		found := false
		for _, c := range e.Error.Details.Conflicts {
			found = found || c.File == ".codex/agents/reviewer.toml" && c.Reason == "foreign"
		}
		if e.Error.Code != contract.CodeAgentsConflict || !found {
			t.Errorf("agents deploy --json: %s\nwant agents_conflict with .codex/agents/reviewer.toml foreign", out)
		}
		_, _, stderr := testkit.Run("agents", "deploy", "--project", "shop")
		if !strings.Contains(stderr, ".codex/agents/reviewer.toml") || !strings.Contains(stderr, msg.Text(msg.AgentsReasonForeign)) {
			t.Errorf("agents deploy: stderr:\n%s\nwant .codex/agents/reviewer.toml with «%s»", stderr, msg.Text(msg.AgentsReasonForeign))
		}
		if read(t, codexFile(a, "reviewer")) != own {
			t.Error("the file of the operator changed")
		}
	})
	t.Run("removed", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		if !testkit.FileExists(codexFile(a, "reviewer")) {
			t.Fatalf("stand: no %s", codexFile(a, "reviewer"))
		}
		testkit.WriteDraft(t, testkit.ShopPlaces(), map[string]string{
			"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n",
		})
		testkit.MustRun(t, "flow", "apply")
		if testkit.FileExists(codexFile(a, "reviewer")) {
			t.Errorf("%s is still there", codexFile(a, "reviewer"))
		}
	})
	t.Run("marked", func(t *testing.T) {
		testkit.ToolsShopFlow(t, false, true)
		specReviewer(t)
		testkit.MustRun(t, "agents", "deploy", "--project", "shop")
		code, stdout, _ := testkit.Run("agents", "deploy", "--project", "shop")
		if first, _, _ := strings.Cut(stdout, "\n"); code != contract.ExitOK || first != msg.Text(msg.AgentsInPlace, "shop") {
			t.Errorf("agents deploy again: exit code %d, stdout:\n%s\nwant 0 and «%s»", code, stdout, msg.Text(msg.AgentsInPlace, "shop"))
		}
	})
}

// deployHeader is the header of the table of agents deploy.
var deployHeader = []string{msg.Text(msg.ColWorktree), msg.Text(msg.ColTask), msg.Text(msg.ColAgents), msg.Text(msg.ColChanges)}

// TestAgentsChanges checks that the output of the layout names a subagent
// without its format: added if a file of it is added.
func TestAgentsChanges(t *testing.T) {
	t.Run("codex_after_claude", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, false)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.ConnectCodex(t)
		if !testkit.FileExists(reviewerFile(a)) {
			t.Fatal("stand: Claude reviewer is missing")
		}
		// Setup may already lay out Codex; this deploy must add its format.
		if err := os.Remove(codexFile(a, "reviewer")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		testkit.WantRun(t, contract.ExitOK, testkit.Lines(msg.Text(msg.AgentsDeployed, "shop"), nextSession, "")+testkit.Table(deployHeader,
			[]string{a, testkit.None, "reviewer", msg.Text(msg.AgentAdded, "reviewer")},
		), "", "agents", "deploy", "--project", "shop")
	})
	t.Run("added_and_updated", func(t *testing.T) {
		testkit.ToolsShopFlow(t, true, true)
		specReviewer(t)
		a := testkit.MustWd(t)
		testkit.WriteFiles(t, a, map[string]string{".claude/agents/reviewer.md": read(t, reviewerFile(a)) + "\nПравка вручную.\n"})
		os.Remove(codexFile(a, "reviewer"))
		testkit.WantRun(t, contract.ExitOK, testkit.Lines(msg.Text(msg.AgentsDeployed, "shop"), nextSession, "")+testkit.Table(deployHeader,
			[]string{a, testkit.None, "reviewer", msg.Text(msg.AgentAdded, "reviewer")},
		), "", "agents", "deploy", "--project", "shop")
	})
}

// TestAgentsDeployHelp checks the description of agents deploy: one line.
func TestAgentsDeployHelp(t *testing.T) {
	code, stdout, _ := testkit.Run("agents", "deploy", "--help")
	ls := strings.Split(stdout, "\n")
	if code != contract.ExitOK || len(ls) < 2 || ls[0] != "Разложить субагентов флоу во все рабочие копии пула проекта в формате каждого подключённого ИИ-инструмента." || ls[1] != "" {
		t.Errorf("agents deploy --help: exit code %d, stdout:\n%s\nwant the description of one line «Разложить субагентов флоу во все рабочие копии пула проекта в формате каждого подключённого ИИ-инструмента.»", code, stdout)
	}
}
