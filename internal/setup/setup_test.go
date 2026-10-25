package setup

import (
	"errors"
	"reflect"
	"testing"

	"github.com/t8nax/gentry/contract"
)

type projectStub struct {
	calls   *[]string
	ids     []string
	listErr error
	fail    map[string]error
}

func (p projectStub) List() ([]string, error) {
	*p.calls = append(*p.calls, "list")
	return p.ids, p.listErr
}

func (p projectStub) Deploy(id string) (contract.UpdateAgents, error) {
	*p.calls = append(*p.calls, id)
	return contract.UpdateAgents{Project: id, Worktrees: []contract.AgentsWorktree{}}, p.fail[id]
}

func TestRun(t *testing.T) {
	broken := errors.New("broken")
	previous := "/previous"
	for _, tt := range []struct {
		name, action        string
		previous            *string
		connectErr, listErr error
		ids                 []string
		fail                map[string]error
		calls               []string
		success             []string
		failed              []string
	}{
		{name: "installed", action: "installed", ids: []string{"admin", "shop"}, calls: []string{"connect", "list", "admin", "shop"}, success: []string{"admin", "shop"}},
		{name: "updated", action: "updated", ids: []string{"shop"}, calls: []string{"connect", "list", "shop"}, success: []string{"shop"}},
		{name: "switched", action: "switched", previous: &previous, ids: []string{"shop"}, calls: []string{"connect", "list", "shop"}, success: []string{"shop"}},
		{name: "previous directory", action: "unchanged", previous: &previous, ids: []string{"shop"}, calls: []string{"connect", "list", "shop"}, success: []string{"shop"}},
		{name: "unchanged", action: "unchanged", listErr: broken, calls: []string{"connect"}},
		{name: "setup refused", connectErr: broken, calls: []string{"connect"}},
		{name: "no projects", action: "installed", calls: []string{"connect", "list"}},
		{name: "list failed", action: "updated", listErr: broken, calls: []string{"connect", "list"}},
		{name: "continue", action: "updated", ids: []string{"admin", "shop"}, fail: map[string]error{"admin": broken}, calls: []string{"connect", "list", "admin", "shop"}, success: []string{"shop"}, failed: []string{"admin"}},
		{name: "all failed", action: "installed", ids: []string{"admin", "shop"}, fail: map[string]error{"admin": broken, "shop": broken}, calls: []string{"connect", "list", "admin", "shop"}, failed: []string{"admin", "shop"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			out := contract.SetupOutput{Tool: "tool", Action: tt.action, PreviousDir: tt.previous, Enabled: false}
			r, err := Run(func() (contract.SetupOutput, error) {
				calls = append(calls, "connect")
				return out, tt.connectErr
			}, projectStub{calls: &calls, ids: tt.ids, listErr: tt.listErr, fail: tt.fail})
			if !reflect.DeepEqual(calls, tt.calls) || !errors.Is(err, tt.connectErr) {
				t.Fatalf("calls=%v, err=%v; want %v, %v", calls, err, tt.calls, tt.connectErr)
			}
			if tt.connectErr != nil {
				if r != nil {
					t.Errorf("failed setup has partial result: %+v", r)
				}
				return
			}
			var successful, failed []string
			for _, a := range r.Setup.Agents {
				successful = append(successful, a.Project)
				if a.Changed {
					t.Errorf("unchanged layout marked changed: %+v", a)
				}
			}
			for _, f := range r.Projects {
				failed = append(failed, f.Project)
				if !errors.Is(f.Err, broken) {
					t.Errorf("failure lost cause: %+v", f)
				}
			}
			if !reflect.DeepEqual(successful, tt.success) || !reflect.DeepEqual(failed, tt.failed) {
				t.Errorf("success=%v, failed=%v; want %v, %v", successful, failed, tt.success, tt.failed)
			}
			if tt.action != "unchanged" && !errors.Is(r.ListErr, tt.listErr) {
				t.Errorf("list error=%v, want %v", r.ListErr, tt.listErr)
			}
			out.Agents = r.Setup.Agents
			if !reflect.DeepEqual(r.Setup, out) {
				t.Errorf("setup lost fields: %+v, want %+v", r.Setup, out)
			}
		})
	}
}
