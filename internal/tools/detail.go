package tools

import "github.com/mark3labs/mcp-go/mcp"

// detailParam is the standard detail parameter added to verbose tools.
var detailParam = mcp.WithString("detail",
	mcp.Description("Response detail: minimal, normal (default), or full."),
	mcp.Enum("minimal", "normal", "full"),
)

// formatParam is the standard output format parameter for tabular tools.
var formatParam = mcp.WithString("format",
	mcp.Description("Output format: text (default) or csv."),
	mcp.Enum("text", "csv"),
)

// maxPageLimit caps the limit parameter on the list tools. Well above any
// realistic client, group or list count, and below the point where a single
// response stops being something a model can read.
const maxPageLimit = 1000

// limitParam and offsetParam are the standard pagination parameters on the
// list tools. FTL returns these collections whole, so the paging is applied
// here; without it a gravity-backed domain list is a single unbounded response.
var limitParam = mcp.WithNumber("limit",
	mcp.Description("Maximum entries to return. Default 0, meaning all."),
	mcp.Min(0), mcp.Max(maxPageLimit),
)

var offsetParam = mcp.WithNumber("offset",
	mcp.Description("Entries to skip before returning results, for paging with limit. Default 0."),
	mcp.Min(0),
)

// getDetail extracts the detail level from a tool request.
func getDetail(req mcp.CallToolRequest) string {
	d := req.GetString("detail", "normal")
	if d != "minimal" && d != "full" {
		return "normal"
	}
	return d
}

// wantCSV returns true if the user requested CSV output format.
func wantCSV(req mcp.CallToolRequest) bool {
	return req.GetString("format", "text") == "csv"
}
