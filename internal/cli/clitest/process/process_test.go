package process_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/cli"
	"github.com/t8nax/gentry/internal/cli/clitest"
	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/internal/home"
	"github.com/t8nax/gentry/internal/msg"
)

// machines are two machines of the operator in a test: their data roots and
// the remote repository of the process. The shop and its knowledge are
// shared, as if cloned on both.
type machines struct {
	t      *testing.T
	a, b   string
	remote string
}

// twoMachines applies the flow of the shop on machine A and sends its
// process to a remote repository, then connects the shop on machine B and
// takes the process there. The test is on B.
func twoMachines(t *testing.T) machines {
	t.Helper()
	clitest.Layer(t, "machines", func() {
		clitest.ShopFlow(t)
		root := filepath.Dir(os.Getenv(home.EnvVar))
		remote := filepath.Join(root, "remote.git")
		gittest.Run(t, root, "init", "--quiet", "--bare", remote)
		clitest.MustRun(t, "process", "remote", "set", remote)
		t.Setenv(home.EnvVar, filepath.Join(root, "home-b"))
		clitest.MustRun(t, "project", "add", "--knowledge", "../shop-knowledge")
		clitest.MustRun(t, "process", "remote", "set", remote)
	})
	root := filepath.Dir(os.Getenv(home.EnvVar))
	return machines{t: t, a: filepath.Join(root, "home"), b: filepath.Join(root, "home-b"), remote: filepath.Join(root, "remote.git")}
}

func (m machines) onA() { m.t.Setenv(home.EnvVar, m.a) }
func (m machines) onB() { m.t.Setenv(home.EnvVar, m.b) }

// The words of the process are those of the catalog: the tests of the output
// in package cli state them.
var (
	synced   = msg.Text(msg.ProcessSynced) + "\n"
	flowShop = msg.Text(msg.KindFlow, "shop")
	// laidOut is the block of the free worktrees of the shop laid out.
	laidOut = clitest.Lines(msg.Text(msg.AgentsFreeSynced, "shop"), msg.Text(msg.AgentsNextSession))
)

func TestProcessRemoteSet(t *testing.T) {
	p := clitest.ShopFlow(t)
	root := filepath.Dir(os.Getenv(home.EnvVar))
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)

	_, stdout, _ := clitest.Run("process", "show")
	if want := clitest.Lines(msg.Text(msg.ProcessNoRemote), "", cli.HintText(msg.HintProcessRemote)); stdout != want {
		t.Errorf("status without a remote:\n%s\nwant:\n%s", stdout, want)
	}
	code, stdout, stderr := clitest.Run("process", "remote", "set", remote)
	if want := clitest.Lines(msg.Text(msg.ProcessRemoteSet, remote), msg.Text(msg.ProcessResultSent)); code != contract.ExitOK || stderr != "" || stdout != want {
		t.Errorf("remote: exit code %d, stderr %q, output:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	_, stdout, _ = clitest.Run("process", "show")
	if want := clitest.Lines(msg.Text(msg.ProcessRemote, remote), msg.Text(msg.ProcessSyncedAt, "<время>")); clitest.Masked(stdout) != want {
		t.Errorf("status:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := clitest.Run("process", "remote", "set", remote); !strings.HasSuffix(stdout, "\n"+msg.Text(msg.ProcessResultUnchanged)+"\n") {
		t.Errorf("remote again:\n%s", stdout)
	}

	// Another machine gets the process and shows the same flow.
	t.Setenv(home.EnvVar, filepath.Join(root, "home-b"))
	clitest.MustRun(t, "project", "add", "--knowledge", "../shop-knowledge")
	if _, stdout, _ := clitest.Run("process", "remote", "set", remote); !strings.HasSuffix(stdout, "\n"+msg.Text(msg.ProcessResultReceived)+"\n") {
		t.Errorf("remote on b:\n%s", stdout)
	}
	b := clitest.ShopPlaces()
	_, stdout, _ = clitest.Run("flow", "show")
	if want := clitest.Lines(msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, "<время>"), msg.Text(msg.FlowDir, b.Dir)) + "\n" +
		clitest.ShopTables(); !strings.HasPrefix(clitest.Masked(stdout), want) || b.Dir == p.Dir {
		t.Errorf("show on b:\n%s", stdout)
	}

	tests := []struct {
		name   string
		args   []string
		exit   int
		stderr string
	}{
		{"no address", []string{"process", "remote", "set"}, contract.ExitUsage,
			msg.Text(msg.ErrRemoteMissing) + "\n\n" + cli.HintText(msg.HintCommandHelp, "process remote set")},
		{"unavailable", []string{"process", "remote", "set", filepath.Join(root, "none.git")}, contract.ExitError,
			msg.Text(msg.ErrRemoteUnavailable, filepath.Join(root, "none.git")) + "\n\n" + msg.Text(msg.HintRemoteAccess, filepath.Join(root, "none.git"))},
		{"code", []string{"process", "remote", "set", filepath.Join(root, "shop")}, contract.ExitError,
			msg.Text(msg.ErrRemoteNotProcess, filepath.Join(root, "shop"))},
	}
	for _, tt := range tests {
		code, stdout, stderr := clitest.Run(tt.args...)
		if code != tt.exit || stdout != "" || stderr != tt.stderr+"\n" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q, want %q", tt.name, code, stdout, stderr, tt.stderr)
		}
	}
	if _, stdout, _ := clitest.Run("process", "show"); !strings.HasPrefix(stdout, msg.Text(msg.ProcessRemote, remote)+"\n") {
		t.Errorf("a refused address is kept:\n%s", stdout)
	}
}

