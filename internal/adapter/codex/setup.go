package codex

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/t8nax/gentry/internal/integration"
)

// The states of the session start hook of Gentry in Codex, as the contract
// names them; and HookFailed, HookUnwritten.
const (
	HookTrusted   = "trusted"   // Gentry trusted the hook now
	HookPresent   = "present"   // Codex trusted the hook already
	HookUntrusted = "untrusted" // Gentry could not trust the hook: HookError tells why
	HookSkipped   = "skipped"   // the administrator turned the hooks off: the hook is not trusted
)

// The states of the hooks of Codex, as the contract names them.
const (
	HooksOn         = "on"          // the settings of the operator leave the hooks on
	HooksEnabled    = "enabled"     // the operator turned the hooks off: Gentry turned them on
	HooksManagedOff = "managed_off" // the administrator turned the hooks off
)

// The states of the data root in the sandbox of Codex, as the contract names
// them.
const (
	SandboxAdded   = "added"   // the data root is added to the folders open for writing
	SandboxPresent = "present" // the data root was open for writing already
	SandboxProfile = "profile" // a profile of permissions sets the sandbox: it is left as it is
)

// A SetupRequest is the integration with Codex asked for.
type SetupRequest struct {
	Dir     string // the folder of the integration in the data root
	Root    string // the data root, opened for writing in the sandbox of Codex
	Exe     string // the program of Gentry, which the plugin and the hook run
	Version string // the version of Gentry
	// Switch moves the plugin from the existing directory of another data
	// root.
	Switch bool
	// Worktrees are the worktrees of the pools, trusted in Codex.
	Worktrees []string
}

// A SetupResult is what Setup did, or what Current found.
type SetupResult struct {
	Action        string // Installed, Updated or Unchanged
	PluginVersion string
	Previous      string // the directory of another data root the plugin was switched from
	Enabled       bool   // whether Codex has the plugin enabled
	Settings      string // the settings of Codex
	HooksFile     string // the settings of the hooks of Codex
	HookTrust     string // HookTrusted, HookPresent, HookUntrusted, HookSkipped, HookFailed or HookUnwritten
	HookError     string // why Gentry could not trust the hook, with HookUntrusted
	HooksFeature  string // HooksOn, HooksEnabled or HooksManagedOff
	Sandbox       string // SandboxAdded, SandboxPresent or SandboxProfile
	Trust         Trust
}

// ElsewhereError means Codex serves the plugin of Gentry from the existing
// directory of another data root.
type ElsewhereError struct{ Registered, Dir string }

func (e *ElsewhereError) Error() string {
	return "codex serves the plugin from " + e.Registered + ", not " + e.Dir
}

// settingsPaths returns the settings of Codex and of its hooks.
func settingsPaths() (cfg, hooks string, err error) {
	home, err := Home()
	if err != nil {
		return "", "", err
	}
	return ConfigPath(home), HooksPath(home), nil
}

