package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	valid := []verifyPreflightItem{{}, {}}
	if err := validateVerifyPreflight(valid); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}

	invalid := []verifyPreflightItem{{}, {Err: errors.New("broken")}}
	err := validateVerifyPreflight(invalid)
	if err == nil {
		t.Fatal("expected error for invalid verify preflight, got nil")
	}
	if !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}

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

	partCount, totalSize, err := inspectBackupParts(dir, entry)
	if err != nil {
		t.Fatalf("inspectBackupParts returned error: %v", err)
	}
	if partCount != 2 {
		t.Fatalf("expected 2 parts, got %d", partCount)
	}
	if totalSize != 5 {
		t.Fatalf("expected total size 5 bytes, got %d", totalSize)
	}

	// Replace sequence 002 with 003 to force a gap.
	if err := os.Remove(part2); err != nil {
		t.Fatalf("failed to remove part2: %v", err)
	}
	part3 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 3)
	if err := os.WriteFile(part3, []byte("ccc"), 0o600); err != nil {
		t.Fatalf("failed to create part3: %v", err)
	}

	_, _, err = inspectBackupParts(dir, entry)
	if err == nil {
		t.Fatal("expected inspectBackupParts to fail for missing sequence")
	}
	if !strings.Contains(err.Error(), "Missing part file 002") {
		t.Fatalf("unexpected missing-sequence error: %v", err)
	}
}

func TestVerifyEntryWrongPassword(t *testing.T) {
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src")
	targetDir := filepath.Join(workspace, "target")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	entry := util.BackupEntry{FolderName: filepath.Base(srcDir), Date: "2026-03-14", ID: util.BackupID("ABC123")}
	cfg := &util.Config{SplitSizeMB: 1, LogLevel: "info"}

	logPath := util.LogFileName(targetDir, entry.Date, entry.ID)
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	t.Cleanup(logger.Close)

	if err := backupFolder(srcDir, entry.FolderName, targetDir, entry.Date, entry.ID, []byte("correct-pass"), cfg, logger); err != nil {
		t.Fatalf("backupFolder returned error: %v", err)
	}

	err = verifyEntry(entry, targetDir, []byte("wrong-pass"), nil)
	if err == nil {
		t.Fatal("expected verifyEntry to fail with wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
