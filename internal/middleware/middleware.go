// Package middleware provides HTTP middleware for the Pi-hole MCP server's
// HTTP and SSE transports. Middleware is applied in the order it is listed
// (outermost first), so the first entry wraps every subsequent one.
package middleware

import "net/http"

// Middleware is the canonical HTTP middleware shape: it takes the next
// handler and returns a wrapping handler.
type Middleware func(http.Handler) http.Handler

// Chain composes the supplied middleware into a single function that wraps
// any http.Handler. The first middleware passed is the outermost wrapper;
// requests pass through it first and responses through it last.
func Chain(middlewares ...Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}