// Setup builds the plugin of Gentry in the folder of the integration,
// installs it in Codex and sets Codex up for Gentry: the session start hook
// in the settings of the hooks, trusted; the hooks on, unless the
// administrator turned them off; the data root open for writing in the
// sandbox, unless a profile sets it; the worktrees trusted. Settings of
// Codex it cannot read or write refuse it before anything: a SettingsError;
// then a program not found, ErrNotFound; a plugin of another data root
// without Switch, an ElsewhereError; a command of Codex that fails, a
// CommandError. A plugin the operator disabled stays disabled. Settings of
// the hooks it cannot read or write and a hook it cannot trust are told in
// the result. A first setup that fails once the folder of the integration
// is written removes it: Codex is connected by a setup that succeeds.
func Setup(req SetupRequest) (res SetupResult, err error) {
	cfgPath, hooksPath, err := settingsPaths()
	if err != nil {
		return SetupResult{}, err
	}
	// Every edit of the settings is tried before Codex is run, so that
	// settings Gentry cannot edit refuse the setup before any change.
	c, err := checkSettings(cfgPath, req)
	if err != nil {
		return SetupResult{}, err
	}
	program, err := Program()
	if err != nil {
		return SetupResult{}, err
	}
	// The plugin serves every session of Codex, whatever the data root of
	// this call: it is moved from an existing directory of another data
	// root only by Switch, and before anything is written.
	registered, err := Registered(program)
	if err != nil {
		return SetupResult{}, err
	}
	previous := ""
	if registered != "" && !samePlace(registered, req.Dir) {
		if exists(registered) && !req.Switch {
			return SetupResult{}, &ElsewhereError{Registered: registered, Dir: req.Dir}
		}
		previous = registered
	}
	p, err := Build(integration.Gentry(req.Exe), req.Version, req.Dir)
	if err != nil {
		return SetupResult{}, err
	}
	if !exists(req.Dir) {
		defer func() {
			if err != nil {
				unregister(program, req.Dir)
			}
		}()
	}
	if err := Write(req.Dir, p); err != nil {
		return SetupResult{}, &fs.PathError{Op: "write", Path: req.Dir, Err: err}
	}
	// Once written, the folder may turn out to be the directory registered,
	// reached by another path: then nothing is switched.
	if previous != "" && samePlace(previous, req.Dir) {
		previous, registered = "", req.Dir
	}
	disabled := disabledIn(c)
	r, err := register(program, req.Dir, registered, p.Version)
	if err != nil {
		return SetupResult{}, err
	}
	res = SetupResult{Action: r.action, PluginVersion: p.Version, Previous: previous, Enabled: r.enabled, Settings: cfgPath, HooksFile: hooksPath}

	// The requirements of the administrator are read before the settings
	// are changed: the hooks they turn off are left off. Requirements that
	// cannot be read turn nothing off.
	managed := false
	if a, err := startAppServer(program, req.Version); err == nil {
		managed, _ = a.managedHooks()
		a.close()
	}

	// Installing the plugin enabled it: a plugin the operator disabled is
	// disabled again.
	restore := r.added && disabled
	if res.HooksFeature, res.Sandbox, res.Trust, err = editSettings(cfgPath, req, managed, restore); err != nil {
		return SetupResult{}, err
	}
	if restore {
		res.Enabled = false
	}

	res.HookTrust = writeHook(hooksPath, req.Exe)
	switch {
	case res.HookTrust != "":
	case managed:
		// Codex lists no hook the administrator turned off.
		res.HookTrust = HookSkipped
	default:
		res.HookTrust, res.HookError = trustHook(program, req.Version, req.Root, hooksPath, req.Exe)
	}
	return res, nil
}

// editSettings makes the edits of setup in the settings of Codex at path
// under their lock: Codex rewrote them, so they are read anew. Gentry tried
// the edits before Codex ran, and Codex keeps the form of its settings when
// it writes them; an edit that still cannot be made refuses the setup as
// settings that cannot be written, the settings left as they are.
func editSettings(path string, req SetupRequest, managed, restore bool) (feature, sandbox string, trust Trust, err error) {
	lock, err := lockSettings(path)
	if err != nil {
		return "", "", Trust{}, err
	}
	defer lock.Release()
	c, err := readConfig(path)
	if err != nil {
		return "", "", Trust{}, err
	}
	before := c.text
	if feature, sandbox, trust, err = c.edits(req, managed, restore); err != nil {
		return "", "", Trust{}, &SettingsError{Path: path, Reason: Unwritable, Err: err}
	}
	if c.text != before {
		if err := writeFile(path, []byte(c.text)); err != nil {
			return "", "", Trust{}, &SettingsError{Path: path, Reason: Unwritable, Err: err}
		}
	}
	return feature, sandbox, trust, nil
}

