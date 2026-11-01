package server

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callThrough(t *testing.T, limit int, result *mcp.CallToolResult) *mcp.CallToolResult {
	t.Helper()
	handler := resultSizeLimit(limit)(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
