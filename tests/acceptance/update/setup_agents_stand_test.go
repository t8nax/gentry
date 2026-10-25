package update_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/t8nax/gentry/internal/gittest"
	"github.com/t8nax/gentry/tests/testkit"
	"github.com/t8nax/gentry/tests/testkit/toml"
)

// GEN-59 deliberately describes its JSON here, without importing new production
// identifiers: these tests must compile before setup implements the contract.
type setupAgentsProject struct {
	Project   string                `json:"project"`
	Changed   *bool                 `json:"changed"`
	Worktrees []setupAgentsCopy     `json:"worktrees"`
	Conflicts []setupAgentsConflict `json:"conflicts,omitempty"`
}
type setupAgentsCopy struct {
	Path    string   `json:"path"`
	Task    *string  `json:"task,omitempty"`
	Missing bool     `json:"missing,omitempty"`
	Agents  []string `json:"agents"`
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}
type setupAgentsConflict struct {
	Worktree string `json:"worktree"`
	Agent    string `json:"agent"`
	File     string `json:"file"`
	Reason   string `json:"reason"`
}
type setupAgentsResult struct {
	Tool          string               `json:"tool"`
	Dir           string               `json:"dir"`
	PluginVersion string               `json:"plugin_version"`
	Action        string               `json:"action"`
	PreviousDir   *string              `json:"previous_dir"`
	Enabled       *bool                `json:"enabled"`
	Permission    string               `json:"permission"`
	HookTrust     string               `json:"hook_trust"`
	HookError     *string              `json:"hook_error"`
	HooksFeature  string               `json:"hooks_feature"`
	Agents        []setupAgentsProject `json:"agents"`
}
type setupAgentsError struct {
	Error struct {
		Code    string                     `json:"code"`
		Message string                     `json:"message"`
		Hint    string                     `json:"hint"`
		Hints   []string                   `json:"hints"`
		Details map[string]json.RawMessage `json:"details"`
	} `json:"error"`
}
type setupAgentsFailure struct {
	Project string `json:"project"`
	Reason  string `json:"reason"`
	Code    string `json:"code"`
}

type setupAgentsStand struct {
	*stand
	tool  string
	tools []string
}

func setupAgentFile(copy, tool, name string) string {
	ext := ".md"
	if tool == "codex" {
		ext = ".toml"
	}
	return filepath.Join(copy, "."+tool, "agents", name+ext)
}

func setupConflictFile(tool, name string) string {
	return filepath.ToSlash(setupAgentFile("", tool, name))
}

func setupConflictPhysical(c setupAgentsConflict) string {
	return filepath.Join(c.Worktree, filepath.FromSlash(c.File))
}

func newSetupAgentsStand(t *testing.T, tool string, both, connected bool) *setupAgentsStand {
	t.Helper()
	s := &setupAgentsStand{stand: installedStand(t, latest, false), tool: tool, tools: []string{tool}}
	if both {
		s.tools = []string{"claude", "codex"}
	}
	writeFile(t, filepath.Join(s.claudeConfig, "settings.json"), `{"permissions":{"allow":["mcp__plugin_gentry_core"]}}`+"\n")
	s.useCodex()
	if connected {
		for _, tool := range s.tools {
			s.mustIn(s.root, "setup", tool)
		}
	}
	template := setupAgentsTemplates[tool]
	if both {
		template = setupAgentsTemplates["both"]
	}
	template.once.Do(func() {
		// Only preparation is shared. Capture the closed project graph after
		// real registration and flow commands, never a tested setup result.
		prepared := buildSetupAgentsStand(t, tool, both)
		template.root = prepared.root
		template.files = setupAgentsGraph(t, prepared)
		if slices.Contains(prepared.tools, "codex") {
			projects, _ := prepared.codexConfig()["projects"].(map[string]any)
			if len(projects) != 4 {
				t.Fatalf("stand: registration did not trust all four copies: %#v", projects)
			}
			template.trust = map[string]string{}
			for path, raw := range projects {
				entry, _ := raw.(map[string]any)
				level, _ := entry["trust_level"].(string)
				if level != "trusted" {
					t.Fatalf("stand: registration left %s untrusted: %#v", path, raw)
				}
				template.trust[path] = level
			}
		}
		// A surviving absolute link would now fail instead of silently reading
		// or modifying the source graph. Only the immutable bytes remain.
		if err := os.RemoveAll(prepared.root); err != nil {
			t.Fatal(err)
		}
	})
	if template.files == nil {
		t.Fatal("stand: preparation of the project graph did not complete")
	}
	if err := os.CopyFS(s.root, template.files); err != nil {
		t.Fatal(err)
	}
	s.relocateGraph(template.root)
	if connected && slices.Contains(s.tools, "codex") {
		// Registration trusts the copies when Codex is already connected.
		// Carry over just those records, produced by real registration above;
		// every other setting and the plugin fingerprint came from this
		// stand's actual initial setup, before its project pool was present.
		s.editCodexConfig(func(cfg map[string]any) {
			for path, level := range template.trust {
				toml.Set(cfg, level, "projects", strings.ReplaceAll(path, template.root, s.root), "trust_level")
			}
		})
	}
	return s
}

