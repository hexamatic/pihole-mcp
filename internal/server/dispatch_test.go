package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

// Dispatch tests drive the server the way a real MCP client does: raw JSON-RPC
// bytes into HandleMessage, raw JSON back out.
//
// Every other test in this repository calls a handler function directly, which
// leaves the whole registration and serialisation layer unguarded. A tool
// registered under the wrong name, a schema that fails to marshal, a resource
// wired to a URI nobody asks for, or a completion provider attached to the
// wrong argument would all keep the unit suite green while breaking every
// client. These tests fail instead.
//
// Assertions go through the wire JSON rather than type-asserting the returned
// mcp.JSONRPCMessage. That is deliberate on two counts. It exercises the real
// serialisation (mcp.CallToolResult has a hand-written MarshalJSON that decides
// whether structuredContent and isError appear at all), and it removes the
// silent-failure mode where the SDK changes a result from a value to a pointer
// and a type assertion starts returning ok=false forever.

// newDispatchFake starts a fake Pi-hole that is shut down when the test ends.
func newDispatchFake(t *testing.T) *piholefake.Fake {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	return f
}

// newDispatchServer wires the supplied fakes into a real registry and builds
// the real MCP server over it. The first fake is the default instance, which
// is what the resources and the completion provider read from.
func newDispatchServer(t *testing.T, fakes ...*piholefake.Fake) *server.MCPServer {
	t.Helper()
	instances := make([]pihole.InstanceConfig, len(fakes))
	for i, f := range fakes {
		instances[i] = pihole.InstanceConfig{Name: instanceNames[i], URL: f.URL(), Password: "x"}
	}
	reg := pihole.NewRegistry(instances)
	t.Cleanup(reg.Close)
	return New(reg)
}

// dispatch pushes one raw JSON-RPC request through the server and returns the
// response envelope decoded as a generic map, exactly as a client sees it.
func dispatch(t *testing.T, srv *server.MCPServer, request string) map[string]any {
	t.Helper()

	resp := srv.HandleMessage(context.Background(), json.RawMessage(request))
	if resp == nil {
		t.Fatalf("HandleMessage returned no response for request %s", request)
	}

	wire, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshalling the response failed: %v (response %#v)", err, resp)
	}

	var envelope map[string]any
	if err := json.Unmarshal(wire, &envelope); err != nil {
		t.Fatalf("the response is not a JSON object: %v (wire %s)", err, wire)
	}
	if envelope["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0 (wire %s)", envelope["jsonrpc"], wire)
	}
	return envelope
}

// dispatchResult unwraps a successful response, failing the test if the server
// answered with a JSON-RPC error instead.
func dispatchResult(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	if e, present := envelope["error"]; present {
		t.Fatalf("expected a result, got a JSON-RPC error: %#v", e)
	}
	res, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is missing or not an object: %#v", envelope["result"])
	}
	return res
}

