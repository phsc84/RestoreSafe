//go:build windows

package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAcquireBackupLockExclusive(t *testing.T) {
	dir := t.TempDir()

	lock1, err := AcquireBackupLock(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	_, err = AcquireBackupLock(dir)
	if err == nil {
		lock1.Release()
		t.Fatal("expected second lock acquisition to fail while first is held")
	}

	lock1.Release()

	lock2, err := AcquireBackupLock(dir)
	if err != nil {
		t.Fatalf("lock acquisition after release failed: %v", err)
	}
	lock2.Release()
}

func TestBackupLockReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireBackupLock(dir)
	if err != nil {
		t.Fatalf("lock acquisition failed: %v", err)
	}
	lock.Release()
	lock.Release() // second call must not panic or error
}

func TestNilBackupLockReleaseIsSafe(t *testing.T) {
	var l *BackupLock
	l.Release() // must not panic
}

func TestReadLocksShareButExcludeABackup(t *testing.T) {
	// The restores and the backup stand for RestoreSafe windows of their own
	// on this PC.
	fakeHolders(t, computerName(), true, true)
	own := thisProcess
	thisProcess = func() uint32 { return 0 }
	t.Cleanup(func() { thisProcess = own })
	dir := t.TempDir()

	read1, err := AcquireReadLock(dir)
	if err != nil {
		t.Fatalf("first read lock: %v", err)
	}
	read2, err := AcquireReadLock(dir)
	if err != nil {
		t.Fatalf("a second restore or verification must run at the same time: %v", err)
	}
	if _, err := AcquireBackupLock(dir); err == nil || !strings.Contains(err.Error(), "Another RestoreSafe window") {
		t.Fatalf("a backup must not start while a restore runs, got %v", err)
	}
	read1.Release()
	if _, err := AcquireBackupLock(dir); err == nil {
		t.Fatal("a backup must not start while one restore still runs")
	}
	read2.Release()
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); err != nil {
		t.Fatalf("a shared holder leaves the lock file: %v", err)
	}

	backup, err := AcquireBackupLock(dir)
	if err != nil {
		t.Fatalf("backup lock after the restores: %v", err)
	}
	if _, err := AcquireReadLock(dir); err == nil || !strings.Contains(err.Error(), "A backup is running") {
		t.Fatalf("a restore must not start while a backup runs, got %v", err)
	}
	backup.Release()
	if fi, err := os.Stat(filepath.Join(dir, lockFileName)); err != nil || fi.Size() != 0 {
		t.Fatalf("the backup keeps the lock file, empty, got %v", err)
	}
	read, err := AcquireReadLock(dir)
	if err != nil {
		t.Fatalf("read lock after the backup: %v", err)
	}
	read.Release()
}

func TestLockWithoutLockFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not there")

	if _, err := AcquireBackupLock(missing); err == nil || !strings.Contains(err.Error(), "can't create its lock file") {
		t.Fatalf("a backup needs the lock file, got %v", err)
	}
	lock, err := AcquireReadLock(missing)
	if !errors.Is(err, ErrLockUnavailable) {
		t.Fatalf("a restore goes on without the lock, with ErrLockUnavailable; got %v", err)
	}
	lock.Release() // holds nothing; must not panic
}

// fakeHolders replaces the checks of who holds a lock for one test.
func fakeHolders(t *testing.T, computer string, running, otherInstance bool) {
	c, p, o := thisComputer, processRunning, otherInstanceRunning
	thisComputer = func() string { return computer }
	processRunning = func(uint32, time.Time) bool { return running }
	otherInstanceRunning = func() bool { return otherInstance }
	t.Cleanup(func() { thisComputer, processRunning, otherInstanceRunning = c, p, o })
}