type setupAgentsTemplate struct {
	once  sync.Once
	root  string
	files fstest.MapFS // immutable bytes; CopyFS creates new files, not hard links
	trust map[string]string
}

var setupAgentsTemplates = map[string]*setupAgentsTemplate{
	"claude": {}, "codex": {}, "both": {},
}

// setupAgentsGraph retains only the reusable project graph. Binaries, homes,
// tool settings and integrations belong to each stand and are not retained.
// The snapshot lives in memory until package exit, independently of the first
// test's temporary directory and cleanup.
func setupAgentsGraph(t *testing.T, s *setupAgentsStand) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"admin", "admin-B", "admin-knowledge", "shop", "shop-B", "shop-knowledge", "data/process", "data/state"} {
		err := filepath.WalkDir(filepath.Join(s.root, filepath.FromSlash(name)), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(s.root, p)
			if err != nil {
				return err
			}
			f := &fstest.MapFile{Mode: info.Mode()}
			if !d.IsDir() {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("stand: project graph contains a non-regular file: %s", p)
				}
				if strings.HasSuffix(p, "state.db-wal") || strings.HasSuffix(p, "state.db-shm") {
					return fmt.Errorf("stand: state database is not closed: %s", p)
				}
				f.Data, err = os.ReadFile(p)
				if err != nil {
					return err
				}
			}
			files[filepath.ToSlash(rel)] = f
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// relocateGraph changes only the fresh copy. Git object/index bytes stay
// untouched; their names are relative. Text settings (notably clone origins),
// instructions and event JSON are relocated with their respective escaping.
func (s *setupAgentsStand) relocateGraph(from string) {
	s.t.Helper()
	encoded := func(p string) string { b, _ := json.Marshal(p); return string(b[1 : len(b)-1]) }
	replace := strings.NewReplacer(encoded(from), encoded(s.root), from, s.root, filepath.ToSlash(from), filepath.ToSlash(s.root))
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "objects" && filepath.Base(filepath.Dir(p)) == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if p == s.stateStore() || filepath.Base(filepath.Dir(p)) == ".git" && d.Name() == "index" || strings.HasPrefix(p, s.bin()+string(filepath.Separator)) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Contains(b, []byte(from)) && !bytes.Contains(b, []byte(filepath.ToSlash(from))) && !bytes.Contains(b, []byte(encoded(from))) {
			return nil
		}
		return os.WriteFile(p, []byte(replace.Replace(string(b))), 0o644)
	})
	if err != nil {
		s.t.Fatal(err)
	}
	db, err := sql.Open("sqlite", s.stateStore())
	if err != nil {
		s.t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		s.t.Fatal(err)
	}
	defer tx.Rollback()
	for _, update := range []struct {
		query string
		args  []any
		count int64
	}{
		{`UPDATE projects SET knowledge = replace(knowledge, ?, ?)`, []any{from, s.root}, 2},
		{`UPDATE worktrees SET path = replace(path, ?, ?)`, []any{from, s.root}, 4},
		{`UPDATE events SET data = replace(replace(replace(data, ?, ?), ?, ?), ?, ?)`, []any{encoded(from), encoded(s.root), from, s.root, filepath.ToSlash(from), filepath.ToSlash(s.root)}, -1},
	} {
		res, err := tx.Exec(update.query, update.args...)
		if err != nil {
			s.t.Fatal(err)
		}
		if n, err := res.RowsAffected(); err != nil || update.count >= 0 && n != update.count {
			s.t.Fatalf("stand: relocation affected %d rows, want %d: %v", n, update.count, err)
		}
	}
	if err := tx.Commit(); err != nil {
		s.t.Fatal(err)
	}
	var tasks int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM tasks) + (SELECT count(*) FROM flow_snapshots)`).Scan(&tasks); err != nil || tasks != 0 {
		s.t.Fatalf("stand: project graph contains tasks or task snapshots: %d (%v)", tasks, err)
	}
	var stale int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE instr(data, ?) > 0 OR instr(data, ?) > 0 OR instr(data, ?) > 0`, from, filepath.ToSlash(from), encoded(from)).Scan(&stale); err != nil || stale != 0 {
		s.t.Fatalf("stand: event data retains template paths: %d (%v)", stale, err)
	}
	rows, err := db.Query(`SELECT knowledge FROM projects UNION ALL SELECT path FROM worktrees`)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			s.t.Fatal(err)
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !testkit.FileExists(filepath.Join(p, ".git")) {
			s.t.Fatalf("stand: project path does not point to its own repository: %s (%v)", p, err)
		}
	}
	if err := rows.Err(); err != nil {
		s.t.Fatal(err)
	}
}

