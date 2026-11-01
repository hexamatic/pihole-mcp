package server

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callThrough(t *testing.T, limit int, result *mcp.CallToolResult) *mcp.CallToolResult {
	t.Helper()
	return callThroughAs(t, limit, true, result)
}

// callThroughAs runs result through the middleware as the result of a tool
// that is read-only or not.
func callThroughAs(t *testing.T, limit int, readOnly bool, result *mcp.CallToolResult) *mcp.CallToolResult {
	t.Helper()
	isReadOnly := func(string) bool { return readOnly }
	handler := resultSizeLimit(limit, isReadOnly)(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result, nil
	})
	req := mcp.CallToolRequest{}
	req.Params.Name = "pihole_example"
	got, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned an error: %v", err)
	}
	return got
}

func TestResultSizeLimitPassesSmallResultsThrough(t *testing.T) {
	want := mcp.NewToolResultText("small")
	if got := callThrough(t, 1024, want); got != want {
		t.Errorf("a result under the limit was replaced: %#v", got)
	}
}

func TestResultSizeLimitReplacesLargeResultsWithGuidance(t *testing.T) {
	got := callThrough(t, 1024, mcp.NewToolResultText(strings.Repeat("x", 4096)))
	if !got.IsError {
		t.Fatal("a result over the limit was sent anyway")
	}
	text := got.Content[0].(mcp.TextContent).Text
	for _, want := range []string{"pihole_example", "limit and offset", "detail=minimal", "instance=all"} {
		if !strings.Contains(text, want) {
			t.Errorf("the replacement does not mention %q, so the model cannot act on it: %s", want, text)
		}
	}
}

// TestResultSizeLimitLeavesWriteToolsAlone pins why only reads are capped. The
// middleware runs after the handler, so a write tool has already changed
// Pi-hole when its result is measured; replacing the result would hide what
// changed, and for a sync, the path of the backup taken before it ran.
func TestResultSizeLimitLeavesWriteToolsAlone(t *testing.T) {
	want := mcp.NewToolResultText(strings.Repeat("x", 4096))
	if got := callThroughAs(t, 1024, false, want); got != want {
		t.Errorf("a write tool's result over the limit was replaced, hiding what it changed: %#v", got)
	}
}

// TestResultSizeLimitAppliesToReadToolsOnlyOverTheWire checks the real lookup
// the server wires in: with a limit every result exceeds, a read tool is
// replaced and a write tool on the same server is not.
func TestResultSizeLimitAppliesToReadToolsOnlyOverTheWire(t *testing.T) {
	srv := newScopedDispatchServer(t, []Option{withResultLimit(8)}, newDispatchFake(t))

	read := dispatchResult(t, dispatch(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{"name":"pihole_domains_list","arguments":{}}}`))
	if isErr, _ := read["isError"].(bool); !isErr {
		t.Errorf("a read tool's result over an 8-byte limit was sent: %#v", read)
	}

	write := dispatchResult(t, dispatch(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call",
		"params":{"name":"pihole_domains_add","arguments":{"domain":"cap.example.com","type":"deny","kind":"exact"}}}`))
	content, _ := write["content"].([]any)
	first, _ := content[0].(map[string]any)
	if text, _ := first["text"].(string); strings.Contains(text, "limit for a single result") {
		t.Errorf("a write tool's result was replaced by the size cap after it had changed Pi-hole: %q", text)
	}
}

// TestResultSizeLimitOnAggregatedSchemaTool is the case the SDK option did not
// cover: a tool that declares an output schema, called with instance=all so
// the result is the multi-instance aggregate. The replacement must reach the
// client as a tool error, not be rejected by output schema validation as a
// result that does not match the declared shape.
func TestResultSizeLimitOnAggregatedSchemaTool(t *testing.T) {
	srv := newScopedDispatchServer(t, []Option{withResultLimit(256)}, newDispatchFake(t), newDispatchFake(t))

	envelope := dispatch(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{"name":"pihole_domains_list","arguments":{"instance":"all"}}}`)
	res := dispatchResult(t, envelope)

	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("an aggregate result over a 256-byte limit was sent: %#v", res)
	}
	content, _ := res["content"].([]any)
	first, _ := content[0].(map[string]any)
	if text, _ := first["text"].(string); !strings.Contains(text, "instance=all") {
		t.Errorf("the replacement does not tell the model to narrow instance=all: %q", text)
	}
}

// TestDefaultResultLimitLeavesNormalCallsAlone guards the other direction: the
// production limit must not trip on an ordinary call.
func TestDefaultResultLimitLeavesNormalCallsAlone(t *testing.T) {
	srv := newScopedDispatchServer(t, nil, newDispatchFake(t), newDispatchFake(t))

	res := dispatchResult(t, dispatch(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{"name":"pihole_domains_list","arguments":{"instance":"all"}}}`))

	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("an ordinary call tripped the default result limit: %#v", res)
	}
}
