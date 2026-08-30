package server

import (
	"runtime/debug"
	"testing"
)

// TestResolveVersion covers the two install routes that report a version.
//
// A release binary carries the version as an ldflag. A binary from
// `go install ...@latest` carries no ldflags at all, so it used to report "dev"
// to every user who installed that way, which is exactly the group least able
// to work out which build they are running from a bug report.
func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name   string
		ldflag string
		info   *debug.BuildInfo
		want   string
	}{
		{
			name:   "ldflag wins over build info",
			ldflag: "0.9.0",
			info:   buildInfo("v0.8.1"),
			want:   "0.9.0",
		},
		{
			name:   "go install falls back to the module version",
			ldflag: "dev",
			info:   buildInfo("v0.9.0"),
			want:   "0.9.0",
		},
		{
			name:   "a working-tree build stays dev",
			ldflag: "dev",
			info:   buildInfo("(devel)"),
			want:   "dev",
		},
		{
			name:   "no build info stays dev",
			ldflag: "dev",
			info:   nil,
			want:   "dev",
		},
		{
			name:   "empty module version stays dev",
			ldflag: "dev",
			info:   buildInfo(""),
			want:   "dev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.ldflag, tc.info); got != tc.want {
				t.Errorf("resolveVersion(%q, %v) = %q, want %q", tc.ldflag, tc.info, got, tc.want)
			}
		})
	}
}

func buildInfo(version string) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: "github.com/hexamatic/pihole-mcp", Version: version}}
}
