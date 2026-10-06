// Package git runs the git program for Gentry. Paths it returns are
// canonical (package paths).
package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/paths"
)

// ErrNotFound means the git program is not found.
var ErrNotFound = errors.New("git not found")

// ErrNotRepo means a directory is not inside a git worktree.
var ErrNotRepo = errors.New("not a git worktree")

// CommandError is a git command that failed.
type CommandError struct {
	Command string // the command line, such as git commit -m …
	Output  string // what git printed, trimmed
}

func (e *CommandError) Error() string { return e.Command + ": " + e.Output }

// NetworkTimeout limits a git command that talks to a remote repository.
var NetworkTimeout = 20 * time.Second

// Opts are options of a git command run by Run.
type Opts struct {
	Stdin string   // the standard input
	Env   []string // variables added to the environment, as NAME=value
	// Network marks a command that talks to a remote repository: it is
	// limited by NetworkTimeout and must not ask for a login, as nobody may
	// be there to answer.
	Network bool
}

// run runs git with args in dir and returns its standard output.
func run(dir string, args ...string) (string, error) { return Run(dir, Opts{}, args...) }

// Run runs git with args in dir with options o and returns its standard
// output. A command that fails returns *CommandError.
func Run(dir string, o Opts, args ...string) (string, error) {
	program, err := exec.LookPath("git")
	if err != nil {
		return "", ErrNotFound
	}
	ctx := context.Background()
	if o.Network {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, NetworkTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	// Messages of git are parsed only to tell a rejected push, but keep them
	// in English for the output in git_failed to match across machines.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=C")
	if o.Network {
		cmd.Env = append(cmd.Env, noPrompts()...)
	}
	cmd.Env = append(cmd.Env, o.Env...)
	// A helper git starts, such as ssh, may outlive git killed at the time
	// limit and keep the pipes open: do not wait for it long.
	cmd.WaitDelay = 2 * time.Second
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ctx.Err() != nil {
			return "", &CommandError{Command: "git " + strings.Join(args, " "), Output: "timed out after " + NetworkTimeout.String()}
		}
		if !errors.As(err, &ee) {
			return "", err
		}
		out := strings.TrimSpace(stderr.String())
		if out == "" {
			out = strings.TrimSpace(stdout.String())
		}
		return "", &CommandError{Command: "git " + strings.Join(args, " "), Output: out}
	}
	return stdout.String(), nil
}

// noPrompts returns the environment that keeps git and the programs it runs
// from asking for a login: a remote that needs one is unavailable.
func noPrompts() []string {
	env := []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"}
	// ssh asks for a password or a passphrase unless in batch mode. A command
	// of the operator's own is left as it is.
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
		if out, err := run("", "config", "--get", "core.sshCommand"); err != nil || strings.TrimSpace(out) == "" {
			env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		}
	}
	return env
}

// Find reports ErrNotFound if git is not installed.
func Find() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrNotFound
	}
	return nil
}

// Root returns the root of the worktree that contains dir, or ErrNotRepo.
func Root(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	var ce *CommandError
	if errors.As(err, &ce) {
		return "", ErrNotRepo
	}
	if err != nil {
		return "", err
	}
	return paths.Canonical(strings.TrimSpace(out))
}

// Worktree is a worktree of a repository as git lists it.
type Worktree struct {
	Path string
	Main bool // the main worktree: the original clone
}

// Worktrees lists the worktrees of the repository of dir, the main one first.
// A bare main repository has no working tree and is left out.
func Worktrees(dir string) ([]Worktree, error) {
	out, err := run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	for i, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var path string
		bare := false
		for _, line := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				path = p
			}
			if line == "bare" {
				bare = true
			}
		}
		if path == "" || bare {
			continue
		}
		p, err := paths.Canonical(path)
		if err != nil {
			return nil, err
		}
		list = append(list, Worktree{Path: p, Main: i == 0})
	}
	return list, nil
}

// Branch returns the branch checked out in the worktree of dir, or "" if the
// worktree is not on a branch.
func Branch(dir string) (string, error) {
	out, err := run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	var ce *CommandError
	if errors.As(err, &ce) && ce.Output == "" {
		return "", nil // detached HEAD: symbolic-ref fails silently
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Changed returns the files of the worktree of dir with uncommitted changes,
// staged or not, and the new files, by path relative to the worktree root
// with forward slashes. Files git ignores are left out.
func Changed(dir string) ([]string, error) {
	out, err := run(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var files []string
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		r := records[i]
		if len(r) < 4 {
			continue
		}
		files = append(files, r[3:])
		// A rename or a copy is followed by the path it came from.
		if r[0] == 'R' || r[0] == 'C' {
			i++
		}
	}
	return files, nil
}

// Init creates a repository in dir, creating dir if needed.
func Init(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_, err := run(dir, "init", "--quiet")
	return err
}

// HasCommits reports whether the repository of dir has a commit.
func HasCommits(dir string) (bool, error) {
	_, err := run(dir, "rev-parse", "--quiet", "--verify", "HEAD")
	var ce *CommandError
	if errors.As(err, &ce) {
		return false, nil
	}
	return err == nil, err
}

// Commit commits file, relative to dir, with message, as the user git is
// configured for.
func Commit(dir, file, message string) error {
	if _, err := run(dir, "add", "--", file); err != nil {
		return err
	}
	_, err := run(dir, "commit", "--quiet", "--message", message, "--", file)
	return err
}