// edits makes the edits of setup in the settings c, in memory: the plugin of
// Gentry disabled if restore is set, the hooks turned on unless the
// administrator turned them off, the data root opened in the sandbox and
// the worktrees trusted.
func (c *config) edits(req SetupRequest, managed, restore bool) (feature, sandbox string, trust Trust, err error) {
	if restore {
		if err := c.set("false", "plugins", pluginID, "enabled"); err != nil {
			return "", "", Trust{}, err
		}
	}
	if feature, err = c.hooksFeature(managed); err != nil {
		return "", "", Trust{}, err
	}
	if sandbox, err = c.sandbox(req.Root); err != nil {
		return "", "", Trust{}, err
	}
	if trust, err = c.trust(req.Worktrees); err != nil {
		return "", "", Trust{}, err
	}
	return feature, sandbox, trust, nil
}

// disabledIn reports whether the operator disabled the plugin of Gentry in
// the settings c.
func disabledIn(c *config) bool {
	v, ok := c.value("plugins", pluginID, "enabled")
	return ok && v == false
}

// unregister undoes a first setup that failed: the marketplace of Gentry in
// Codex is removed if it is that of the folder of the integration dir,
// which removes its plugin too, and the folder is removed, so that Codex is
// not connected and the next setup installs the plugin anew. What fails
// here is left: the failure of the setup is the one told.
func unregister(program, dir string) {
	if registered, err := Registered(program); err == nil && registered != "" && samePlace(registered, dir) {
		(cli{program: program}).run("plugin", "marketplace", "remove", integration.Name, "--json")
	}
	os.RemoveAll(dir)
}

// exists reports whether the path is there.
func exists(p string) bool {
	_, err := os.Stat(filepath.Clean(p))
	return !errors.Is(err, fs.ErrNotExist)
}

// hooksFeature turns on the hooks the operator turned off, by the feature
// hooks or by its former name codex_hooks, unless the administrator turned
// them off.
func (c *config) hooksFeature(managed bool) (string, error) {
	if managed {
		return HooksManagedOff, nil
	}
	hooks, _ := c.value("features", "hooks")
	legacy, _ := c.value("features", "codex_hooks")
	if hooks != false && legacy != false {
		return HooksOn, nil
	}
	if err := c.set("true", "features", "hooks"); err != nil {
		return "", err
	}
	if err := c.remove("features", "codex_hooks"); err != nil {
		return "", err
	}
	return HooksEnabled, nil
}

// sandbox opens the data root for writing in the sandbox of Codex, unless a
// profile of permissions sets the sandbox: Codex does not read the folders
// open for writing then.
func (c *config) sandbox(root string) (string, error) {
	if _, ok := c.value("default_permissions"); ok {
		return SandboxProfile, nil
	}
	v, _ := c.value("sandbox_workspace_write", "writable_roots")
	roots, _ := v.([]any)
	for _, r := range roots {
		if s, ok := r.(string); ok && SamePath(s, root) {
			return SandboxPresent, nil
		}
	}
	if err := c.appendTo(tomlString(root), "sandbox_workspace_write", "writable_roots"); err != nil {
		return "", err
	}
	return SandboxAdded, nil
}

// trustHook trusts the handler of Gentry by the app-server of Codex, as its
// screen /hooks does: Codex lists the hook with its key and its hash, and
// the hash is written as trusted unless it is already. A failure is told by
// its reason.
func trustHook(program, version, root, hooksPath, exe string) (state, reason string) {
	a, err := startAppServer(program, version)
	if err != nil {
		return HookUntrusted, reasonOf(err)
	}
	defer a.close()
	h, found, err := a.hook(root, hooksPath, hookCommands(exe))
	switch {
	case err != nil:
		return HookUntrusted, reasonOf(err)
	case !found:
		return HookUntrusted, notListed(exe)
	case h.TrustStatus == HookTrusted:
		return HookPresent, ""
	}
	if err := a.trustHook(h); err != nil {
		return HookUntrusted, reasonOf(err)
	}
	return HookTrusted, ""
}

