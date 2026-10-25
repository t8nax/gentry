// Package setup connects an AI tool and, after a plugin change, lays out the
// subagents of every project. The entry supplies the adapters and translates
// failures and the result into output; the scenario prints nothing.
package setup

import "github.com/t8nax/gentry/contract"

// Projects supplies the projects of the current data root and their layouts.
// List only reads the state store and does not create an absent one. Deploy
// skips conflicting files, and reports other failures without rolling back.
type Projects interface {
	List() ([]string, error)
	Deploy(project string) (contract.UpdateAgents, error)
}

// Result preserves the successful setup and layouts even if another project
// fails, or if the projects cannot be listed.
type Result struct {
	Setup    contract.SetupOutput
	Projects []ProjectFailure
	ListErr  error
}

// ProjectFailure is a project whose subagents could not be laid out.
type ProjectFailure struct {
	Project string
	Err     error
}

// Changed reports whether the plugin was installed, updated or switched,
// including a move reported through previous_dir. Settings alone do not
// trigger a layout.
func Changed(out contract.SetupOutput) bool {
	return out.Action != "unchanged" || out.PreviousDir != nil
}

// Run connects the tool before reading any projects. A setup failure stops
// the scenario; a project failure leaves the setup and other layouts intact.
func Run(connect func() (contract.SetupOutput, error), projects Projects) (*Result, error) {
	out, err := connect()
	if err != nil {
		return nil, err
	}
	r := &Result{Setup: out}
	if !Changed(out) {
		return r, nil
	}
	ids, err := projects.List()
	if err != nil {
		r.ListErr = err
		return r, nil
	}
	for _, id := range ids {
		a, err := projects.Deploy(id)
		if err != nil {
			r.Projects = append(r.Projects, ProjectFailure{Project: id, Err: err})
			continue
		}
		r.Setup.Agents = append(r.Setup.Agents, a)
	}
	return r, nil
}
