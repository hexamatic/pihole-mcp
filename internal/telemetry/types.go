// Package telemetry exposes a transport-independent Provider interface so
// that callers can hold an OTel TracerProvider (in default builds) or a nil
// value (in slim builds without OTel deps compiled in) behind the same shape.
package telemetry

import "context"

// Provider is the minimal interface main.go needs to clean up a tracer
// provider on shutdown. It matches *sdktrace.TracerProvider.Shutdown without
// pulling in any OTel symbols.
type Provider interface {
	Shutdown(ctx context.Context) error
}
