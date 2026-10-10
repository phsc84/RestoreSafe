package health

import (
	"context"
	"sync"
	"time"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// SnapshotTimeout is how long the user interface waits for a snapshot
// before it reports the backup directory as unreachable (GUI spec OV-8).
const SnapshotTimeout = 5 * time.Second

// Checker takes snapshots, at most one at a time. A snapshot can block for
// a long time on an unreachable drive or share (Windows waits for the
// network); a caller waits only as long as its context allows and then gets
// a snapshot saying the backup directory does not respond. The blocked
// snapshot keeps running, and later callers wait for it instead of starting
// another one.
type Checker struct {
	mu      sync.Mutex
	running *flight
	// take computes a snapshot; nil means TakeSnapshot.
	take func(Params) Snapshot
}

type flight struct {
	done chan struct{}
	snap Snapshot
}

// Snapshot returns the snapshot for p, or, when ctx ends first, a snapshot
// that reports the backup directory as unreachable.
func (c *Checker) Snapshot(ctx context.Context, p Params) Snapshot {
	c.mu.Lock()
	f := c.running
	if f == nil {
		f = &flight{done: make(chan struct{})}
		c.running = f
		take := c.take
		if take == nil {
			take = TakeSnapshot
		}
		go func() {
			snap := take(p)
			c.mu.Lock()
			c.running = nil
			c.mu.Unlock()
			f.snap = snap
			close(f.done)
		}()
	}
	c.mu.Unlock()

	select {
	case <-f.done:
		return f.snap
	case <-ctx.Done():
		return unresponsive(p)
	}
}

// unresponsive is the snapshot when the check does not finish in time.
func unresponsive(p Params) Snapshot {
	backupDir := fsx.ResolveDir(p.Config.BackupDirectory, p.ExeDir)
	item := healthItem{
		Severity: healthError,
		Code:     interact.CodeBackupDirUnreachable,
		Scope:    healthScopeBackupDirectory,
		Detail:   backupDir + " does not respond. Remedy: Check the drive or the network connection, then check again.",
	}
	return Snapshot{
		State:     StateError,
		Problems:  []Problem{{Code: item.Code, Status: interact.StatusError, Path: backupDir, Detail: item.Detail}},
		BackupDir: backupDir,
		Check:     buildResult([]healthItem{item}),
		Checked:   p.Now,
	}
}
