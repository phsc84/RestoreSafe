package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnableSourceCountCountsOnlyRunnablePlans(t *testing.T) {
	t.Parallel()

	plans := []backupSourcePlan{
		{Resolved: "A"},
		{Resolved: "B", Skip: true},
		{Resolved: "C", Err: errors.New("inaccessible")},
		{Resolved: "D"},
	}

	got := runnableSourceCount(plans)
	if got != 2 {
		t.Fatalf("expected runnable count 2, got %d", got)
	}
}

func TestValidateSourceFoldersIncludesFailureCount(t *testing.T) {
	t.Parallel()

	err := validateSourceFolders([]backupSourcePlan{{Resolved: "A", Err: errors.New("bad")}, {Resolved: "B", Err: errors.New("bad")}})
	if err == nil {
		t.Fatal("expected preflight validation error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2 source folder(s)") {
		t.Fatalf("expected failure count in message, got %q", msg)
	}
}

func TestEstimateSelectedSourceBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcA := filepath.Join(root, "A")
	srcB := filepath.Join(root, "B")
	if err := os.MkdirAll(srcA, 0o750); err != nil {
		t.Fatalf("failed to create source A: %v", err)
	}
	if err := os.MkdirAll(srcB, 0o750); err != nil {
		t.Fatalf("failed to create source B: %v", err)
	}

	if err := os.WriteFile(filepath.Join(srcA, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source A file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcB, "two.bin"), []byte("1234567890"), 0o600); err != nil {
		t.Fatalf("failed to write source B file: %v", err)
	}

	sources := []backupSourcePlan{
		{Resolved: srcA},
		{Resolved: srcB, Skip: true},
		{Resolved: filepath.Join(root, "missing"), Err: os.ErrNotExist},
	}

	total, warnings := estimateSelectedSourceBytes(sources)
	if total != 5 {
		t.Fatalf("expected total 5 bytes from runnable source, got %d", total)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestEstimateSelectedSourceBytesWarningOnUnreadablePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-dir-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write file path: %v", err)
	}

	total, warnings := estimateSelectedSourceBytes([]backupSourcePlan{{Resolved: filePath}})
	if total != 0 {
		t.Fatalf("expected total 0 bytes when estimation fails, got %d", total)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", warnings)
	}
}

func TestPrintBackupPreflightSuppressesSameVolumeWarningOnLocalDrive(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	targetDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSourcePlan{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflight(cfg, targetDir, sources, stagingPlan)
	})

	warnLinePrefix := "[WARN]  Source folder warning: Source and target folders are on the same drive/share"
	if strings.Contains(output, warnLinePrefix) {
		t.Fatalf("did not expect same-volume warning on local drive/share, got output: %q", output)
	}
}

func TestPrintBackupPreflightShowsSameVolumeWarningForNetworkShare(t *testing.T) {
	t.Parallel()

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	targetDir := `\\server\share\target`
	sources := []backupSourcePlan{{Resolved: `\\server\share\source`}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflight(cfg, targetDir, sources, stagingPlan)
	})

	warnLinePrefix := "[WARN]  Source folder warning: Source and target folders are on the same drive/share"
	if !strings.Contains(output, warnLinePrefix) {
		t.Fatalf("expected same-volume warning line for network share, got: %q", output)
	}
}
