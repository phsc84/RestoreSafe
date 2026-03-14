package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndRestoreEntryRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src-data")
	targetDir := filepath.Join(workspace, "target")
	restoreRoot := filepath.Join(workspace, "restore")

	if err := os.MkdirAll(filepath.Join(srcDir, "nested"), 0o750); err != nil {
		t.Fatalf("failed to create source folder structure: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	smallContent := []byte("hello restoresafe")
	if err := os.WriteFile(filepath.Join(srcDir, "nested", "small.txt"), smallContent, 0o600); err != nil {
		t.Fatalf("failed to write small source file: %v", err)
	}

	// Slightly above 2 MiB to force multiple split files when split_size_mb = 1.
	largeContent := bytes.Repeat([]byte("A"), 2*1024*1024+256)
	if err := os.WriteFile(filepath.Join(srcDir, "large.bin"), largeContent, 0o600); err != nil {
		t.Fatalf("failed to write large source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, LogLevel: "info", IODiagnostics: false}
	backupDate := "2026-03-14"
	backupID := util.BackupID("ABC123")
	folderName := filepath.Base(srcDir)
	password := []byte("integration-test-password")

	logPath := util.LogFileName(targetDir, backupDate, backupID)
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	t.Cleanup(logger.Close)

	if err := backupFolder(srcDir, folderName, targetDir, backupDate, backupID, password, cfg, logger); err != nil {
		t.Fatalf("backupFolder returned error: %v", err)
	}

	entry := util.BackupEntry{FolderName: folderName, Date: backupDate, ID: backupID}
	parts := collectParts(targetDir, entry)
	if len(parts) < 2 {
		t.Fatalf("expected multiple split parts, got %d", len(parts))
	}

	if err := restoreEntry(entry, targetDir, restoreRoot, password, nil); err != nil {
		t.Fatalf("restoreEntry returned error: %v", err)
	}

	restoredDir := filepath.Join(restoreRoot, folderName)
	assertFileContentEqual(t, filepath.Join(srcDir, "nested", "small.txt"), filepath.Join(restoredDir, "nested", "small.txt"))
	assertFileContentEqual(t, filepath.Join(srcDir, "large.bin"), filepath.Join(restoredDir, "large.bin"))
}

func TestRestoreEntryWrongPassword(t *testing.T) {
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src")
	targetDir := filepath.Join(workspace, "target")
	restoreRoot := filepath.Join(workspace, "restore")

	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, LogLevel: "info", IODiagnostics: false}
	backupDate := "2026-03-14"
	backupID := util.BackupID("DEF456")
	folderName := filepath.Base(srcDir)

	logger, err := util.NewLogger(util.LogFileName(targetDir, backupDate, backupID), "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	t.Cleanup(logger.Close)

	if err := backupFolder(srcDir, folderName, targetDir, backupDate, backupID, []byte("correct-password"), cfg, logger); err != nil {
		t.Fatalf("backupFolder returned error: %v", err)
	}

	entry := util.BackupEntry{FolderName: folderName, Date: backupDate, ID: backupID}
	err = restoreEntry(entry, targetDir, restoreRoot, []byte("wrong-password"), nil)
	if err == nil {
		t.Fatal("expected restoreEntry to fail for wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}

func assertFileContentEqual(t *testing.T, expectedPath, actualPath string) {
	t.Helper()

	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read expected file %s: %v", expectedPath, err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("failed to read actual file %s: %v", actualPath, err)
	}

	if !bytes.Equal(expected, actual) {
		t.Fatalf("file contents differ for %s vs %s", expectedPath, actualPath)
	}
}
