// Package resources registers MCP resources for the Pi-hole MCP server.
package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAll registers all MCP resources and resource templates. Resources
// always read from the default (first-declared) instance; use the per-tool
// "instance" argument to reach other instances.
func RegisterAll(s *server.MCPServer, r *pihole.Registry) {
	c := r.Default()

	// Static resources.
	s.AddResource(
		mcp.NewResource("pihole://status", "Pi-hole Status",
			mcp.WithResourceDescription("Current blocking status, version, and health."),
			mcp.WithMIMEType("text/markdown"),
		),
		statusResourceHandler(c),
	)

	s.AddResource(
		mcp.NewResource("pihole://summary", "Query Summary",
			mcp.WithResourceDescription("Query statistics: total, blocked, cached, forwarded, active clients, gravity size."),
			mcp.WithMIMEType("text/markdown"),
		),
		summaryResourceHandler(c),
	)

	// Resource templates.
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("pihole://clients/{client}", "Client Detail",
			mcp.WithTemplateDescription("Configuration and group assignments for a specific client."),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		clientResourceHandler(c),
	)

	s.AddResourceTemplate(
		mcp.NewResourceTemplate("pihole://domains/{type}/{kind}", "Domain List",
			mcp.WithTemplateDescription("Domains on a specific list (e.g. deny/exact, allow/regex)."),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		domainListResourceHandler(c),
	)

	s.AddResourceTemplate(
		mcp.NewResourceTemplate("pihole://lists/{address}", "List Detail",
			mcp.WithTemplateDescription("Details of a specific blocklist or allowlist by URL."),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		listDetailResourceHandler(c),
	)
}

func statusResourceHandler(c *pihole.Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		var blocking pihole.BlockingStatus
		var ver pihole.VersionInfo

		if err := c.Get(ctx, "/dns/blocking", &blocking); err != nil {
			return nil, fmt.Errorf("getting blocking status: %w", err)
		}
		_ = c.Get(ctx, "/info/version", &ver)

		var b strings.Builder
		fmt.Fprintf(&b, "# Pi-hole Status\n\n")
		fmt.Fprintf(&b, "- **Blocking:** %s\n", blocking.Blocking)
		if blocking.Timer != nil {
			fmt.Fprintf(&b, "- **Timer:** %.0f seconds remaining\n", *blocking.Timer)
		}
		if ver.Version.FTL.Local.Version != "" {
			fmt.Fprintf(&b, "- **FTL:** %s\n", ver.Version.FTL.Local.Version)
			fmt.Fprintf(&b, "- **Core:** %s\n", ver.Version.Core.Local.Version)
			fmt.Fprintf(&b, "- **Web:** %s\n", ver.Version.Web.Local.Version)
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "pihole://status",
				MIMEType: "text/markdown",
				Text:     b.String(),
			},
		}, nil
	}
}

func summaryResourceHandler(c *pihole.Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		var stats pihole.StatsSummary
		if err := c.Get(ctx, "/stats/summary", &stats); err != nil {
			return nil, fmt.Errorf("getting stats: %w", err)
		}

		q := stats.Queries
		var b strings.Builder
		fmt.Fprintf(&b, "# Pi-hole Summary\n\n")
		fmt.Fprintf(&b, "- **Queries:** %s\n", format.Number(q.Total))
		fmt.Fprintf(&b, "- **Blocked:** %s (%s)\n", format.Number(q.Blocked), format.Percent(q.PercentBlocked))
		fmt.Fprintf(&b, "- **Cached:** %s\n", format.Number(q.Cached))
		fmt.Fprintf(&b, "- **Forwarded:** %s\n", format.Number(q.Forwarded))
		fmt.Fprintf(&b, "- **Active clients:** %d\n", stats.Clients.Active)
		fmt.Fprintf(&b, "- **Gravity domains:** %s\n", format.Number(stats.Gravity.DomainsBeingBlocked))

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "pihole://summary",
				MIMEType: "text/markdown",
				Text:     b.String(),
			},
		}, nil
	}
}

func clientResourceHandler(c *pihole.Client) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// Extract client from URI: pihole://clients/{client}
		uri := req.Params.URI
		client := strings.TrimPrefix(uri, "pihole://clients/")

		var result pihole.ClientsResponse
		if err := c.Get(ctx, "/clients/"+client, &result); err != nil {
			return nil, fmt.Errorf("getting client %s: %w", client, err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "# Client: %s\n\n", client)
		for _, cl := range result.Clients {
			fmt.Fprintf(&b, "- **Name:** %s\n", cl.Name)
			fmt.Fprintf(&b, "- **Comment:** %s\n", cl.Comment)
			fmt.Fprintf(&b, "- **Groups:** %v\n", cl.Groups)
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: "text/markdown",
				Text:     b.String(),
			},
		}, nil
	}
}

func domainListResourceHandler(c *pihole.Client) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// Extract type/kind from URI: pihole://domains/{type}/{kind}
		uri := req.Params.URI
		path := strings.TrimPrefix(uri, "pihole://domains/")

		var result pihole.DomainsResponse
		if err := c.Get(ctx, "/domains/"+path, &result); err != nil {
			return nil, fmt.Errorf("getting domains %s: %w", path, err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "# Domains: %s (%d)\n\n", path, len(result.Domains))
		for _, d := range result.Domains {
			fmt.Fprintf(&b, "- %s (enabled: %s)\n", d.Domain, format.Bool(d.Enabled))
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: "text/markdown",
				Text:     b.String(),
			},
		}, nil
	}
}

func listDetailResourceHandler(c *pihole.Client) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		uri := req.Params.URI
		address := strings.TrimPrefix(uri, "pihole://lists/")

		var result pihole.ListsResponse
		if err := c.Get(ctx, "/lists/"+address, &result); err != nil {
			return nil, fmt.Errorf("getting list %s: %w", address, err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "# List: %s\n\n", address)
		for _, l := range result.Lists {
			fmt.Fprintf(&b, "- **Type:** %s\n", l.Type)
			fmt.Fprintf(&b, "- **Domains:** %s\n", format.Number(l.Number))
			fmt.Fprintf(&b, "- **Enabled:** %s\n", format.Bool(l.Enabled))
			fmt.Fprintf(&b, "- **Comment:** %s\n", l.Comment)
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: "text/markdown",
				Text:     b.String(),
			},
		}, nil
	}
}
