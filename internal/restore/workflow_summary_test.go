package restore

import (
	"RestoreSafe/internal/util"
	"errors"
	"strings"
	"testing"
)

func TestDisplayRestoreOutputDirReturnsForwardSlashPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result := displayRestoreOutputDir(dir)
	if strings.Contains(result, "\\") {
		t.Fatalf("expected forward slashes in result, got %q", result)
	}
}

func TestEstimateRestoreBytesSkipsItemsWithErrors(t *testing.T) {
	t.Parallel()
	items := []restorePreflightItem{
		{TotalSizeBytes: 100},
		{TotalSizeBytes: 200},
		{TotalSizeBytes: 300, Err: errors.New("broken")},
	}
	got := estimateRestoreBytes(items)
	if got != 300 {
		t.Fatalf("expected 300 bytes (100+200), got %d", got)
	}
}

func TestEstimateRestoreBytesReturnsZeroForAllErrors(t *testing.T) {
	t.Parallel()
	items := []restorePreflightItem{
		{TotalSizeBytes: 500, Err: errors.New("broken")},
	}
	if got := estimateRestoreBytes(items); got != 0 {
		t.Fatalf("expected 0 when all items have errors, got %d", got)
	}
}

func TestStageBackupEntryLocallyReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	backupDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Ghost", Date: "2026-03-14", ID: util.BackupID("GHO001")}

	_, err := stageBackupEntryLocally(backupDir, entry, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error when no parts found, got nil")
	}
	if !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got: %v", err)
	}
}

func TestRestoreEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	backupDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Ghost", Date: "2026-03-14", ID: util.BackupID("GHO001")}

	_, err := restoreEntry(entry, backupDir, t.TempDir(), []byte("pw"), nil)
	if err == nil {
		t.Fatal("expected error when no parts found, got nil")
	}
	if !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got: %v", err)
	}
}
