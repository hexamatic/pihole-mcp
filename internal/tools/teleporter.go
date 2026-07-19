package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTeleporter registers teleporter export and import tools.
func RegisterTeleporter(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_teleporter_export",
		mcp.WithTitleAnnotation("Export Backup"),
		mcp.WithDescription("Export a full Pi-hole configuration backup as a zip archive. Returns the saved file path and size."),
		mcp.WithReadOnlyHintAnnotation(true),
	), teleporterExportHandler(r))

	addTool(s, r, mcp.NewTool("pihole_teleporter_import",
		mcp.WithTitleAnnotation("Import Backup"),
		mcp.WithDescription("Import a Pi-hole configuration backup from a zip archive. Selectively import config, gravity tables, and DHCP leases."),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("Absolute path to the backup zip file.")),
		mcp.WithBoolean("config", mcp.Description("Import Pi-hole configuration (default true).")),
		mcp.WithBoolean("gravity", mcp.Description("Import gravity database tables (default true).")),
		mcp.WithBoolean("dhcp_leases", mcp.Description("Import DHCP leases (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
	), teleporterImportHandler(r))
}

func teleporterExportHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := c.DoRaw(ctx, "GET", "/teleporter", nil)
		if err != nil {
			return toolError("export backup", err), nil
		}
		defer func() { _ = resp.Body.Close() }()

		tmpFile, err := os.CreateTemp("", "pihole-backup-*.zip")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create temp file: %v", err)), nil
		}

		n, err := io.Copy(tmpFile, resp.Body)
		if closeErr := tmpFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save backup: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"**Backup saved.** File: %s (%s bytes, %s)",
			tmpFile.Name(), format.Number(int(n)),
			format.Timestamp(float64(time.Now().Unix())))), nil
	}
}

func teleporterImportHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		filePath, err := req.RequireString("file_path")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'file_path' is required"), nil
		}

		importConfig := req.GetBool("config", true)
		importGravity := req.GetBool("gravity", true)
		importDHCP := req.GetBool("dhcp_leases", true)

		importOptions := map[string]any{
			"config":      importConfig,
			"dhcp_leases": importDHCP,
			"gravity": map[string]any{
				"group":               importGravity,
				"adlist":              importGravity,
				"adlist_by_group":     importGravity,
				"domainlist":          importGravity,
				"domainlist_by_group": importGravity,
				"client":              importGravity,
				"client_by_group":     importGravity,
			},
		}

		var result pihole.TeleporterImportResponse
		if err := c.PostMultipart(ctx, "/teleporter", filePath, importOptions, &result); err != nil {
			return toolError("import backup", err), nil
		}

		var b strings.Builder
		b.WriteString("**Import complete.**\n")
		for _, item := range result.Processed {
			fmt.Fprintf(&b, "- %s\n", item)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}
