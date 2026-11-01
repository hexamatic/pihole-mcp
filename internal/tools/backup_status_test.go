package tools

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// The backup path used to accept any reply. An expired session, a refusal or a
// reverse proxy's login page all came back as "Backup saved", and
// pihole_instance_sync kept the same bytes as its rollback point and went on to
// change the target. These tests pin each way a backup can fail.

func TestTeleporterExport_ErrorReplyIsAnErrorAndWritesNothing(t *testing.T) {
	c := newTestClient(t, piholeErrorServer(http.StatusInternalServerError, "server_error", "Unable to create archive", ""))
	out := filepath.Join(t.TempDir(), "backup.zip")

	text := callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": out})
	if !strings.Contains(text, "Unable to create archive") {
		t.Errorf("error should carry Pi-hole's message, got: %s", text)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a failed export left a file at output_path (stat err %v)", err)
	}
}

func TestTeleporterExport_NonZipReplyIsAnErrorAndWritesNothing(t *testing.T) {
	// A reverse proxy in front of Pi-hole answering 200 with its own login page.
	c := newTestClient(t, piholeRawHandler(nil, map[string]string{
		"/teleporter": "<html><body>Please sign in</body></html>",
	}))
	out := filepath.Join(t.TempDir(), "backup.zip")

	text := callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": out})
	if !strings.Contains(text, "not a zip archive") && !strings.Contains(text, "other than a zip archive") {
		t.Errorf("error should say the reply was not an archive, got: %s", text)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a non-archive reply was written to output_path (stat err %v)", err)
	}
}

func TestTeleporterExport_AppPasswordRefusalExplainsAppSudo(t *testing.T) {
	// FTL v6.8 refuses an export to an application-password session unless
	// webserver.api.app_sudo is on, with this exact message and hint.
	c := newTestClient(t, piholeErrorServer(http.StatusForbidden, "forbidden", "Unable to export configuration",
		"The current app session is not allowed to export Pi-hole config settings (webserver.api.app_sudo is false)"))

	text := callToolExpectError(t, teleporterExportHandler, c, map[string]any{
		"output_path": filepath.Join(t.TempDir(), "backup.zip"),
	})
	for _, want := range []string{"application password", "webserver.api.app_sudo", "admin password"} {
		if !strings.Contains(text, want) {
			t.Errorf("app_sudo refusal should mention %q, got: %s", want, text)
		}
	}
}