func buildSetupAgentsStand(t *testing.T, tool string, both bool) *setupAgentsStand {
	t.Helper()
	s := &setupAgentsStand{stand: installedStand(t, latest, false), tool: tool, tools: []string{tool}}
	if both {
		s.tools = []string{"claude", "codex"}
	}
	writeFile(t, filepath.Join(s.claudeConfig, "settings.json"), `{"permissions":{"allow":["mcp__plugin_gentry_core"]}}`+"\n")
	s.useCodex()
	for _, tool := range s.tools {
		s.mustIn(s.root, "setup", tool)
	}
	// Register both copies before applying the final flow. The general project
	// stand applies a reviewer-only flow first, which this stand would replace
	// immediately and deploy twice more while adding B and the final agents.
	for _, project := range []string{"shop", "admin"} {
		a := gittest.Repo(t, s.copy(project, "A"))
		s.mustIn(a, "project", "add", project, "--knowledge", filepath.Join(s.root, project+"-knowledge"))
		b := s.clone(project, "B") // Independent repositories, not linked git worktrees.
		s.mustIn(a, "worktree", "add", b)
		process := filepath.Join(s.data, "process")
		if !testkit.FileExists(filepath.Join(process, "agents", "reviewer.md")) {
			if err := os.CopyFS(filepath.Join(process, "agents"), os.DirFS(filepath.Join(flowTestdata(), "agents"))); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.CopyFS(filepath.Join(process, project, "flow"), os.DirFS(filepath.Join(flowTestdata(), "shop", "flow"))); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(s.data, "process", "agents", "reviewer.md"), "Действующая инструкция\n")
	writeFile(t, filepath.Join(s.data, "process", "agents", "coder.yaml"), "purpose: реализация\ncapabilities: [read, search, edit, run]\n")
	writeFile(t, filepath.Join(s.data, "process", "agents", "coder.md"), "Написать код задачи.\n")
	for _, project := range []string{"admin", "shop"} {
		writeFile(t, filepath.Join(s.data, "process", project, "flow", "stages", "implementation.yaml"), "title: Реализация\nexit: изменения сделаны, тесты проходят\nexecutor: coder\n")
		s.mustIn(s.copy(project, "A"), "flow", "apply")
		for _, copy := range s.copies(project) {
			for _, tool := range s.tools {
				if !testkit.FileExists(setupAgentFile(copy, tool, "reviewer")) {
					t.Fatalf("stand: no subagent reviewer of %s in %s", tool, copy)
				}
			}
		}
	}
	return s
}

func (s *setupAgentsStand) copy(project, copy string) string {
	if copy == "A" {
		return filepath.Join(s.root, project)
	}
	return s.worktree(project, copy)
}

func (s *setupAgentsStand) copies(project string) []string {
	return []string{s.copy(project, "A"), s.copy(project, "B")}
}

// A pair has exactly the same pre-command files. Its reference already has
// all post-setup integrations; only an explicit agents deploy changes it.
func setupAgentsPair(t *testing.T, tool string, both, connected bool) (*setupAgentsStand, *setupAgentsStand) {
	t.Helper()
	s := newSetupAgentsStand(t, tool, both, connected)
	e := newSetupAgentsStand(t, tool, both, true)
	s.copyAgentState(e)
	return s, e
}

func (s *setupAgentsStand) copyAgentState(from *setupAgentsStand) {
	s.t.Helper()
	for _, project := range []string{"admin", "shop"} {
		for _, c := range []string{"A", "B"} {
			dst, src := s.copy(project, c), from.copy(project, c)
			for _, tool := range []string{"claude", "codex"} {
				dir := filepath.Join(dst, "."+tool, "agents")
				if err := os.RemoveAll(dir); err != nil {
					s.t.Fatal(err)
				}
			}
			for rel, content := range setupAgentFiles(s.t, src) {
				writeFile(s.t, filepath.Join(dst, rel), strings.ReplaceAll(content, from.root, s.root))
			}
			writeFile(s.t, filepath.Join(dst, ".git", "info", "exclude"), readFile(s.t, filepath.Join(src, ".git", "info", "exclude")))
		}
	}
}

func setupAgentFiles(t *testing.T, copy string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tool := range []string{"claude", "codex"} {
		dir := filepath.Join(copy, "."+tool, "agents")
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(copy, p)
				out[rel] = readFile(t, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func (s *setupAgentsStand) stale() {
	s.t.Helper()
	for _, project := range []string{"admin", "shop"} {
		for _, copy := range s.copies(project) {
			for _, tool := range s.tools {
				p := setupAgentFile(copy, tool, "reviewer")
				text := readFile(s.t, p)
				text = strings.ReplaceAll(text, "Действующая инструкция", "Прежняя инструкция")
				text = strings.ReplaceAll(text, "Инструкция задачи", "Прежняя инструкция задачи")
				writeFile(s.t, p, text)
				writeFile(s.t, setupAgentFile(copy, tool, "obsolete"), text)
				if err := os.Remove(setupAgentFile(copy, tool, "coder")); err != nil && !os.IsNotExist(err) {
					s.t.Fatal(err)
				}
				writeFile(s.t, setupAgentFile(copy, tool, "personal"), "Личный файл оператора.\n")
			}
		}
	}
}

func (s *setupAgentsStand) stalePlugin() {
	if s.tool == "codex" {
		s.staleCodexPlugin()
	} else {
		s.editPlugin(func(p *claudePlugin) { p.Version = "0.0.0+00000000" })
	}
}

func (s *setupAgentsStand) disable() {
	if s.tool == "codex" {
		s.disableCodexPlugin()
	} else {
		s.editPlugin(func(p *claudePlugin) { p.Enabled = false })
	}
}

func (s *setupAgentsStand) setup(jsonMode bool, flags ...string) result {
	args := append([]string{"setup", s.tool}, flags...)
	if jsonMode {
		args = append(args, "--json")
	}
	return s.gentry(args...)
}

func (s *setupAgentsStand) parse(r result, action string) setupAgentsResult {
	s.t.Helper()
	wantCode(s.t, r, 0)
	if r.stderr != "" {
		s.t.Errorf("JSON command wrote stderr: %s", r.stderr)
	}
	testkit.Validate(s.t, "schemas/setup.json", r.stdout)
	var out setupAgentsResult
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		s.t.Fatalf("setup JSON: %v\n%s", err, r)
	}
	if out.Action != action || out.Tool != s.tool || out.PluginVersion == "" || !samePath(out.Dir, filepath.Join(s.data, "integrations", s.tool)) {
		s.t.Errorf("setup fields: %+v; want action=%s, tool=%s, dir=current integration, nonempty plugin_version", out, action, s.tool)
	}
	return out
}

func (s *setupAgentsStand) reference(projects ...string) []setupAgentsProject {
	s.t.Helper()
	var out []setupAgentsProject
	for _, project := range projects {
		r := s.gentry("agents", "deploy", "--project", project, "--json")
		if r.code != 0 || r.stderr != "" {
			s.t.Fatalf("reference agents deploy: %s", r)
		}
		var a setupAgentsProject
		if err := json.Unmarshal([]byte(r.stdout), &a); err != nil {
			s.t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func normalizeSetupAgents[T any](t *testing.T, value T, root string) T {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// Work through decoded strings: replacing JSON-escaped Windows paths is
	// different from replacing the same bytes in generated files.
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return strings.ReplaceAll(x, root, "<stand>")
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	b, err = json.Marshal(walk(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A file oracle is captured before any setup control can change the reference.
// Every comparison gets its own map so exclusions and path normalization cannot
// alter the original bytes seen by later assertions.
type setupAgentsFileSnapshot map[string]map[string]string

func (s *setupAgentsStand) snapshotReference(projects []setupAgentsProject) setupAgentsFileSnapshot {
	s.t.Helper()
	files := setupAgentsFileSnapshot{}
	for _, project := range projects {
		for _, copy := range s.copies(project.Project) {
			files[copy] = setupAgentFiles(s.t, copy)
		}
	}
	return files
}

func (snapshot setupAgentsFileSnapshot) files(t *testing.T, copy string) map[string]string {
	t.Helper()
	files, ok := snapshot[copy]
	if !ok {
		t.Fatalf("reference snapshot has no copy %s", copy)
	}
	return maps.Clone(files)
}

func (s *setupAgentsStand) wantReference(e *setupAgentsStand, got, want []setupAgentsProject, excluded ...string) {
	s.t.Helper()
	s.wantReferenceFiles(e, got, want, nil, excluded...)
}

func (s *setupAgentsStand) wantReferenceFiles(e *setupAgentsStand, got, want []setupAgentsProject, snapshot setupAgentsFileSnapshot, excluded ...string) {
	s.t.Helper()
	if got != nil && !reflect.DeepEqual(normalizeSetupAgents(s.t, got, s.root), normalizeSetupAgents(s.t, want, e.root)) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		s.t.Errorf("agents differs from independent agents deploy:\ngot %s\nwant %s", g, w)
	}
	for _, project := range want {
		for _, c := range []string{"A", "B"} {
			a, b := s.copy(project.Project, c), e.copy(project.Project, c)
			gotFiles := setupAgentFiles(s.t, a)
			var wantFiles map[string]string
			if snapshot != nil {
				wantFiles = snapshot.files(s.t, b)
			} else {
				wantFiles = setupAgentFiles(s.t, b)
			}
			for _, p := range excluded {
				rel, err := filepath.Rel(a, p)
				if err == nil {
					delete(gotFiles, rel)
					delete(wantFiles, rel)
				}
			}
			for rel, text := range gotFiles {
				gotFiles[rel] = strings.ReplaceAll(text, s.root, "<stand>")
			}
			for rel, text := range wantFiles {
				wantFiles[rel] = strings.ReplaceAll(text, e.root, "<stand>")
			}
			if !maps.Equal(gotFiles, wantFiles) {
				s.t.Errorf("agent files of %s differ:\ngot %#v\nwant %#v", a, gotFiles, wantFiles)
			}
		}
	}
}

func (s *setupAgentsStand) wantContract(r result, out setupAgentsResult, projects ...string) {
	s.t.Helper()
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(r.stdout), &fields)
	if len(projects) == 0 {
		if _, ok := fields["agents"]; ok {
			s.t.Errorf("agents must be omitted: %s", r.stdout)
		}
		return
	}
	var raw []map[string]json.RawMessage
	json.Unmarshal(fields["agents"], &raw)
	var names []string
	for i, a := range out.Agents {
		names = append(names, a.Project)
		if a.Changed == nil || len(a.Worktrees) != 2 {
			s.t.Errorf("incomplete project: %+v", a)
		}
		if _, ok := raw[i]["tools"]; ok {
			s.t.Error("update-style agents must omit tools")
		}
		if _, ok := raw[i]["conflicts"]; ok && len(a.Conflicts) == 0 {
			s.t.Error("empty conflicts must be omitted")
		}
		for j, w := range a.Worktrees {
			if j >= 2 {
				s.t.Errorf("unexpected extra worktree: %+v", w)
				continue
			}
			c := []string{"A", "B"}[j]
			if !samePath(w.Path, s.copy(a.Project, c)) || w.Agents == nil || w.Added == nil || w.Updated == nil || w.Removed == nil {
				s.t.Errorf("incomplete or unordered worktree: %+v", w)
			}
			wantNames := []string{"coder", "reviewer"}
			// A name is laid out if at least one connected format succeeds;
			// conflicts in all formats leave it out, as agents deploy does.
			for _, name := range []string{"coder", "reviewer"} {
				n := 0
				for _, c := range a.Conflicts {
					if samePath(c.Worktree, w.Path) && c.Agent == name {
						n++
					}
				}
				if n == len(s.tools) && n > 0 {
					wantNames = slices.DeleteFunc(wantNames, func(x string) bool { return x == name })
				}
			}
			if !w.Missing && !slices.Equal(w.Agents, wantNames) {
				s.t.Errorf("agents in %s: %q", w.Path, w.Agents)
			}
		}
	}
	if !slices.Equal(names, projects) {
		s.t.Errorf("project order: %q, want %q", names, projects)
	}
}

func (s *setupAgentsStand) snapshot() map[string]string {
	out := map[string]string{}
	for _, project := range []string{"admin", "shop"} {
		for _, c := range s.copies(project) {
			for p, text := range setupAgentFiles(s.t, c) {
				out[filepath.Join(c, p)] = text
			}
		}
	}
	return out
}

func (s *setupAgentsStand) wantUnchanged(before map[string]string) {
	s.t.Helper()
	if after := s.snapshot(); !maps.Equal(after, before) {
		s.t.Errorf("agent files changed:\nbefore %#v\nafter %#v", before, after)
	}
}

func (s *setupAgentsStand) wantHidden() {
	s.t.Helper()
	for _, project := range []string{"admin", "shop"} {
		for _, copy := range s.copies(project) {
			if !testkit.FileExists(filepath.Join(copy, ".git")) {
				continue
			}
			status := gittest.Run(s.t, copy, "status", "--porcelain", "--untracked-files=all")
			for _, line := range strings.Split(status, "\n") {
				if strings.Contains(line, "/agents/") && !strings.HasSuffix(line, "personal.md") && !strings.HasSuffix(line, "personal.toml") {
					s.t.Errorf("managed file visible in git status of %s: %s", copy, line)
				}
			}
		}
	}
}

func (s *setupAgentsStand) wantPluginCurrent() {
	s.t.Helper()
	if s.tool == "codex" {
		p, ok := s.codexPlugin()
		if !ok || p.Version != s.codexManifestVersion() {
			s.t.Errorf("plugin not updated: %+v", p)
		}
	} else {
		p := s.plugin()
		version := manifestVersion(s.t, filepath.Join(s.integration(), "gentry", ".claude-plugin", "plugin.json"))
		if p.Version != version {
			s.t.Errorf("plugin version %s, want %s", p.Version, version)
		}
	}
}

// updatedSetupControl captures every existing setup field independently of the
// failing command. Call it before introducing faults into the reference stand.
// Project results have a separate oracle and are deliberately excluded here.
func (s *setupAgentsStand) updatedSetupControl(pluginVersion string) map[string]any {
	s.t.Helper()
	s.stalePlugin()
	r := s.setup(true)
	s.parse(r, "updated")
	var fields map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &fields); err != nil {
		s.t.Fatal(err)
	}
	delete(fields, "agents")
	// The fingerprint includes absolute plugin/program paths, so distinct
	// stands have distinct versions. Use the subject's pre-command manifest
	// from the installed task build for this one path-dependent field.
	fields["plugin_version"] = pluginVersion
	return normalizeSetupAgents(s.t, fields, s.root)
}

func (s *setupAgentsStand) setupManifestVersion() string {
	if s.tool == "codex" {
		return s.codexManifestVersion()
	}
	return manifestVersion(s.t, filepath.Join(s.integration(), "gentry", ".claude-plugin", "plugin.json"))
}

func (s *setupAgentsStand) failure(r result, code string, wantSetup map[string]any) (setupAgentsError, setupAgentsResult, []setupAgentsFailure) {
	s.t.Helper()
	wantCode(s.t, r, 1)
	if r.stderr != "" {
		s.t.Errorf("JSON error wrote stderr: %s", r.stderr)
	}
	var out setupAgentsError
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		s.t.Fatalf("error JSON: %v\n%s", err, r)
	}
	if out.Error.Code != "" {
		testkit.Validate(s.t, "schemas/error.json", r.stdout)
	}
	if out.Error.Code != code {
		s.t.Errorf("error.code=%s, want %s", out.Error.Code, code)
	}
	var setup setupAgentsResult
	if b, ok := out.Error.Details["setup"]; !ok {
		s.t.Errorf("partial details.setup absent: %s", r.stdout)
	} else {
		if err := json.Unmarshal(b, &setup); err != nil {
			s.t.Fatal(err)
		}
		testkit.Validate(s.t, "schemas/setup.json", string(b))
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			s.t.Fatal(err)
		}
		if len(setup.Agents) == 0 {
			if _, ok := fields["agents"]; ok {
				s.t.Errorf("empty partial agents must be omitted: %s", b)
			}
		}
		var complete map[string]any
		if err := json.Unmarshal(b, &complete); err != nil {
			s.t.Fatal(err)
		}
		delete(complete, "agents")
		if got := normalizeSetupAgents(s.t, complete, s.root); !reflect.DeepEqual(got, wantSetup) {
			s.t.Errorf("partial setup differs from independent successful setup:\ngot %#v\nwant %#v", got, wantSetup)
		}
	}
	var failures []setupAgentsFailure
	if b, ok := out.Error.Details["agents"]; ok {
		if code != "agents_failed" {
			s.t.Error("state failure must omit details.agents")
		}
		if err := json.Unmarshal(b, &failures); err != nil {
			s.t.Fatal(err)
		}
	}
	return out, setup, failures
}

func (s *setupAgentsStand) wantFailureProjects(failures []setupAgentsFailure, names ...string) {
	s.t.Helper()
	var got []string
	for _, f := range failures {
		got = append(got, f.Project)
		if f.Reason == "" || f.Code == "" {
			s.t.Errorf("incomplete failure: %+v", f)
		}
	}
	if !slices.Equal(got, names) {
		s.t.Errorf("failed projects: %q, want %q", got, names)
	}
}

func setupAgentsTitle(tool string) string {
	if tool == "codex" {
		return "Codex"
	}
	return "Claude Code"
}
func setupAgentsUpdated(tool string) string {
	return fmt.Sprintf("Плагин Gentry в %s обновлён.\nИзменения вступят в силу со следующей сессии.\n", setupAgentsTitle(tool))
}
