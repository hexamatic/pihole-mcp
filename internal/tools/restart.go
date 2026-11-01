package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
)

// awaitRestartNote waits for FTL to come back after a write sent at sent that
// makes it restart, and returns a sentence for the reply. A write reports
// only once FTL is answering again, so the caller's next request cannot land
// in the restart window, where FTL can apply a change and lose the reply.
//
// A restart that does not finish in time is not an error: the write itself
// succeeded. The sentence says so, and tells the caller to check first.
func awaitRestartNote(ctx context.Context, c *pihole.Client, sent time.Time) string {
	if err := c.AwaitRestart(ctx, sent); err != nil {
		return fmt.Sprintf(" Pi-hole restarts to apply this, but %v. Check that it is answering before making further changes.", err)
	}
	return " Pi-hole restarted to apply it and is answering again."
}

// restartsFor reports whether a write to the dotted config element will make
// FTL restart, when the caller has not passed restart=false. A failed lookup
// counts as no restart: the write goes ahead as it always has, without the
// wait.
func restartsFor(ctx context.Context, c *pihole.Client, element string, restart bool) bool {
	if !restart {
		return false
	}
	restarts, err := c.RestartsFTL(ctx, element)
	return err == nil && restarts
}

// restartNoteIf is awaitRestartNote when waits is set, and "" otherwise.
func restartNoteIf(ctx context.Context, c *pihole.Client, waits bool, sent time.Time) string {
	if !waits {
		return ""
	}
	return awaitRestartNote(ctx, c, sent)
}
