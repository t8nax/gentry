package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/t8nax/gentry/internal/integration"
)

// ProgramEnv names the codex program to run; without it codex is looked up
// in PATH. Tests and checks point it at a fake codex.
const ProgramEnv = "GENTRY_CODEX"

// pluginID is the plugin of Gentry in Codex: the plugin gentry of the
// marketplace gentry.
const pluginID = integration.Name + "@" + integration.Name

// Actions of Register.
const (
	Installed = "installed"
	Updated   = "updated"
	Unchanged = "unchanged"
)

// commandTimeout limits one codex command, and a session of its app-server.
const commandTimeout = 2 * time.Minute

// ErrNotFound means the codex program cannot be found.
var ErrNotFound = errors.New("codex program not found")

// CommandError is a codex command that failed.
type CommandError struct {
	Command string // the command as typed, such as "codex plugin add gentry@gentry --json"
	Output  string // what codex said about the failure
}

func (e *CommandError) Error() string { return e.Command + ": " + e.Output }

// Program returns the codex program to run.
func Program() (string, error) {
	if p := os.Getenv(ProgramEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", ErrNotFound
		}
		return p, nil
	}
	p, err := exec.LookPath("codex")
	if err != nil {
		return "", ErrNotFound
	}
	return p, nil
}

type cli struct{ program string }

// exec runs codex with args and returns its stdout.
func (c cli) exec(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.program, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{Command: "codex " + strings.Join(args, " "), Output: failure(stdout.Bytes(), stderr.Bytes(), err)}
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
		return &CommandError{Command: "codex " + strings.Join(args, " "), Output: fmt.Sprintf("unexpected output: %v", err)}
	}
	return nil
}

// failure extracts what codex said: stderr, else stdout, else the process
// error.
func failure(stdout, stderr []byte, err error) string {
	if s := strings.TrimSpace(string(stderr)); s != "" {
		return s
	}
	if s := strings.TrimSpace(string(stdout)); s != "" {
		return s
	}
	return err.Error()
}

// Registered returns the root of the marketplace of Gentry Codex has, or ""
// if its marketplace is not added.
func Registered(program string) (string, error) {
	var list struct {
		Marketplaces []struct{ Name, Root string } `json:"marketplaces"`
	}
	if err := (cli{program: program}).list(&list, "plugin", "marketplace", "list", "--json"); err != nil {
		return "", err
	}
	for _, m := range list.Marketplaces {
		if m.Name == integration.Name {
			return m.Root, nil
		}
	}
	return "", nil
}

// installedPlugin is the plugin as codex plugin list --json shows it.
type installedPlugin struct {
	PluginID string `json:"pluginId"`
	Version  string `json:"version"`
	Enabled  bool   `json:"enabled"`
}

// plugin returns the plugin of Gentry Codex has installed, if it has.
func (c cli) plugin() (installedPlugin, bool, error) {
	var list struct {
		Installed []installedPlugin `json:"installed"`
	}
	if err := c.list(&list, "plugin", "list", "--json"); err != nil {
		return installedPlugin{}, false, err
	}
	for _, p := range list.Installed {
		if p.PluginID == pluginID {
			return p, true, nil
		}
	}
	return installedPlugin{}, false, nil
}

// registration is what register did.
type registration struct {
	action  string // Installed, Updated or Unchanged
	enabled bool   // whether Codex has the plugin enabled
	added   bool   // the plugin was installed now: Codex enabled it
}

// register makes Codex serve the plugin built in dir with the version: it
// adds the marketplace of dir, after removing the marketplace of Gentry of
// registered, another directory, and installs the plugin unless Codex has
// it of the version. Codex reads the marketplace of a local directory from
// it, so the marketplace of dir is not added again.
func register(program, dir, registered, version string) (registration, error) {
	c := cli{program: program}
	if registered != "" && !samePlace(registered, dir) {
		if err := c.run("plugin", "marketplace", "remove", integration.Name, "--json"); err != nil {
			return registration{}, err
		}
		registered = ""
	}
	if registered == "" {
		if err := c.run("plugin", "marketplace", "add", dir, "--json"); err != nil {
			return registration{}, err
		}
	}
	p, found, err := c.plugin()
	if err != nil {
		return registration{}, err
	}
	if found && p.Version == version {
		return registration{action: Unchanged, enabled: p.Enabled}, nil
	}
	if err := c.run("plugin", "add", pluginID, "--json"); err != nil {
		return registration{}, err
	}
	action := Installed
	if found {
		action = Updated
	}
	return registration{action: action, enabled: true, added: true}, nil
}

// appServer is a session of the app-server of Codex: JSON-RPC over the
// standard input and output, a message a line.
type appServer struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	in     io.WriteCloser
	out    *bufio.Reader
	stderr bytes.Buffer
	next   int
	done   bool // the session is over
}

// appServerCommand is the command of the app-server as typed.
const appServerCommand = "codex app-server"

