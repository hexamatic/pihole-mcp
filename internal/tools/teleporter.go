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
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

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
		mcp.WithDescription("Export a full Pi-hole configuration backup as a zip archive to a new file on the server's host, and return its path and size. It never replaces an existing file. The file persists after the call and is the caller's to move or delete; under Docker it is written inside the container unless output_path points at a mounted volume."),
		mcp.WithString("output_path", mcp.Description("Absolute path for the new .zip file, which must not exist yet (inside PIHOLE_BACKUP_DIR when that is set). Defaults to a new file in PIHOLE_BACKUP_DIR or the system temp directory.")),
		// It writes a file on the server's host, so it is not read-only, and
		// it is not offered in read-only mode. It replaces nothing, so it is
		// not destructive either.
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
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
		// Checked before Pi-hole is asked for anything: a backup with nowhere
		// valid to land should not be fetched at all.
		if err := checkOutputPath(outputPath); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		reapStaleBackups(outputPath)

		archive, done, err := openBackupStream(ctx, c)
		if err != nil {
			return toolError("export backup", err), nil
		}
		defer done()

		// 0600: the archive holds the admin password's hash, every DHCP lease
		// and the long-term query database.
		out, err := createBackupFile(outputPath, "backup")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create the backup file, so nothing was saved: %v", err)), nil
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
		archiveFile, archiveInfo, err := openBackupForImport(filePath)
		if err != nil {
			return mcp.NewToolResultError("Invalid file_path: " + err.Error()), nil
		}
		defer func() { _ = archiveFile.Close() }()

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
			return importPlan(archiveFile, archiveInfo, gravityArchivePath(ctx, c), importConfig, importGravity, importDHCP), nil
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
		if err := c.PostMultipartReader(ctx, "/teleporter", archiveInfo.Name(), archiveFile, importOptions, &result); err != nil {
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
			fmt.Fprintf(&b, "- %s\n", format.Untrusted(item))
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

// maxPlanTOML bounds how much of an archive's pihole.toml the plan reads.
// FTL writes about 60 KB, comments included.
const maxPlanTOML = 4 << 20

// archiveGravityPath reads files.gravity from an archive's pihole.toml, as
// FTL writes it: a gravity key under a [files] table, or a dotted
// files.gravity key at the top. ok is false when the setting is absent,
// which leaves FTL's current one in force, or is written some other way.
func archiveGravityPath(f *zip.File) (path string, ok bool) {
	r, err := f.Open()
	if err != nil {
		return "", false
	}
	defer func() { _ = r.Close() }()
	table := ""
	sc := bufio.NewScanner(io.LimitReader(r, maxPlanTOML))
	sc.Buffer(make([]byte, 0, 64*1024), maxPlanTOML)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			table = ""
			if end := strings.IndexByte(line, ']'); end > 0 {
				table = strings.TrimSpace(line[1:end])
			}
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if (table == "files" && key == "gravity") || (table == "" && key == "files.gravity") {
			return tomlString(strings.TrimSpace(value))
		}
	}
	return "", false
}

// tomlString reads a TOML string without escapes at the start of value,
// ignoring a trailing comment.
func tomlString(value string) (string, bool) {
	if value == "" || (value[0] != '"' && value[0] != '\'') {
		return "", false
	}
	end := strings.IndexByte(value[1:], value[0])
	if end < 0 {
		return "", false
	}
	s := value[1 : end+1]
	if value[0] == '"' && strings.ContainsRune(s, '\\') {
		return "", false
	}
	return s, s != ""
}

// importPlan lists what an archive holds and what an import with the given
// options would replace, reading the archive locally and changing nothing.
// gravityEntry is the entry FTL takes the gravity database from on this
// Pi-hole; "" means it could not be read and the default is assumed.
func importPlan(archive *os.File, info os.FileInfo, gravityEntry string, config, gravity, dhcp bool) *mcp.CallToolResult {
	zr, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s is not a readable zip archive: %v", archive.Name(), err))
	}

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
	fmt.Fprintf(&b, "**Import plan for %s.** Nothing has been changed.\n", info.Name())
	replaces := 0
	moved := ""
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
		// FTL works out the gravity entry afresh for each entry, from the
		// configuration in force, and an imported pihole.toml replaces that
		// configuration part-way through. Entries after it are matched
		// against the archive's own files.gravity (src/zip/teleporter.c).
		if f.Name == "etc/pihole/pihole.toml" && config {
			if path, ok := archiveGravityPath(f); ok {
				entry := strings.TrimPrefix(path, "/")
				if _, taken := parts[entry]; !taken {
					gp := parts[gravityEntry]
					delete(parts, gravityEntry)
					parts[entry] = gp
					gravityEntry, moved = entry, path
				}
			}
		}
	}
	if assumed {
		b.WriteString("\nPi-hole's gravity database location (files.gravity) could not be read, so this assumes the default, etc/pihole/gravity.db.\n")
	}
	if moved != "" {
		fmt.Fprintf(&b, "\nThe archive's configuration keeps the gravity database at %s, so entries after etc/pihole/pihole.toml are matched against %s.\n", format.CodeSpan(moved), format.CodeSpan(gravityEntry))
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
