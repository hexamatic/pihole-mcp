package tools

import (
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// The catalogue records every tool as registered, in registration order,
// so cmd/toolsdoc can generate docs/TOOLS.md from the same definitions the
// server actually serves — a hand-maintained tool list cannot drift, a
// recorded one cannot lie.
var (
	catalogueMu sync.Mutex
	catalogue   []mcp.Tool
)

// recordTool appends a registered tool to the catalogue.
func recordTool(tool mcp.Tool) {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	catalogue = append(catalogue, tool)
}

// Catalogue returns a snapshot of every tool registered so far, in
// registration order.
func Catalogue() []mcp.Tool {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	out := make([]mcp.Tool, len(catalogue))
	copy(out, catalogue)
	return out
}

// ResetCatalogue clears the catalogue. Used by cmd/toolsdoc between
// registration passes; a running server never calls it.
func ResetCatalogue() {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	catalogue = nil
}
