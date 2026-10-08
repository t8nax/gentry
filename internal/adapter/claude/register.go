package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/integration"
)

// ProgramEnv names the claude program to run; without it claude is looked up
// in PATH. Tests and checks point it at a fake claude.
const ProgramEnv = "GENTRY_CLAUDE"

// Actions of Register.
const (
	Installed = "installed"
	Updated   = "updated"
	Unchanged = "unchanged"
)

// commandTimeout limits one claude command.
const commandTimeout = 2 * time.Minute

// Result tells what Register did.
type Result struct {
	Action  string // Installed, Updated or Unchanged
	Enabled bool   // whether the plugin is enabled in Claude Code
}

// ErrNotFound means the claude program cannot be found.
var ErrNotFound = errors.New("claude program not found")

// CommandError is a claude command that failed.
type CommandError struct {
	Command string // the command as typed, such as "claude plugin install gentry@gentry"
	Output  string // what claude said about the failure
}

func (e *CommandError) Error() string { return e.Command + ": " + e.Output }

// Program returns the claude program to run.
func Program() (string, error) {
	if p := os.Getenv(ProgramEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", ErrNotFound
		}
		return p, nil
	}
	p, err := exec.LookPath("claude")
	if err != nil {
		return "", ErrNotFound
	}
	return p, nil
}

// Registered returns the directory Claude Code serves the plugin of Gentry
// from, or "" if its marketplace is not added.
func Registered(program string) (string, error) {
	var markets []struct{ Name, Path string }
	if err := (cli{program: program}).list(&markets, "plugin", "marketplace", "list", "--json"); err != nil {
		return "", err
	}
	for _, m := range markets {
		if m.Name == integration.Name {
			return m.Path, nil
		}
	}
	return "", nil
}

// Register makes Claude Code serve the plugin built in dir with the given
// version: it adds the marketplace (again, if it points elsewhere), installs
// the plugin for the user or updates it. It does only what is missing and
// leaves a plugin the operator disabled disabled.
func Register(program, dir, version string) (Result, error) {
	c := cli{program: program}
	id := integration.Name + "@" + integration.Name

	var markets []struct{ Name, Path string }
	if err := c.list(&markets, "plugin", "marketplace", "list", "--json"); err != nil {
		return Result{}, err
	}
	registered := false
	for _, m := range markets {
		if m.Name != integration.Name {
			continue
		}
		if SamePath(m.Path, dir) {
			registered = true
		} else if err := c.run("plugin", "marketplace", "remove", integration.Name, "--json"); err != nil {
			return Result{}, err
		}
	}
	if registered {
		if err := c.run("plugin", "marketplace", "update", integration.Name, "--json"); err != nil {
			return Result{}, err
		}
	} else if err := c.run("plugin", "marketplace", "add", dir, "--json"); err != nil {
		return Result{}, err
	}

	var plugins []struct {
		ID, Version, Scope string
		Enabled            bool
	}
	if err := c.list(&plugins, "plugin", "list", "--json"); err != nil {
		return Result{}, err
	}
	for _, p := range plugins {
		if p.ID != id || p.Scope != "user" {
			continue
		}
		if p.Version == version {
			return Result{Action: Unchanged, Enabled: p.Enabled}, nil
		}
		if err := c.run("plugin", "update", id, "--json"); err != nil {
			return Result{}, err
		}
		return Result{Action: Updated, Enabled: p.Enabled}, nil
	}
	if err := c.run("plugin", "install", id, "--scope", "user", "--json"); err != nil {
		return Result{}, err
	}
	return Result{Action: Installed, Enabled: true}, nil
}

// SamePath tells whether two paths name one directory: case-insensitively on
// Windows.
func SamePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

type cli struct{ program string }

// exec runs claude with args and returns its stdout.
func (c cli) exec(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.program, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{Command: "claude " + strings.Join(args, " "), Output: failure(stdout.Bytes(), stderr.Bytes(), err)}
	}
	return stdout.Bytes(), nil
}

func (c cli) run(args ...string) error {
	_, err := c.exec(args...)
	return err
}

func (c cli) list(v any, args ...string) error {
	out, err := c.exec(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return &CommandError{Command: "claude " + strings.Join(args, " "), Output: fmt.Sprintf("unexpected output: %v", err)}
	}
	return nil
}

// failure extracts what claude said: the message of its last JSON line on
// stdout, else stderr, else the process error.
func failure(stdout, stderr []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	var result struct{ Message string }
	if json.Unmarshal([]byte(lines[len(lines)-1]), &result) == nil && result.Message != "" {
		return result.Message
	}
	if s := strings.TrimSpace(string(stderr)); s != "" {
		return s
	}
	return err.Error()
}