// notListed is the reason of a hook Codex does not list, which Gentry wrote
// for the program exe, in the words of the commands of Codex.
func notListed(exe string) string {
	return appServerCommand + " hooks/list: no handler with the command " + HookCommand(exe)
}

// reasonOf is what Codex said about a failure.
func reasonOf(err error) string {
	var ce *CommandError
	if errors.As(err, &ce) {
		return ce.Output
	}
	return err.Error()
}

// Elsewhere returns the existing directory of another data root Codex
// serves the plugin of Gentry from, as Setup refuses it; "" if it serves
// none, or that of req.Dir, or one that is no more. Settings of Codex that
// cannot be read or written, or in which an edit of Setup cannot be made,
// are a SettingsError, before the program is looked for, as Setup refuses
// them.
func Elsewhere(req SetupRequest) (string, error) {
	dir := req.Dir
	cfgPath, _, err := settingsPaths()
	if err != nil {
		return "", err
	}
	if _, err := checkSettings(cfgPath, req); err != nil {
		return "", err
	}
	program, err := Program()
	if err != nil {
		return "", err
	}
	registered, err := Registered(program)
	if err != nil {
		return "", err
	}
	if registered == "" || samePlace(registered, dir) || !exists(registered) {
		return "", nil
	}
	return registered, nil
}

// Current returns the integration in the folder dir if it is as Setup would
// make it, nothing written or installed: the plugin of the version in dir
// installed from the marketplace of dir, the handler of Gentry in the
// settings of the hooks and trusted unless the administrator turned the
// hooks off, the hooks not turned off by the operator, and the data root
// open for writing in the sandbox unless a profile sets it.
func Current(req SetupRequest) (SetupResult, bool) {
	cfgPath, hooksPath, err := settingsPaths()
	if err != nil {
		return SetupResult{}, false
	}
	c, err := readConfig(cfgPath)
	if err != nil {
		return SetupResult{}, false
	}
	program, err := Program()
	if err != nil {
		return SetupResult{}, false
	}
	p, err := Build(integration.Gentry(req.Exe), req.Version, req.Dir)
	if err != nil || manifestVersion(req.Dir) != p.Version {
		return SetupResult{}, false
	}
	if registered, err := Registered(program); err != nil || registered == "" || !samePlace(registered, req.Dir) {
		return SetupResult{}, false
	}
	installed, found, err := (cli{program: program}).plugin()
	if err != nil || !found || installed.Version != p.Version {
		return SetupResult{}, false
	}
	if !hookCurrent(hooksPath, req.Exe) {
		return SetupResult{}, false
	}
	res := SetupResult{Action: Unchanged, PluginVersion: p.Version, Enabled: installed.Enabled, Settings: cfgPath, HooksFile: hooksPath, HookTrust: HookPresent}
	a, err := startAppServer(program, req.Version)
	if err != nil {
		return SetupResult{}, false
	}
	defer a.close()
	managed, err := a.managedHooks()
	if err != nil {
		return SetupResult{}, false
	}
	if res.HooksFeature, err = c.hooksFeature(managed); err != nil || res.HooksFeature == HooksEnabled {
		return SetupResult{}, false
	}
	if managed {
		res.HookTrust = HookSkipped
	} else if h, found, err := a.hook(req.Root, hooksPath, hookCommands(req.Exe)); err != nil || !found || h.TrustStatus != HookTrusted {
		return SetupResult{}, false
	}
	if res.Sandbox, err = c.sandbox(req.Root); err != nil || res.Sandbox == SandboxAdded {
		return SetupResult{}, false
	}
	res.Trust = Trust{Trusted: []string{}, Distrusted: distrusted(c, req.Worktrees)}
	return res, true
}

// manifestVersion returns the version of the plugin built in the folder of
// the integration dir; "" if it cannot be read.
func manifestVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, integration.Name, ".codex-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.Version
}
