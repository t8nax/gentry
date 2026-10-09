package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// artifactFields are the fields of artifact save; the name is the argument.
var artifactFields = textFields("name", "file", "url")

func runArtifactSave(args []string, env Env) int {
	const cmd = "artifact save"
	f := newFlags(cmd)
	flags := map[string]*stringFlag{"file": f.String("file"), "url": f.String("url")}
	key := f.String("task")
	input := f.String("input")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	values, bad := commandFields(cmd, input, flags, f.args, env.Stdin, artifactFields)
	if bad != nil {
		return fail(env, *bad)
	}
	name, file, u := text(values, "name"), text(values, "file"), text(values, "url")
	if !input.Set && len(f.args) > 0 {
		name = f.args[0]
	}
	help := msg.Text(msg.HintCommandHelp, cmd)
	switch {
	case name == "":
		return fail(env, missingField(cmd, "name", msg.Text(msg.ErrArtifactNameMissing), help))
	case !task.ValidArtifactName(name):
		return fail(env, fieldInvalid("name", "invalid_name", msg.Text(msg.ErrArtifactNameInvalid, name), msg.Text(msg.HintArtifactName)))
	case file != "" && u != "":
		return fail(env, conflictingFlags(cmd, []string{"--file", "--url"}))
	case file == "" && u == "":
		return fail(env, missingField(cmd, "file", msg.Text(msg.ErrArtifactSourceMissing), help))
	case u != "" && !task.ValidURL(u):
		return fail(env, fieldInvalid("url", "invalid_url", msg.Text(msg.ErrArtifactURLInvalid, u), msg.Text(msg.HintArtifactURL)))
	}
	if file != "" && !filepath.IsAbs(file) {
		wd, err := os.Getwd()
		if err != nil {
			return fail(env, internal(err))
		}
		file = filepath.Join(wd, file)
	}
	w, bad := openWayTask(key, false)
	if bad != nil {
		return fail(env, *bad)
	}
	defer w.close()
	a, replaced, err := task.SaveArtifact(w.st, w.task, name, file, u, source(env))
	if err != nil {
		f := wayFailure(cmd, err)
		if f.code == contract.CodeFieldInvalid {
			// The path as given, not as resolved.
			f.message = strings.Replace(f.message, file, text(values, "file"), 1)
		}
		return w.fail(env, f)
	}
	path, _ := task.ArtifactPath(w.task, name)
	if *asJSON {
		if err := writeJSON(env, contract.ArtifactSaveOutput{Task: w.task.Key(), Artifact: artifactJSON(a, path), Replaced: replaced}); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	var b strings.Builder
	k := msg.ArtifactSaved
	if replaced {
		k = msg.ArtifactReplaced
	}
	fmt.Fprintln(&b, msg.Text(k, name))
	if a.Kind == state.ArtifactFile {
		fmt.Fprintln(&b, msg.Text(msg.ArtifactFileLine, path))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.ArtifactURLLine, a.URL))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// artifactJSON returns an artifact as the contract has it; path is where
// the copy of a file lies.
func artifactJSON(a state.Artifact, path string) contract.TaskArtifact {
	ca := contract.TaskArtifact{Name: a.Name, Kind: contract.TaskArtifactKind(a.Kind),
		Source: contract.TaskArtifactSource(a.Source), Saved: a.Saved}
	if a.Kind == state.ArtifactFile {
		ca.Path = &path
	} else {
		u := a.URL
		ca.Url = &u
	}
	return ca
}
