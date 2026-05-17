//go:build windows

package util

import (
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
