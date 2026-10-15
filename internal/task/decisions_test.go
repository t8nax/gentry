package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// TestAllowUnlimitedReturn checks that an allowance of one more return is
// refused at the merge, a stage of the operator, whose only return has no
// limit: to the node of that return as there is nothing to allow, to any
// other node with no returns to name, as only those with a limit can be
// allowed. Nothing refused is recorded.
func TestAllowUnlimitedReturn(t *testing.T) {
	t.Parallel()
	s := newActiveShop(t)
	feature, err := os.ReadFile(filepath.Join("..", "flow", "testdata", "process", "shop", "flow", "scenarios", "feature.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s.write(process.FlowOf("shop"), map[string]string{"scenarios/feature.yaml": strings.Replace(string(feature),
		"  merge:           { stage: merge, next: finish }\n",
		"  merge:\n    stage: merge\n    next:\n      - to: implementation\n        if: оператор назвал, что не так\n      - to: finish\n", 1)})
	s.apply(process.FlowOf("shop"))
	task := s.take()
	for _, to := range []string{"plan", "implementation", "review", "merge"} {
		if _, err := CloseStage(context.Background(), s.st, Close{Task: task.ID, Skip: true, Reason: "не нужен", To: to, Source: state.SourceOperator}); err != nil {
			t.Fatal(err)
		}
	}

	_, err = RecordDecision(s.st, Decision{Task: task.ID, Answer: "Да", AllowReturn: "implementation", Source: state.SourceOperator})
	var unlimited *ReturnUnlimitedError
	if !errors.As(err, &unlimited) || unlimited.Node != "merge" || unlimited.To != "implementation" {
		t.Errorf("an allowance of the return without a limit: %v", err)
	}
	_, err = RecordDecision(s.st, Decision{Task: task.ID, Answer: "Да", AllowReturn: "finish", Source: state.SourceOperator})
	var notFound *ReturnNotFoundError
	if !errors.As(err, &notFound) || notFound.Node != "merge" || notFound.To != "finish" || notFound.Returns == nil || len(notFound.Returns) != 0 {
		t.Errorf("an allowance of a transition forward: %#v", err)
	}
	if ds, err := s.st.Decisions(task.ID); err != nil || len(ds) != 0 {
		t.Errorf("decisions recorded: %v, %+v", err, ds)
	}
}
