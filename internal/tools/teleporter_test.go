package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeleporterExport_Success(t *testing.T) {
	h := piholeRawHandler(
		nil,
		map[string]string{
			"/teleporter": "PK\x03\x04fakezipdata",
		},
	)
	c := newTestClient(t, h)

	text := callTool(t, teleporterExportHandler, c, nil)
	if !strings.Contains(text, "Backup saved") {
		t.Errorf("expected 'Backup saved' message, got: %s", text)
	}
	if !strings.Contains(text, "bytes") {
		t.Errorf("expected file size in output, got: %s", text)
	}

	// Export and import are the same path and differ only by method, and the
	// fake routes on path alone. A POST here would hand Pi-hole an empty
	// multipart body to import over a live configuration while the tool still
	// reported a saved backup.
	h.Only(t, "GET", "/teleporter").AssertNoQueryString(t)

	// Clean up the temp file created by the handler.
	// Extract the file path from the output (between "File: " and " (").
	if idx := strings.Index(text, "File: "); idx >= 0 {
		rest := text[idx+6:]
		if end := strings.Index(rest, " ("); end >= 0 {
			_ = os.Remove(rest[:end])
		}
	}
}

func TestTeleporterImport_MissingFile(t *testing.T) {
	h := piholeHandler(map[string]any{})
	c := newTestClient(t, h)

	text := callToolExpectError(t, teleporterImportHandler, c, map[string]any{
		"file_path": "/nonexistent/path/to/backup.zip",
	})
	if !strings.Contains(text, "Failed to import") {
		t.Errorf("expected import failure message, got: %s", text)
	}

	// The multipart body is assembled before the client authenticates, so an
	// unreadable file must cost nothing on the wire. Pi-hole caps concurrent
	// API sessions (16 by default), and a login spent on a request that was
	// always going to fail locally takes a seat from a real client. Checked
	// against every recorded request, login included.
	for _, sent := range h.AllRequests() {
		t.Errorf("a file that cannot be opened must not reach the API, but this request was sent: %s", sent.Describe())
	}
}

func TestTeleporterImport_SendsTheBackupAsMultipart(t *testing.T) {
	// The import options travel as a JSON blob inside a multipart field, which
	// no assertion on the rendered result can see. Three separate booleans are
	// funnelled through it, so they are passed here with three different
	// meanings: crossing any two of them would restore the wrong half of a
	// backup and still report "Import complete".
	zipBytes := []byte("PK\x03\x04fakezipdata")
	backup := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backup, zipBytes, 0o600); err != nil {
		t.Fatalf("writing test backup: %v", err)
	}

	h := piholeHandler(map[string]any{
		"/teleporter": map[string]any{
			"processed": []any{"config", "gravity"},
			"took":      0.1,
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, teleporterImportHandler, c, map[string]any{
		"file_path":   backup,
		"config":      false,
		"gravity":     true,
		"dhcp_leases": false,
	})
	if !strings.Contains(text, "Import complete") {
		t.Errorf("expected import confirmation, got: %s", text)
	}

	req := h.Only(t, "POST", "/teleporter")
	req.AssertNoQueryString(t)

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type %q is not parseable: %v", req.Header.Get("Content-Type"), err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type is %q, want multipart/form-data", mediaType)
	}
	form, err := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("multipart body does not parse: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	// Pi-hole reads the archive from the "file" field. Under any other field
	// name FTL answers 400 rather than importing, so the field name is part of
	// the contract, not an implementation detail.
	files := form.File["file"]
	if len(files) != 1 {
		t.Fatalf("multipart body has %d part(s) under field \"file\", want 1 (file fields present: %v, value fields present: %v)",
			len(files), formFieldNames(form.File), formFieldNames(form.Value))
	}
	if got := files[0].Filename; got != "backup.zip" {
		t.Errorf("file part filename is %q, want %q", got, "backup.zip")
	}
	part, err := files[0].Open()
	if err != nil {
		t.Fatalf("opening the uploaded file part: %v", err)
	}
	defer func() { _ = part.Close() }()
	got, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("reading the uploaded file part: %v", err)
	}
	if !bytes.Equal(got, zipBytes) {
		t.Errorf("uploaded archive is %q, want the file's own bytes %q", got, zipBytes)
	}

	opts := form.Value["import"]
	if len(opts) != 1 {
		t.Fatalf("multipart body has %d \"import\" field(s), want 1 (value fields present: %v)",
			len(opts), formFieldNames(form.Value))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(opts[0]), &decoded); err != nil {
		t.Fatalf("import options field is not JSON: %v (raw: %s)", err, opts[0])
	}
	if decoded["config"] != false {
		t.Errorf("import options config is %#v, want false (raw: %s)", decoded["config"], opts[0])
	}
	if decoded["dhcp_leases"] != false {
		t.Errorf("import options dhcp_leases is %#v, want false (raw: %s)", decoded["dhcp_leases"], opts[0])
	}
	gravity, ok := decoded["gravity"].(map[string]any)
	if !ok {
		t.Fatalf("import options gravity is %#v, want an object of per-table flags (raw: %s)", decoded["gravity"], opts[0])
	}
	// One tool argument expands into seven gravity tables. Restoring only some
	// of them leaves the database internally inconsistent: adlists without
	// their group memberships, clients without theirs.
	for _, table := range []string{"group", "adlist", "adlist_by_group", "domainlist", "domainlist_by_group", "client", "client_by_group"} {
		if gravity[table] != true {
			t.Errorf("import options gravity.%s is %#v, want true (raw: %s)", table, gravity[table], opts[0])
		}
	}
}

// formFieldNames lists the field names in a parsed multipart form, so a failure
// about a missing field also says what was actually sent.
func formFieldNames[T any](fields map[string][]T) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}
