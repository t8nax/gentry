package cli

import (
	"testing"

	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/state"
	"github.com/t8nax/gentry/internal/task"
)

// TestIntroStepsText checks the line of the steps of the current stage in the
// introduction of the session: dropped steps do not count. The other lines
// of the introduction are built from the state store; the acceptance tests
// of the hook state the words of the line of no task, in its channel, the
// agent's.
func TestIntroStepsText(t *testing.T) {
	pass := func(steps ...state.Step) []state.Pass {
		return []state.Pass{{Node: "branch", Stage: "branch", Round: 1, Steps: steps}}
	}
	tests := []struct {
		name string
		v    task.View
		want string
	}{
		{"no steps", task.View{Path: pass()}, "Шаги этапа: не заданы"},
		{"steps", task.View{Path: pass(
			state.Step{Number: 1, State: state.StepDone},
			state.Step{Number: 2, State: state.StepPlanned},
			state.Step{Number: 3, State: state.StepDropped},
		)}, "Шаги этапа: выполнено 1 из 2"},
		{"no current pass", task.View{}, "Шаги этапа: не заданы"},
	}
	for _, tt := range tests {
		if got := introSteps(tt.v); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestIntroCodexUntrustedText states the words of the warning of the hook of
// Codex in a worktree Codex does not trust; the acceptance tests of the hook
// check where it is, before the introduction.
func TestIntroCodexUntrustedText(t *testing.T) {
	want := "Рабочая копия не доверена в Codex: субагенты флоу в этой сессии недоступны.\nСообщить оператору."
	if got := msg.Text(msg.IntroCodexUntrusted); got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
