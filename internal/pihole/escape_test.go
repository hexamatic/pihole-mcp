package pihole_test

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
)

// The characters below were settled against a live Pi-hole (FTL v6.7) rather
// than read off the RFC, because the RFC and FTL disagree about '+'.
func TestEscapePathSegment(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{"plain domain is untouched", "ads.example.com", "ads.example.com"},
		{"plus becomes %2B", "^ads[0-9]+\\.example\\.com", "%5Eads%5B0-9%5D%2B%5C.example%5C.com"},
		{"hash becomes %23", "127.0.0.1#5335", "127.0.0.1%235335"},
		// A colon and an equals sign are legal inside a path segment and stay
		// literal; both spellings reach the same row on FTL v6.7.
		{"question mark becomes %3F", "https://l.example.com/a.txt?t=1", "https:%2F%2Fl.example.com%2Fa.txt%3Ft=1"},
		{"slash becomes %2F", "a/b", "a%2Fb"},
		{"space becomes %20", "192.168.1.2 pi.hole", "192.168.1.2%20pi.hole"},
		{"empty stays empty", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := pihole.EscapePathSegment(tt.in); got != tt.want {
				t.Errorf("EscapePathSegment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
