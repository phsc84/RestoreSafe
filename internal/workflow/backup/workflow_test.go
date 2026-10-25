package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/inventory"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"

	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

// runBackupDirectory writes one full set of a small source and returns the
// log content.
func runBackupDirectory(t *testing.T, level string) (string, string) {
	t.Helper()
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	createFile(t, filepath.Join(sourceDir, "sample.txt"), "hello")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(backupDir, fmt.Sprintf("test-%d.log", time.Now().UnixNano()))
	logger, err := logging.NewLogger(logPath, level, nil)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	cfg := &config.Config{SplitSizeMB: 1}
	entry := naming.BackupEntry{DirectoryName: "source", ChainID: "ORD123", Date: "2026-03-18"}
	op := &operation{cfg: cfg, log: logger, backupDir: backupDir, runID: "ORD123", keySet: ks, master: master}
	_, backupErr := op.backupDirectory(context.Background(), nil, sourceDir, entry, nil)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupDirectory failed: %v", backupErr)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	return string(data), backupDir
}

func TestBackupDirectoryWritesCompleteSet(t *testing.T) {
	_, backupDir := runBackupDirectory(t, "info")
	info := inventory.Find(t, backupDir, naming.BackupEntry{DirectoryName: "source", ChainID: "ORD123", Date: "2026-03-18"})
	if !info.Complete() {
		t.Fatalf("expected a complete set, got %v", info.Err)
	}
}

func TestBackupDirectoryLogsTarCreationAtDebugLevel(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "debug")
	if !strings.Contains(logContent, "Starting TAR creation and encryption for:") {
		t.Fatalf("expected TAR creation debug line in log, got: %q", logContent)
	}
}

func TestBackupDirectoryLogsIODiagnosticsAtDebugLevel(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "debug")
	if !strings.Contains(logContent, "I/O diagnostics") {
		t.Fatalf("expected I/O diagnostics lines in log, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Part 001 size:") {
		t.Fatalf("expected per-part size line in log, got: %q", logContent)
	}
}

func TestBackupDirectoryLogsPartNamesAtInfoLevel(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "info")

	partIdx := strings.Index(logContent, "Part 001: [source]_ORD123_2026-03-18_FULL-001.enc")
	createdIdx := strings.Index(logContent, "Created: 1 part file(s)")
	if partIdx < 0 || createdIdx < 0 {
		t.Fatalf("expected part and created lines in log, got: %q", logContent)
	}
	if createdIdx < partIdx {
		t.Fatalf("expected created summary after part lines, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Backed up: 1 file(s), 0 directory(s)") {
		t.Fatalf("expected file summary in log, got: %q", logContent)
	}
	if strings.Contains(logContent, "I/O diagnostics") {
		t.Fatalf("did not expect I/O diagnostics at info level, got: %q", logContent)
	}
}

