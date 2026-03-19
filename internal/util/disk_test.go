//go:build windows

package util

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

func TestIsNetworkVolumeLocalTempDir(t *testing.T) {
	t.Parallel()

	if IsNetworkVolume(t.TempDir()) {
		t.Fatal("expected local temp directory to be classified as local volume")
	}
}

func TestIsNetworkVolumeUNCPath(t *testing.T) {
	t.Parallel()

	if !IsNetworkVolume(`\\server\share\folder`) {
		t.Fatal("expected UNC path to be classified as network volume")
	}
}
