//go:build !slim

package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestInit_DisabledWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	tp, err := Init("pihole-mcp-test", "0.0.0")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if tp != nil {
		t.Fatal("Init must return a nil provider when no endpoint is configured")
	}
}

func TestInit_EnabledWithEndpoint(t *testing.T) {
	// The OTLP HTTP exporter connects lazily, so an unreachable endpoint
	// still yields a working provider.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	tp, err := Init("pihole-mcp-test", "0.0.0")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if tp == nil {
		t.Fatal("Init must return a provider when the endpoint is set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Shutdown flushes to the unreachable endpoint; the deadline error (or a
	// clean nil on fast failure) is acceptable — what matters is no panic and
	// a bounded return.
	_ = tp.Shutdown(ctx)
}
