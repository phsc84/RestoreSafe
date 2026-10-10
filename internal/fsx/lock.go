//go:build windows

package fsx

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/phsc84/restoresafe/internal/problem"

	"golang.org/x/sys/windows"
)

const lockFileName = "restoresafe.lock"

// ErrLockUnavailable reports that the lock file of a backup directory can't
// be created or opened, e.g. on a read-only medium: the directory can't be
// locked at all.
var ErrLockUnavailable = errors.New("RestoreSafe can't lock the backup directory; don't start a backup in it while this runs")

// BackupLock is a Windows file lock on the backup directory, held by one
// RestoreSafe process. A backup holds it exclusively, restores and
// verifications hold it shared, so a backup never deletes parts (retention)
// that another process is reading. Windows releases it when the process exits.
type BackupLock struct {
	file      *os.File
	exclusive bool
}

// AcquireBackupLock locks backupDir exclusively, for a backup. It fails while
// another RestoreSafe process backs up, restores or verifies in backupDir,
// and when the lock file can't be created: a directory that can't take the
// lock file can't take a backup either.
func AcquireBackupLock(backupDir string) (*BackupLock, error) {
	f, err := os.OpenFile(filepath.Join(backupDir, lockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, problem.Errorf("RestoreSafe can't create its lock file in %q: %w.", filepath.ToSlash(backupDir), err).WithRemedy("Check the write permissions of the backup directory.")
	}
	if err := lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK); err != nil {
		f.Close()
		return nil, problem.Errorf("Another RestoreSafe window is backing up, restoring or verifying in %q.", filepath.ToSlash(backupDir)).WithRemedy("Wait until it has finished, then start the backup again.")
	}
	return &BackupLock{file: f, exclusive: true}, nil
}

// AcquireReadLock locks backupDir shared, for a restore or a verification:
// any number of them may run at once, but no backup. It fails while a backup
// runs in backupDir. When the directory can't be locked (read-only medium),
// it returns a lock that holds nothing and ErrLockUnavailable, so the caller
// can go on with a warning; Release is safe on that lock.
func AcquireReadLock(backupDir string) (*BackupLock, error) {
	path := filepath.Join(backupDir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		// A read-only medium may still hold the lock file of a backup.
		if f, err = os.Open(path); err != nil {
			return &BackupLock{}, ErrLockUnavailable
		}
	}
	if err := lockFile(f, 0); err != nil {
		f.Close()
		return nil, problem.Errorf("A backup is running in %q.", filepath.ToSlash(backupDir)).WithRemedy("Wait until it has finished, then start again.")
	}
	return &BackupLock{file: f}, nil
}

// lockFile locks the first byte of f without waiting; flags 0 is a shared
// lock.
func lockFile(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}

// Release unlocks and closes the lock file. The holder of an exclusive lock
// also removes the file, as best-effort cleanup; a shared holder leaves it,
// since another restore or verification may hold it too. Safe to call on a
// nil receiver, more than once, and on a lock that holds nothing.
func (l *BackupLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, new(windows.Overlapped)) //nolint:errcheck
	name := l.file.Name()
	l.file.Close()
	l.file = nil
	if l.exclusive {
		os.Remove(name) //nolint:errcheck
	}
}
