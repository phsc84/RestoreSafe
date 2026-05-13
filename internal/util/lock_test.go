//go:build windows

package util

import (
	"testing"
)

func TestAcquireTargetLockExclusive(t *testing.T) {
	dir := t.TempDir()

	lock1, err := AcquireTargetLock(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	_, err = AcquireTargetLock(dir)
	if err == nil {
		lock1.Release()
		t.Fatal("expected second lock acquisition to fail while first is held")
	}

	lock1.Release()

	lock2, err := AcquireTargetLock(dir)
	if err != nil {
		t.Fatalf("lock acquisition after release failed: %v", err)
	}
	lock2.Release()
}

func TestTargetLockReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireTargetLock(dir)
	if err != nil {
		t.Fatalf("lock acquisition failed: %v", err)
	}
	lock.Release()
	lock.Release() // second call must not panic or error
}

func TestNilTargetLockReleaseIsSafe(t *testing.T) {
	var l *TargetLock
	l.Release() // must not panic
}
