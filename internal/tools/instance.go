package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// instanceArg is the name of the optional tool argument that selects which
// configured Pi-hole a call targets.
const instanceArg = "instance"

// maxFanout bounds the number of instances queried concurrently for an
// instance=all aggregation. Fleets are small, so this is a generous ceiling
// that mainly guards against an unusually large configuration.
const maxFanout = 8

// getInstance resolves the Pi-hole client a request targets. An empty
// "instance" argument selects the default (first-declared) instance. The
// special value "all" is only meaningful to read-only tools and is handled by
// instanceAware before a handler runs, so reaching it here is an error.
func getInstance(req mcp.CallToolRequest, r *pihole.Registry) (*pihole.Client, error) {
	name := req.GetString(instanceArg, "")
	if name == "" {
		return r.Default(), nil
	}
	if name == "all" {
		return nil, fmt.Errorf("instance=all is only supported for read-only tools; target a single instance (one of: %s)", strings.Join(r.Names(), ", "))
	}
	return r.Get(name)
}

// instanceAware wraps a single-instance handler with multi-instance routing.
//
//   - When instance != "all", the handler runs unchanged; its getInstance call
//     resolves the named (or default) instance. In a multi-instance setup the
//     result text is prefixed with an "instance: <name>" line so the model
//     always knows which Pi-hole produced it.
//   - When instance == "all" on a read-only tool, the handler is invoked
//     concurrently once per configured instance. The aggregate is returned as
//     structured content (an AggregateOutput labelling every record with its
//     source instance, plus a success/failure summary) together with a
//     "### instance: <name>" text fallback for clients that ignore structured
//     content. Partial failures are surfaced per instance; the call only fails
//     wholesale when every instance fails.
//   - When instance == "all" on a state-changing tool, the call is rejected
//     before any API request is made.
//
// In a single-instance setup the wrapper is a passthrough.
func instanceAware(r *pihole.Registry, tool mcp.Tool, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	multi := r.Len() > 1
	readOnly := isReadOnly(tool)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		isAll := req.GetString(instanceArg, "") == "all"

		// Single-target path (also covers instance=all in a single-instance
		// setup, where the handler's getInstance rejects "all").
		if !isAll || !multi {
			res, err := h(ctx, req)
			if err == nil && multi && !isAll && res != nil && !res.IsError {
				res = withProvenance(res, instanceName(req, r))
			}
			return res, err
		}

		if !readOnly {
			return mcp.NewToolResultError("instance=all is not supported for tools that modify state; target a single instance by name"), nil
		}

		return aggregateAll(ctx, r, h, req), nil
	}
}

// aggregateAll fans a request out across every configured instance concurrently
// (bounded by maxFanout) and merges the results into a structured
// AggregateOutput with a text fallback. Results are written by position, so the
// output order is deterministic regardless of completion order.
func aggregateAll(ctx context.Context, r *pihole.Registry, h server.ToolHandlerFunc, req mcp.CallToolRequest) *mcp.CallToolResult {
	names := r.Names()
	results := make([]AggregateInstanceResult, len(names))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxFanout)
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ir := AggregateInstanceResult{Instance: name, OK: true}
			res, err := h(ctx, withInstance(req, name))
			switch {
			case err != nil:
				ir.OK = false
				ir.Error = err.Error()
			case res != nil && res.IsError:
				ir.OK = false
				ir.Error = resultText(res)
			default:
				ir.Text = resultText(res)
				if res != nil {
					ir.Data = res.StructuredContent
				}
			}
			results[i] = ir
		}(i, name)
	}
	wg.Wait()

	var b strings.Builder
	okCount := 0
	for i, ir := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "### instance: %s\n", ir.Instance)
		if ir.OK {
			okCount++
			b.WriteString(ir.Text)
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "error: %s\n", ir.Error)
		}
	}

	out := AggregateOutput{
		Summary: AggregateSummary{
			Total:  len(results),
			OK:     okCount,
			Failed: len(results) - okCount,
		},
		Instances: results,
	}

	// Every instance failed — report it as an error result so the client and
	// model treat the call as failed rather than a successful empty aggregate.
	if okCount == 0 {
		return mcp.NewToolResultError(b.String())
	}
	return mcp.NewToolResultStructured(out, b.String())
}

// instanceName resolves the instance a single-target request addressed: the
// explicit "instance" argument, or the default (first-declared) instance when
// the argument is empty.
func instanceName(req mcp.CallToolRequest, r *pihole.Registry) string {
	if name := req.GetString(instanceArg, ""); name != "" {
		return name
	}
	return r.Names()[0]
}

// withProvenance prefixes a successful result's first text block with an
// "instance: <name>" line. Structured content and the error flag are left
// unchanged. A result with no text block gains a leading text block.
func withProvenance(res *mcp.CallToolResult, name string) *mcp.CallToolResult {
	if res == nil {
		return res
	}
	prefix := fmt.Sprintf("instance: %s\n", name)
	for i, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			tc.Text = prefix + tc.Text
			res.Content[i] = tc
			return res
		}
	}
	res.Content = append([]mcp.Content{mcp.NewTextContent(strings.TrimRight(prefix, "\n"))}, res.Content...)
	return res
}

// withInstance returns a copy of req with the instance argument set to name.
func withInstance(req mcp.CallToolRequest, name string) mcp.CallToolRequest {
	args := make(map[string]any, len(req.GetArguments())+1)
	for k, v := range req.GetArguments() {
		args[k] = v
	}
	args[instanceArg] = name

	out := req
	out.Params.Arguments = args
	return out
}

// isReadOnly reports whether a tool is annotated read-only.
func isReadOnly(tool mcp.Tool) bool {
	return tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint
}

// resultText extracts the first text content block from a tool result.
func resultText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// addInstanceParam advertises the optional "instance" argument on a tool's
// input schema. Called only when more than one instance is configured, so
// single-instance setups keep a clutter-free schema.
func addInstanceParam(tool *mcp.Tool, r *pihole.Registry) {
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = map[string]any{}
	}
	desc := fmt.Sprintf("Target Pi-hole instance: one of %s. Read-only tools also accept 'all' to aggregate. Default: %s.",
		strings.Join(r.Names(), ", "), r.Names()[0])
	tool.InputSchema.Properties[instanceArg] = map[string]any{
		"type":        "string",
		"description": desc,
	}
}
