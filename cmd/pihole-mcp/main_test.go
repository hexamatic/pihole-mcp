package main

import (
	"strings"
	"testing"
)

func TestRun_MissingConfig(t *testing.T) {
	t.Setenv("PIHOLE_URL", "")
	t.Setenv("PIHOLE_PASSWORD", "")

	err := run("stdio", "localhost:0")
	if err == nil {
		t.Fatal("expected configuration error with empty PIHOLE_URL")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("err = %v, want configuration error", err)
	}
}

func TestRun_UnknownTransport(t *testing.T) {
	t.Setenv("PIHOLE_URL", "http://pihole.invalid")
	t.Setenv("PIHOLE_PASSWORD", "x")

	err := run("carrier-pigeon", "localhost:0")
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
	if !strings.Contains(err.Error(), "unknown transport") ||
		!strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("err = %v, want unknown-transport error naming the input", err)
	}
}

func TestRun_InvalidTimezoneStillStarts(t *testing.T) {
	// An unloadable TZ must not prevent startup (it warns and falls back);
	// the unknown transport proves run() got past config and TZ resolution.
	t.Setenv("PIHOLE_URL", "http://pihole.invalid")
	t.Setenv("PIHOLE_PASSWORD", "x")
	t.Setenv("TZ", "Not/AZone")

	err := run("bogus", "localhost:0")
	if err == nil || !strings.Contains(err.Error(), "unknown transport") {
		t.Errorf("err = %v, want unknown-transport (i.e. TZ handled non-fatally)", err)
	}
}
