//go:build slim

// Slim build: telemetry is a no-op. Build with `-tags slim` to strip all
// OpenTelemetry dependencies (otel SDK, gRPC, protobuf, grpc-gateway,
// semconv) from the binary, dropping ~5-8MB at the cost of losing OTel
// support. The runtime contract is identical: Init returns (nil, nil),
// so callers don't need to know which build they got.
package telemetry

// Init is a no-op in slim builds. Returns (nil, nil) so caller logic that
// guards on `if tp != nil { defer tp.Shutdown(...) }` keeps working.
func Init(_, _ string) (Provider, error) {
	return nil, nil
}