func TestProcessSync(t *testing.T) {
	m := twoMachines(t)
	b := clitest.ShopPlaces()
	clitest.WriteDraft(t, b, map[string]string{"stages/merge.md": "Влить ветку задачи в main после ревью.\n"})
	if code, _, stderr := clitest.Run("flow", "apply"); code != contract.ExitOK || stderr != "" {
		t.Fatalf("apply on b: exit code %d, stderr:\n%s", code, stderr)
	}
	m.onA()
	code, stdout, stderr := clitest.Run("flow", "show", "--stage", "merge")
	if want := clitest.Lines(msg.Text(msg.SyncReceived), "  "+flowShop, ""); code != contract.ExitOK || stderr != want ||
		!strings.HasSuffix(stdout, "  Влить ветку задачи в main после ревью.\n") {
		t.Errorf("show on a: exit code %d, stderr:\n%s\noutput:\n%s", code, stderr, stdout)
	}
	if _, stdout, stderr := clitest.Run("process", "sync"); stdout != synced || stderr != "" {
		t.Errorf("sync with nothing new: %q, %q", stdout, stderr)
	}

	// The file of changes of b stays until a synchronization brings a change.
	m.onB()
	clitest.WriteDraft(t, b, map[string]string{"stages/merge.md": "Влить ветку задачи.\n"})
	clitest.MustRun(t, "flow", "diff")
	report := filepath.Join(filepath.Dir(b.Dir), "changes.md")
	if _, err := os.Stat(report); err != nil {
		t.Errorf("the file of changes on b: %v", err)
	}
	m.onA()

	// The shared subagents go the same way, applied by flow apply.
	a := clitest.ShopPlaces()
	clitest.WriteFiles(t, a.Shared, map[string]string{"reviewer.md": "Проверить изменения задачи и тексты.\n"})
	if _, stdout, _ := clitest.Run("flow", "apply"); clitest.Masked(stdout) != clitest.Lines(msg.Text(msg.FlowApplied),
		msg.Text(msg.FlowProject, "shop"), msg.Text(msg.FlowAppliedAt, "<время>"), "", msg.Text(msg.AgentsFreeSynced, "shop"), msg.Text(msg.AgentsNextSession)) {
		t.Errorf("flow apply of the shared subagents:\n%s", stdout)
	}
	m.onB()
	if _, stdout, _ := clitest.Run("process", "sync"); stdout != synced+clitest.Lines("", msg.Text(msg.ProcessReceived), "  "+msg.Text(msg.KindShared)) {
		t.Errorf("sync on b:\n%s", stdout)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Errorf("the file of changes on b after a change of the shared subagents: %v", err)
	}
	clitest.MustRun(t, "flow", "discard")
	clitest.WriteFiles(t, b.Shared, map[string]string{"reviewer.md": "Черновик.\n"})
	if _, stdout, _ := clitest.Run("flow", "discard"); stdout != clitest.Lines(msg.Text(msg.FlowDiscarded), msg.Text(msg.FlowProject, "shop")) {
		t.Errorf("flow discard of the shared subagents:\n%s", stdout)
	}
	if b, _ := os.ReadFile(filepath.Join(b.Shared, "reviewer.md")); string(b) != "Проверить изменения задачи и тексты.\n" {
		t.Errorf("reviewer after discard: %q", b)
	}
}

