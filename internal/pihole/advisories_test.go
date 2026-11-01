package pihole

import (
	"strings"
	"testing"
)

func TestParseFTLVersion(t *testing.T) {
	for in, want := range map[string]FTLVersion{"v6.7.1": {6, 7, 1}, "v6.8": {6, 8, 0}, "6.5": {6, 5, 0}} {
		if got, ok := ParseFTLVersion(in); !ok || got != want {
			t.Errorf("ParseFTLVersion(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"vDev-1a2b3c4", "", "v6", "v6.x"} {
		if _, ok := ParseFTLVersion(in); ok {
			t.Errorf("ParseFTLVersion(%q) should not parse", in)
		}
	}
}

func TestAdvisoriesFor(t *testing.T) {
	// v6.8 fixes every advisory published to date; v6.7.1 misses the eight
	// fixed in v6.8; v6.7 misses four more fixed in v6.7.1.
	for v, want := range map[FTLVersion]int{{6, 8, 0}: 0, {6, 7, 1}: 8, {6, 7, 0}: 12, {6, 0, 0}: len(ftlAdvisories)} {
		if got := len(AdvisoriesFor(v)); got != want {
			t.Errorf("AdvisoriesFor(%v) = %d, want %d", v, got, want)
		}
	}
}

func TestAdvisoryWarning(t *testing.T) {
	w := AdvisoryWarning("v6.7")
	for _, want := range []string{"FTL v6.7 is affected by 12 published security advisories", "the worst rated high", "FTL v6.8 fixes them"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q lacks %q", w, want)
		}
	}
	if w := AdvisoryWarning("v6.8"); w != "" {
		t.Errorf("v6.8 should have no warning, got %q", w)
	}
	if w := AdvisoryWarning("vDev-abc"); w != "" {
		t.Errorf("a development build cannot be judged, got %q", w)
	}
}
