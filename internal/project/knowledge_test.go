package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t8nax/gentry/internal/msg"
)

func TestInfoRoundTrip(t *testing.T) {
	for _, lang := range msg.KnowledgeLanguages {
		dir := t.TempDir()
		want := Info{Format: KnowledgeFormat, Project: "my-shop", Language: lang}
		if err := WriteInfo(dir, want); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, InfoFile))
		if !strings.HasPrefix(string(b), "# "+msg.Knowledge(lang, msg.YAMLHeader)+"\n") {
			t.Errorf("%s: comments not in the language of the knowledge:\n%s", lang, b)
		}
		got, err := ReadInfo(dir)
		if err != nil || got != want {
			t.Errorf("%s: read %+v, %v; want %+v", lang, got, err, want)
		}
	}
}

func TestReadInfo(t *testing.T) {
	tests := []struct {
		content string
		cause   string // empty for a newer format
	}{
		{"format: [1\n", msg.Text(msg.CauseInfoSyntax)},
		{"", msg.Text(msg.CauseInfoSyntax)},
		{"project: shop\nlanguage: ru\n", msg.Text(msg.CauseInfoMissing, "format")},
		{"format: one\nproject: shop\nlanguage: ru\n", msg.Text(msg.CauseInfoValue, "format", "one")},
		{"format: 0\nproject: shop\nlanguage: ru\n", msg.Text(msg.CauseInfoValue, "format", 0)},
		{"format: 1\nproject: shop\nlanguage: ru\nprefix: SHOP\n", msg.Text(msg.CauseInfoUnknown, "prefix")},
		{"format: 1\nlanguage: ru\n", msg.Text(msg.CauseInfoMissing, "project")},
		{"format: 1\nproject: Shop\nlanguage: ru\n", msg.Text(msg.CauseInfoValue, "project", "Shop")},
		{"format: 1\nproject: shop\n", msg.Text(msg.CauseInfoMissing, "language")},
		{"format: 1\nproject: shop\nlanguage: de\n", msg.Text(msg.CauseInfoValue, "language", "de")},
		{"format: 7\nproject: shop\nfuture: yes\n", ""},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, InfoFile), []byte(tt.content), 0o644)
		_, err := ReadInfo(dir)
		var ie *InvalidInfoError
		var ne *NewerFormatError
		switch {
		case tt.cause == "":
			if !errors.As(err, &ne) || ne.Format != 7 || ne.Supported != KnowledgeFormat {
				t.Errorf("%q: got %v, want NewerFormatError", tt.content, err)
			}
		case !errors.As(err, &ie) || ie.Cause != tt.cause:
			t.Errorf("%q: got %v, want cause %q", tt.content, err, tt.cause)
		}
	}
	if _, err := ReadInfo(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: got %v", err)
	}
}
