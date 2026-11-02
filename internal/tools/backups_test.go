package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withBackupDir confines backups to a fresh directory for one test.
func withBackupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetBackupDir(dir)
	t.Cleanup(func() { SetBackupDir("") })
	return dir
}

func exportClient(t *testing.T) *recorder {
	t.Helper()
	return piholeRawHandler(nil, map[string]string{"/teleporter": "PK\x03\x04fakezipdata"})
}

func TestTeleporterExport_NeverReplacesAFile(t *testing.T) {
	// The export opened output_path with O_TRUNC, so it could replace any
	// file the server could write.
	dir := t.TempDir()
	existing := filepath.Join(dir, "keep.zip")
	if err := os.WriteFile(existing, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, exportClient(t))
	text := callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": existing})
	if !strings.Contains(text, "already exists") {
		t.Errorf("expected a refusal, got: %s", text)
	}
	if data, _ := os.ReadFile(existing); string(data) != "precious" { //nolint:gosec // test fixture
		t.Errorf("the existing file was changed to %q", data)
	}

	// A symlink at the path is refused too, rather than followed.
	target := filepath.Join(dir, "elsewhere.txt")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.zip")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": link})
	if data, _ := os.ReadFile(target); string(data) != "untouched" { //nolint:gosec // test fixture
		t.Errorf("the symlink's target was changed to %q", data)
	}
}

func TestTeleporterExport_OutputPathMustBeAZip(t *testing.T) {
	rec := exportClient(t)
	msg := callToolExpectError(t, teleporterExportHandler, newTestClient(t, rec),
		map[string]any{"output_path": filepath.Join(t.TempDir(), "notes.txt")})
	if !strings.Contains(msg, ".zip") {
		t.Errorf("expected a .zip error, got: %s", msg)
	}
	rec.AssertNone(t, "", "")
}

func TestBackupDir_ConfinesExportsAndImports(t *testing.T) {
	dir := withBackupDir(t)
	c := newTestClient(t, exportClient(t))

	// The default backup lands in the backup directory.
	text := callTool(t, teleporterExportHandler, c, nil)
	if !strings.Contains(text, dir) {
		t.Errorf("expected the backup in %s, got: %s", dir, text)
	}

	// A path outside it is refused before anything is fetched or written.
	outside := filepath.Join(t.TempDir(), "out.zip")
	msg := callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": outside})
	if !strings.Contains(msg, "inside PIHOLE_BACKUP_DIR") {
		t.Errorf("expected the path to be refused, got: %s", msg)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("a file was written outside the backup directory: %v", err)
	}
	msg = callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": filepath.Join(dir, "..", "escape.zip")})
	if !strings.Contains(msg, "inside PIHOLE_BACKUP_DIR") {
		t.Errorf("expected .. to be refused, got: %s", msg)
	}

	// A symlink inside the directory that leads out of it is refused.
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(dir, "sub")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	callToolExpectError(t, teleporterExportHandler, c, map[string]any{"output_path": filepath.Join(dir, "sub", "out.zip")})
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("a file was written through the symlink: %v", entries)
	}

	// Imports are confined the same way.
	archive := writeArchive(t, t.TempDir(), "backup-*.zip", "etc/pihole/pihole.toml")
	msg = callToolExpectError(t, teleporterImportHandler, c, map[string]any{"file_path": archive, "mode": "plan"})
	if !strings.Contains(msg, "inside PIHOLE_BACKUP_DIR") {
		t.Errorf("expected an import from outside to be refused, got: %s", msg)
	}
}

func TestReapStaleBackups_StaysInADirectoryWithPatternCharacters(t *testing.T) {
	// The clean-up globbed the directory name with the pattern, so a
	// directory called "backups[1]" made it search "backups1" instead.
	parent := t.TempDir()
	dir := filepath.Join(parent, "backups[1]")
	sibling := filepath.Join(parent, "backups1")
	for _, d := range []string{dir, sibling} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	SetBackupDir(dir)
	t.Cleanup(func() { SetBackupDir("") })

	inside := filepath.Join(dir, "pihole-mcp-backup-111.zip")
	outside := filepath.Join(sibling, "pihole-mcp-backup-222.zip")
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{inside, outside} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	reapStaleBackups()
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Errorf("the stale backup inside the directory survived: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a file outside the backup directory was removed: %v", err)
	}
}

func TestTeleporterExport_KeepsADayOldFileAtOutputPath(t *testing.T) {
	// The clean-up ran before the existing-file check, so a day-old file at
	// output_path with a name the server generates was deleted first.
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	target := filepath.Join(dir, "pihole-mcp-backup-123.zip")
	if err := os.WriteFile(target, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}
	rec := exportClient(t)
	callToolExpectError(t, teleporterExportHandler, newTestClient(t, rec), map[string]any{"output_path": target})
	if data, _ := os.ReadFile(target); string(data) != "precious" { //nolint:gosec // test fixture
		t.Errorf("the file at output_path is now %q", data)
	}
	rec.AssertNone(t, "", "")
}

func TestTeleporterExport_RefusesAPathThatIsNotWrittenPlainly(t *testing.T) {
	// With ".." after a missing directory, the existence check and the
	// clean-up saw a path that did not resolve while the open saw it
	// cleaned, so a day-old backup at the cleaned path was replaced.
	dir := withBackupDir(t)
	existing := filepath.Join(dir, "pihole-mcp-backup-123.zip")
	if err := os.WriteFile(existing, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(existing, old, old); err != nil {
		t.Fatal(err)
	}
	rec := exportClient(t)
	msg := callToolExpectError(t, teleporterExportHandler, newTestClient(t, rec),
		map[string]any{"output_path": dir + "/missing/../pihole-mcp-backup-123.zip"})
	if !strings.Contains(msg, "written plainly") {
		t.Errorf("expected the path to be refused, got: %s", msg)
	}
	if data, _ := os.ReadFile(existing); string(data) != "precious" { //nolint:gosec // test fixture
		t.Errorf("the existing backup is now %q", data)
	}
	rec.AssertNone(t, "", "")
}
