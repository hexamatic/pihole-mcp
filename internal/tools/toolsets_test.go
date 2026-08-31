package tools

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/toolsets"
	"github.com/mark3labs/mcp-go/server"
)

// TestEveryRegisteredToolHasAToolset is the guard that keeps PIHOLE_TOOLSETS
// honest as the server grows. Toolset membership is derived from a tool's
// family token, so a new registration group whose token nobody added to the
// table would leave its tools reachable by no toolset at all: invisible to
// anyone who scopes, and silently so. This fails the moment that happens.
func TestEveryRegisteredToolHasAToolset(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.0")
	ResetCatalogue()
	RegisterAll(srv, dummyRegistry(2))

	catalogue := Catalogue()
	if len(catalogue) == 0 {
		t.Fatal("no tools registered")
	}

	for _, tool := range catalogue {
		if _, ok := toolsets.Of(tool.Name); !ok {
			t.Errorf("%s belongs to no toolset; add its token %q to internal/toolsets",
				tool.Name, toolsets.Token(tool.Name))
		}
	}
}
