package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/plan"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type operationEnv struct {
	srcDir    string
	backupDir string
	logPath   string
	logger    *logging.Logger
	cfg       *config.Config
	sources   []plan.Source
	// out receives what the operation writes to its UI and logger.
	out testutil.Output
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
	env.sources = plan.ResolveSources([]string{env.srcDir}, "")
	env.logPath = filepath.Join(env.backupDir, "operation.log")
	logger, err := logging.NewLogger(env.logPath, "info", &env.out)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	env.logger = logger
	env.cfg = &config.Config{SplitSizeMB: 1, Argon2: testutil.FastArgon2Config}
	return env
}

func (env *operationEnv) run(t *testing.T, id naming.BackupID) string {
	t.Helper()
	ks, master := testutil.NewPasswordKeySet(t, []byte("op-pw"))
	op := &operation{cfg: env.cfg, log: env.logger, logPath: env.logPath, backupDir: env.backupDir, date: "2026-05-31", runID: id, keySet: ks, master: master}
	err := op.run(context.Background(), &interacttest.Script{Out: &env.out}, env.sources, nil)
	env.logger.Close()
	if err != nil {
		t.Fatalf("backup run failed: %v", err)
	}
	return env.out.String()
}

// TestRunBackupOperationWritesCompleteSet exercises the operation half of the
// backup workflow directly: with the key set passed in, no password prompt or
// stdin mocking is required.
func TestRunBackupOperationWritesCompleteSet(t *testing.T) {
	env := newOperationEnv(t, "operation payload")
	output := env.run(t, "OPS001")
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
	info := catalog.InspectSet(env.backupDir, naming.BackupEntry{DirectoryName: "source", ChainID: "OPS001", Date: "2026-05-31"})
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
	output := env.run(t, "OPS002")
	if !strings.Contains(output, "Post-backup verification successful") {
		t.Fatalf("expected verification success message in output, got: %q", output)
	}
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
}

func TestRunBackupOperationPrintsLogFileLast(t *testing.T) {
	env := newOperationEnv(t, "operation payload")
	output := env.run(t, "OPS004")

	successIndex := strings.Index(output, "Backup completed successfully")
	logFileIndex := strings.LastIndex(output, "Log file:")
	if successIndex == -1 || logFileIndex == -1 {
		t.Fatalf("expected completion and log file lines, got: %q", output)
	}
	if successIndex > logFileIndex {
		t.Fatalf("expected success before log file, got: %q", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "Log file: "+env.logPath) {
		t.Fatalf("expected log file line to be last, got: %q", output)
	}
	info := catalog.InspectSet(env.backupDir, naming.BackupEntry{DirectoryName: "source", ChainID: "OPS004", Date: "2026-05-31"})
	if !info.Complete() {
		t.Fatalf("set not complete in backup directory: %v", info.Err)
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

	failures, _ := verifyBackupAfterWrite(context.Background(), nil, fx.BackupDir, []naming.BackupEntry{fx.Entry}, fx.Master, logging.NewConsoleLogger("info", nil))
	if failures != 1 {
		t.Fatalf("expected 1 verification failure, got %d", failures)
	}
	remaining, _ := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if len(remaining) != len(parts) {
		t.Fatalf("expected backup files to be kept (%d), got %d", len(parts), len(remaining))
	}
}
