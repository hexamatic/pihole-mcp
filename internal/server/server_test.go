package server

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
)

func newRegistry(t *testing.T, n int) *pihole.Registry {
	t.Helper()
	instances := make([]pihole.InstanceConfig, n)
	for i := range instances {
		instances[i] = pihole.InstanceConfig{
			Name:     map[int]string{0: "primary", 1: "secondary"}[i],
			URL:      "http://pihole.invalid",
			Password: "x",
		}
	}
	r := pihole.NewRegistry(instances)
	t.Cleanup(r.Close)
	return r
}

func TestNew_SingleInstance(t *testing.T) {
	if s := New(newRegistry(t, 1)); s == nil {
		t.Fatal("New returned nil server")
	}
}

func TestNew_MultiInstance(t *testing.T) {
	// Multi-instance wiring registers the sync tools, per-instance resources,
	// and the widened output schemas; a regression here bricks the binary.
	if s := New(newRegistry(t, 2)); s == nil {
		t.Fatal("New returned nil server")
	}
}

func TestVersionDefault(t *testing.T) {
	if Version == "" {
		t.Error("Version must have a non-empty default (ldflags overwrite it at release)")
	}
}