func TestProcessConflict(t *testing.T) {
	m := twoMachines(t)
	b := clitest.ShopPlaces()
	m.onA()
	a := clitest.ShopPlaces()
	clitest.WriteDraft(t, a, map[string]string{"stages/review.md": "Проверить изменения по списку машины А.\n"})
	clitest.MustRun(t, "flow", "apply")
	m.onB()
	clitest.WriteDraft(t, b, map[string]string{"stages/review.md": "Проверить изменения по списку машины Б.\n"})
	code, stdout, stderr := clitest.Run("flow", "apply")
	want := clitest.Lines(msg.Text(msg.SyncFlowConflict, "shop"), msg.Text(msg.SyncConflictDir, b.Conflict), "",
		msg.Text(msg.SyncBothChanged), "  "+msg.Text(msg.FlowObjStage, "review"), "", cli.HintFor(msg.HintFlowDiffProject, "project", "shop"))
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("apply on b: exit code %d, stdout %q, stderr:\n%s\nwant:\n%s", code, stdout, stderr, want)
	}
	if text, err := os.ReadFile(filepath.Join(b.Conflict, "stages", "review.md")); err != nil || string(text) != "Проверить изменения по списку машины А.\n" {
		t.Errorf("variant of a: %q, %v", text, err)
	}
	_, stdout, _ = clitest.Run("process", "show")
	if want := clitest.Lines(msg.Text(msg.ProcessRemote, m.remote), msg.Text(msg.ProcessSyncedAt, "<время>"), "", msg.Text(msg.ProcessDrafts),
		"  "+flowShop, "", msg.Text(msg.ProcessConflicts), "  "+flowShop); clitest.Masked(stdout) != want {
		t.Errorf("status:\n%s\nwant:\n%s", stdout, want)
	}
	if _, stdout, _ := clitest.Run("flow", "diff"); !strings.Contains(stdout, "\n"+clitest.Lines(msg.Text(msg.DiffStages), "  ~ "+msg.Text(msg.DiffStage, "Ревью", "review"))) {
		t.Errorf("diff:\n%s", stdout)
	}
	_, stdout, _ = clitest.Run("flow", "show", "--json")
	var out contract.FlowShowOutput
	json.Unmarshal([]byte(stdout), &out)
	if out.Draft == nil || out.Draft.ConflictDir == nil || *out.Draft.ConflictDir != b.Conflict || len(out.Draft.Conflicts) != 1 ||
		out.Draft.Conflicts[0].Object != "stage" || *out.Draft.Conflicts[0].Id != "review" {
		t.Errorf("show --json: %s", stdout)
	}
	// Applied again, the variant of this machine wins, and the conflict ends.
	if code, _, stderr := clitest.Run("flow", "apply"); code != contract.ExitOK || stderr != "" {
		t.Errorf("apply again: exit code %d, stderr:\n%s", code, stderr)
	}
	if _, err := os.Stat(b.Conflict); !os.IsNotExist(err) {
		t.Errorf("conflict/ after apply: %v", err)
	}
	m.onA()
	if _, stdout, _ := clitest.Run("flow", "show", "--stage", "review"); !strings.Contains(stdout, "машины Б.") {
		t.Errorf("a after the conflict:\n%s", stdout)
	}

	// The --json refusal has its code.
	m.onB()
	clitest.WriteDraft(t, b, map[string]string{"stages/review.md": "Б снова.\n"})
	m.onA()
	clitest.WriteDraft(t, a, map[string]string{"stages/review.md": "А снова.\n"})
	clitest.MustRun(t, "flow", "apply")
	m.onB()
	_, stdout, _ = clitest.Run("flow", "apply", "--json")
	clitest.Validate(t, "schemas/error.json", stdout)
	if !strings.HasPrefix(stdout, `{"error":{"code":"flow_conflict","details":{"conflict_dir":`+clitest.JSONString(b.Conflict)+`,"project":"shop"},`) {
		t.Errorf("--json: %s", stdout)
	}
}

