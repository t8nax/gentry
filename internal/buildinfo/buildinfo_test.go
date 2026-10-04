package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestDevVersion(t *testing.T) {
	settings := func(kv ...string) *debug.BuildInfo {
		info := &debug.BuildInfo{}
		for i := 0; i < len(kv); i += 2 {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return info
	}

	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "dev"},
		{"no revision", settings(), true, "dev"},
		{"revision", settings("vcs.revision", "0123456789abcdef", "vcs.modified", "false"), true, "dev+0123456789ab"},
		{"modified", settings("vcs.revision", "0123456789abcdef", "vcs.modified", "true"), true, "dev+0123456789ab.dirty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := devVersion(tt.info, tt.ok); got != tt.want {
				t.Errorf("devVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
