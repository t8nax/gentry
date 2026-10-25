// Command fakecodex imitates the commands of Codex CLI 0.159.3 Gentry runs,
// with the same output, so that the integration with Codex can be tested
// without a real Codex. Its state is in the files Codex keeps in CODEX_HOME:
// config.toml, hooks.json and plugins/cache/<marketplace>/<plugin>/<version>.
//
// It runs plugin marketplace add|list|remove|upgrade, plugin add, plugin
// list and plugin remove, with and without --json, as Codex 0.159.3 does
// them (checked on Codex 0.159.3 with a CODEX_HOME of its own): the
// marketplace of another source under a name taken is refused, the removal
// of a marketplace leaves its plugin out of the list, and plugin add writes
// enabled = true. Its app-server answers initialize, hooks/list,
// config/read, config/batchWrite, config/value/write and
// configRequirements/read over JSON-RPC on the standard input and output;
// with the hooks off, hooks/list gives no hook.
//
// FAKECODEX_FAIL=<command> makes that command fail, such as «plugin add»,
// «plugin marketplace list» or «app-server». FAKECODEX_REQUIREMENTS names a
// file with the answer to configRequirements/read, the requirements of an
// administrator. FAKECODEX_LOG names a file the fake adds a line to for each
// command it runs and each method its app-server answers, such as «plugin add
// gentry@gentry --json» or «app-server config/batchWrite».
//
// Build: go build -o codex.exe ./tests/testkit/fakecodex
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/t8nax/gentry/tests/testkit/toml"
)

// Version is the version of Codex the fake imitates.
const Version = "0.159.3"

func main() {
	args := os.Args[1:]
	logLine(strings.Join(args, " "))
	os.Exit(run(args))
}

// logLine adds the line to the file FAKECODEX_LOG names.
func logLine(line string) {
	p := os.Getenv("FAKECODEX_LOG")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintln(f, line)
	f.Close()
}

// flagsWithValue are the flags of Codex that take a value.
var flagsWithValue = []string{"-c", "--config", "--enable", "--disable", "-m", "--marketplace", "--ref", "--sparse", "--listen", "--code-mode-host"}

// command is a call: its words, its arguments and its flags.
type command struct {
	words []string // such as plugin marketplace add
	args  []string
	json  bool
	flags map[string]string
}

// The commands the fake runs, by their words.
var commands = []string{"plugin marketplace add", "plugin marketplace list", "plugin marketplace remove", "plugin marketplace upgrade",
	"plugin add", "plugin list", "plugin remove", "app-server", "--version", "version"}

func parse(args []string) command {
	c := command{flags: map[string]string{}}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			c.json = true
		case slices.Contains(flagsWithValue, a) && i+1 < len(args):
			c.flags[a] = args[i+1]
			i++
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
			name, value, _ := strings.Cut(a, "=")
			c.flags[name] = value
		case a == "--version" || a == "-V":
			pos = append(pos, "--version")
		case strings.HasPrefix(a, "-"):
		default:
			pos = append(pos, a)
		}
	}
	for _, name := range commands {
		w := strings.Fields(name)
		if len(pos) >= len(w) && slices.Equal(pos[:len(w)], w) {
			c.words, c.args = w, pos[len(w):]
			return c
		}
	}
	c.args = pos
	return c
}

func run(args []string) int {
	c := parse(args)
	name := strings.Join(c.words, " ")
	if fail := os.Getenv("FAKECODEX_FAIL"); fail != "" && name != "" && fail == name {
		return failed("simulated failure of codex " + name)
	}
	switch name {
	case "--version", "version":
		fmt.Println("codex-cli " + Version)
		return 0
	case "app-server":
		return appServer(os.Stdin, os.Stdout)
	case "":
		fmt.Fprintf(os.Stderr, "error: unrecognized subcommand '%s'\n", strings.Join(c.args, " "))
		return 2
	}
	st, err := load()
	if err != nil {
		return failed(err.Error())
	}
	switch name {
	case "plugin marketplace add":
		return st.marketplaceAdd(c)
	case "plugin marketplace list":
		return st.marketplaceList(c)
	case "plugin marketplace remove":
		return st.marketplaceRemove(c)
	case "plugin marketplace upgrade":
		return st.marketplaceUpgrade(c)
	case "plugin add":
		return st.pluginAdd(c)
	case "plugin list":
		return st.pluginList(c)
	case "plugin remove":
		return st.pluginRemove(c)
	}
	return failed("unknown command " + name)
}