// The handshake. "initialize" and "notifications/initialized" are MCP wire
// literals: the protocol spells them, we do not, so the American spelling is
// mandatory here and the UK-spelling linter is silenced for this function
// alone rather than repo-wide.
//
// Four spellings trip the linter here (the two literals, this comment quoting
// them, and the test's own name), so one directive on the declaration is
// tidier than four on separate lines. If more dispatch tests land, the repo
// already has the better shape for this: the self-mapping extra-words entries
// in .golangci.yml that neutralise "unauthorized". Adding "initialize",
// "initialized" and "Initialize" there would retire this directive and keep
// the UK check live over the prose in between.
//
//nolint:misspell // MCP protocol method names are wire literals
func TestDispatch_Initialize(t *testing.T) {
	srv := newDispatchServer(t, newDispatchFake(t))

	// 2025-06-18 is a valid protocol version but not the newest one the SDK
	// knows, so an echo is distinguishable from a server that always answers
	// with its own latest regardless of what the client asked for.
	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-06-18",
			"capabilities": {},
			"clientInfo": {"name": "dispatch-test", "version": "0.0.0"}
		}
	}`)
	res := dispatchResult(t, envelope)

	if got := res["protocolVersion"]; got != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the negotiated 2025-06-18", got)
	}

	info, ok := res["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo is missing or not an object: %#v", res["serverInfo"])
	}
	if got := info["name"]; got != "pihole-mcp" {
		t.Errorf("serverInfo.name = %v, want pihole-mcp; clients key their config off this name", got)
	}
	if got, _ := info["version"].(string); got == "" {
		t.Error("serverInfo.version is empty; ldflags overwrite it at release but the default must still ship")
	}

	// The capability block is how a client decides which requests are worth
	// making at all. Dropping one silently disables a whole feature clientside.
	caps, ok := res["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is missing or not an object: %#v", res["capabilities"])
	}
	for _, want := range []string{"tools", "resources", "prompts", "completions", "logging"} {
		if _, present := caps[want]; !present {
			t.Errorf("capabilities.%s is missing; a client would never call that surface", want)
		}
	}

	if got, _ := res["instructions"].(string); !strings.Contains(got, "pihole_padd") {
		t.Errorf("instructions do not mention pihole_padd, so the onboarding text was lost: %q", got)
	}

	// The initialised notification carries no id, so the server must answer
	// with nothing at all rather than an empty response envelope. A stdio
	// client that receives a response to a notification treats the stream as
	// corrupt.
	if resp := srv.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)); resp != nil {
		t.Errorf("the initialised notification answered with %#v, want no response at all", resp)
	}
}

func TestDispatch_ToolsList(t *testing.T) {
	// The catalogue in internal/tools is a package-level accumulator: every
	// call to New appends to it. Resetting immediately before building the
	// server is what makes the comparison below one registration pass against
	// one tool list, rather than against every server this binary has built.
	// Nothing in this package may call t.Parallel while that holds.
	tools.ResetCatalogue()
	srv := newDispatchServer(t, newDispatchFake(t))
	catalogue := tools.Catalogue()

	envelope := dispatch(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	res := dispatchResult(t, envelope)

	listed, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools is missing or not an array: %#v", res["tools"])
	}
	if len(listed) == 0 {
		t.Fatal("tools/list returned no tools; every client would show an empty server")
	}
	if len(listed) != len(catalogue) {
		t.Errorf("tools/list served %d tools but the catalogue recorded %d; a tool was registered without being recorded, or recorded without being served",
			len(listed), len(catalogue))
	}

	// The equality above compares two numbers that move together: deleting a
	// whole RegisterX call from RegisterAll drops the tools from the served
	// list AND from the catalogue, so it still balances and this package stays
	// green. Only the docs drift check catches that today, and it is a
	// different gate. An absolute floor plus a named sample from several
	// different registration functions fails here instead.
	const minTools = 70
	if len(listed) < minTools {
		t.Errorf("tools/list served only %d tools, expected at least %d; a whole category looks deregistered",
			len(listed), minTools)
	}

	// No pagination limit is configured, so a client that reads only the first
	// page must still see everything. A stray cursor would silently truncate.
	if cursor, present := res["nextCursor"]; present && cursor != "" {
		t.Errorf("nextCursor = %v, want none; tools/list must fit in one page", cursor)
	}

	byName := make(map[string]map[string]any, len(listed))
	for i, entry := range listed {
		tool, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("tools[%d] is not an object: %#v", i, entry)
		}
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tools[%d] has no name: %#v", i, tool)
		}
		if desc, _ := tool["description"].(string); desc == "" {
			t.Errorf("tool %q has no description; the model has nothing to select on", name)
		}
		byName[name] = tool
	}

	// One name from each of several different registration functions. The
	// count checks above cannot see a renamed tool at all, and cannot see a
	// deregistered category because the catalogue moves with it. These can.
	for _, want := range []string{
		"pihole_search_domains",   // RegisterSearch
		"pihole_domains_list",     // RegisterDomains
		"pihole_lists_list",       // RegisterLists
		"pihole_groups_list",      // RegisterGroups
		"pihole_clients_list",     // RegisterClients
		"pihole_config_get",       // RegisterConfig
		"pihole_auth_sessions",    // RegisterAuth
		"pihole_dns_get_blocking", // RegisterDNS
		"pihole_dhcp_leases",      // RegisterDHCP
		"pihole_logs_dns",         // RegisterLogs
	} {
		if _, present := byName[want]; !present {
			t.Errorf("%s is not served; its registration function looks removed or the tool was renamed", want)
		}
	}

	const want = "pihole_stats_summary"
	tool, present := byName[want]
	if !present {
		t.Fatalf("%s is not in tools/list (%d tools served); it was renamed or dropped", want, len(byName))
	}

	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no inputSchema object: %#v", want, tool["inputSchema"])
	}
	if got := schema["type"]; got != "object" {
		t.Errorf("%s inputSchema.type = %v, want object", want, got)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Fatalf("%s inputSchema.properties is empty, so its arguments are unreachable: %#v", want, schema["properties"])
	}
	if _, present := props["detail"]; !present {
		t.Errorf("%s inputSchema.properties is missing \"detail\"", want)
	}

	// This tool declares a structured output schema. Losing it on the wire is
	// exactly the regression multi-instance mode once shipped, and no
	// handler-level test can see it.
	outSchema, ok := tool["outputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no outputSchema object: %#v", want, tool["outputSchema"])
	}
	outProps, ok := outSchema["properties"].(map[string]any)
	if !ok || len(outProps) == 0 {
		t.Fatalf("%s outputSchema.properties is empty: %#v", want, outSchema["properties"])
	}
	if _, present := outProps["total_queries"]; !present {
		t.Errorf("%s outputSchema.properties is missing \"total_queries\": %#v", want, outProps)
	}
}

func TestDispatch_ToolsList_MultiInstance(t *testing.T) {
	// A second Pi-hole registers the two sync tools and widens every schema.
	// That rewiring is invisible to a handler-level test because it lives in
	// the registration path, so this is the only place it can be checked.
	tools.ResetCatalogue()
	newDispatchServer(t, newDispatchFake(t))
	single := len(tools.Catalogue())

	tools.ResetCatalogue()
	srv := newDispatchServer(t, newDispatchFake(t), newDispatchFake(t))

	envelope := dispatch(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`)
	res := dispatchResult(t, envelope)

	listed, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools is missing or not an array: %#v", res["tools"])
	}
	if len(listed) <= single {
		t.Errorf("multi-instance served %d tools, single-instance served %d; the sync tools did not register",
			len(listed), single)
	}

	byName := make(map[string]map[string]any, len(listed))
	for _, entry := range listed {
		tool, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		byName[name] = tool
	}

	for _, want := range []string{"pihole_instance_diff", "pihole_instance_sync"} {
		if _, present := byName[want]; !present {
			t.Errorf("%s is not served with two instances configured", want)
		}
	}

	// Without the instance argument on the schema, a client cannot address the
	// second Pi-hole at all, which is the entire point of multi-instance mode.
	tool, present := byName["pihole_stats_summary"]
	if !present {
		t.Fatal("pihole_stats_summary is not served with two instances configured")
	}
	schema, _ := tool["inputSchema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	if _, present := props["instance"]; !present {
		t.Errorf("pihole_stats_summary has no instance argument with two instances configured: %#v", props)
	}

	// The single-instance case above asserts the output schema survives, and
	// the comment there calls losing it "exactly the regression multi-instance
	// mode once shipped". Multi-instance is where that regression actually
	// lived, and until now nothing checked it here: setting
	// tool.RawOutputSchema = nil in widenOutputSchema reinstates the historical
	// bug verbatim and left this whole package green.
	outSchema, ok := tool["outputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("pihole_stats_summary has no outputSchema with two instances configured: %#v", tool["outputSchema"])
	}
	// Widened, the schema is a oneOf over the single result and the aggregate
	// envelope rather than the plain single-result object.
	oneOf, ok := outSchema["oneOf"].([]any)
	if !ok || len(oneOf) == 0 {
		t.Errorf("pihole_stats_summary outputSchema is not the widened oneOf form: %#v", outSchema)
	}
}

