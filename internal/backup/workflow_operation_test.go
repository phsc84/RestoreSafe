package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type operationEnv struct {
	srcDir    string
	backupDir string
	logPath   string
	logger    *util.Logger
	cfg       *util.Config
	sources   []backupSource
}

func newOperationEnv(t *testing.T, payload string) *operationEnv {
	t.Helper()
	tempRoot := t.TempDir()
	env := &operationEnv{
		srcDir:    filepath.Join(tempRoot, "source"),
		backupDir: filepath.Join(tempRoot, "target"),
	}
	createFile(t, filepath.Join(env.srcDir, "sample.txt"), payload)
	if err := os.MkdirAll(env.backupDir, 0o750); err != nil {
		t.Fatal(err)
	}
	env.sources = resolveBackupSources([]string{env.srcDir}, "")
	env.logPath = filepath.Join(env.backupDir, "operation.log")
	logger, err := util.NewLogger(env.logPath, "info", nil)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	env.logger = logger
	env.cfg = &util.Config{SplitSizeMB: 1, Argon2: testutil.FastArgon2Config}
	return env
}

func (env *operationEnv) run(t *testing.T, plan operation.LocalStagingPlan, id util.BackupID) string {
	t.Helper()
	ks, master := testutil.NewPasswordKeySet(t, []byte("op-pw"))
	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runBackupOperation(os.Stdout, env.cfg, env.logger, env.logPath, env.backupDir, env.sources, plan, "2026-05-31", id, ks, master, nil)
	})
	env.logger.Close()
	if runErr != nil {
		t.Fatalf("runBackupOperation failed: %v", runErr)
	}
	return output
}

// TestRunBackupOperationWritesCompleteSet exercises the operation half of the
// backup workflow directly: with the key set passed in, no password prompt or
// stdin mocking is required.
func TestRunBackupOperationWritesCompleteSet(t *testing.T) {
	env := newOperationEnv(t, "operation payload")
	output := env.run(t, operation.LocalStagingPlan{}, "OPS001")
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
	info := catalog.InspectSet(env.backupDir, util.BackupEntry{DirectoryName: "source", ChainID: "OPS001", Date: "2026-05-31"})
	if !info.Complete() {
		t.Fatalf("expected complete set, got %v", info.Err)
	}
}

// TestRunBackupOperationVerifiesAfterBackup runs a backup with
// verify_after_backup enabled and confirms the freshly written set is
// re-read, decrypted, and checked in the same run.
func TestRunBackupOperationVerifiesAfterBackup(t *testing.T) {
	env := newOperationEnv(t, "verify payload")
	env.cfg.VerifyAfterBackup = true
	output := env.run(t, operation.LocalStagingPlan{}, "OPS002")
	if !strings.Contains(output, "Post-backup verification successful") {
		t.Fatalf("expected verification success message in output, got: %q", output)
	}
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
}

func TestRunBackupOperationCleansStagingBeforeSuccessAndPrintsLogFileLast(t *testing.T) {
	env := newOperationEnv(t, "staged payload")
	stagingTempDir := filepath.Join(filepath.Dir(env.srcDir), "temp")
	if err := os.MkdirAll(stagingTempDir, 0o750); err != nil {
		t.Fatal(err)
	}
	output := env.run(t, operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: stagingTempDir}, "OPS004")

	cleanupIndex := strings.Index(output, "Removed staging directory:")
	successIndex := strings.Index(output, "Backup completed successfully")
	logFileIndex := strings.LastIndex(output, "Log file:")
	if cleanupIndex == -1 || successIndex == -1 || logFileIndex == -1 {
		t.Fatalf("expected cleanup, completion, and log file lines, got: %q", output)
	}
	if !(cleanupIndex < successIndex && successIndex < logFileIndex) {
		t.Fatalf("expected cleanup before success and success before log file, got: %q", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "Log file: "+env.logPath) {
		t.Fatalf("expected log file line to be last, got: %q", output)
	}
	info := catalog.InspectSet(env.backupDir, util.BackupEntry{DirectoryName: "source", ChainID: "OPS004", Date: "2026-05-31"})
	if !info.Complete() {
		t.Fatalf("staged set not complete in backup directory: %v", info.Err)
	}
	if temps, _ := catalog.ListTempParts(env.backupDir); len(temps) != 0 {
		t.Fatalf("temporary parts left behind: %v", temps)
	}
}

// TestVerifyBackupAfterWriteReportsCorruptPart confirms a corrupted part is
// reported as a verification failure and the backup files are left in place.
func TestVerifyBackupAfterWriteReportsCorruptPart(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("op-pw"))
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if err != nil || len(parts) < 2 {
		t.Fatalf("expected several parts, got %d (err: %v)", len(parts), err)
	}
	data, err := os.ReadFile(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	data[100] ^= 0xFF
	if err := os.WriteFile(parts[1], data, 0o600); err != nil {
		t.Fatal(err)
	}

	var failures int
	testutil.CaptureStdout(t, func() {
		failures = verifyBackupAfterWrite(fx.BackupDir, []util.BackupEntry{fx.Entry}, fx.Master, util.NewConsoleLogger("info", nil))
	})
	if failures != 1 {
		t.Fatalf("expected 1 verification failure, got %d", failures)
	}
	remaining, _ := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if len(remaining) != len(parts) {
		t.Fatalf("expected backup files to be kept (%d), got %d", len(parts), len(remaining))
	}
}
