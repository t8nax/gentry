package agents

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	// A new file takes the place of the old one: a failure halfway leaves
	// the old one whole, with the lines of the operator.
	tmp, err := os.CreateTemp(filepath.Dir(file), "exclude-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(next)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	// The file keeps its permissions: CreateTemp makes it readable by the
	// owner alone, and a shared repository reads it by the group too.
	mode := fs.FileMode(0o644)
	if fi, serr := os.Stat(file); serr == nil {
		mode = fi.Mode().Perm()
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = rename(tmp.Name(), file)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// rename puts file from in the place of to. On Windows the place cannot be
// taken while another program, such as git, reads the file without sharing
// its deletion: it is tried again for a while.
func rename(from, to string) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || !errors.Is(err, fs.ErrPermission) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
