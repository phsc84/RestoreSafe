//go:build windows

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(err) {
		t.Fatalf("the backup removes the lock file, got %v", err)
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
