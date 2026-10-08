package agents

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/t8nax/gentry/internal/msg"
)

// MarkPrefix starts the lines of Gentry in files it shares with others: the
// bounds of its block in info/exclude and the mark of a file of a subagent.
// The text after it is for the operator and may change.
const MarkPrefix = "# Gentry:"

// writeBlock makes the block of Gentry in the info/exclude file list
// patterns, in the line ends of the file; without patterns the block is
// removed. The lines outside the block are kept as they are.
func writeBlock(file string, patterns []string) error {
	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	eol := "\n"
	if bytes.Contains(old, []byte("\r\n")) {
		eol = "\r\n"
	}
	text := strings.ReplaceAll(string(old), "\r\n", "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	var kept []string
	in := false
	for _, line := range lines {
		if strings.HasPrefix(line, MarkPrefix) {
			in = !in
			continue
		}
		if !in {
			kept = append(kept, line)
		}
	}
	if len(patterns) > 0 {
		kept = append(kept, MarkPrefix+" "+msg.Text(msg.ExcludeBlockStart))
		kept = append(kept, patterns...)
		kept = append(kept, MarkPrefix+" "+msg.Text(msg.ExcludeBlockEnd))
	}
	next := ""
	if len(kept) > 0 {
		next = strings.Join(kept, eol) + eol
	}
	if next == string(old) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(next), 0o644)
}
