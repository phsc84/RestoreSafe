package backup

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopyFileWithCountersReturnsErrorForMissingSource(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nonexistent.bin")
	dst := filepath.Join(t.TempDir(), "dst.bin")
	var in, out, calls atomic.Int64
	err := copyFileWithCounters(missing, dst, &in, &out, &calls)
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to open source file") {
		t.Fatalf("expected open-source error, got: %v", err)
	}
}

func TestCopyFileWithCountersReturnsErrorForBadDestination(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "src.bin")
	if err := os.WriteFile(srcFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	nonExistentDstDir := filepath.Join(t.TempDir(), "missing")
	dst := filepath.Join(nonExistentDstDir, "dst.bin")
	var in, out, calls atomic.Int64
	err := copyFileWithCounters(srcFile, dst, &in, &out, &calls)
	if err == nil {
		t.Fatal("expected error for bad destination, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to create destination file") {
		t.Fatalf("expected create-destination error, got: %v", err)
	}
}

func TestCopyBackupResultsCopiesOnlyEncryptedAndChallengeFiles(t *testing.T) {
	t.Parallel()

	stagingDir := t.TempDir()
	backupDir := t.TempDir()

	files := map[string]string{
		"[alpha]_2026-03-21_AB12CD-001.enc": "enc-part",
		"alpha_2026_AAA111.challenge":        "challenge",
		"notes.txt":                          "ignore",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	if err := copyBackupResults(stagingDir, backupDir, nil, nil, nil); err != nil {
		t.Fatalf("copyBackupResults returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(backupDir, "[alpha]_2026-03-21_AB12CD-001.enc")); err != nil {
		t.Fatalf("expected .enc file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "alpha_2026_AAA111.challenge")); err != nil {
		t.Fatalf("expected .challenge file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "notes.txt")); err == nil {
		t.Fatal("did not expect non-backup file to be copied")
	}
}

func TestCopyBackupResultsFailsWhenStagingDirMissing(t *testing.T) {
	t.Parallel()

	err := copyBackupResults(filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing staging directory, got nil")
	}
}

func TestCopyBackupResultsLogsCopyBeforeAndSummaryAfter(t *testing.T) {
	stagingDir := t.TempDir()
	backupDir := t.TempDir()

	files := map[string]string{
		"[alpha]_2026-03-21_AB12CD-001.enc": "enc-part-1",
		"[alpha]_2026-03-21_AB12CD-002.enc": "enc-part-2",
		"alpha_2026_AAA111.challenge":        "challenge",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	logPath := filepath.Join(backupDir, fmt.Sprintf("copy-log-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	if err := copyBackupResults(stagingDir, backupDir, nil, nil, logger); err != nil {
		logger.Close()
		t.Fatalf("copyBackupResults returned error: %v", err)
	}
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "Copy: [alpha]_2026-03-21_AB12CD-001.enc") {
		t.Fatalf("expected pre-copy log for part 1, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Copy: [alpha]_2026-03-21_AB12CD-002.enc") {
		t.Fatalf("expected pre-copy log for part 2, got: %q", logContent)
	}
	if !strings.Contains(logContent, `Copied: 2 part file(s) - [alpha] successfully copied`) {
		t.Fatalf("expected copied summary for alpha, got: %q", logContent)
	}
	if strings.Contains(logContent, "alpha_2026_AAA111.challenge") {
		t.Fatalf("did not expect challenge file to appear in info log, got: %q", logContent)
	}

	// "Copy:" lines must appear before the "Copied:" summary.
	copyIdx := strings.Index(logContent, "Copy: [alpha]_2026-03-21_AB12CD-001.enc")
	summaryIdx := strings.Index(logContent, "Copied: 2 part file(s)")
	if copyIdx < 0 || summaryIdx < 0 {
		t.Fatalf("expected both Copy and Copied lines, got: %q", logContent)
	}
	if summaryIdx < copyIdx {
		t.Fatalf("expected Copied summary after Copy lines, got: %q", logContent)
	}
}

func TestCopyBackupResultsLogsPerDirectoryHeaderAndSummary(t *testing.T) {
	stagingDir := t.TempDir()
	backupDir := t.TempDir()

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

	logPath := filepath.Join(backupDir, fmt.Sprintf("copy-log-finished-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	directorySourcePaths := map[string]string{
		"00_Gemeinsam": "/data/00_Gemeinsam",
		"10_Daten":     "/data/10_Daten",
	}
	if err := copyBackupResults(stagingDir, backupDir, nil, directorySourcePaths, logger); err != nil {
		logger.Close()
		t.Fatalf("copyBackupResults returned error: %v", err)
	}
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "Copying backup files of source directory: /data/00_Gemeinsam") {
		t.Fatalf("expected directory header for 00_Gemeinsam, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Copying backup files of source directory: /data/10_Daten") {
		t.Fatalf("expected directory header for 10_Daten, got: %q", logContent)
	}
	if !strings.Contains(logContent, `Copied: 2 part file(s) - [00_Gemeinsam] successfully copied`) {
		t.Fatalf("expected completion message for 00_Gemeinsam, got: %q", logContent)
	}
	if !strings.Contains(logContent, `Copied: 1 part file(s) - [10_Daten] successfully copied`) {
		t.Fatalf("expected completion message for 10_Daten, got: %q", logContent)
	}

	// "Copied:" summary must appear after the last "Copy:" of its directory.
	part2Idx := strings.Index(logContent, "Copy: [00_Gemeinsam]_2026-03-21_AB12CD-002.enc")
	finishedIdx := strings.Index(logContent, `Copied: 2 part file(s) - [00_Gemeinsam] successfully copied`)
	if part2Idx < 0 || finishedIdx < 0 {
		t.Fatalf("expected part 2 copy line and completion line in log, got: %q", logContent)
	}
	if finishedIdx < part2Idx {
		t.Fatalf("expected completion message after last copied part, got: %q", logContent)
	}
}
