package tools

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
//
// keep names files to leave alone whatever their age: the archive an import
// is about to read may be an old export, and the backup taken before that
// import used to delete it first.
func reapStaleBackups(keep ...string) {
	var kept []os.FileInfo
	for _, k := range keep {
		if info, err := os.Stat(k); err == nil {
			kept = append(kept, info)
		}
	}
	cutoff := time.Now().Add(-staleBackupAge)
	for _, pattern := range backupFileGlobs {
		matches, err := filepath.Glob(filepath.Join(os.TempDir(), pattern))
		if err != nil {
			continue
		}
	files:
		for _, m := range matches {
			info, statErr := os.Stat(m)
			if statErr != nil || info.ModTime().After(cutoff) {
				continue
			}
			// Compared as files, not paths: the temp directory has more than
			// one spelling on some systems (/var and /private/var on macOS).
			for _, k := range kept {
				if os.SameFile(info, k) {
					continue files
				}
			}
			_ = os.Remove(m)
		}
	}
}

// zipSignature is the local file header every zip archive starts with. FTL
// checks the same four bytes before it will import an upload.
var zipSignature = []byte("PK\x03\x04")

// openBackupStream requests a teleporter export and confirms the reply is a
// zip archive before anything is written to disk. done closes the response.
//
// Both callers used to copy whatever came back straight into a file and
// report success. An error reply therefore became a 70-byte "backup", and
// pihole_instance_sync kept one as its rollback point and went on to change
// the target. The status check in DoRaw stops the error replies; the
// signature check stops anything else that is not an archive, such as a
// login page from a reverse proxy, before a file exists.
func openBackupStream(ctx context.Context, c *pihole.Client) (archive io.Reader, done func(), err error) {
	resp, err := c.DoRaw(ctx, http.MethodGet, "/teleporter", nil) //nolint:bodyclose // closed below on failure, otherwise by the returned done
	if err != nil {
		return nil, nil, err
	}
	br := bufio.NewReader(resp.Body)
	head, err := br.Peek(len(zipSignature))
	if err != nil || !bytes.Equal(head, zipSignature) {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("pi-hole answered the backup request with something other than a zip archive "+
			"(HTTP %d, Content-Type %q), so nothing was saved", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return br, func() { _ = resp.Body.Close() }, nil
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
		mcp.WithDescription("Import a Pi-hole configuration backup from a zip archive, replacing the configuration, gravity tables or DHCP leases it holds. Backs up the current configuration first; mode=plan shows what the archive holds without importing."),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("Absolute path to the backup zip file.")),
		mcp.WithBoolean("config", mcp.Description("Import Pi-hole configuration (default true).")),
		mcp.WithBoolean("gravity", mcp.Description("Import gravity database tables (default true).")),
		mcp.WithBoolean("dhcp_leases", mcp.Description("Import DHCP leases (default true).")),
		mcp.WithString("mode", mcp.Enum("apply", "plan"), mcp.Description("'apply' (default) imports; 'plan' lists what the archive holds and what would be replaced, and changes nothing.")),
		mcp.WithBoolean("snapshot", mcp.Description("Export a backup of the current configuration before importing, as a rollback point (default true).")),
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

		archive, done, err := openBackupStream(ctx, c)
		if err != nil {
			return toolError("export backup", err), nil
		}
		defer done()

		// 0600, matching what os.CreateTemp gives the default path. The archive
		// holds the admin password's hash in pihole.toml, every DHCP lease and
		// the long-term query database; os.Create would have made it 0644 under
		// the usual umask, readable by every account on the host. The mode only
		// applies when the file is created, so a path the caller prepared with
		// its own permissions keeps them.
		var out *os.File
		if outputPath != "" {
			out, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // output_path is a user-provided MCP tool parameter, validated absolute above
		} else {
			out, err = os.CreateTemp("", "pihole-backup-*.zip")
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create backup file: %v", err)), nil
		}

		n, err := io.Copy(out, archive)
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			// A truncated archive is worse than none: it looks like a backup
			// until the day it is needed.
			_ = os.Remove(out.Name())
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save backup, so the partial file was removed: %v", err)), nil
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

		if req.GetString("mode", "apply") == "plan" {
			return importPlan(filePath, gravityArchivePath(ctx, c), importConfig, importGravity, importDHCP), nil
		}

		// An import replaces whole parts of the configuration and cannot be
		// undone through the API, so the current state is kept first.
		var snapshot string
		if req.GetBool("snapshot", true) {
			path, serr := exportSnapshot(ctx, c, filePath)
			if serr != nil {
				return mcp.NewToolResultError(withAdvice(toolErrorText("back up the current configuration before importing, so nothing was imported", serr),
					"To import without a rollback point, pass snapshot=false.")), nil
			}
			snapshot = path
		}

		sent := time.Now()
		var result pihole.TeleporterImportResponse
		if err := c.PostMultipart(ctx, "/teleporter", filePath, importOptions, &result); err != nil {
			text := toolErrorText("import backup", err)
			if snapshot != "" {
				text = withAdvice(text, "The configuration from before the import is backed up at "+snapshot+".")
			}
			return mcp.NewToolResultError(text), nil
		}

		imported := result.ImportedFiles()
		if len(imported) == 0 && (importConfig || importGravity || importDHCP) {
			text := "Pi-hole accepted the archive but reported importing nothing from it. " +
				"Check that the file is a Pi-hole v6 teleporter export and that the parts you asked for are in it."
			if snapshot != "" {
				// Said on every path after the backup exists: it holds the
				// password hash and the query database, and an unmentioned
				// file is one nobody deletes.
				text += " The backup taken before the import is at " + snapshot + "."
			}
			return mcp.NewToolResultError(text), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Import complete.** Pi-hole imported %d item(s):\n", len(imported))
		for _, item := range imported {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		if snapshot != "" {
			fmt.Fprintf(&b, "The configuration from before the import is backed up at %s; import it to roll back.\n", snapshot)
		}
		// FTL always restarts after an import (src/api/teleporter.c).
		b.WriteString(strings.TrimSpace(awaitRestartNote(ctx, c, sent)) + "\n")

		return mcp.NewToolResultText(b.String()), nil
	}
}

// gravityArchivePath is the archive entry FTL reads the gravity database
// from: its files.gravity setting without the leading slash
// (src/zip/teleporter.c), so a Pi-hole with the database elsewhere looks for
// a different entry. "" when the setting cannot be read.
func gravityArchivePath(ctx context.Context, c *pihole.Client) string {
	var resp pihole.ConfigResponse
	if err := c.Get(ctx, "/config/files/gravity", &resp); err != nil {
		return ""
	}
	files, _ := resp.Config["files"].(map[string]any)
	path, _ := files["gravity"].(string)
	return strings.TrimPrefix(path, "/")
}

// importPlan lists what an archive holds and what an import with the given
// options would replace, reading the archive locally and changing nothing.
// gravityEntry is the entry FTL takes the gravity database from on this
// Pi-hole; "" means it could not be read and the default is assumed.
func importPlan(filePath, gravityEntry string, config, gravity, dhcp bool) *mcp.CallToolResult {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s is not a readable zip archive: %v", filePath, err))
	}
	defer func() { _ = zr.Close() }()

	assumed := gravityEntry == ""
	if assumed {
		gravityEntry = "etc/pihole/gravity.db"
	}
	// The entries FTL imports, and the option that controls each
	// (src/zip/teleporter.c, v6.7 and v6.8).
	parts := map[string]struct {
		enabled bool
		option  string
		what    string
	}{
		"etc/pihole/pihole.toml": {config, "config", "Pi-hole configuration"},
		gravityEntry:             {gravity, "gravity", "lists, rules, groups and clients"},
		"etc/pihole/dhcp.leases": {dhcp, "dhcp_leases", "DHCP leases"},
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Import plan for %s.** Nothing has been changed.\n", filepath.Base(filePath))
	replaces := 0
	for _, f := range zr.File {
		part, known := parts[f.Name]
		switch {
		case !known:
			fmt.Fprintf(&b, "- %s: not imported\n", f.Name)
		case part.enabled:
			fmt.Fprintf(&b, "- %s: replaces the %s (%s)\n", f.Name, part.what, format.Bytes(float64(f.UncompressedSize64)))
			replaces++
		default:
			fmt.Fprintf(&b, "- %s: skipped, since %s=false\n", f.Name, part.option)
		}
	}
	if assumed {
		fmt.Fprintf(&b, "\nPi-hole's gravity database location (files.gravity) could not be read, so this assumes the default, %s.\n", gravityEntry)
	}
	// FTL restarts after every import, whatever it replaced
	// (src/api/teleporter.c).
	if replaces == 0 {
		b.WriteString("\nWith these options the import would replace nothing, but Pi-hole still restarts FTL after any import.")
		return mcp.NewToolResultText(b.String())
	}
	b.WriteString("\nImporting restarts FTL. Call again without mode=plan to import; the current configuration is backed up first unless snapshot=false.")
	return mcp.NewToolResultText(b.String())
}
