package cli

import (
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

// TestEventsRefusalText checks the refusal of a number of an event after
// which the journal is read.
func TestEventsRefusalText(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		wantRefusal(t, value, "events", []string{"--after", value}, lines(
			"Недопустимое значение флага --after: «"+value+"».",
			"",
			msg.Text(msg.HintEventsAfter),
		), "")
	}
}