func TestTeleporterExport_LogsInAgainAfterAnExpiredSession(t *testing.T) {
	// FTL forgets sessions when it restarts. The first export after that gets a
	// 401, which used to be saved to disk as the backup.
	var logins atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			n := logins.Add(1)
			writeTestJSON(w, map[string]any{"session": map[string]any{"valid": true, "sid": "sid-" + strconv.Itoa(int(n))}})
			return
		}
		if r.Header.Get("X-FTL-SID") == "sid-1" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"key":"unauthorized","message":"Unauthorized"}}`))
			return
		}
		_, _ = w.Write([]byte("PK\x03\x04archive"))
	})
	c := newTestClient(t, h)
	out := filepath.Join(t.TempDir(), "backup.zip")

	callTool(t, teleporterExportHandler, c, map[string]any{"output_path": out})
	got, err := os.ReadFile(out) //nolint:gosec // built from t.TempDir()
	if err != nil {
		t.Fatalf("reading the backup: %v", err)
	}
	if !bytes.HasPrefix(got, zipSignature) {
		t.Errorf("backup = %q, want the archive from the replayed request", got)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logins = %d, want 2 (the original and one after the 401)", n)
	}
}

func TestTeleporterImport_ListsTheFilesPiholeReports(t *testing.T) {
	backup := writeTestBackup(t)
	c := newTestClient(t, piholeHandler(map[string]any{
		"/teleporter": map[string]any{
			"files": []any{"etc/pihole/pihole.toml", "gravity.db->adlist"},
			"took":  0.2,
		},
		"/info/ftl": map[string]any{"ftl": map[string]any{"uptime": 0}},
	}))

	text := callTool(t, teleporterImportHandler, c, map[string]any{"file_path": backup, "snapshot": false})
	for _, want := range []string{"etc/pihole/pihole.toml", "gravity.db->adlist", "2 item", "restarted to apply it"} {
		if !strings.Contains(text, want) {
			t.Errorf("import result should list %q, got: %s", want, text)
		}
	}
}

func TestTeleporterImport_NothingImportedIsAnError(t *testing.T) {
	backup := writeTestBackup(t)
	c := newTestClient(t, piholeHandler(map[string]any{
		"/teleporter": map[string]any{"files": []any{}, "took": 0.1},
	}))

	text := callToolExpectError(t, teleporterImportHandler, c, map[string]any{"file_path": backup, "snapshot": false})
	if !strings.Contains(text, "importing nothing") {
		t.Errorf("an empty import should say nothing was imported, got: %s", text)
	}
}

func writeTestBackup(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(path, []byte("PK\x03\x04fakezipdata"), 0o600); err != nil {
		t.Fatalf("writing test backup: %v", err)
	}
	return path
}

// failingExport wraps a fake so its teleporter export answers with status
// while every other endpoint behaves normally.
func failingExport(f *piholefake.Fake, status int) http.Handler {
	inner := f.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/teleporter" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"key":"server_error","message":"Unable to create archive"}}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func TestSync_SnapshotFailureChangesNothing(t *testing.T) {
	src := piholefake.New()
	t.Cleanup(src.Close)
	tgt := piholefake.New()
	t.Cleanup(tgt.Close)
	seedSource(src)

	tgtSrv := httptest.NewServer(failingExport(tgt, http.StatusInternalServerError))
	t.Cleanup(tgtSrv.Close)
	reg := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: src.URL(), Password: "x"},
		{Name: "secondary", URL: tgtSrv.URL, Password: "x"},
	})
	t.Cleanup(reg.Close)

	plan := syncOutput(t, reg, map[string]any{"target": "secondary"})
	res := callRaw(t, instanceSyncHandler(reg), map[string]any{
		"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken,
	})
	if !res.IsError {
		t.Fatalf("apply went ahead after the rollback snapshot failed: %s", resultText(res))
	}
	if text := resultText(res); !strings.Contains(text, "nothing was changed") || !strings.Contains(text, "snapshot=false") {
		t.Errorf("snapshot failure should say nothing changed and how to proceed, got: %s", text)
	}
	if tgt.DomainCount() != 0 || tgt.ListCount() != 0 || tgt.GroupCount() != 0 || len(tgt.Hosts()) != 0 {
		t.Errorf("target was changed after a failed snapshot: domains=%d lists=%d groups=%d hosts=%v",
			tgt.DomainCount(), tgt.ListCount(), tgt.GroupCount(), tgt.Hosts())
	}
}

func TestSync_DefaultSnapshotIsARealArchive(t *testing.T) {
	// Every other sync test passes snapshot=false, so the default path, the one
	// users get, went untested.
	reg, f1, f2 := twoFakeRegistry(t)
	seedSource(f1)

	plan := syncOutput(t, reg, map[string]any{"target": "secondary"})
	applied := syncOutput(t, reg, map[string]any{
		"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken,
	})
	if applied.SnapshotPath == "" {
		t.Fatal("apply with the default snapshot setting recorded no snapshot")
	}
	t.Cleanup(func() { _ = os.Remove(applied.SnapshotPath) })

	got, err := os.ReadFile(applied.SnapshotPath)
	if err != nil {
		t.Fatalf("reading the snapshot: %v", err)
	}
	if !bytes.HasPrefix(got, zipSignature) {
		t.Errorf("snapshot does not start with a zip header: %q", got)
	}
	if applied.Failed != 0 || f2.DomainCount() != 1 {
		t.Errorf("apply after a good snapshot: failed=%d target domains=%d", applied.Failed, f2.DomainCount())
	}
}

func TestTeleporterImport_BacksUpTheCurrentConfigurationFirst(t *testing.T) {
	// An import replaces whole parts of the configuration with no way back
	// through the API, so the current state is exported before it.
	f := piholefake.New()
	t.Cleanup(f.Close)
	rec := wiretest.Proxy(f.Handler())
	c := newTestClient(t, rec)

	text := callTool(t, teleporterImportHandler, c, map[string]any{"file_path": writeTestBackup(t)})
	_, after, ok := strings.Cut(text, "backed up at ")
	if !ok {
		t.Fatalf("expected the reply to give the backup's path, got: %s", text)
	}
	path, _, _ := strings.Cut(after, ";")
	t.Cleanup(func() { _ = os.Remove(path) })
	data, err := os.ReadFile(path) //nolint:gosec // a path this test just received from the tool
	if err != nil || !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		t.Fatalf("backup at %q is not a zip archive (%v)", path, err)
	}

	var order []string
	for _, r := range rec.Select("", "/teleporter") {
		order = append(order, r.Method)
	}
	if strings.Join(order, ",") != "GET,POST" {
		t.Errorf("teleporter requests = %v, want the export before the import", order)
	}
}

func TestTeleporterImport_AFailedBackupImportsNothing(t *testing.T) {
	f := piholefake.New()
	t.Cleanup(f.Close)
	rec := wiretest.Proxy(failingExport(f, http.StatusInternalServerError))
	c := newTestClient(t, rec)

	text := callToolExpectError(t, teleporterImportHandler, c, map[string]any{"file_path": writeTestBackup(t)})
	if !strings.Contains(text, "so nothing was imported") || !strings.Contains(text, ". To import without a rollback point, pass snapshot=false.") {
		t.Errorf("expected the failed backup to stop the import and say how to skip it, got: %s", text)
	}
	rec.AssertNone(t, "POST", "/teleporter")
}

func TestTeleporterImport_PlanListsWhatWouldBeReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.zip")
	out, err := os.Create(path) //nolint:gosec // a path inside the test's own temp dir
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, name := range []string{"etc/pihole/pihole.toml", "etc/pihole/gravity.db", "etc/pihole/dhcp.leases", "etc/hosts"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("data"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()

	rec := piholeHandler(map[string]any{
		"/config/files/gravity": map[string]any{"config": map[string]any{"files": map[string]any{"gravity": "/etc/pihole/gravity.db"}}},
	})
	text := callTool(t, teleporterImportHandler, newTestClient(t, rec), map[string]any{
		"file_path": path, "mode": "plan", "dhcp_leases": false,
	})
	for _, want := range []string{
		"Nothing has been changed",
		"etc/pihole/pihole.toml: replaces the Pi-hole configuration",
		"etc/pihole/gravity.db: replaces the lists, rules, groups and clients",
		"etc/pihole/dhcp.leases: skipped, since dhcp_leases=false",
		"etc/hosts: not imported",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the plan, got:\n%s", want, text)
		}
	}
	// The archive is read locally; the only request is for where this
	// Pi-hole keeps its gravity database.
	rec.Only(t, "GET", "/config/files/gravity")
	if n := len(rec.Requests()); n != 1 {
		t.Errorf("a plan changes nothing, but %d request(s) reached Pi-hole:\n%s", n, rec.Dump())
	}
}

// writeArchive writes a zip holding the named entries and returns its path.
func writeArchive(t *testing.T, dir string, pattern string, names ...string) string {
	t.Helper()
	out, err := os.CreateTemp(dir, pattern)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, name := range names {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("data"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	return out.Name()
}

func TestTeleporterImport_PlanFollowsThePiholesGravityPath(t *testing.T) {
	// FTL takes the gravity database from the archive entry named by its own
	// files.gravity setting, so a Pi-hole keeping it elsewhere imports a
	// different entry from the default one.
	path := writeArchive(t, t.TempDir(), "backup-*.zip", "data/gravity.db", "etc/pihole/gravity.db")
	rec := piholeHandler(map[string]any{
		"/config/files/gravity": map[string]any{"config": map[string]any{"files": map[string]any{"gravity": "/data/gravity.db"}}},
	})
	text := callTool(t, teleporterImportHandler, newTestClient(t, rec), map[string]any{
		"file_path": path, "mode": "plan", "config": false, "dhcp_leases": false,
	})
	if !strings.Contains(text, "data/gravity.db: replaces the lists") || !strings.Contains(text, "etc/pihole/gravity.db: not imported") {
		t.Errorf("expected the plan to follow files.gravity, got:\n%s", text)
	}
}

func TestTeleporterImport_PlanSaysWhenItAssumesTheGravityPath(t *testing.T) {
	path := writeArchive(t, t.TempDir(), "backup-*.zip", "etc/pihole/gravity.db")
	text := callTool(t, teleporterImportHandler, newTestClient(t, piholeHandler(nil)), map[string]any{"file_path": path, "mode": "plan"})
	if !strings.Contains(text, "could not be read, so this assumes the default") {
		t.Errorf("expected the plan to say it assumed the default path, got:\n%s", text)
	}
}

func TestTeleporterImport_PlanWarnsOfTheRestartEvenWhenNothingIsReplaced(t *testing.T) {
	// FTL restarts after every import, whatever it replaced.
	path := writeArchive(t, t.TempDir(), "backup-*.zip", "etc/hosts")
	text := callTool(t, teleporterImportHandler, newTestClient(t, piholeHandler(nil)), map[string]any{"file_path": path, "mode": "plan"})
	if !strings.Contains(text, "replace nothing, but Pi-hole still restarts FTL") {
		t.Errorf("expected a restart warning, got:\n%s", text)
	}
}

func TestTeleporterImport_KeepsAnOldExportItIsImporting(t *testing.T) {
	// The backup taken before an import clears day-old backups from the temp
	// directory, and used to delete the very export being imported.
	// Named as the server names its own exports, so the clean-up would take it.
	input := writeArchive(t, os.TempDir(), "pihole-mcp-backup-*.zip", "etc/pihole/pihole.toml")
	t.Cleanup(func() { _ = os.Remove(input) })
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(input, old, old); err != nil {
		t.Fatal(err)
	}
	f := piholefake.New()
	t.Cleanup(f.Close)

	text := callTool(t, teleporterImportHandler, newTestClient(t, f.Handler()), map[string]any{"file_path": input})
	if _, err := os.Stat(input); err != nil {
		t.Errorf("the archive being imported was deleted: %v", err)
	}
	if _, after, ok := strings.Cut(text, "backed up at "); ok {
		snapshot, _, _ := strings.Cut(after, ";")
		_ = os.Remove(snapshot)
	}
}

func TestTeleporterImport_NothingImportedStillNamesTheBackup(t *testing.T) {
	// The backup holds the password hash and the query database, and an
	// error that did not mention it left a file nobody would delete.
	f := piholefake.New()
	t.Cleanup(f.Close)
	inner := f.Handler()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/teleporter" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[],"took":0.1}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	text := callToolExpectError(t, teleporterImportHandler, newTestClient(t, h), map[string]any{"file_path": writeTestBackup(t)})
	_, after, ok := strings.Cut(text, "The backup taken before the import is at ")
	if !ok {
		t.Fatalf("expected the error to name the backup, got: %s", text)
	}
	_ = os.Remove(strings.TrimSuffix(after, "."))
}
