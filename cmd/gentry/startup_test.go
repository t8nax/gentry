package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// buildGentry builds the gentry binary with release flags into a temporary
// directory and returns its path.
func buildGentry(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gentry")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// measure runs the binary with args warmup times unmeasured, then runs times
// measured, and returns the sorted durations.
func measure(t *testing.T, bin string, warmup, runs int, args ...string) []time.Duration {
	t.Helper()
	run := func() time.Duration {
		start := time.Now()
		if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", bin, args, err, out)
		}
		return time.Since(start)
	}
	for range warmup {
		run()
	}
	durations := make([]time.Duration, runs)
	for i := range durations {
		durations[i] = run()
	}
	slices.Sort(durations)
	return durations
}

// median and p95 expect sorted durations.
func median(d []time.Duration) time.Duration {
	n := len(d)
	if n%2 == 1 {
		return d[n/2]
	}
	return (d[n/2-1] + d[n/2]) / 2
}

func p95(d []time.Duration) time.Duration {
	i := (len(d)*95 + 99) / 100 // ceil(0.95 * n)
	return d[i-1]
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000)
}

// writeSummary appends markdown to the GitHub Actions job summary, if any.
func writeSummary(t *testing.T, markdown string) {
	t.Helper()
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(markdown); err != nil {
		t.Fatal(err)
	}
}

// TestStartupTime reports how long `gentry version` takes to run. It has no
// threshold: it gives numbers to compare across platforms and over time.
func TestStartupTime(t *testing.T) {
	if testing.Short() {
		t.Skip("startup measurement is skipped in short mode")
	}
	const warmup, runs = 3, 30
	d := measure(t, buildGentry(t), warmup, runs, "version")
	platform := runtime.GOOS + "/" + runtime.GOARCH
	t.Logf("gentry version on %s, %d runs: median %s, p95 %s", platform, runs, ms(median(d)), ms(p95(d)))
	writeSummary(t, fmt.Sprintf(
		"### Startup time of `gentry version`, %s\n\n| Runs | Median | p95 |\n| --- | --- | --- |\n| %d | %s | %s |\n\n",
		platform, runs, ms(median(d)), ms(p95(d))))
}

func TestStats(t *testing.T) {
	d := []time.Duration{1, 2, 3, 4}
	if got := median(d); got != 2 { // (2+3)/2 truncated
		t.Errorf("median = %d, want 2", got)
	}
	if got := median(d[:3]); got != 2 {
		t.Errorf("median = %d, want 2", got)
	}
	d = make([]time.Duration, 30)
	for i := range d {
		d[i] = time.Duration(i + 1)
	}
	if got := p95(d); got != 29 {
		t.Errorf("p95 = %d, want 29", got)
	}
}
