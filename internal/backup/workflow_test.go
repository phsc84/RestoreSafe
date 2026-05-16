package backup

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrintBackupCompletionSummaryWithWarnings(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	printBackupCompletionSummary(&sb, []string{"Docs", "Photos"}, 4, "/target/backup.log", 2)
	output := sb.String()

	if !strings.Contains(output, "Processed directories: 2") {
		t.Fatalf("expected directory count in summary, got: %q", output)
	}
	if !strings.Contains(output, "Parts created        : 4") {
		t.Fatalf("expected parts count in summary, got: %q", output)
	}
	if !strings.Contains(output, "Warnings             : 2") {
		t.Fatalf("expected warning count in summary, got: %q", output)
	}
}

func TestPrintBackupCompletionSummaryNoWarnings(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	printBackupCompletionSummary(&sb, []string{"Docs"}, 1, "/target/backup.log", 0)
	output := sb.String()

	if !strings.Contains(output, "Warnings             : none") {
		t.Fatalf("expected 'none' for zero warnings, got: %q", output)
	}
}

func TestBackupDirectoryLogsTarBeforeEncryption(t *testing.T) {
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
	_, backupErr := backupDirectory(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("ORD123"), []byte("pw"), security.DefaultArgon2Params, cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupDirectory failed: %v", backupErr)
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

func TestBackupDirectoryLogsIODiagnosticsWhenEnabled(t *testing.T) {
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	targetDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "data.bin"), []byte("hello world"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("diag-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "debug")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, IODiagnostics: true}
	_, backupErr := backupDirectory(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("DIA999"), []byte("pw"), security.DefaultArgon2Params, cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupDirectory failed: %v", backupErr)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "I/O diagnostics") {
		t.Fatalf("expected I/O diagnostics lines in log, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Part 001 size:") {
		t.Fatalf("expected per-part size line in log, got: %q", logContent)
	}
}

func TestRunReturnsErrorWhenTargetDirCannotBeCreated(t *testing.T) {
	t.Parallel()
	// Use an existing file as the target path so MkdirAll fails.
	base := t.TempDir()
	filePath := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// Append a subdir to the file path — MkdirAll will fail.
	cfg := &util.Config{TargetDirectory: filepath.Join(filePath, "sub")}
	err := Run(cfg, "")
	if err == nil {
		t.Fatal("expected error when target dir cannot be created, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to create target directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReturnsErrorWhenAllSourcesFail(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	cfg := &util.Config{
		TargetDirectory:  targetDir,
		SourceDirectories: []string{filepath.Join(targetDir, "nonexistent-source")},
	}
	err := Run(cfg, "")
	if err == nil {
		t.Fatal("expected error when all sources fail, got nil")
	}
	if !strings.Contains(err.Error(), "Backup preflight failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCancelsBackupWhenUserEntersN(t *testing.T) {
	// NOT parallel — modifies os.Stdin.
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "f.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	fmt.Fprintln(w, "n")
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; r.Close() })

	cfg := &util.Config{
		TargetDirectory:  targetDir,
		SourceDirectories: []string{sourceDir},
	}
	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = Run(cfg, "")
	})
	if runErr != nil {
		t.Fatalf("expected nil error on cancel, got: %v", runErr)
	}
	if !strings.Contains(output, "Backup cancelled.") {
		t.Fatalf("expected 'Backup cancelled.' in output, got: %q", output)
	}
}

func TestBackupDirectoryLogsPartNamesAtInfoLevel(t *testing.T) {
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
	_, backupErr := backupDirectory(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("ORD124"), []byte("pw"), security.DefaultArgon2Params, cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupDirectory failed: %v", backupErr)
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
