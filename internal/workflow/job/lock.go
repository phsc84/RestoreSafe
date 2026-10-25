package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// How often and how long a run tries again for a lock that is probably left
// by this PC after a lost connection (core spec 14): the network share
// releases it within a few minutes (2½ measured, refactoring plan RF-126).
const lockRetrySeconds = 5

var (
	lockRetry   = lockRetrySeconds * time.Second
	lockWaitMax = 5 * time.Minute
)

// Lock takes the lock of a backup, restore or verification with acquire.
// When the lock is probably an orphan of this PC (fsx.LockBusy.Orphan), it
// says so through u and tries again every lockRetry, for up to lockWaitMax:
// cancelling ctx stops the wait, and the error then matches
// context.Canceled. Any other error of acquire is returned at once.
func Lock(ctx context.Context, u interact.UI, acquire func() (*fsx.BackupLock, error)) (*fsx.BackupLock, error) {
	lock, err := acquire()
	var busy *fsx.LockBusy
	if err == nil || !errors.As(err, &busy) || !busy.Orphan() {
		return lock, err
	}
	u.WaitingForLock(fmt.Sprintf("The backup directory %q is locked, probably by this PC after a lost connection. A network drive releases such a lock within a few minutes; RestoreSafe tries again every %d seconds.", filepath.ToSlash(busy.Dir), lockRetrySeconds))
	deadline := time.Now().Add(lockWaitMax)
	tick := time.NewTicker(lockRetry)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
		lock, err = acquire()
		if err == nil || !errors.As(err, &busy) || !busy.Orphan() || !time.Now().Before(deadline) {
			return lock, err
		}
	}
}

// LockForReading takes the shared lock of backupDir for a restore or a
// verification, so no backup (and its retention) runs there at the same
// time, waiting as Lock does. When the directory can't be locked at all
// (read-only medium), the operation goes on: issue is the warning for its
// plan and log.
func LockForReading(ctx context.Context, u interact.UI, backupDir string) (lock *fsx.BackupLock, issue *interact.Issue, err error) {
	lock, err = Lock(ctx, u, func() (*fsx.BackupLock, error) { return fsx.AcquireReadLock(backupDir) })
	if errors.Is(err, fsx.ErrLockUnavailable) {
		return lock, &interact.Issue{
			Status: interact.StatusWarn,
			Code:   interact.CodeBackupDirNotLocked,
			Text:   "RestoreSafe can't lock the backup directory.",
			Remedy: "Don't start a backup in it while this runs.",
		}, nil
	}
	return lock, nil, err
}
