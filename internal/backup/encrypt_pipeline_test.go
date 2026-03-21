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

func TestCopyBackupResultsCopiesOnlyEncryptedAndChallengeFiles(t *testing.T) {
	t.Parallel()

	stagingDir := t.TempDir()
	targetDir := t.TempDir()

	files := map[string]string{
		"alpha-001.enc":               "enc-part",
		"alpha_2026_AAA111.challenge": "challenge",
		"notes.txt":                   "ignore",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	if err := copyBackupResults(stagingDir, targetDir, nil); err != nil {
		t.Fatalf("copyBackupResults returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "alpha-001.enc")); err != nil {
		t.Fatalf("expected .enc file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "alpha_2026_AAA111.challenge")); err != nil {
		t.Fatalf("expected .challenge file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "notes.txt")); err == nil {
		t.Fatal("did not expect non-backup file to be copied")
	}
}

func TestCopyBackupResultsFailsWhenStagingDirMissing(t *testing.T) {
	t.Parallel()

	err := copyBackupResults(filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for missing staging directory, got nil")
	}
}

func TestCopyBackupResultsLogsCopiedParts(t *testing.T) {
	stagingDir := t.TempDir()
	targetDir := t.TempDir()

	files := map[string]string{
		"alpha-001.enc":               "enc-part-1",
		"alpha-002.enc":               "enc-part-2",
		"alpha_2026_AAA111.challenge": "challenge",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("copy-log-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	if err := copyBackupResults(stagingDir, targetDir, logger); err != nil {
		logger.Close()
		t.Fatalf("copyBackupResults returned error: %v", err)
	}
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "Copied: alpha-001.enc") {
		t.Fatalf("expected copied-part progress for alpha-001.enc, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Copied: alpha-002.enc") {
		t.Fatalf("expected copied-part progress for alpha-002.enc, got: %q", logContent)
	}
	if strings.Contains(logContent, "Copied: alpha_2026_AAA111.challenge") {
		t.Fatalf("did not expect challenge file to be logged at info copy-progress level, got: %q", logContent)
	}
}

func TestCopyBackupResultsLogsSourceFolderCopyFinished(t *testing.T) {
	stagingDir := t.TempDir()
	targetDir := t.TempDir()

	files := map[string]string{
		"[00_Gemeinsam]_2026-03-21_AB12CD-001.enc": "part-1",
		"[00_Gemeinsam]_2026-03-21_AB12CD-002.enc": "part-2",
		"[10_Daten]_2026-03-21_EF34GH-001.enc":     "part-1",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("copy-log-finished-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	if err := copyBackupResults(stagingDir, targetDir, logger); err != nil {
		logger.Close()
		t.Fatalf("copyBackupResults returned error: %v", err)
	}
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, `Source folder "00_Gemeinsam" copy finished`) {
		t.Fatalf("expected completion message for 00_Gemeinsam, got: %q", logContent)
	}
	if !strings.Contains(logContent, `Source folder "10_Daten" copy finished`) {
		t.Fatalf("expected completion message for 10_Daten, got: %q", logContent)
	}

	part2Idx := strings.Index(logContent, "Copied: [00_Gemeinsam]_2026-03-21_AB12CD-002.enc")
	finishedIdx := strings.Index(logContent, `Source folder "00_Gemeinsam" copy finished`)
	if part2Idx < 0 || finishedIdx < 0 {
		t.Fatalf("expected part and completion lines in log, got: %q", logContent)
	}
	if finishedIdx < part2Idx {
		t.Fatalf("expected completion message after last copied part, got: %q", logContent)
	}
}