func TestProcessOffline(t *testing.T) {
	m := twoMachines(t)
	if err := os.Rename(m.remote, m.remote+"-away"); err != nil {
		t.Fatal(err)
	}
	b := clitest.ShopPlaces()
	clitest.WriteDraft(t, b, map[string]string{"stages/merge.md": "Влить ветку задачи в main без сети.\n"})
	code, stdout, stderr := clitest.Run("flow", "apply")
	if code != contract.ExitOK || stderr != msg.Text(msg.SyncDeferred)+"\n\n" ||
		!strings.HasPrefix(stdout, msg.Text(msg.FlowApplied)+"\n") {
		t.Errorf("apply offline: exit code %d, stderr %q, output:\n%s", code, stderr, stdout)
	}
	if _, _, stderr := clitest.Run("flow", "show"); stderr != msg.Text(msg.SyncSkipped)+"\n\n" {
		t.Errorf("show offline: stderr %q", stderr)
	}
	_, stdout, _ = clitest.Run("process", "show")
	if want := "\n" + clitest.Lines(msg.Text(msg.ProcessUnsent), "  "+flowShop, "", cli.HintText(msg.HintProcessSync)); !strings.HasSuffix(stdout, want) {
		t.Errorf("status:\n%s", stdout)
	}
	code, stdout, stderr = clitest.Run("process", "sync")
	if want := clitest.Lines(msg.Text(msg.ErrRemoteUnavailable, m.remote), "", msg.Text(msg.HintRemoteAccess, m.remote)); code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("sync offline: exit code %d, stderr:\n%s", code, stderr)
	}
	if err := os.Rename(m.remote+"-away", m.remote); err != nil {
		t.Fatal(err)
	}
	if _, stdout, _ := clitest.Run("process", "sync"); stdout != synced+clitest.Lines("", msg.Text(msg.ProcessSent), "  "+flowShop) {
		t.Errorf("sync online:\n%s", stdout)
	}
}

