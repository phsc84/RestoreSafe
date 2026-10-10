//go:build windows

package fsx

import "testing"

func TestQueryFreeSpaceBytesReturnsValueForExistingPath(t *testing.T) {
	t.Parallel()

	freeBytes, err := QueryFreeSpaceBytes(t.TempDir())
	if err != nil {
		t.Fatalf("expected QueryFreeSpaceBytes to succeed for temp dir, got error: %v", err)
	}
	if freeBytes == 0 {
		t.Fatal("expected QueryFreeSpaceBytes to return a positive value")
	}
}

func TestQueryFreeSpaceBytesRejectsPathWithNulByte(t *testing.T) {
	t.Parallel()

	if _, err := QueryFreeSpaceBytes("C:/bad\x00path"); err == nil {
		t.Fatal("expected QueryFreeSpaceBytes to fail for path with NUL byte")
	}
}

func TestQueryDiskSpaceReturnsFreeAndTotal(t *testing.T) {
	free, total, err := QueryDiskSpace(t.TempDir())
	if err != nil || total == 0 || free > total {
		t.Fatalf("expected free <= total > 0, got free=%d total=%d err=%v", free, total, err)
	}
}
