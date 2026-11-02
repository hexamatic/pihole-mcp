package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// A teleporter archive holds the admin password's hash, every DHCP lease and
// the long-term query database, and these files are written and read on the
// host this server runs on, which may not be the caller's. This file decides
// where backups go and how they are opened.
//
// pihole_teleporter_export used to open any absolute path with O_TRUNC, so a
// call could replace any file the server could write, and it was marked
// read-only, so read-only mode offered it. Backups are now always new files:
// an existing file, a symlink included, is refused rather than overwritten.
// PIHOLE_BACKUP_DIR confines every backup the server writes or reads to one
// directory, opened with os.OpenRoot so neither ".." nor a symlink leads out
// of it.

// backupDir is PIHOLE_BACKUP_DIR, or "" when backups may go anywhere.
var backupDir atomic.Value

// SetBackupDir confines the backups the tools write and read to dir. An
// empty dir lifts the confinement: default backups then go to the system
// temp directory, and output_path and file_path may be any absolute path.
func SetBackupDir(dir string) {
	backupDir.Store(dir)
}

func currentBackupDir() string {
	dir, _ := backupDir.Load().(string)
	return dir
}

// backupFilePrefix starts the name of every backup this server creates under
// a name of its own choosing. reapStaleBackups deletes only files that carry
// it followed by os.CreateTemp's random number, so it cannot remove a file
// someone named themselves.
const backupFilePrefix = "pihole-mcp-"

// staleBackupAge is how long a default export or rollback snapshot is kept
// in the system temp directory before it is reaped, a safety net against an
// unbounded temp directory. The export says so when it writes one there.
const staleBackupAge = 24 * time.Hour

// createBackupFile creates a new file for a backup and returns it open for
// writing, mode 0600. outputPath, when given, must be absolute, end in .zip,
// not exist yet, and lie inside PIHOLE_BACKUP_DIR when that is set. Without
// it the file gets a fresh name made from kind ("backup", or an instance's
// snapshot), in PIHOLE_BACKUP_DIR or the system temp directory.
func createBackupFile(outputPath, kind string) (*os.File, error) {
	dir := currentBackupDir()
	if outputPath == "" {
		if dir == "" {
			dir = os.TempDir()
		}
		return os.CreateTemp(dir, backupFilePrefix+kind+"-*.zip")
	}
	if err := checkOutputPath(outputPath); err != nil {
		return nil, err
	}
	// O_EXCL refuses an existing file, and an existing symlink to one, so a
	// backup can never replace a file that is already there.
	const flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if dir == "" {
		f, err := os.OpenFile(outputPath, flags, 0o600) //nolint:gosec // output_path is the caller's choice of a new file; see above
		if os.IsExist(err) {
			return nil, fmt.Errorf("%s already exists, and a backup never replaces a file; choose a new name", outputPath)
		}
		return f, err
	}
	root, rel, err := openInBackupDir(dir, outputPath, "output_path")
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(rel, flags, 0o600)
	if os.IsExist(err) {
		return nil, fmt.Errorf("%s already exists, and a backup never replaces a file; choose a new name", outputPath)
	}
	if err != nil {
		return nil, fmt.Errorf("output_path: %w", err)
	}
	return f, nil
}

// checkOutputPath rejects an output_path that createBackupFile would refuse
// for its form alone: not absolute, not a .zip, or outside PIHOLE_BACKUP_DIR.
// "" is the default and passes.
func checkOutputPath(outputPath string) error {
	if outputPath == "" {
		return nil
	}
	if !filepath.IsAbs(outputPath) {
		return fmt.Errorf("output_path: must be an absolute path")
	}
	// The existence check, the clean-up's keep list and the open must all
	// see one path. With ".." after a missing directory the first two saw a
	// path that did not resolve while the open saw it cleaned, and a day-old
	// backup at the cleaned path was deleted and replaced.
	if filepath.Clean(outputPath) != outputPath {
		return fmt.Errorf("output_path: must be written plainly, without \"..\", \".\" or doubled separators (as %s)", filepath.Clean(outputPath))
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".zip") {
		return fmt.Errorf("output_path: must end in .zip")
	}
	if dir := currentBackupDir(); dir != "" {
		if _, err := relInBackupDir(dir, outputPath, "output_path"); err != nil {
			return err
		}
	}
	// Refused before anything is fetched or cleaned up. The O_EXCL open
	// refuses it again, but by then the clean-up of old backups had already
	// deleted a day-old file at this path if its name was one the server
	// generates.
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("%s already exists, and a backup never replaces a file; choose a new name", outputPath)
	}
	return nil
}

