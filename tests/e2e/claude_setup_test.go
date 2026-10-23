package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/t8nax/gentry/internal/adapter/claude"
	"github.com/t8nax/gentry/internal/filelock"
	"github.com/t8nax/gentry/internal/home"
)

// TestSetupClaudeRollbackConcurrent makes the first setup wait in install
// while another process tries to connect the same Claude configuration.
// Without serialization the second succeeds first, then the failed setup
// must not remove its integration. With serialization the failed setup is
// released first, so the waiting setup can finish. Both orders must keep
// the successful connection.
func TestSetupClaudeRollbackConcurrent(t *testing.T) {
	bin := buildGentry(t)
	tools := t.TempDir()
	buildFake := func(name, source string) string {
		t.Helper()
		p := filepath.Join(tools, name)
		if runtime.GOOS == "windows" {
			p += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", p, source)
		cmd.Dir = filepath.Join("..", "..")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		return p
	}
	fake := buildFake("fakeclaude", "./internal/adapter/claude/testdata/fakeclaude")
	gated := buildFake("gatedclaude", "./internal/adapter/claude/testdata/gatedclaude")
	for _, different := range []bool{false, true} {
		name := "same-root"
		if different {
			name = "shared-config-different-roots"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			user := filepath.Join(base, "user")
			t.Setenv("HOME", user)
			t.Setenv("USERPROFILE", user)
			t.Setenv("LOCALAPPDATA", filepath.Join(base, "cache"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
			cache, err := os.UserCacheDir()
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(base, "config")
			gate := filepath.Join(base, "gate")
			for _, p := range []string{cache, config, gate, filepath.Join(base, "tmp1"), filepath.Join(base, "tmp2")} {
				if err := os.MkdirAll(p, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(claude.ProgramEnv, gated)
			t.Setenv(claude.ConfigDirEnv, config)
			t.Setenv("FAKECLAUDE_DELEGATE", fake)
			state := filepath.Join(base, "state.json")
			t.Setenv("FAKECLAUDE_STATE", state)
			firstRoot, secondRoot := filepath.Join(base, "first"), filepath.Join(base, "first")
			if different {
				secondRoot = filepath.Join(base, "second")
			}
			type outcome struct {
				err error
				out string
			}
			start := func(root, tmp, configPath, barrier string) <-chan outcome {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				t.Cleanup(cancel)
				cmd := exec.CommandContext(ctx, bin, "setup", "claude", "--switch", "--json")
				cmd.Env = append(os.Environ(), home.EnvVar+"="+root, "TMP="+tmp, "TEMP="+tmp,
					claude.ConfigDirEnv+"="+configPath, "FAKECLAUDE_INSTALL_GATE="+barrier)
				var out bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &out
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan outcome, 1)
				go func() {
					err := cmd.Wait()
					done <- outcome{err: err, out: out.String()}
				}()
				return done
			}
			release := func() { os.WriteFile(filepath.Join(gate, "release"), nil, 0o644) }
			defer release()
			first := start(firstRoot, filepath.Join(base, "tmp1"), config, gate)
			deadline := time.Now().Add(45 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(gate, "entered")); err == nil {
					break
				}
				select {
				case r := <-first:
					t.Fatalf("first setup did not reach install: %v\n%s", r.err, r.out)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("first setup did not reach install barrier")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// The lock is an observable process resource, not a timing guess.
			// Before the fix there is none: let the other setup finish before
			// failing the first, reproducing the lost successful connection.
			locked := false
			if err := filepath.WalkDir(cache, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(p, ".lock") {
					held, err := filelock.Held(p)
					if err != nil {
						return err
					}
					locked = locked || held
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			alias := config + string(filepath.Separator) + ".." + string(filepath.Separator) + "config"
			if runtime.GOOS == "windows" {
				alias = strings.ToUpper(alias)
			}
			second := start(secondRoot, filepath.Join(base, "tmp2"), alias, "")
			if locked {
				release()
			}
			secondResult := <-second
			if secondResult.err != nil {
				t.Fatalf("second setup: %v\n%s", secondResult.err, secondResult.out)
			}
			if !locked {
				release()
			}
			firstResult := <-first
			if firstResult.err == nil || !strings.Contains(firstResult.out, `"code":"tool_failed"`) ||
				!strings.Contains(firstResult.out, "simulated gated install failure") {
				t.Fatalf("first setup must retain its install error: %v\n%s", firstResult.err, firstResult.out)
			}
			secondDir := filepath.Join(secondRoot, "integrations", "claude")
			if fi, err := os.Stat(secondDir); err != nil || !fi.IsDir() {
				t.Errorf("successful integration was removed: %v", err)
			}
			var s struct {
				Marketplaces []struct{ Name, Path string }
				Plugins      []struct{ ID string }
			}
			b, err := os.ReadFile(state)
			if err != nil || json.Unmarshal(b, &s) != nil {
				t.Fatalf("read fake state: %v\n%s", err, b)
			}
			if len(s.Marketplaces) != 1 || s.Marketplaces[0].Name != "gentry" ||
				!claude.SamePath(s.Marketplaces[0].Path, secondDir) || len(s.Plugins) != 1 || s.Plugins[0].ID != "gentry@gentry" {
				t.Errorf("successful Claude registration was removed: %s", b)
			}
		})
	}
}
