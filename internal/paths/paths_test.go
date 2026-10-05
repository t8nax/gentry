package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonical(t *testing.T) {
	dir, err := Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got, err := Canonical(filepath.Join("a", "..", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "b", "c"); got != want {
		t.Errorf("missing path: got %q, want %q", got, want)
	}
	if got, _ := Canonical(strings.ReplaceAll(dir, `\`, "/")); got != dir {
		t.Errorf("forward slashes: got %q, want %q", got, dir)
	}
	if runtime.GOOS == "windows" {
		if got, _ := Canonical(strings.ToUpper(dir)); got != dir {
			t.Errorf("letter case: got %q, want %q", got, dir)
		}
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	if got, _ := Canonical(filepath.Join(link, "x")); got != filepath.Join(dir, "x") {
		t.Errorf("symbolic link: got %q", got)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "dev", "shop")
	tests := []struct {
		p    string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "src"), true},
		{root + "-knowledge", false},
		{filepath.Dir(root), false},
	}
	for _, tt := range tests {
		if got := Within(tt.p, root); got != tt.want {
			t.Errorf("Within(%q, %q) = %v, want %v", tt.p, root, got, tt.want)
		}
	}
	if caseless && !Within(strings.ToUpper(filepath.Join(root, "src")), root) {
		t.Error("case must not matter on this system")
	}
}
