//go:build windows

package util

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const lockFileName = "restoresafe.lock"

// TargetLock holds an exclusive Windows file lock on the backup target directory.
// The lock is automatically released when the holding process exits.
type TargetLock struct {
	file *os.File
}

// AcquireTargetLock opens (or creates) the lock file in targetDir and acquires
// an exclusive byte-range lock via LockFileEx. Returns an error if another
// RestoreSafe process already holds the lock on the same directory.
//
// If the lock file cannot be created (e.g. read-only media), locking is skipped
// and a no-op TargetLock is returned so the caller can always call Release().
func AcquireTargetLock(targetDir string) (*TargetLock, error) {
	lockPath := filepath.Join(targetDir, lockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		// Cannot create lock file (read-only volume, permissions). Skip locking.
		return &TargetLock{}, nil
	}

	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol,
	); err != nil {
		f.Close()
		return nil, fmt.Errorf(
			"Another RestoreSafe backup is already running in %q. Wait for it to complete and try again.",
			filepath.ToSlash(targetDir),
		)
	}

	return &TargetLock{file: f}, nil
}

// Release unlocks and closes the lock file, then removes it as best-effort cleanup.
// Safe to call on a nil receiver or a no-op lock.
func (l *TargetLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, ol) //nolint:errcheck
	name := l.file.Name()
	l.file.Close()
	os.Remove(name) //nolint:errcheck
}
