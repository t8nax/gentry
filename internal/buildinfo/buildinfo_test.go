package buildinfo

import "testing"

func TestFromModule(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "0.0.0-dev"},
		{"(devel)", "0.0.0-dev"},
		{"v0.1.0", "0.1.0"},
		{"v0.0.0-20261004175847-74a0120d2d39", "0.0.0-20261004175847-74a0120d2d39"},
		{"v0.1.1-0.20261004175847-74a0120d2d39+dirty", "0.1.1-0.20261004175847-74a0120d2d39+dirty"},
	}
	for _, tt := range tests {
		if got := fromModule(tt.in); got != tt.want {
			t.Errorf("fromModule(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
