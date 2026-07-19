package config

import (
	"strings"
	"testing"
	"time"

	// Embed the IANA timezone database so TZ resolution tests pass on
	// runners without system zoneinfo.
	_ "time/tzdata"
)

func TestTimeLocation_Valid(t *testing.T) {
	t.Setenv("TZ", "Australia/Adelaide")
	loc, err := TimeLocation()
	if err != nil {
		t.Fatalf("TimeLocation() error = %v", err)
	}
	if loc.String() != "Australia/Adelaide" {
		t.Errorf("TimeLocation() = %q, want %q", loc, "Australia/Adelaide")
	}
}

func TestTimeLocation_LeadingColon(t *testing.T) {
	t.Setenv("TZ", ":Australia/Adelaide")
	loc, err := TimeLocation()
	if err != nil {
		t.Fatalf("TimeLocation() error = %v", err)
	}
	if loc.String() != "Australia/Adelaide" {
		t.Errorf("TimeLocation() = %q, want %q", loc, "Australia/Adelaide")
	}
}

func TestTimeLocation_Empty(t *testing.T) {
	t.Setenv("TZ", "")
	loc, err := TimeLocation()
	if err != nil {
		t.Fatalf("TimeLocation() error = %v", err)
	}
	if loc != time.Local {
		t.Errorf("TimeLocation() = %q, want time.Local", loc)
	}
}

func TestTimeLocation_Invalid(t *testing.T) {
	t.Setenv("TZ", "Not/AZone")
	loc, err := TimeLocation()
	if err == nil {
		t.Fatal("TimeLocation() error = nil, want error for invalid zone")
	}
	if !strings.Contains(err.Error(), "Not/AZone") {
		t.Errorf("error %q does not name the invalid zone", err)
	}
	if loc != time.Local {
		t.Errorf("TimeLocation() fallback = %q, want time.Local", loc)
	}
}
