package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// staleBackupAge is how long a teleporter export or sync snapshot is kept in
// the system temp directory before it is reaped. Both file families are the
// caller's to collect promptly (see pihole_teleporter_export's description),
// so this is a safety net against an unbounded temp directory, not the
// primary cleanup path.
const staleBackupAge = 24 * time.Hour

// backupFileGlobs are the two filename patterns a backup lands under: an
// on-demand teleporter export, and the rollback snapshot pihole_instance_sync
// takes of the target before it applies (sync.go's exportSnapshot).
var backupFileGlobs = []string{"pihole-backup-*.zip", "pihole-*-snapshot-*.zip"}

// reapStaleBackups deletes files older than staleBackupAge matching
// backupFileGlobs from the system temp directory. Best-effort: a failure to
// glob, stat or remove one file is skipped rather than failing the caller —
// this runs ahead of every new export, and a full temp directory is a worse
// failure mode to compound than a few stale files left behind.
func reapStaleBackups() {
	cutoff := time.Now().Add(-staleBackupAge)
	for _, pattern := range backupFileGlobs {
		matches, err := filepath.Glob(filepath.Join(os.TempDir(), pattern))
		if err != nil {
			continue
		}
		for _, m := range matches {
			info, statErr := os.Stat(m)
			if statErr != nil || info.ModTime().After(cutoff) {
				continue
			}
			_ = os.Remove(m)
		}
	}
}

// RegisterTeleporter registers teleporter export and import tools.
func RegisterTeleporter(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_teleporter_export",
		mcp.WithTitleAnnotation("Export Backup"),
		mcp.WithDescription("Export a full Pi-hole configuration backup as a zip archive. Returns the saved file path and size. The file persists on disk after the call returns and is the caller's to move or delete; under Docker it is written inside the container unless output_path points at a mounted volume."),
		mcp.WithString("output_path", mcp.Description("Absolute path to save the backup to. Defaults to a system temp file.")),
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

		outputPath := req.GetString("output_path", "")
		if outputPath != "" && !filepath.IsAbs(outputPath) {
			return mcp.NewToolResultError("Invalid output_path: must be an absolute path"), nil
		}

		reapStaleBackups()

		resp, err := c.DoRaw(ctx, "GET", "/teleporter", nil)
		if err != nil {
			return toolError("export backup", err), nil
		}
		defer func() { _ = resp.Body.Close() }()

		var out *os.File
		if outputPath != "" {
			out, err = os.Create(outputPath) //nolint:gosec // output_path is a user-provided MCP tool parameter, validated absolute above
		} else {
			out, err = os.CreateTemp("", "pihole-backup-*.zip")
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create backup file: %v", err)), nil
		}

		n, err := io.Copy(out, resp.Body)
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save backup: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"**Backup saved.** File: %s (%s bytes, %s). The file persists on disk; deleting it is your responsibility.",
			out.Name(), format.Number(int(n)),
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
		if err := validateBackupFilePath(filePath); err != nil {
			return mcp.NewToolResultError("Invalid file_path: " + err.Error()), nil
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