// holdWithLine takes the exclusive lock of dir on a handle of its own, as
// another process would, with line as the holder line ("" for none).
func holdWithLine(t *testing.T, dir, line string) {
	f, err := os.OpenFile(filepath.Join(dir, lockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err := lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK); err != nil {
		t.Fatal(err)
	}
	if line != "" {
		if _, err := f.WriteAt([]byte(line+"\n"), holderOffset); err != nil {
			t.Fatal(err)
		}
	}
}

// A backup writes its holder line after the locked byte, where a run that
// can't get the lock reads it, and empties the file on release (core spec
// 14, refactoring plan RF-126).
func TestBackupLockWritesItsHolderLine(t *testing.T) {
	fakeHolders(t, "PC-HOME", true, false)
	dir := t.TempDir()
	backup, err := AcquireBackupLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	h := readHolder(f) // through another handle, while the byte is locked
	f.Close()
	if h == nil || h.Computer != "PC-HOME" || h.PID != uint32(os.Getpid()) || h.Started.IsZero() || h.Since.Before(h.Started) {
		t.Fatalf("holder line: %+v", h)
	}
	_, err = AcquireBackupLock(dir)
	var busy *LockBusy
	// Its own line can't be a running operation of this process: it is left
	// from an earlier run (refactoring plan RF-126).
	if !errors.As(err, &busy) || busy.Holder == nil || !busy.Orphan() || !strings.Contains(err.Error(), "probably by this PC") {
		t.Fatalf("a line of this process: got %v, %+v", err, busy)
	}
	backup.Release()
	f, err = os.Open(filepath.Join(dir, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if h := readHolder(f); h != nil {
		t.Fatalf("the line outlived the backup: %+v", h)
	}
}

func TestLockBusyNamesItsHolder(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	line := func(computer string) string {
		return fmt.Sprintf(`{"computer":%q,"user":"philip","pid":4242,"started":%q,"since":%q}`, computer, since.Add(-time.Minute).Format(time.RFC3339Nano), since.Format(time.RFC3339Nano))
	}
	for _, c := range []struct {
		name                   string
		line                   string
		running, otherInstance bool
		orphan                 bool
		backupText, readText   string
	}{
		{"another PC", line("PC-OFFICE"), true, true, false,
			"RestoreSafe on PC-OFFICE is backing up in", "RestoreSafe on PC-OFFICE is backing up in"},
		{"this PC, running", line("pc-home"), true, false, false,
			"Another RestoreSafe window", "A backup is running"},
		{"this PC, not running", line("PC-HOME"), false, true, true,
			"probably by this PC after a lost connection", "probably by this PC after a lost connection"},
		{"no line, another RestoreSafe runs here", "", false, true, false,
			"Another RestoreSafe window", "A backup is running"},
		{"no line", "", true, false, true,
			"probably by this PC after a lost connection", "probably by this PC after a lost connection"},
		{"a broken line", "{not json", false, false, true,
			"probably by this PC", "probably by this PC"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeHolders(t, "PC-HOME", c.running, c.otherInstance)
			dir := t.TempDir()
			holdWithLine(t, dir, c.line)
			for _, try := range []struct {
				acquire func(string) (*BackupLock, error)
				text    string
			}{{AcquireBackupLock, c.backupText}, {AcquireReadLock, c.readText}} {
				_, err := try.acquire(dir)
				var busy *LockBusy
				if !errors.As(err, &busy) || busy.Orphan() != c.orphan || !strings.Contains(err.Error(), try.text) {
					t.Fatalf("got %v (%+v), want orphan %v and %q", err, busy, c.orphan, try.text)
				}
			}
		})
	}
}

func TestClockTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.Local)
	if got := clockTime(time.Date(2026, 10, 25, 8, 12, 0, 0, time.Local), now); got != "08:12" {
		t.Fatalf("today: %q", got)
	}
	if got := clockTime(time.Date(2026, 10, 24, 20, 5, 0, 0, time.Local), now); got != "Sat 24 Oct, 20:05" {
		t.Fatalf("another day: %q", got)
	}
}

// The three-part test: this process is a running RestoreSafe (the test
// binary's image is its own) once it had started; an ID that isn't running,
// another program and, here, another process of this binary are not.
func TestRunningRestoreSafe(t *testing.T) {
	t.Parallel()
	pid := uint32(os.Getpid())
	if !runningRestoreSafe(pid, time.Now()) {
		t.Fatal("this process counts as running")
	}
	if runningRestoreSafe(pid, processStart().Add(-time.Minute)) {
		t.Fatal("a process started after the line's time is not the holder")
	}
	if runningRestoreSafe(0xFFFFFFF0, time.Now()) {
		t.Fatal("an ID without a process is not running")
	}
	if runningRestoreSafe(uint32(os.Getppid()), time.Now()) {
		t.Fatal("a running program of another name is not RestoreSafe")
	}
	if otherRestoreSafeRunning() {
		t.Fatal("no other process of this test binary runs")
	}
	if (&LockBusy{}).Error() == "" {
		t.Fatal("LockBusy has a text")
	}
}
