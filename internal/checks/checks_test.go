package checks

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{"go test ./...", []string{"go", "test", "./..."}},
		{"  go\tvet  ./... ", []string{"go", "vet", "./..."}},
		{`python -c "import sys; print('stop')" "a b" c|d 'e"f' $HOME`,
			[]string{"python", "-c", "import sys; print('stop')", "a b", "c|d", `e"f`, "$HOME"}},
		{`go test -run="Total Sum" ./...`, []string{"go", "test", "-run=Total Sum", "./..."}},
		{`a"b c"'d e'f`, []string{"ab cd ef"}},
		{`sh -c "go test ./... | tee test.log" && > *`, []string{"sh", "-c", "go test ./... | tee test.log", "&&", ">", "*"}},
		{`echo "" ''`, []string{"echo", "", ""}},
		{`dir C:\Program\ Files`, []string{"dir", `C:\Program\`, "Files"}},
		{"", nil},
	}
	for _, tt := range tests {
		got, err := Split(tt.command)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("Split(%q) = %q, %v; want %q", tt.command, got, err, tt.want)
		}
	}
	for _, command := range []string{`go test -run "TestTotal ./...`, `echo 'it`, `a"b`} {
		var qe *QuoteError
		if _, err := Split(command); !errors.As(err, &qe) || qe.Command != command {
			t.Errorf("Split(%q): %v, want a quote not closed", command, err)
		}
	}
}

func TestParseTimeout(t *testing.T) {
	for s, want := range map[string]time.Duration{
		"90s": 90 * time.Second, "20m": 20 * time.Minute, "1h30m": 90 * time.Minute, "1h": time.Hour,
		"2m30s": 150 * time.Second, "10m": 10 * time.Minute, "0h1s": time.Second,
	} {
		if got, ok := ParseTimeout(s); !ok || got != want {
			t.Errorf("ParseTimeout(%q) = %v, %v; want %v", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "600", "0s", "0h0m", "1.5m", "500ms", "30m1h", "1d", "-5m", " 5m", "5 m", "99999999999999999999h"} {
		if got, ok := ParseTimeout(s); ok {
			t.Errorf("ParseTimeout(%q) = %v, want refused", s, got)
		}
	}
	if d, ok := ParseTimeout(DefaultTimeout); !ok || d != 10*time.Minute {
		t.Errorf("the default time %q is %v, %v", DefaultTimeout, d, ok)
	}
}