func failed(message string) int {
	fmt.Fprintln(os.Stderr, "Error: "+message)
	return 1
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// home is CODEX_HOME, or .codex in the home of the user.
func home() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}

func configPath() string { return filepath.Join(home(), "config.toml") }
func hooksPath() string  { return filepath.Join(home(), "hooks.json") }

// state is the settings of Codex as the fake reads them.
type state struct {
	config map[string]any
}

// load reads config.toml: Codex refuses to run with settings it cannot read.
func load() (*state, error) {
	b, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) {
		return &state{config: map[string]any{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error loading config.toml: %v", err)
	}
	cfg, err := toml.Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("error loading config.toml: %v", err)
	}
	return &state{config: cfg}, nil
}

func (s *state) save() error {
	if err := os.MkdirAll(home(), 0o755); err != nil {
		return fmt.Errorf("failed to write config.toml: %v", err)
	}
	if err := os.WriteFile(configPath(), []byte(toml.Marshal(s.config)), 0o644); err != nil {
		return fmt.Errorf("failed to write config.toml: %v", err)
	}
	return nil
}

// stripVerbatim removes the prefix \\?\ Codex writes before the local paths
// on Windows.
func stripVerbatim(p string) string { return strings.TrimPrefix(p, `\\?\`) }

// verbatim gives the path as Codex writes the source of a local marketplace.
func verbatim(p string) string {
	if runtime.GOOS == "windows" {
		return `\\?\` + p
	}
	return p
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(stripVerbatim(a)), filepath.Clean(stripVerbatim(b))
	return a == b || runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

func (s *state) table(keys ...string) map[string]any {
	v, _ := toml.At(s.config, keys...)
	m, _ := v.(map[string]any)
	return m
}

// marketplaces are the marketplaces configured, by name: their sources.
func (s *state) marketplaces() map[string]string {
	out := map[string]string{}
	for name, v := range s.table("marketplaces") {
		if m, ok := v.(map[string]any); ok {
			src, _ := m["source"].(string)
			out[name] = src
		}
	}
	return out
}

// manifest is the marketplace.json of a marketplace.
type manifest struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name   string          `json:"name"`
		Source json.RawMessage `json:"source"`
	} `json:"plugins"`
}

func readManifest(root string) (manifest, error) {
	var m manifest
	for _, rel := range []string{filepath.Join(".agents", "plugins", "marketplace.json"), filepath.Join(".claude-plugin", "marketplace.json")} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return m, err
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return m, fmt.Errorf("invalid marketplace file %s: %v", filepath.Join(root, rel), err)
		}
		return m, nil
	}
	return m, fmt.Errorf("no marketplace file found in %s", root)
}

// pluginDir returns the folder of the plugin in the marketplace of root.
func (m manifest) pluginDir(root, name string) (string, bool) {
	for _, p := range m.Plugins {
		if p.Name != name {
			continue
		}
		var path string
		if json.Unmarshal(p.Source, &path) != nil {
			var obj struct {
				Path string `json:"path"`
			}
			json.Unmarshal(p.Source, &obj)
			path = obj.Path
		}
		if path == "" {
			return "", false
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		return path, true
	}
	return "", false
}

func (s *state) marketplaceAdd(c command) int {
	if len(c.args) == 0 {
		return failed("the source of the marketplace is missing")
	}
	root, err := filepath.Abs(c.args[0])
	if err == nil {
		_, err = os.Stat(root)
	}
	if err != nil {
		return failed("failed to resolve local marketplace source path: " + err.Error())
	}
	m, err := readManifest(root)
	if err != nil {
		return failed(err.Error())
	}
	already := false
	if src, ok := s.marketplaces()[m.Name]; ok {
		if !samePath(src, root) {
			return failed(fmt.Sprintf("marketplace '%s' is already added from a different source; remove it before adding this source", m.Name))
		}
		already = true
	} else {
		toml.Set(s.config, map[string]any{"source_type": "local", "source": verbatim(root)}, "marketplaces", m.Name)
		if err := s.save(); err != nil {
			return failed(err.Error())
		}
	}
	if c.json {
		printJSON(map[string]any{"marketplaceName": m.Name, "installedRoot": root, "alreadyAdded": already})
		return 0
	}
	if already {
		fmt.Printf("Marketplace `%s` is already added from %s.\n", m.Name, verbatim(root))
	} else {
		fmt.Printf("Added marketplace `%s` from %s.\n", m.Name, verbatim(root))
	}
	fmt.Printf("Installed marketplace root: %s\n", root)
	return 0
}

func (s *state) marketplaceList(c command) int {
	ms := s.marketplaces()
	names := make([]string, 0, len(ms))
	for n := range ms {
		names = append(names, n)
	}
	slices.Sort(names)
	if c.json {
		list := []map[string]string{}
		for _, n := range names {
			list = append(list, map[string]string{"name": n, "root": stripVerbatim(ms[n])})
		}
		printJSON(map[string]any{"marketplaces": list})
		return 0
	}
	fmt.Println("MARKETPLACE  ROOT")
	for _, n := range names {
		fmt.Printf("%-12s %s\n", n, stripVerbatim(ms[n]))
	}
	return 0
}

func (s *state) marketplaceRemove(c command) int {
	if len(c.args) == 0 {
		return failed("the name of the marketplace is missing")
	}
	name := c.args[0]
	if _, ok := s.marketplaces()[name]; !ok {
		return failed(fmt.Sprintf("marketplace `%s` is not configured or installed", name))
	}
	toml.Delete(s.config, "marketplaces", name)
	if len(s.table("marketplaces")) == 0 {
		toml.Delete(s.config, "marketplaces")
	}
	if err := s.save(); err != nil {
		return failed(err.Error())
	}
	if c.json {
		printJSON(map[string]any{"marketplaceName": name, "installedRoot": nil})
		return 0
	}
	fmt.Printf("Removed marketplace `%s`.\n", name)
	return 0
}

// marketplaceUpgrade refreshes the Git marketplaces, as Codex does: the
// marketplaces of the fake are local, so a marketplace named is refused and
// with none named nothing is upgraded.
func (s *state) marketplaceUpgrade(c command) int {
	if len(c.args) > 0 {
		return failed(fmt.Sprintf("marketplace `%s` is not configured as a Git marketplace", c.args[0]))
	}
	if c.json {
		printJSON(map[string]any{"selectedMarketplaces": []any{}, "upgradedRoots": []any{}, "errors": []any{}})
		return 0
	}
	fmt.Println("No Git marketplaces to upgrade.")
	return 0
}

// selector splits PLUGIN@MARKETPLACE, or PLUGIN with --marketplace.
func selector(c command) (plugin, market string, ok bool) {
	if len(c.args) == 0 {
		return "", "", false
	}
	plugin, market, found := strings.Cut(c.args[0], "@")
	if !found {
		market = c.flags["-m"] + c.flags["--marketplace"]
	}
	return plugin, market, plugin != "" && market != ""
}

func cacheDir(market, plugin string) string {
	return filepath.Join(home(), "plugins", "cache", market, plugin)
}

// pluginVersion is the version of the manifest of the plugin in dir; local
// without one.
func pluginVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "local"
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil || m.Version == "" {
		return "local"
	}
	return m.Version
}

func (s *state) pluginAdd(c command) int {
	plugin, market, ok := selector(c)
	if !ok {
		return failed("the plugin to install is missing")
	}
	src, ok := s.marketplaces()[market]
	if !ok {
		return failed(fmt.Sprintf("marketplace `%s` is not configured or installed", market))
	}
	root := stripVerbatim(src)
	m, err := readManifest(root)
	if err != nil {
		return failed(err.Error())
	}
	dir, ok := m.pluginDir(root, plugin)
	if !ok {
		return failed(fmt.Sprintf("plugin `%s` was not found in marketplace `%s`", plugin, market))
	}
	if _, err := os.Stat(dir); err != nil {
		return failed(fmt.Sprintf("plugin `%s` was not found in marketplace `%s`: %v", plugin, market, err))
	}
	version := pluginVersion(dir)
	cache := cacheDir(market, plugin)
	if err := os.RemoveAll(cache); err != nil {
		return failed(err.Error())
	}
	installed := filepath.Join(cache, version)
	if err := os.CopyFS(installed, os.DirFS(dir)); err != nil {
		return failed(err.Error())
	}
	toml.Set(s.config, true, "plugins", plugin+"@"+market, "enabled")
	if err := s.save(); err != nil {
		return failed(err.Error())
	}
	if c.json {
		printJSON(map[string]any{"pluginId": plugin + "@" + market, "name": plugin, "marketplaceName": market,
			"version": version, "installedPath": installed, "authPolicy": "ON_INSTALL"})
		return 0
	}
	fmt.Printf("Installed plugin `%s@%s` %s.\n", plugin, market, version)
	return 0
}

// installedVersion returns the version of the plugin in the cache; false if
// it is not there.
func installedVersion(market, plugin string) (string, bool) {
	entries, err := os.ReadDir(cacheDir(market, plugin))
	if err != nil {
		return "", false
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return "", false
	}
	slices.Sort(versions)
	return versions[len(versions)-1], true
}

func (s *state) pluginList(c command) int {
	ms := s.marketplaces()
	installed := []map[string]any{}
	available := []map[string]any{}
	ids := make([]string, 0)
	for id := range s.table("plugins") {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	seen := map[string]bool{}
	only := c.flags["-m"] + c.flags["--marketplace"]
	for _, id := range ids {
		plugin, market, _ := strings.Cut(id, "@")
		src, ok := ms[market]
		if !ok || only != "" && market != only {
			continue
		}
		v, ok := installedVersion(market, plugin)
		if !ok {
			continue
		}
		enabled := true
		if e, ok := toml.At(s.config, "plugins", id, "enabled"); ok {
			enabled, _ = e.(bool)
		}
		root := stripVerbatim(src)
		path := filepath.Join(root, plugin)
		if m, err := readManifest(root); err == nil {
			if d, ok := m.pluginDir(root, plugin); ok {
				path = d
			}
		}
		seen[id] = true
		installed = append(installed, map[string]any{
			"pluginId": id, "name": plugin, "marketplaceName": market, "version": v,
			"installed": true, "enabled": enabled,
			"source":            map[string]any{"source": "local", "path": path},
			"marketplaceSource": map[string]any{"sourceType": "local", "source": src},
			"installPolicy":     "AVAILABLE", "authPolicy": "ON_INSTALL",
		})
	}
	names := make([]string, 0, len(ms))
	for n := range ms {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, market := range names {
		if only != "" && market != only {
			continue
		}
		root := stripVerbatim(ms[market])
		m, err := readManifest(root)
		if err != nil {
			continue
		}
		for _, p := range m.Plugins {
			id := p.Name + "@" + market
			if seen[id] {
				continue
			}
			dir, _ := m.pluginDir(root, p.Name)
			available = append(available, map[string]any{
				"pluginId": id, "name": p.Name, "marketplaceName": market, "version": pluginVersion(dir),
				"installed": false, "enabled": false,
				"source":            map[string]any{"source": "local", "path": dir},
				"marketplaceSource": map[string]any{"sourceType": "local", "source": ms[market]},
				"installPolicy":     "AVAILABLE", "authPolicy": "ON_INSTALL",
			})
		}
	}
	if c.json {
		printJSON(map[string]any{"installed": installed, "available": available})
		return 0
	}
	fmt.Println("PLUGIN  VERSION  ENABLED")
	for _, p := range installed {
		fmt.Printf("%s  %s  %v\n", p["pluginId"], p["version"], p["enabled"])
	}
	return 0
}

func (s *state) pluginRemove(c command) int {
	plugin, market, ok := selector(c)
	if !ok {
		return failed("the plugin to remove is missing")
	}
	id := plugin + "@" + market
	if _, ok := s.table("plugins")[id]; !ok {
		return failed(fmt.Sprintf("plugin `%s` is not installed", id))
	}
	toml.Delete(s.config, "plugins", id)
	os.RemoveAll(cacheDir(market, plugin))
	if err := s.save(); err != nil {
		return failed(err.Error())
	}
	if c.json {
		printJSON(map[string]any{"pluginId": id, "name": plugin, "marketplaceName": market})
		return 0
	}
	fmt.Printf("Removed plugin `%s`.\n", id)
	return 0
}

// A request is a message of JSON-RPC from the client.
type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// appServer answers the requests of the input, a message a line, until the
// input ends.
func appServer(in io.Reader, out io.Writer) int {
	sc := bufio.NewScanner(in)
	sc.Buffer(nil, 16<<20)
	w := bufio.NewWriter(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r request
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to deserialize JSONRPCMessage: %v\n", err)
			continue
		}
		if len(r.ID) == 0 || string(r.ID) == "null" {
			continue // a notification
		}
		logLine("app-server " + r.Method)
		result, rerr := answer(r)
		msg := map[string]any{"id": r.ID}
		if rerr != nil {
			msg["error"] = rerr
		} else {
			msg["result"] = result
		}
		b, _ := json.Marshal(msg)
		w.Write(append(b, '\n'))
		w.Flush()
	}
	return 0
}

func answer(r request) (any, *rpcError) {
	switch r.Method {
	case "initialize":
		return map[string]any{"userAgent": "fakecodex/" + Version, "codexHome": home(),
			"platformFamily": runtime.GOOS, "platformOs": runtime.GOOS}, nil
	case "configRequirements/read":
		return requirements()
	case "config/read":
		st, err := load()
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		return map[string]any{"config": st.config, "origins": map[string]any{}, "layers": nil}, nil
	case "config/batchWrite", "config/value/write":
		return configWrite(r)
	case "hooks/list":
		var p struct {
			Cwds []string `json:"cwds"`
		}
		json.Unmarshal(r.Params, &p)
		return hooksList(p.Cwds)
	}
	return nil, &rpcError{-32601, "method not found: " + r.Method}
}

// requirements answers configRequirements/read: the file of
// FAKECODEX_REQUIREMENTS, or no requirements.
func requirements() (any, *rpcError) {
	p := os.Getenv("FAKECODEX_REQUIREMENTS")
	if p == "" {
		return map[string]any{"requirements": nil}, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	return v, nil
}

// requirement returns the field of the requirements of the administrator.
func requirement(keys ...string) (any, bool) {
	v, rerr := requirements()
	if rerr != nil {
		return nil, false
	}
	m, _ := v.(map[string]any)
	cur, _ := m["requirements"].(map[string]any)
	for i, k := range keys {
		x, ok := cur[k]
		if !ok {
			return nil, false
		}
		if i == len(keys)-1 {
			return x, true
		}
		cur, _ = x.(map[string]any)
	}
	return nil, false
}

func version(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// splitKeyPath splits a dotted key path; a part may be quoted.
func splitKeyPath(p string) []string {
	var keys []string
	var cur strings.Builder
	quote := rune(0)
	for _, r := range p {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == '.':
			keys = append(keys, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(keys, cur.String())
}

// tomlValue turns a value of JSON into one of TOML.
func tomlValue(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = tomlValue(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if e != nil {
				out[k] = tomlValue(e)
			}
		}
		return out
	}
	return v
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

type edit struct {
	KeyPath       string `json:"keyPath"`
	Value         any    `json:"value"`
	MergeStrategy string `json:"mergeStrategy"`
}

func configWrite(r request) (any, *rpcError) {
	var p struct {
		Edits           []edit  `json:"edits"`
		FilePath        *string `json:"filePath"`
		ExpectedVersion *string `json:"expectedVersion"`
		edit
	}
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return nil, &rpcError{-32602, err.Error()}
	}
	if r.Method == "config/value/write" {
		p.Edits = []edit{p.edit}
	}
	// GEN-59 can refuse only the hook's trust write while every other
	// app-server request and setting succeeds, just as Codex may do.
	if reason := os.Getenv("FAKECODEX_HOOK_TRUST_ERROR"); reason != "" {
		for _, e := range p.Edits {
			if e.KeyPath == "hooks.state" {
				return nil, &rpcError{-32603, reason}
			}
		}
	}
	path := configPath()
	if p.FilePath != nil && *p.FilePath != "" {
		path = *p.FilePath
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &rpcError{-32603, err.Error()}
	}
	if p.ExpectedVersion != nil && *p.ExpectedVersion != "" && *p.ExpectedVersion != version(b) {
		return nil, &rpcError{-32600, "config version conflict"}
	}
	cfg := map[string]any{}
	if len(b) > 0 {
		if cfg, err = toml.Parse(string(b)); err != nil {
			return nil, &rpcError{-32603, "error loading config.toml: " + err.Error()}
		}
	}
	for _, e := range p.Edits {
		keys := splitKeyPath(e.KeyPath)
		if e.Value == nil {
			toml.Delete(cfg, keys...)
			continue
		}
		v := tomlValue(e.Value)
		if old, ok := toml.At(cfg, keys...); ok && e.MergeStrategy == "upsert" {
			om, ok1 := old.(map[string]any)
			nm, ok2 := v.(map[string]any)
			if ok1 && ok2 {
				merge(om, nm)
				continue
			}
		}
		toml.Set(cfg, v, keys...)
	}
	text := []byte(toml.Marshal(cfg))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	if err := os.WriteFile(path, text, 0o644); err != nil {
		return nil, &rpcError{-32603, "failed to write config: " + err.Error()}
	}
	return map[string]any{"status": "ok", "version": version(text), "filePath": path, "overriddenMetadata": nil}, nil
}

// events are the events of hooks in the order Codex lists them.
var events = []string{"PreToolUse", "PermissionRequest", "PostToolUse", "PreCompact", "PostCompact", "SessionStart",
	"SessionEnd", "UserPromptSubmit", "SubagentStart", "SubagentStop", "Stop", "Interrupt"}

// snake turns SessionStart into session_start.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// camel turns SessionStart into sessionStart.
func camel(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// hooksOn reports whether the hooks are on: the feature hooks, by its
// deprecated name codex_hooks too, and the requirements of the
// administrator.
func hooksOn(cfg map[string]any) bool {
	if v, ok := requirement("featureRequirements", "hooks"); ok && v == false {
		return false
	}
	if v, ok := requirement("allowManagedHooksOnly"); ok && v == true {
		return false
	}
	if v, ok := toml.At(cfg, "features", "hooks"); ok {
		return v != false
	}
	if v, ok := toml.At(cfg, "features", "codex_hooks"); ok {
		return v != false
	}
	return true
}

func hooksList(cwds []string) (any, *rpcError) {
	if len(cwds) == 0 {
		wd, _ := os.Getwd()
		cwds = []string{wd}
	}
	st, err := load()
	if err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	hooks, warnings := userHooks(st.config)
	if !hooksOn(st.config) {
		hooks = []map[string]any{}
	}
	var data []map[string]any
	for _, cwd := range cwds {
		data = append(data, map[string]any{"cwd": cwd, "hooks": hooks, "warnings": warnings, "errors": []any{}})
	}
	return map[string]any{"data": data}, nil
}

// userHooks lists the hooks of hooks.json in CODEX_HOME with their trust.
func userHooks(cfg map[string]any) ([]map[string]any, []string) {
	hooks, warnings := []map[string]any{}, []string{}
	path := hooksPath()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return hooks, warnings
	}
	var file struct {
		Hooks map[string][]struct {
			Matcher *string          `json:"matcher"`
			Hooks   []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err == nil {
		err = json.Unmarshal(b, &file)
	}
	if err != nil {
		return hooks, append(warnings, fmt.Sprintf("failed to parse hooks config %s: %v", path, err))
	}
	order := slices.Clone(events)
	var other []string
	for e := range file.Hooks {
		if !slices.Contains(events, e) {
			other = append(other, e)
		}
	}
	slices.Sort(other)
	order = append(order, other...)
	display := 0
	for _, event := range order {
		for gi, group := range file.Hooks[event] {
			for hi, h := range group.Hooks {
				key := fmt.Sprintf("%s:%s:%d:%d", path, snake(event), gi, hi)
				canon, _ := json.Marshal(map[string]any{"event": snake(event), "matcher": group.Matcher, "handler": h})
				sum := sha256.Sum256(canon)
				hash := "sha256:" + hex.EncodeToString(sum[:])
				trust := "untrusted"
				enabled := true
				if st, ok := toml.At(cfg, "hooks", "state", key); ok {
					if m, ok := st.(map[string]any); ok {
						if th, ok := m["trusted_hash"].(string); ok {
							trust = "modified"
							if th == hash {
								trust = "trusted"
							}
						}
						if e, ok := m["enabled"].(bool); ok {
							enabled = e
						}
					}
				}
				timeout := any(600)
				if t, ok := h["timeout"]; ok {
					timeout = t
				}
				handlerType, _ := h["type"].(string)
				if handlerType == "" {
					handlerType = "command"
				}
				entry := map[string]any{
					"key": key, "eventName": camel(event), "handlerType": handlerType,
					"command": h["command"], "async": h["async"] == true,
					"matcher": group.Matcher, "timeoutSec": timeout, "statusMessage": h["statusMessage"],
					"additionalContextLimit": h["additionalContextLimit"], "sourcePath": path, "source": "user",
					"pluginId": nil, "displayOrder": display, "enabled": enabled, "isManaged": false,
					"currentHash": hash, "trustStatus": trust,
				}
				hooks = append(hooks, entry)
				display++
			}
		}
	}
	return hooks, warnings
}
