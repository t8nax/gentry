package update_test

import (
	"strings"
	"testing"
)

// 7. Сверка программы и плагина в хуке: the hook of the program installed
// in the data root, which the stand has built.

// TestHookFullPath checks that the hint of the hook of the installed
// program names setup with the full path of the program, not a tool.
func TestHookFullPath(t *testing.T) {
	parallel(t)
	s := installedStand(t, latest, false)
	s.stalePlugin()
	r := s.hookIn(t.TempDir())
	wantCode(t, r, 0)
	want := pluginStaleHint + s.program() + " setup claude"
	if ls := lines(r.stdout); len(ls) < 2 || ls[1] != want {
		t.Errorf("hook: %s\nwant the second line %q", r, want)
	}
	if strings.Contains(r.stdout, "setup (tool: claude)") {
		t.Errorf("the hook names the tool setup:\n%s", r.stdout)
	}
}