// A schema that survives tools/list is only half of it: the value still has to
// travel with two instances configured. This drives a real call through the
// JSON-RPC envelope against a named instance and reads the figure back.
func TestDispatch_ToolsCall_StructuredContentSurvivesMultiInstance(t *testing.T) {
	tools.ResetCatalogue()
	primary, secondary := newDispatchFake(t), newDispatchFake(t)
	const seeded = 5151
	primary.SetSummaryTotal(9999)
	secondary.SetSummaryTotal(seeded)

	srv := newDispatchServer(t, primary, secondary)

	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 31,
		"method": "tools/call",
		"params": {
			"name": "pihole_stats_summary",
			"arguments": {"detail": "minimal", "instance": "secondary"}
		}
	}`)
	res := dispatchResult(t, envelope)

	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("tools/call reported an error result: %#v", res)
	}
	structured, ok := res["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is missing with two instances configured: %#v", res["structuredContent"])
	}
	total, ok := structured["total_queries"].(float64)
	if !ok {
		t.Fatalf("structuredContent.total_queries is missing or not a number: %#v", structured["total_queries"])
	}
	if int(total) != seeded {
		t.Errorf("structuredContent.total_queries = %v, want the seeded %d from the named instance", total, seeded)
	}
}

func TestDispatch_ToolsCall_StatsSummary(t *testing.T) {
	fake := newDispatchFake(t)

	// A distinctive seed is the whole point. Asserting only that the call did
	// not fail would pass against a handler that returns a hardcoded blank.
	const seeded = 4242
	const seededText = "4,242" // format.Number groups thousands.
	fake.SetSummaryTotal(seeded)

	srv := newDispatchServer(t, fake)

	// detail=minimal is the branch that produces structured content; the
	// normal and full branches build markdown only.
	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {
			"name": "pihole_stats_summary",
			"arguments": {"detail": "minimal"}
		}
	}`)
	res := dispatchResult(t, envelope)

	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("tools/call reported an error result: %#v", res)
	}

	structured, ok := res["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is missing or not an object: %#v", res["structuredContent"])
	}
	total, ok := structured["total_queries"].(float64)
	if !ok {
		t.Fatalf("structuredContent.total_queries is missing or not a number: %#v", structured["total_queries"])
	}
	if int(total) != seeded {
		t.Errorf("structuredContent.total_queries = %v, want the seeded %d; the value did not travel from the backend to the wire",
			total, seeded)
	}

	// Clients that ignore structured content read the text block instead, so
	// it has to carry the same figure.
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content is missing or empty: %#v", res["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %#v", content[0])
	}
	if got := block["type"]; got != "text" {
		t.Errorf("content[0].type = %v, want text", got)
	}
	text, _ := block["text"].(string)
	if !strings.Contains(text, seededText) {
		t.Errorf("content[0].text does not contain the seeded %s: %q", seededText, text)
	}
}

