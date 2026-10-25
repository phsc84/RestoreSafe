//go:build windows

package fsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phsc84/restoresafe/internal/problem"

	"golang.org/x/sys/windows"
)

const lockFileName = "restoresafe.lock"

// holderOffset is where the holder line starts: after the locked byte, so
// that a run which can't get the lock can still read it.
const holderOffset = 1

// maxHolderLine bounds what is read of a holder line.
const maxHolderLine = 1024

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

// LockHolder is the holder line a backup writes into the lock file while it
// holds the lock (core spec 14). Restores and verifications, which share the
// lock, write none.
type LockHolder struct {
	Computer string `json:"computer"`
	User     string `json:"user"`
	PID      uint32 `json:"pid"`
	// Started is when the holding process started, Since when it took the
	// lock.
	Started time.Time `json:"started"`
	Since   time.Time `json:"since"`
}

// LockBusy is the cause of the error of a lock that is held: who holds it,
// as far as the lock file tells. It is reached with errors.As.
type LockBusy struct {
	Dir string
	// Holder is the holder line, nil when the file has none: restores or
	// verifications hold the lock, or a backup that couldn't write it.
	Holder *LockHolder
	// OtherPC is set when the holder line names another computer.
	OtherPC bool
	// Running is set when a RestoreSafe on this PC holds the lock: the
	// process of the holder line runs (the three-part test of
	// processRunning) and is not this process, or, without a line, another
	// RestoreSafe process runs on this PC.
	Running bool
}

func (b *LockBusy) Error() string { return "the backup directory is locked" }

// Orphan reports whether the lock is probably left by this PC after a lost
// connection: no RestoreSafe on this PC holds it, and no other PC is named.
// A network share keeps such a lock for a few minutes (2½ measured on the
// Mac, refactoring plan RF-126) and then releases it.
func (b *LockBusy) Orphan() bool { return !b.OtherPC && !b.Running }

// The checks of who holds a lock; tests replace them.
var (
	thisComputer         = computerName
	thisProcess          = func() uint32 { return uint32(os.Getpid()) }
	processRunning       = runningRestoreSafe
	otherInstanceRunning = otherRestoreSafeRunning
)

// AcquireBackupLock locks backupDir exclusively, for a backup, and writes the
// holder line. It fails while another RestoreSafe process backs up, restores
// or verifies in backupDir, and when the lock file can't be created: a
// directory that can't take the lock file can't take a backup either.
func AcquireBackupLock(backupDir string) (*BackupLock, error) {
	f, err := os.OpenFile(filepath.Join(backupDir, lockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, problem.Errorf("RestoreSafe can't create its lock file in %q: %w.", filepath.ToSlash(backupDir), err).WithRemedy("Check the write permissions of the backup directory.")
	}
	if err := lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK); err != nil {
		busy := inspect(f, backupDir)
		f.Close()
		return nil, busyError(busy, true)
	}
	writeHolder(f) // informational: a run that can't get the lock reads it
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
		busy := inspect(f, backupDir)
		f.Close()
		return nil, busyError(busy, false)
	}
	return &BackupLock{file: f}, nil
}

// lockFile locks the first byte of f without waiting; flags 0 is a shared
// lock.
func lockFile(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}

// writeHolder writes the holder line of this process after the locked byte.
// A failure is ignored: the line only words the message of another run.
func writeHolder(f *os.File) {
	h := LockHolder{
		Computer: thisComputer(),
		User:     os.Getenv("USERNAME"),
		PID:      uint32(os.Getpid()),
		Started:  processStart(),
		Since:    time.Now(),
	}
	line, err := json.Marshal(h)
	if err != nil {
		return
	}
	f.WriteAt(append(line, '\n'), holderOffset) //nolint:errcheck
	f.Sync()                                    //nolint:errcheck
}

// readHolder reads the holder line of f; nil when there is none.
func readHolder(f *os.File) *LockHolder {
	buf := make([]byte, maxHolderLine)
	n, _ := f.ReadAt(buf, holderOffset)
	line, _, found := bytes.Cut(buf[:n], []byte{'\n'})
	if !found {
		return nil
	}
	var h LockHolder
	if json.Unmarshal(line, &h) != nil || h.Computer == "" {
		return nil
	}
	return &h
}

// inspect tells who holds the lock of f, which this process couldn't get.
func inspect(f *os.File, dir string) *LockBusy {
	b := &LockBusy{Dir: dir, Holder: readHolder(f)}
	switch {
	case b.Holder == nil:
		b.Running = otherInstanceRunning()
	case !strings.EqualFold(b.Holder.Computer, thisComputer()):
		b.OtherPC = true
	case b.Holder.PID == thisProcess():
		// This process runs one operation at a time and holds no lock when
		// it starts one: its own line is left from an earlier run whose
		// close a server kept after a network drop (refactoring plan
		// RF-126).
	default:
		b.Running = processRunning(b.Holder.PID, b.Holder.Since)
	}
	return b
}

// busyError words a lock that is held, for a backup (exclusive) or a
// restore or verification.
func busyError(b *LockBusy, exclusive bool) error {
	dir := filepath.ToSlash(b.Dir)
	var e *problem.Error
	switch {
	case b.OtherPC:
		e = problem.Errorf("RestoreSafe on %s is backing up in %q since %s.", b.Holder.Computer, dir, clockTime(b.Holder.Since, time.Now())).
			WithRemedy(fmt.Sprintf("Wait until it has finished, then start again. If %s was switched off or lost its connection, the lock is released within a few minutes.", b.Holder.Computer))
	case b.Running && exclusive:
		e = problem.Errorf("Another RestoreSafe window is backing up, restoring or verifying in %q.", dir).WithRemedy("Wait until it has finished, then start the backup again.")
	case b.Running:
		e = problem.Errorf("A backup is running in %q.", dir).WithRemedy("Wait until it has finished, then start again.")
	default:
		e = problem.Errorf("The backup directory %q is still locked, probably by this PC after a lost connection.", dir).WithRemedy("A network drive releases such a lock within a few minutes. Wait, then start again.")
	}
	e.Err = b
	return e
}

// clockTime words t for a message: the time of day when it is today, else
// with the day ("Sun 25 Oct, 08:12"), as the window words dates.
func clockTime(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	return t.Format("Mon 2 Jan, 15:04")
}

// Release unlocks and closes the lock file. The holder of an exclusive lock
// first empties it, so that no holder line outlives the backup; the file
// stays, since its existence means nothing (core spec 14). Safe to call on a
// nil receiver, more than once, and on a lock that holds nothing.
func (l *BackupLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	if l.exclusive {
		l.file.Truncate(0) //nolint:errcheck
	}
	windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, new(windows.Overlapped)) //nolint:errcheck
	l.file.Close()
	l.file = nil
}
