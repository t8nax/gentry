package flow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/msg"
)

// withCRM connects a second project, the crm, whose flow is the flow of the
// shop and so names the shared reviewer too, and applies it. It returns the
// places of the flow of the crm; the test runs in the main worktree of the
// shop.
func withCRM(t *testing.T, p flow.Places) flow.Places {
	t.Helper()
	shop, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	crm := gittest.Repo(t, filepath.Join(filepath.Dir(shop), "crm"))
	t.Chdir(crm)
	clitest.MustRun(t, "project", "add", "crm", "--knowledge", "../crm-knowledge")
	c := p
	c.Project, c.Dir, c.Conflict = "crm", filepath.Join(filepath.Dir(p.Library), "crm", "flow"), filepath.Join(filepath.Dir(p.Library), "crm", "conflict")
	clitest.CopyShopFlow(t, c)
	clitest.MustRun(t, "flow", "apply")
	t.Chdir(shop)
	return c
}

// lastEvent returns the last event of the journal.
func lastEvent(t *testing.T) contract.Event {
	t.Helper()
	_, stdout, _ := clitest.Run("events")
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var e contract.Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// TestFlowApplyShared checks flow apply of a draft of the shared subagents:
// it is applied from the project with one event of the project, and names
// the other projects whose flows name the subagent changed.
func TestFlowApplyShared(t *testing.T) {
	p := clitest.ShopFlow(t)
	withCRM(t, p)
	text, err := os.ReadFile(filepath.Join(p.Library, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteFiles(t, p.Library, map[string]string{"reviewer.md": string(text) + "Проверить логи.\n"})
	stdout := clitest.WantJSON(t, contract.ExitOK, "schemas/flow-apply.json", "flow", "apply")
	var out contract.FlowApplyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Project != "shop" || !slices.Equal(out.Projects, []string{"crm"}) {
		t.Errorf("apply: %s", stdout)
	}
	e := lastEvent(t)
	data, _ := json.Marshal(e.Data)
	clitest.Validate(t, "schemas/events/flow.applied.json", string(data))
	if e.Type != flow.EventApplied || e.Project == nil || *e.Project != "shop" ||
		string(data) != `{"commit":"`+out.Applied.Commit+`","projects":["crm"]}` {
		t.Errorf("event: %+v, data %s", e, data)
	}

	// The flow alone concerns no other project.
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	_, stdout, _ = clitest.Run("flow", "apply")
	if want := clitest.Lines(msg.Text(msg.FlowApplied), msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, "<время>")); !strings.HasPrefix(clitest.Masked(stdout), want) ||
		strings.Contains(stdout, msg.Text(msg.FlowApplyAffects)) {
		t.Errorf("apply of the flow:\n%s", stdout)
	}
	e = lastEvent(t)
	if data, _ := json.Marshal(e.Data); !strings.HasSuffix(string(data), `"projects":[]}`) {
		t.Errorf("event of the flow: %s", data)
	}
}

// TestFlowApplyBreaksOther checks that a change of the shared subagents that
// breaks the active flow of another project is refused, and nothing is
// applied.
func TestFlowApplyBreaksOther(t *testing.T) {
	p := clitest.ShopFlow(t)
	withCRM(t, p)
	clitest.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания записаны\nexecutor: orchestrator\n"})
	clitest.WriteFiles(t, p.Library, map[string]string{"reviewer.yaml": "", "reviewer.md": ""})
	unknown := msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewer")
	code, _, stderr := clitest.Run("flow", "apply")
	want := clitest.Lines(msg.Text(msg.ErrSharedBreaksFlows), msg.Text(msg.FlowDir, p.Dir), "",
		msg.Text(msg.FlowProjectProblems, "crm"), "  "+unknown)
	if code != contract.ExitError || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ := clitest.Run("flow", "apply", "--json")
	var refused struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &refused); err != nil {
		t.Fatal(err)
	}
	clitest.Validate(t, "schemas/flow-draft-invalid.json", string(refused.Error.Details))
	if refused.Error.Code != contract.CodeFlowDraftInvalid ||
		!strings.Contains(string(refused.Error.Details), `"flows":[{"problems":[{"code":"unknown_executor","file":"stages/review.yaml",`) ||
		!strings.Contains(string(refused.Error.Details), `"problems":[],"project":"shop"}`) {
		t.Errorf("--json: %s", stdout)
	}
	// Nothing is applied: the review stage of the shop is the one of the
	// active flow.
	if _, stdout, _ := clitest.Run("flow", "show", "--stage", "review"); !strings.Contains(stdout, msg.Text(msg.FlowExecutor, "reviewer")+"\n") {
		t.Errorf("show --stage review:\n%s", stdout)
	}
}

// TestFlowDiscardShared checks that flow discard brings back the flow of the
// project and the shared subagents with one event, and leaves the draft of
// another project.
func TestFlowDiscardShared(t *testing.T) {
	p := clitest.ShopFlow(t)
	crm := withCRM(t, p)
	active, err := os.ReadFile(filepath.Join(p.Library, "reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteDraft(t, p, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	clitest.WriteFiles(t, p.Library, map[string]string{"reviewer.md": string(active) + "Проверить логи.\n"})
	clitest.WriteDraft(t, crm, map[string]string{"stages/merge.md": "Влить ветку в main.\n"})
	clitest.MustRun(t, "flow", "discard")
	if b, _ := os.ReadFile(filepath.Join(p.Library, "reviewer.md")); string(b) != string(active) {
		t.Errorf("reviewer.md after discard: %q", b)
	}
	if e := lastEvent(t); e.Type != flow.EventDraftDiscarded || e.Project == nil || *e.Project != "shop" {
		t.Errorf("event: %+v", e)
	}
	if code, _, _ := clitest.Run("flow", "diff", "--project", "crm"); code != contract.ExitOK {
		t.Errorf("the draft of the crm is gone: exit code %d", code)
	}
	if code, _, _ := clitest.Run("flow", "diff"); code != contract.ExitError {
		t.Errorf("the draft of the shop is there: exit code %d", code)
	}
}
