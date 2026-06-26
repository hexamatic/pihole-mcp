package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterSearch registers the domain search tool.
func RegisterSearch(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_search_domains",
		mcp.WithTitleAnnotation("Search Domains in Lists"),
		mcp.WithDescription("Search for a domain across all allow/deny lists and gravity blocklists. Use before modifying lists to check current state."),
		mcp.WithString("domain", mcp.Required(), mcp.Description("Domain to search for.")),
		mcp.WithBoolean("partial", mcp.Description("Enable partial/substring matching (default false).")),
		mcp.WithNumber("max_results", mcp.Description("Max results per category (default 20).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), searchDomainsHandler(r))
}

func searchDomainsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		domain, err := req.RequireString("domain")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'domain' is required"), nil
		}

		params := make(map[string]string)
		if req.GetBool("partial", false) {
			params["partial"] = "true"
		}
		n := int(req.GetFloat("max_results", 20))
		params["N"] = fmt.Sprintf("%d", n)

		path := "/search/" + domain + format.QueryParams(params)
		var result pihole.SearchResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("search domains", err), nil
		}

		s := result.Search
		var b strings.Builder
		fmt.Fprintf(&b, "**Search: %s** — %d matches (domains: %d exact + %d regex, gravity: %d block + %d allow)\n",
			domain, s.Results.Total, s.Results.Domains.Exact, s.Results.Domains.Regex,
			s.Results.Gravity.Block, s.Results.Gravity.Allow)

		if len(s.Domains) > 0 {
			b.WriteString("\n**Domain list matches:**\n")
			for _, d := range s.Domains {
				fmt.Fprintf(&b, "- %s (%s/%s, enabled=%s)\n", d.Domain, d.Type, d.Kind, format.Bool(d.Enabled))
			}
		}

		if len(s.Gravity) > 0 {
			b.WriteString("\n**Gravity matches:**\n")
			for _, g := range s.Gravity {
				fmt.Fprintf(&b, "- %s via %s (%s)\n", g.Domain, g.Address, g.Type)
			}
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}