// TestSharedRefusals checks a draft of the shared subagents with problems of
// a project without a flow: the problems of the shared subagents are those of
// the draft, without a file, and nothing is applied.
func TestSharedRefusals(t *testing.T) {
	clitest.ConnectedShop(t)
	p := clitest.ShopPlaces()
	clitest.WriteFiles(t, p.Shared, map[string]string{"tester.yaml": "capabilities: [read]\n", "notes.txt": "x\n"})
	code, _, stderr := clitest.Run("flow", "apply")
	tester := msg.Text(msg.FlowObjSharedAgent, "tester")
	want := clitest.Lines(msg.Text(msg.ErrDraftInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems),
		"  "+msg.Text(msg.ProblemMissingField, tester, "purpose"), "  "+msg.Text(msg.ProblemMissingInstruction, tester),
		"  "+msg.Text(msg.ProblemSharedExtraFile, "notes.txt"))
	if code != contract.ExitError || stderr != want {
		t.Errorf("invalid: exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ := clitest.Run("flow", "apply", "--json")
	var out struct {
		Error struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	clitest.Validate(t, "schemas/flow-draft-invalid.json", string(out.Error.Details))
	if out.Error.Code != contract.CodeFlowDraftInvalid || strings.Contains(string(out.Error.Details), `"file"`) {
		t.Errorf("--json: %s", stdout)
	}
	if code, c := clitest.ErrorCode(t, "library", "apply"); code != contract.ExitUsage || c != contract.CodeUnknownCommand {
		t.Errorf("library apply: exit code %d, code %s", code, c)
	}
}

func TestProcessJSON(t *testing.T) {
	clitest.ShopFlow(t)
	root := filepath.Dir(os.Getenv(home.EnvVar))
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, root, "init", "--quiet", "--bare", remote)
	_, stdout, _ := clitest.Run("process", "show", "--json")
	clitest.Validate(t, "schemas/process-show.json", stdout)
	if stdout != `{"conflicts":[],"drafts":[],"unsent":[]}`+"\n" {
		t.Errorf("status: %s", stdout)
	}
	_, stdout, _ = clitest.Run("process", "remote", "set", remote, "--json")
	clitest.Validate(t, "schemas/process-remote-set.json", stdout)
	if want := `{"drafts":[],"remote":` + clitest.JSONString(remote) + `,"result":"sent"}` + "\n"; stdout != want {
		t.Errorf("remote: %s, want %s", stdout, want)
	}
	p := clitest.ShopPlaces()
	clitest.WriteFiles(t, p.Shared, map[string]string{"reviewer.md": "Новая инструкция.\n"})
	_, stdout, _ = clitest.Run("flow", "diff", "--json")
	clitest.Validate(t, "schemas/flow-diff.json", stdout)
	if !strings.Contains(stdout, `"changes":[],"project":"shop","report":`) ||
		!strings.Contains(stdout, `"shared":{"applied":{"commit":"`) || !strings.Contains(stdout, `"changes":[{"change":"modified","id":"reviewer","projects":["shop"]}]}`) {
		t.Errorf("flow diff: %s", stdout)
	}
	_, stdout, _ = clitest.Run("flow", "apply", "--json")
	clitest.Validate(t, "schemas/flow-apply.json", stdout)
	if !strings.Contains(stdout, `"projects":[],"sent":true,"sync":{"sent":[{"kind":"shared"}]}`) {
		t.Errorf("flow apply: %s", stdout)
	}
	clitest.WriteFiles(t, p.Shared, map[string]string{"reviewer.md": "Ещё.\n"})
	_, stdout, _ = clitest.Run("process", "show", "--json")
	clitest.Validate(t, "schemas/process-show.json", stdout)
	if !strings.Contains(stdout, `"drafts":[{"kind":"shared"}]`) {
		t.Errorf("process show with a draft of the shared subagents: %s", stdout)
	}
	_, stdout, _ = clitest.Run("flow", "discard", "--json")
	clitest.Validate(t, "schemas/flow-discard.json", stdout)
	_, stdout, _ = clitest.Run("process", "sync", "--json")
	clitest.Validate(t, "schemas/process-sync.json", stdout)
	if stdout != `{"conflicts":[],"received":[],"restored":[],"sent":[]}`+"\n" {
		t.Errorf("sync: %s", stdout)
	}
	_, stdout, _ = clitest.Run("process", "show", "--json")
	clitest.Validate(t, "schemas/process-show.json", stdout)
	if !strings.Contains(stdout, `"remote":`+clitest.JSONString(remote)+`,"synced":"`) {
		t.Errorf("status with a remote: %s", stdout)
	}

	// Events of the process have no project and match their schemas; the
	// shared subagents are applied and discarded by the events of the flow.
	_, stdout, _ = clitest.Run("events")
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var e contract.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(e.Type, "process.") {
			continue
		}
		if e.Project != nil {
			t.Errorf("%s has a project", e.Type)
		}
		data, _ := json.Marshal(e.Data)
		clitest.Validate(t, "schemas/events/"+e.Type+".json", string(data))
		types = append(types, e.Type)
	}
	if got := strings.Join(types, " "); got != "process.remote_set process.synced process.synced" {
		t.Errorf("events: %s", got)
	}
}

// TestProjectAddMakesFlowDir checks that connecting a project prepares the
// place of its flow in the process repository.
func TestProjectAddMakesFlowDir(t *testing.T) {
	clitest.ConnectedShop(t)
	p := clitest.ShopPlaces()
	process := filepath.Dir(p.Shared)
	if log := gittest.Run(t, process, "log", "--format=%s"); log != "Create the process repository\n" {
		t.Errorf("log: %q", log)
	}
	if fi, err := os.Stat(filepath.Join(p.Dir, "stages")); err != nil || !fi.IsDir() {
		t.Errorf("flow directory: %v", err)
	}
}

// TestSharedBreaksFlow checks that a change of the shared subagents that
// breaks the flow of the project is a problem of its draft, and is applied
// with a change of the flow that does without the subagent.
func TestSharedBreaksFlow(t *testing.T) {
	p := clitest.ShopFlow(t)
	clitest.WriteFiles(t, p.Shared, map[string]string{"reviewer.yaml": "", "reviewer.md": ""})
	code, stdout, stderr := clitest.Run("flow", "apply")
	noReviewer := msg.Text(msg.ProblemUnknownExecutor, msg.Text(msg.FlowObjStage, "review"), "reviewer")
	want := clitest.Lines(msg.Text(msg.ErrDraftInvalid, "shop"), msg.Text(msg.FlowDir, p.Dir), "", msg.Text(msg.FlowProblems), "  "+noReviewer)
	if code != contract.ExitError || stdout != "" || stderr != want {
		t.Errorf("exit code %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	_, stdout, _ = clitest.Run("flow", "apply", "--json")
	var out struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	clitest.Validate(t, "schemas/flow-draft-invalid.json", string(out.Error.Details))
	if want := `{"dir":` + clitest.JSONString(p.Dir) + `,"problems":[{"code":"unknown_executor","file":"stages/review.yaml","line":3,` +
		`"message":` + clitest.JSONString(noReviewer) + `}],"project":"shop"}`; string(out.Error.Details) != want {
		t.Errorf("details:\n%s\nwant:\n%s", out.Error.Details, want)
	}

	// Once the flow does without the subagent, both are applied.
	clitest.WriteDraft(t, p, map[string]string{"stages/review.yaml": "title: Ревью\nexit: замечания ревью записаны и разобраны\nexecutor: orchestrator\ninclude: [review-checklist]\n"})
	clitest.MustRun(t, "flow", "apply")
}

func TestSyncLaysOutFreeWorktrees(t *testing.T) {
	m := twoMachines(t)
	shop := clitest.MustWd(t)
	file := filepath.Join(shop, ".claude", "agents", "reviewer.md")

	m.onA()
	clitest.WriteFiles(t, clitest.ShopPlaces().Shared, map[string]string{"reviewer.md": "Проверить изменения и тексты.\n"})
	clitest.MustRun(t, "flow", "apply")
	os.Remove(file)

	m.onB()
	_, stdout, _ := clitest.Run("process", "sync")
	want := synced + clitest.Lines("", msg.Text(msg.ProcessReceived), "  "+msg.Text(msg.KindShared), "") + laidOut
	if stdout != want {
		t.Errorf("process sync:\n%s\nwant:\n%s", stdout, want)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), "Проверить изменения и тексты.") {
		t.Errorf("reviewer after the sync: %q, %v", b, err)
	}
}

func TestImplicitSyncLaysOut(t *testing.T) {
	m := twoMachines(t)
	shop := clitest.MustWd(t)
	file := filepath.Join(shop, ".claude", "agents", "reviewer.md")

	m.onA()
	clitest.WriteFiles(t, clitest.ShopPlaces().Shared, map[string]string{"reviewer.md": "Проверить изменения и тексты.\n"})
	clitest.MustRun(t, "flow", "apply")
	os.Remove(file)

	// flow show takes the shared subagents on its way: the block goes with the
	// messages of the synchronization, before the output.
	m.onB()
	_, _, stderr := clitest.Run("flow", "show")
	if !strings.Contains(stderr, "\n"+laidOut) {
		t.Errorf("flow show, stderr:\n%s", stderr)
	}
	if !clitest.FileExists(file) {
		t.Error("reviewer is not laid out")
	}
}

func TestProjectAddWithFlow(t *testing.T) {
	m := twoMachines(t)
	shop := clitest.MustWd(t)
	file := filepath.Join(shop, ".claude", "agents", "reviewer.md")
	os.Remove(file)

	// A third machine takes the process first, then connects the shop.
	t.Setenv(home.EnvVar, filepath.Join(filepath.Dir(m.a), "home-c"))
	clitest.MustRun(t, "process", "remote", "set", m.remote)
	_, stdout, _ := clitest.Run("project", "add", "--knowledge", "../shop-knowledge")
	if !strings.Contains(stdout, "\n\n"+laidOut) {
		t.Errorf("project add:\n%s", stdout)
	}
	if !clitest.FileExists(file) {
		t.Error("reviewer is not laid out")
	}
}