// startAppServer starts the app-server of Codex and initializes the session.
func startAppServer(program, version string) (*appServer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	a := &appServer{cmd: exec.CommandContext(ctx, program, "app-server"), cancel: cancel}
	a.cmd.Stderr = &a.stderr
	in, err := a.cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := a.cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	a.in, a.out = in, bufio.NewReader(out)
	if err := a.cmd.Start(); err != nil {
		cancel()
		return nil, &CommandError{Command: appServerCommand, Output: err.Error()}
	}
	params := map[string]any{"clientInfo": map[string]any{"name": integration.Name, "title": "Gentry", "version": version}}
	if err := a.call("initialize", params, nil); err != nil {
		a.close()
		return nil, err
	}
	if err := a.notify("initialized"); err != nil {
		return nil, a.failed(err)
	}
	return a, nil
}

// send writes a message.
func (a *appServer) send(msg map[string]any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = a.in.Write(append(b, '\n'))
	return err
}

func (a *appServer) notify(method string) error {
	return a.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

// call sends the request of method with params and reads its answer into
// result, skipping the other messages of the server.
func (a *appServer) call(method string, params, result any) error {
	a.next++
	id := a.next
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := a.send(msg); err != nil {
		return a.failed(err)
	}
	for {
		line, err := a.out.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var answer struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(line, &answer) == nil && answer.Method == "" && string(answer.ID) == fmt.Sprint(id) {
				if answer.Error != nil {
					return &CommandError{Command: appServerCommand + " " + method, Output: answer.Error.Message}
				}
				if result != nil {
					if err := json.Unmarshal(answer.Result, result); err != nil {
						return &CommandError{Command: appServerCommand + " " + method, Output: fmt.Sprintf("unexpected output: %v", err)}
					}
				}
				return nil
			}
		}
		if err != nil {
			return a.failed(err)
		}
	}
}

// failed ends the session after err and returns what Codex said.
func (a *appServer) failed(err error) error {
	if a.done {
		return &CommandError{Command: appServerCommand, Output: failure(nil, a.stderr.Bytes(), err)}
	}
	a.done = true
	a.in.Close()
	io.Copy(io.Discard, a.out)
	if werr := a.cmd.Wait(); werr != nil {
		err = werr
	}
	a.cancel()
	return &CommandError{Command: appServerCommand, Output: failure(nil, a.stderr.Bytes(), err)}
}

// close ends the session: the end of the input ends the app-server.
func (a *appServer) close() {
	if a.done {
		return
	}
	a.done = true
	a.in.Close()
	done := make(chan struct{})
	go func() {
		io.Copy(io.Discard, a.out)
		a.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		a.cmd.Process.Kill()
		<-done
	}
	a.cancel()
}

// managedHooks reports whether the requirements of the administrator of
// Codex turn the hooks off: only the hooks of the administrator are
// allowed, or the feature hooks is required off.
func (a *appServer) managedHooks() (bool, error) {
	var res struct {
		Requirements *struct {
			AllowManagedHooksOnly *bool          `json:"allowManagedHooksOnly"`
			FeatureRequirements   map[string]any `json:"featureRequirements"`
		} `json:"requirements"`
	}
	if err := a.call("configRequirements/read", map[string]any{}, &res); err != nil {
		return false, err
	}
	r := res.Requirements
	if r == nil {
		return false, nil
	}
	if r.AllowManagedHooksOnly != nil && *r.AllowManagedHooksOnly {
		return true, nil
	}
	return r.FeatureRequirements["hooks"] == false, nil
}

// listedHook is a hook as hooks/list of the app-server gives it.
type listedHook struct {
	Key         string `json:"key"`
	Command     string `json:"command"`
	SourcePath  string `json:"sourcePath"`
	CurrentHash string `json:"currentHash"`
	TrustStatus string `json:"trustStatus"`
}

// hook returns the hook of the settings of the hooks at path with the
// command, as Codex lists it for the folder cwd.
func (a *appServer) hook(cwd, path, command string) (listedHook, bool, error) {
	var res struct {
		Data []struct {
			Hooks []listedHook `json:"hooks"`
		} `json:"data"`
	}
	if err := a.call("hooks/list", map[string]any{"cwds": []string{cwd}}, &res); err != nil {
		return listedHook{}, false, err
	}
	for _, d := range res.Data {
		for _, h := range d.Hooks {
			if h.Command == command && (h.SourcePath == "" || samePlace(h.SourcePath, path)) {
				return h, true, nil
			}
		}
	}
	return listedHook{}, false, nil
}

// trustHook writes the hash of the hook as trusted in the settings of
// Codex, as the screen /hooks of Codex does.
func (a *appServer) trustHook(h listedHook) error {
	edit := map[string]any{
		"keyPath":       "hooks.state",
		"value":         map[string]any{h.Key: map[string]any{"trusted_hash": h.CurrentHash}},
		"mergeStrategy": "upsert",
	}
	return a.call("config/batchWrite", map[string]any{"edits": []any{edit}}, nil)
}
