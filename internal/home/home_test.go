package home

import (
	"path/filepath"
	"testing"
)

func TestRootFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvVar, dir)
	if got, err := Root(); err != nil || got != dir {
		t.Errorf("Root() = %q, %v; want %q", got, err, dir)
	}
}

func TestRootRelativeEnvIsMadeAbsolute(t *testing.T) {
	t.Setenv(EnvVar, "relative")
	got, err := Root()
	if err != nil || !filepath.IsAbs(got) || filepath.Base(got) != "relative" {
		t.Errorf("Root() = %q, %v; want an absolute path ending in relative", got, err)
	}
}

func TestRootDefault(t *testing.T) {
	user := t.TempDir()
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", user)
	t.Setenv("USERPROFILE", user)
	want := filepath.Join(user, ".gentry")
	if got, err := Root(); err != nil || got != want {
		t.Errorf("Root() = %q, %v; want %q", got, err, want)
	}
	wantIntegration := filepath.Join(want, "integrations", "claude")
	if got, err := Integration("claude"); err != nil || got != wantIntegration {
		t.Errorf("Integration() = %q, %v; want %q", got, err, wantIntegration)
	}
	if got, err := Process(); err != nil || got != filepath.Join(want, "process") {
		t.Errorf("Process() = %q, %v; want %q", got, err, filepath.Join(want, "process"))
	}
}
