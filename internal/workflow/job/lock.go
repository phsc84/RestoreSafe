package job

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/workflow/interact"
	"errors"
)

// LockForReading takes the shared lock of backupDir for a restore or a
// verification, so no backup (and its retention) runs there at the same
// time. When the directory can't be locked at all (read-only medium), the
// operation goes on: issue is the warning for its plan and log.
func LockForReading(backupDir string) (lock *fsx.BackupLock, issue *interact.Issue, err error) {
	lock, err = fsx.AcquireReadLock(backupDir)
	if errors.Is(err, fsx.ErrLockUnavailable) {
		return lock, &interact.Issue{
			Status: interact.StatusWarn,
			Code:   interact.CodeBackupDirNotLocked,
			Text:   "RestoreSafe can't lock the backup directory. Remedy: Don't start a backup in it while this runs.",
		}, nil
	}
	return lock, nil, err
}
