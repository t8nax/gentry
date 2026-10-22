package integration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Fingerprint is a short hash of the files of a plugin by their
// slash-separated paths: a version of the plugin that changes whenever its
// content does, as the AI tools update an installed plugin only when its
// version changes.
func Fingerprint(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(files[p]))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// WriteDir replaces dir with the files by their slash-separated paths. The
// files are written to a sibling directory first, so a failure midway
// leaves the old plugin intact.
func WriteDir(dir string, files map[string][]byte) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	rand.Read(suffix)
	tmp := dir + ".tmp-" + hex.EncodeToString(suffix)
	if err := WriteFiles(tmp, files); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// WriteFiles writes the files by their slash-separated paths in dir.
func WriteFiles(dir string, files map[string][]byte) error {
	for path, b := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
