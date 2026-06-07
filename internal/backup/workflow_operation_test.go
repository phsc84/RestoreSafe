package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunBackupOperationWritesPartsWithSuppliedPassword exercises the operation
// half of the backup workflow directly: with credentials passed in as a
// parameter, no password prompt / stdin mocking is required.
func TestRunBackupOperationWritesPartsWithSuppliedPassword(t *testing.T) {
	tempRoot := t.TempDir()
	srcDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sample.txt"), []byte("operation payload"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	sources := resolveBackupSources([]string{srcDir}, "")

	logPath := filepath.Join(backupDir, "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Use the minimum valid Argon2 parameters to keep the test fast while staying
	// within the bounds that security.Encrypt enforces.
	cfg := &util.Config{SplitSizeMB: 1}
	cfg.Argon2 = util.Argon2Config{Time: util.Argon2MinTime, MemoryMB: util.Argon2MinMemoryMB, Threads: util.Argon2MinThreads}

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runBackupOperation(cfg, logger, logPath, backupDir, sources, operation.LocalStagingPlan{}, "2026-05-31", util.BackupID("OPS001"), []byte("op-pw"), "")
	})
	logger.Close()
	if runErr != nil {
		t.Fatalf("runBackupOperation failed: %v", runErr)
	}

	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}

	matches, err := filepath.Glob(filepath.Join(backupDir, "*.enc"))
	if err != nil {
		t.Fatalf("failed to glob parts: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one .enc part to be written, got none")
	}
}

// TestRunBackupOperationVerifiesAfterBackup runs a backup with
// verify_after_backup enabled and confirms the freshly written parts are
// re-read, decrypted, and validated in the same run.
func TestRunBackupOperationVerifiesAfterBackup(t *testing.T) {
	tempRoot := t.TempDir()
	srcDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sample.txt"), []byte("verify payload"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	sources := resolveBackupSources([]string{srcDir}, "")
	logPath := filepath.Join(backupDir, "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Verification decrypts the parts, and decryption validates the Argon2
	// header params against the enforced bounds, so use in-range values here
	// (kept at the minimums to stay fast).
	cfg := &util.Config{SplitSizeMB: 1, VerifyAfterBackup: true}
	cfg.Argon2 = util.Argon2Config{Time: util.Argon2MinTime, MemoryMB: util.Argon2MinMemoryMB, Threads: util.Argon2MinThreads}

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runBackupOperation(cfg, logger, logPath, backupDir, sources, operation.LocalStagingPlan{}, "2026-05-31", util.BackupID("OPS002"), []byte("op-pw"), "")
	})
	logger.Close()
	if runErr != nil {
		t.Fatalf("runBackupOperation failed: %v", runErr)
	}
	if !strings.Contains(output, "Post-backup verification successful") {
		t.Fatalf("expected verification success message in output, got: %q", output)
	}
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
}

func TestRunBackupOperationCleansStagingBeforeSuccessAndPrintsLogFileLast(t *testing.T) {
	tempRoot := t.TempDir()
	srcDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	stagingTempDir := filepath.Join(tempRoot, "temp")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(stagingTempDir, 0o750); err != nil {
		t.Fatalf("failed to create staging temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sample.txt"), []byte("staged payload"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	sources := resolveBackupSources([]string{srcDir}, "")
	logPath := filepath.Join(backupDir, "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1}
	cfg.Argon2 = util.Argon2Config{Time: util.Argon2MinTime, MemoryMB: util.Argon2MinMemoryMB, Threads: util.Argon2MinThreads}
	stagingPlan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: stagingTempDir}

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runBackupOperation(cfg, logger, logPath, backupDir, sources, stagingPlan, "2026-05-31", util.BackupID("OPS004"), []byte("op-pw"), "")
	})
	logger.Close()
	if runErr != nil {
		t.Fatalf("runBackupOperation failed: %v", runErr)
	}

	cleanupIndex := strings.Index(output, "Removed staging directory:")
	successIndex := strings.Index(output, "Backup completed successfully")
	logFileIndex := strings.LastIndex(output, "Log file:")
	if cleanupIndex == -1 {
		t.Fatalf("expected staging cleanup message in output, got: %q", output)
	}
	if successIndex == -1 {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
	if logFileIndex == -1 {
		t.Fatalf("expected log file message in output, got: %q", output)
	}
	if !(cleanupIndex < successIndex && successIndex < logFileIndex) {
		t.Fatalf("expected cleanup before success and success before log file, got: %q", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "Log file: "+logPath) {
		t.Fatalf("expected log file line to be last, got: %q", output)
	}
}

// TestVerifyBackupAfterWriteReportsCorruptPart confirms a corrupted part is
// reported as a verification failure and the backup files are left in place.
func TestVerifyBackupAfterWriteReportsCorruptPart(t *testing.T) {
	tempRoot := t.TempDir()
	srcDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sample.txt"), []byte("corrupt me"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	sources := resolveBackupSources([]string{srcDir}, "")
	logPath := filepath.Join(backupDir, "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	cfg := &util.Config{SplitSizeMB: 1}
	cfg.Argon2 = util.Argon2Config{Time: util.Argon2MinTime, MemoryMB: util.Argon2MinMemoryMB, Threads: util.Argon2MinThreads}

	date := "2026-05-31"
	id := util.BackupID("OPS003")
	password := []byte("op-pw")
	testutil.CaptureStdout(t, func() {
		if err := runBackupOperation(cfg, logger, logPath, backupDir, sources, operation.LocalStagingPlan{}, date, id, password, ""); err != nil {
			t.Fatalf("runBackupOperation failed: %v", err)
		}
	})

	parts, err := filepath.Glob(filepath.Join(backupDir, "*.enc"))
	if err != nil || len(parts) == 0 {
		t.Fatalf("expected at least one .enc part, got %d (err: %v)", len(parts), err)
	}
	// Corrupt the first part so decryption/authentication fails.
	if err := os.WriteFile(parts[0], []byte("not a valid encrypted part"), 0o600); err != nil {
		t.Fatalf("failed to corrupt part: %v", err)
	}

	var failures int
	testutil.CaptureStdout(t, func() {
		failures = verifyBackupAfterWrite(backupDir, date, id, []string{"source"}, password, logger)
	})
	if failures != 1 {
		t.Fatalf("expected 1 verification failure, got %d", failures)
	}
	// Files must be kept on a failed verification.
	if remaining, _ := filepath.Glob(filepath.Join(backupDir, "*.enc")); len(remaining) != len(parts) {
		t.Fatalf("expected backup files to be kept (%d), got %d", len(parts), len(remaining))
	}
}
