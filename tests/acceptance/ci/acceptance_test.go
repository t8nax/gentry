package ci_test

import (
	"slices"
	"testing"
)

// A newly added area is absent from any list in the workflow. Its call is
// in a helper.go file, not in the test function or even in a _test.go file.
func TestAcceptanceDiscoversNewArea(t *testing.T) {
	for _, runner := range []string{"Linux", "Windows", "macOS"} {
		t.Run(runner, func(t *testing.T) {
			s := acceptanceStand(t, runner, "install", "update", "release", "probe", "ordinary")
			s.processExit, s.failedArea = 23, "probe"
			code, calls := s.run(t)
			s.wantPasses(t, calls, wantedAreas(runner, "probe"))
			if code == 0 {
				t.Error("CI reported success although the new area fails in the process pass")
			}
		})
	}
}

// This is existing behavior: the wildcard pass covers ordinary areas, and
// only the areas that skip it receive a process pass, with release on Linux.
func TestAcceptancePassesAndEnvironment(t *testing.T) {
	for _, runner := range []string{"Linux", "Windows", "macOS"} {
		t.Run(runner, func(t *testing.T) {
			s := acceptanceStand(t, runner, "install", "update", "release", "ordinary")
			code, calls := s.run(t)
			s.wantPasses(t, calls, wantedAreas(runner))
			if code != 0 {
				t.Errorf("successful passes exited with %d", code)
			}
		})
	}
}

func TestAcceptancePassFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		binaryExit  int
		processExit int
	}{
		{"first", 19, 0},
		{"second", 0, 23},
		{"both", 19, 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := acceptanceStand(t, "Linux", "install", "update", "release", "ordinary")
			s.binaryExit, s.processExit = tc.binaryExit, tc.processExit
			code, calls := s.run(t)
			s.wantPasses(t, calls, wantedAreas("Linux"))
			if code == 0 {
				t.Errorf("a failed pass (%d, %d) became a successful step", tc.binaryExit, tc.processExit)
			}
		})
	}
}

func TestAcceptanceEmptyProcessSet(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runner string
		areas  []string
	}{
		{"no_calls", "Linux", []string{"ordinary"}},
		{"release_on_windows", "Windows", []string{"ordinary", "release"}},
		{"release_on_macos", "macOS", []string{"ordinary", "release"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := acceptanceStand(t, tc.runner, tc.areas...)
			code, calls := s.run(t)
			s.wantPasses(t, calls, nil)
			if code != 0 {
				t.Errorf("an empty process set exited with %d", code)
			}
		})
	}
}

func TestAcceptanceDiscoveryFailure(t *testing.T) {
	// The required search root is missing, rather than an existing directory
	// with no calls. This must not be mistaken for a successful empty search.
	s := acceptanceStand(t, "Linux")
	code, _ := s.run(t)
	if code == 0 {
		t.Error("CI reported success with the acceptance search directory missing")
	}
}

func wantedAreas(runner string, extra ...string) []string {
	areas := append([]string{"install", "update"}, extra...)
	if runner == "Linux" {
		areas = append(areas, "release")
	}
	slices.Sort(areas)
	return areas
}
