package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runBackupDirectory writes one full set of a small source and returns the
// log content.
func runBackupDirectory(t *testing.T, level string, ioDiagnostics bool) (string, string) {
	t.Helper()
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	backupDir := filepath.Join(tempRoot, "target")
	createFile(t, filepath.Join(sourceDir, "sample.txt"), "hello")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(backupDir, fmt.Sprintf("test-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, level, nil)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	cfg := &util.Config{SplitSizeMB: 1, IODiagnostics: ioDiagnostics}
	entry := util.BackupEntry{DirectoryName: "source", ChainID: "ORD123", Date: "2026-03-18"}
	_, backupErr := backupDirectory(context.Background(), nil, sourceDir, entry, "ORD123", nil, backupDir, backupDir, ks, master, cfg, true, logger)
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
	_, backupDir := runBackupDirectory(t, "info", false)
	info := catalog.InspectSet(backupDir, util.BackupEntry{DirectoryName: "source", ChainID: "ORD123", Date: "2026-03-18"})
	if !info.Complete() {
		t.Fatalf("expected a complete set, got %v", info.Err)
	}
}

func TestBackupDirectoryLogsTarCreationAtDebugLevel(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "debug", false)
	if !strings.Contains(logContent, "Starting TAR creation and encryption for:") {
		t.Fatalf("expected TAR creation debug line in log, got: %q", logContent)
	}
}

func TestBackupDirectoryLogsIODiagnosticsWhenEnabled(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "debug", true)
	if !strings.Contains(logContent, "I/O diagnostics") {
		t.Fatalf("expected I/O diagnostics lines in log, got: %q", logContent)
	}
	if !strings.Contains(logContent, "Part 001 size:") {
		t.Fatalf("expected per-part size line in log, got: %q", logContent)
	}
}

func TestBackupDirectoryLogsPartNamesAtInfoLevel(t *testing.T) {
	logContent, _ := runBackupDirectory(t, "info", false)

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
	cfg := &util.Config{BackupDirectory: filepath.Join(filePath, "sub")}
	err := Run(context.Background(), &ui.Console{}, cfg, "")
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
	cfg := &util.Config{
		BackupDirectory:   backupDir,
		SourceDirectories: []string{filepath.Join(backupDir, "nonexistent-source")},
	}
	err := Run(context.Background(), &ui.Console{}, cfg, "")
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
	backupDir := t.TempDir()
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
		BackupDirectory:   backupDir,
		SourceDirectories: []string{sourceDir},
	}
	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = Run(context.Background(), &ui.Console{}, cfg, "")
	})
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
	removeLeftoverTempParts(dir, util.NewConsoleLogger("info", nil))
	assertNotExists(t, temp)
	assertExists(t, keep)
	assertExists(t, other)
}

func TestNewRunIDAvoidsUsedIDs(t *testing.T) {
	t.Parallel()
	used := make([]catalog.SetInfo, 0)
	for i := 0; i < 5; i++ {
		id, _ := util.NewBackupID()
		used = append(used, catalog.SetInfo{Entry: util.BackupEntry{ChainID: id}})
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
