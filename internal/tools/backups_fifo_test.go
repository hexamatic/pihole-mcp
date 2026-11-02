//go:build !windows

package tools

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenBackupForImport_RefusesAFIFOWithoutBlocking(t *testing.T) {
	// Opening a FIFO for reading waits for a writer, so a pipe named
	// backup.zip held the import, and a tool worker, forever.
	fifo := filepath.Join(t.TempDir(), "backup.zip")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("cannot make a FIFO here:", err)
	}
	done := make(chan error, 1)
	go func() {
		f, _, err := openBackupForImport(fifo)
		if err == nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was accepted as a backup")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening a FIFO blocked")
	}
}