func TestDispatch_ResourcesRead_Summary(t *testing.T) {
	fake := newDispatchFake(t)

	// pihole://summary is backed by /stats/summary, which the fake serves.
	// pihole://status is not usable here: it reads /dns/blocking and
	// /info/version, neither of which the fake routes.
	const seeded = 424242
	const seededText = "424,242"
	fake.SetSummaryTotal(seeded)

	srv := newDispatchServer(t, fake)

	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 5,
		"method": "resources/read",
		"params": {"uri": "pihole://summary"}
	}`)
	res := dispatchResult(t, envelope)

	contents, ok := res["contents"].([]any)
	if !ok {
		t.Fatalf("contents is missing or not an array: %#v", res["contents"])
	}
	if len(contents) == 0 {
		t.Fatal("resources/read returned no contents; the resource resolved to nothing")
	}
	entry, ok := contents[0].(map[string]any)
	if !ok {
		t.Fatalf("contents[0] is not an object: %#v", contents[0])
	}
	if got := entry["uri"]; got != "pihole://summary" {
		t.Errorf("contents[0].uri = %v, want pihole://summary", got)
	}
	if got := entry["mimeType"]; got != "text/markdown" {
		t.Errorf("contents[0].mimeType = %v, want text/markdown", got)
	}
	text, _ := entry["text"].(string)
	if text == "" {
		t.Fatal("contents[0].text is empty; the resource rendered nothing")
	}
	if !strings.Contains(text, seededText) {
		t.Errorf("contents[0].text does not contain the seeded %s, so the resource did not read the backend: %q",
			seededText, text)
	}
}

func TestDispatch_CompletionComplete_InvestigateDomain(t *testing.T) {
	fake := newDispatchFake(t)
	fake.AddDomain("deny", "exact", "ads.example.com", "seeded", true)
	fake.AddDomain("allow", "exact", "example.com", "seeded", true)
	// A seeded domain that must NOT come back, so the test proves the value
	// filter ran rather than that some list was returned wholesale.
	fake.AddDomain("deny", "exact", "tracker.invalid", "seeded", true)

	srv := newDispatchServer(t, fake)

	// investigate_domain / domain is the only pair the provider implements.
	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 6,
		"method": "completion/complete",
		"params": {
			"ref": {"type": "ref/prompt", "name": "investigate_domain"},
			"argument": {"name": "domain", "value": "example"}
		}
	}`)
	res := dispatchResult(t, envelope)

	completion, ok := res["completion"].(map[string]any)
	if !ok {
		t.Fatalf("completion is missing or not an object: %#v", res["completion"])
	}
	rawValues, ok := completion["values"].([]any)
	if !ok {
		t.Fatalf("completion.values is missing or not an array: %#v", completion["values"])
	}
	values := make([]string, len(rawValues))
	for i, v := range rawValues {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("completion.values[%d] is not a string: %#v", i, v)
		}
		values[i] = s
	}

	want := []string{"ads.example.com", "example.com"}
	if strings.Join(values, ",") != strings.Join(want, ",") {
		t.Errorf("completion.values = %v, want %v (sorted, matching the value prefix, excluding tracker.invalid)",
			values, want)
	}
	if total, _ := completion["total"].(float64); int(total) != len(want) {
		t.Errorf("completion.total = %v, want %d", completion["total"], len(want))
	}

	// An argument the provider does not implement must answer with an empty
	// completion, not with the domain list it happens to have loaded.
	other := dispatchResult(t, dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 7,
		"method": "completion/complete",
		"params": {
			"ref": {"type": "ref/prompt", "name": "review_top_blocked"},
			"argument": {"name": "count", "value": "2"}
		}
	}`))
	otherCompletion, ok := other["completion"].(map[string]any)
	if !ok {
		t.Fatalf("completion is missing or not an object: %#v", other["completion"])
	}
	if vals, _ := otherCompletion["values"].([]any); len(vals) != 0 {
		t.Errorf("review_top_blocked/count completed with %v, want nothing", vals)
	}
}

func TestDispatch_ToolsCall_UnknownTool(t *testing.T) {
	srv := newDispatchServer(t, newDispatchFake(t))

	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 8,
		"method": "tools/call",
		"params": {"name": "pihole_not_a_real_tool", "arguments": {}}
	}`)

	// This is the shape the server actually produces: a JSON-RPC error with
	// INVALID_PARAMS (-32602), not a result carrying isError, and certainly
	// not an empty success. Pinning the shape matters because the two are
	// handled on completely different paths by every client.
	if _, present := envelope["result"]; present {
		t.Errorf("an unknown tool produced a result envelope: %#v", envelope["result"])
	}
	failure, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("an unknown tool did not produce a JSON-RPC error: %#v", envelope)
	}
	code, ok := failure["code"].(float64)
	if !ok {
		t.Fatalf("error.code is missing or not a number: %#v", failure["code"])
	}
	if int(code) != -32602 {
		t.Errorf("error.code = %v, want -32602 (INVALID_PARAMS)", int(code))
	}
	message, _ := failure["message"].(string)
	if !strings.Contains(message, "pihole_not_a_real_tool") {
		t.Errorf("error.message does not name the tool that was asked for: %q", message)
	}
	if !strings.Contains(message, "not found") {
		t.Errorf("error.message does not say the tool was not found: %q", message)
	}
}

func TestDispatch_ToolsCall_UnknownInstanceIsErrorResult(t *testing.T) {
	// The counterpart to the case above. A tool that exists but cannot do what
	// was asked answers with a normal result carrying isError, so the model
	// sees the explanation. Only an unroutable request becomes a JSON-RPC
	// error. Both shapes are asserted so a change that collapses one into the
	// other is caught.
	srv := newDispatchServer(t, newDispatchFake(t), newDispatchFake(t))

	envelope := dispatch(t, srv, `{
		"jsonrpc": "2.0",
		"id": 9,
		"method": "tools/call",
		"params": {
			"name": "pihole_stats_summary",
			"arguments": {"instance": "nonexistent"}
		}
	}`)
	res := dispatchResult(t, envelope)

	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("an unknown instance did not set isError: %#v", res)
	}
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("an error result carried no content to explain itself: %#v", res["content"])
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if !strings.Contains(text, "nonexistent") {
		t.Errorf("the error text does not name the instance that was asked for: %q", text)
	}
}