// openBackupForImport opens an archive to import and returns it with its
// details, read from the open file so they describe what is uploaded.
// filePath must be absolute, end in .zip, be a regular file within FTL's size
// limit, and lie inside PIHOLE_BACKUP_DIR when that is set.
func openBackupForImport(filePath string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(filePath) {
		return nil, nil, fmt.Errorf("must be an absolute path")
	}
	if !strings.EqualFold(filepath.Ext(filePath), ".zip") {
		return nil, nil, fmt.Errorf("must be a .zip file")
	}
	// O_NONBLOCK: opening a FIFO for reading otherwise waits for a writer,
	// so a pipe named backup.zip held the call, and a tool worker, forever,
	// before the regular-file check below could refuse it. On a regular file
	// it changes nothing.
	const flags = os.O_RDONLY | syscall.O_NONBLOCK
	var f *os.File
	var err error
	if dir := currentBackupDir(); dir == "" {
		f, err = os.OpenFile(filePath, flags, 0) //nolint:gosec // file_path is the caller's choice of archive; checked below
	} else {
		root, rel, rerr := openInBackupDir(dir, filePath, "file_path")
		if rerr != nil {
			return nil, nil, rerr
		}
		f, err = root.OpenFile(rel, flags, 0)
		_ = root.Close()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open file: %w", err)
	}
	info, err := f.Stat()
	switch {
	case err != nil:
		err = fmt.Errorf("cannot read file details: %w", err)
	case !info.Mode().IsRegular():
		err = fmt.Errorf("must be a regular file")
	case info.Size() > maxTeleporterFileSize:
		err = fmt.Errorf("must be at most %d bytes (got %d)", maxTeleporterFileSize, info.Size())
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// openInBackupDir opens dir as a root and returns path relative to it, or an
// error naming param when path lies outside it.
func openInBackupDir(dir, path, param string) (*os.Root, string, error) {
	rel, err := relInBackupDir(dir, path, param)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open PIHOLE_BACKUP_DIR: %w", err)
	}
	return root, rel, nil
}

// relInBackupDir returns path relative to dir, or an error naming param when
// path lies outside it. os.OpenRoot then refuses a symlink that leads out.
func relInBackupDir(dir, path, param string) (string, error) {
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s must be inside PIHOLE_BACKUP_DIR (%s)", param, dir)
	}
	return rel, nil
}

// reapStaleBackups deletes the backups this server created under a name of
// its own choosing in the system temp directory once they are older than
// staleBackupAge. Best-effort: a failure on one file is skipped rather than
// failing the caller, since this runs ahead of every new backup and a full
// disk is a worse failure to compound.
//
// It does nothing when PIHOLE_BACKUP_DIR is set. That is a directory someone
// chose to keep backups in, and cleaning it up deleted last week's backup
// when this week's was taken.
//
// Only names this server generates are touched: backupFilePrefix, a kind,
// and os.CreateTemp's random number. The patterns used to be broad enough to
// delete a file the user had named "pihole-backup-old.zip" themselves.
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
	if currentBackupDir() != "" {
		return
	}
	dir := os.TempDir()
	// Listed rather than globbed: a directory name holding "[" or "*" made
	// the glob a pattern of its own, which matched and cleaned up a
	// different directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleBackupAge)
files:
	for _, e := range entries {
		if !generatedBackupName(e.Name()) {
			continue
		}
		m := filepath.Join(dir, e.Name())
		info, statErr := os.Lstat(m)
		if statErr != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		// Compared as files, not paths: the temp directory has more than one
		// spelling on some systems (/var and /private/var on macOS).
		for _, k := range kept {
			if os.SameFile(info, k) {
				continue files
			}
		}
		_ = os.Remove(m)
	}
}

// reapedBackup reports whether reapStaleBackups will remove the file at path
// once it is old enough: no PIHOLE_BACKUP_DIR is set, and the file is in the
// system temp directory under a name of the form the server generates,
// whether the server chose it or an output_path did.
func reapedBackup(path string) bool {
	if currentBackupDir() != "" || !generatedBackupName(filepath.Base(path)) {
		return false
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false
	}
	tmp, err := os.Stat(os.TempDir())
	return err == nil && os.SameFile(dir, tmp)
}

// generatedBackupName reports whether name is one createBackupFile made
// without an output_path: the prefix, a kind, a hyphen, and the random
// decimal string os.CreateTemp puts in place of the "*".
func generatedBackupName(name string) bool {
	rest, ok := strings.CutPrefix(name, backupFilePrefix)
	if !ok {
		return false
	}
	stem, ok := strings.CutSuffix(rest, ".zip")
	if !ok {
		return false
	}
	i := strings.LastIndexByte(stem, '-')
	if i <= 0 || i == len(stem)-1 {
		return false
	}
	for _, r := range stem[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
