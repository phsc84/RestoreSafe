package backup

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupFolderLogsTarBeforeEncryption(t *testing.T) {
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	targetDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "sample.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("order-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "debug")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, IODiagnostics: false}
	_, backupErr := backupFolder(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("ORD123"), []byte("pw"), cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupFolder failed: %v", backupErr)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	tarLine := "Starting TAR creation for: " + sourceDir
	encryptLine := "Starting encryption..."
	tarIndex := strings.Index(logContent, tarLine)
	encryptIndex := strings.Index(logContent, encryptLine)
	if tarIndex < 0 {
		t.Fatalf("expected TAR creation debug line in log, got: %q", logContent)
	}
	if encryptIndex < 0 {
		t.Fatalf("expected encryption debug line in log, got: %q", logContent)
	}
	if tarIndex > encryptIndex {
		t.Fatalf("expected TAR creation log before encryption log, got log: %q", logContent)
	}
}

func TestBackupFolderLogsPartNamesAtInfoLevel(t *testing.T) {
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	targetDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "sample.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("order-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, IODiagnostics: false}
	_, backupErr := backupFolder(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("ORD124"), []byte("pw"), cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupFolder failed: %v", backupErr)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "Created: 1 part file(s)") {
		t.Fatalf("expected created-part summary in log, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Part 001:") {
		t.Fatalf("expected per-part filename line in log, got: %q", logContent)
	}

	partIdx := strings.Index(logContent, "Part 001:")
	createdIdx := strings.Index(logContent, "Created: 1 part file(s)")
	if partIdx < 0 || createdIdx < 0 {
		t.Fatalf("expected part and created lines in log, got: %q", logContent)
	}
	if createdIdx < partIdx {
		t.Fatalf("expected created summary after part lines, got: %q", logContent)
	}
}
