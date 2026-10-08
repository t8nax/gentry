package cli

import (
	"fmt"
	"strings"

	"github.com/t8nax/gentry/internal/msg"
)

// The text output follows principle 12 of the technical solution: a list of
// plain values goes under a heading, one value per line; a list of objects
// with several properties is a table with a header row.

// tableGap separates the columns of a table.
const tableGap = 2

// writeList prints heading and then each item on its own line, indented.
func writeList(b *strings.Builder, heading string, items []string) {
	fmt.Fprintln(b, heading)
	for _, it := range items {
		fmt.Fprintf(b, "  %s\n", it)
	}
}

// writeTable prints rows, the header row first, as aligned columns. The last
// column is not padded, so lines have no trailing spaces.
func writeTable(b *strings.Builder, rows [][]string) {
	var widths []int
	for _, r := range rows {
		for i, cell := range r {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}
	for _, r := range rows {
		for i, cell := range r {
			if i == len(r)-1 {
				b.WriteString(cell)
				break
			}
			// fmt pads by runes, so Cyrillic headers line up.
			fmt.Fprintf(b, "%-*s", widths[i]+tableGap, cell)
		}
		b.WriteString("\n")
	}
}

// orNone returns s, or the mark of a missing value if s is empty.
func orNone(s string) string {
	if s == "" {
		return msg.Text(msg.ValueNone)
	}
	return s
}
