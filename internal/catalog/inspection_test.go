package catalog

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectBackupPartsTotalsAndMissingSequence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part1 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 2)
	if err := os.MkdirAll(filepath.Dir(part1), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(part1, []byte("aa"), 0o600); err != nil {
		t.Fatalf("failed to create part1: %v", err)
	}
	if err := os.WriteFile(part2, []byte("bbb"), 0o600); err != nil {
		t.Fatalf("failed to create part2: %v", err)
	}

	partCount, totalSize, err := InspectBackupParts(dir, entry)
	if err != nil {
		t.Fatalf("InspectBackupParts returned error: %v", err)
	}
	if partCount != 2 {
		t.Fatalf("expected 2 parts, got %d", partCount)
	}
	if totalSize != 5 {
		t.Fatalf("expected total size 5 bytes, got %d", totalSize)
	}

	// Remove part2 and add part3, creating a gap at sequence 2.
	if err := os.Remove(part2); err != nil {
		t.Fatalf("failed to remove part2: %v", err)
	}
	part3 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 3)
	if err := os.WriteFile(part3, []byte("ccc"), 0o600); err != nil {
		t.Fatalf("failed to create part3: %v", err)
	}

	_, _, err = InspectBackupParts(dir, entry)
	if err == nil {
		t.Fatal("expected InspectBackupParts to fail for missing sequence")
	}
	if !strings.Contains(err.Error(), "Missing part file 002") {
		t.Fatalf("unexpected missing-sequence error: %v", err)
	}
}

func TestInspectBackupPartsReturnsErrorForNoParts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	_, _, err := InspectBackupParts(dir, entry)
	if err == nil {
		t.Fatal("expected InspectBackupParts to return error for empty directory")
	}
	if !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed: %s", path)
	}
}