func TestRunReturnsErrorWhenBackupDirCannotBeCreated(t *testing.T) {
	t.Parallel()
	// Use an existing file as the target path so MkdirAll fails.
	base := t.TempDir()
	filePath := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// Append a subdir to the file path — MkdirAll will fail.
	cfg := &config.Config{BackupDirectory: filepath.Join(filePath, "sub")}
	err := Run(context.Background(), &interacttest.Script{}, cfg, "")
	if err == nil {
		t.Fatal("expected error when target dir cannot be created, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to create backup directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReturnsErrorWhenAllSourcesFail(t *testing.T) {
	t.Parallel()
	backupDir := t.TempDir()
	cfg := &config.Config{
		BackupDirectory:   backupDir,
		SourceDirectories: []string{filepath.Join(backupDir, "nonexistent-source")},
	}
	err := Run(context.Background(), &interacttest.Script{}, cfg, "")
	if err == nil {
		t.Fatal("expected error when all sources fail, got nil")
	}
	if !strings.Contains(err.Error(), "Backup preflight failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCancelsBackupWhenUserEntersN(t *testing.T) {
	t.Parallel()
	sourceDir := t.TempDir()
	backupDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "f.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	cfg := &config.Config{
		BackupDirectory:   backupDir,
		SourceDirectories: []string{sourceDir},
	}
	var out strings.Builder
	runErr := Run(context.Background(), &interacttest.Script{Out: &out, ReadLine: interacttest.Answers("n")}, cfg, "")
	output := out.String()
	if runErr != nil {
		t.Fatalf("expected nil error on cancel, got: %v", runErr)
	}
	if !strings.Contains(output, "Backup cancelled.") {
		t.Fatalf("expected 'Backup cancelled.' in output, got: %q", output)
	}
	if !strings.Contains(output, "new keys will be created") {
		t.Fatalf("expected key plan in preflight, got: %q", output)
	}
}

func TestRemoveLeftoverTempPartsDeletesOnlyTempParts(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, "[Docs]_ABC123_2026-03-18_FULL-001.enc.tmp")
	keep := filepath.Join(dir, "[Docs]_ABC123_2026-03-18_FULL-001.enc")
	other := filepath.Join(dir, "notes.tmp")
	for _, p := range []string{temp, keep, other} {
		createFile(t, p, "x")
	}
	removeLeftoverTempParts(dir, logging.NewOutputLogger("info", nil))
	assertNotExists(t, temp)
	assertExists(t, keep)
	assertExists(t, other)
}

func TestNewRunIDAvoidsUsedIDs(t *testing.T) {
	t.Parallel()
	used := make([]catalog.SetInfo, 0)
	for i := 0; i < 5; i++ {
		id, _ := naming.NewBackupID()
		used = append(used, catalog.SetInfo{Entry: naming.BackupEntry{ChainID: id}})
	}
	id, err := newRunID(used)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range used {
		if u.Entry.ChainID == id {
			t.Fatalf("newRunID returned used ID %s", id)
		}
	}
}

func TestRemoveLeftoverTempPartsWarnsWhenItCannotLook(t *testing.T) {
	t.Parallel()
	var out testutil.Output
	removeLeftoverTempParts(filepath.Join(t.TempDir(), "missing"), logging.NewOutputLogger("info", &out))
	if !strings.Contains(out.String(), "Failed to look for leftovers") {
		t.Fatalf("expected a warning for an unreadable directory, got %q", out.String())
	}
}

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

// TestOperationRunWritesCompleteSet exercises the operation half of the
// backup workflow directly: with the key set passed in, no password prompt or
// stdin mocking is required.
func TestOperationRunWritesCompleteSet(t *testing.T) {
	env := newOperationEnv(t, "operation payload")
	output := env.run(t, "OPS001")
	if !strings.Contains(output, "Backup completed successfully") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
	info := inventory.Find(t, env.backupDir, naming.BackupEntry{DirectoryName: "source", ChainID: "OPS001", Date: "2026-05-31"})
	if !info.Complete() {
		t.Fatalf("expected complete set, got %v", info.Err)
	}
}

// TestOperationRunVerifiesAfterBackup runs a backup with
// verify_after_backup enabled and confirms the freshly written set is
// re-read, decrypted, and checked in the same run.
func TestOperationRunVerifiesAfterBackup(t *testing.T) {
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

func TestOperationRunPrintsLogFileLast(t *testing.T) {
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
	info := inventory.Find(t, env.backupDir, naming.BackupEntry{DirectoryName: "source", ChainID: "OPS004", Date: "2026-05-31"})
	if !info.Complete() {
		t.Fatalf("set not complete in backup directory: %v", info.Err)
	}
	if temps, _ := catalog.ListTempParts(env.backupDir); len(temps) != 0 {
		t.Fatalf("temporary parts left behind: %v", temps)
	}
}

// TestOperationRunReportsALogLeftInTemp makes the log's append to the
// backup directory fail (its path is a directory): the backup still
// succeeds, its result counts the problem as a warning, and the UI learns
// where the run's lines are, before the result (RF-109).
func TestOperationRunReportsALogLeftInTemp(t *testing.T) {
	env := newOperationEnv(t, "operation payload")
	if err := os.Mkdir(env.logPath, 0o750); err != nil {
		t.Fatal(err)
	}
	ks, master := testutil.NewPasswordKeySet(t, []byte("op-pw"))
	op := &operation{cfg: env.cfg, log: env.logger, logPath: env.logPath, backupDir: env.backupDir, date: "2026-05-31", runID: "OPS005", keySet: ks, master: master}
	ui := &interacttest.Script{Out: &env.out}
	if err := op.run(context.Background(), ui, env.sources, nil); err != nil {
		t.Fatalf("backup run failed: %v", err)
	}
	if len(ui.LogFailures) != 1 {
		t.Fatalf("log failures = %+v, want one", ui.LogFailures)
	}
	f := ui.LogFailures[0]
	t.Cleanup(func() { os.Remove(f.Kept) })
	if f.Kind != interact.LogKept || f.Kept == "" {
		t.Fatalf("failure = %+v, want the copy in TEMP", f)
	}
	data, err := os.ReadFile(f.Kept)
	if err != nil || !strings.Contains(string(data), "Backup completed successfully") {
		t.Fatalf("the copy in TEMP lacks the run's lines: %v", err)
	}
	if out := env.out.String(); !strings.Contains(out, "Warnings: 1") {
		t.Fatalf("the result doesn't count the problem as a warning: %q", out)
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

	failures, _ := verifyBackupAfterWrite(context.Background(), nil, fx.BackupDir, []naming.BackupEntry{fx.Entry}, fx.Master, logging.NewOutputLogger("info", nil))
	if failures != 1 {
		t.Fatalf("expected 1 verification failure, got %d", failures)
	}
	remaining, _ := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if len(remaining) != len(parts) {
		t.Fatalf("expected backup files to be kept (%d), got %d", len(parts), len(remaining))
	}
}
