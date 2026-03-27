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
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	warnLinePrefix := "-> Source and target folders are on the same drive/share"
	if strings.Contains(output, warnLinePrefix) {
		t.Fatalf("did not expect same-volume warning on local drive/share, got output: %q", output)
	}
}

func TestPrintBackupPreflightShowsSameVolumeWarningForNetworkShare(t *testing.T) {
	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	targetDir := `\\server\share\target`
	sources := []backupSourcePlan{{Resolved: `\\server\share\source`}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	warnLinePrefix := "-> Source and target folders are on the same drive/share"
	if !strings.Contains(output, warnLinePrefix) {
		t.Fatalf("expected same-volume warning line for network share, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsYubiKeyOKAfterAuthentication(t *testing.T) {
	tempRoot := t.TempDir()
	targetDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: util.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSourcePlan{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	authLine := "Authentication   : password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected now before starting backup."
	logLine := "Log level        : debug"
	authIdx := strings.Index(output, authLine)
	okIdx := strings.Index(output, okLine)
	logIdx := strings.Index(output, logLine)
	if authIdx < 0 || okIdx < 0 || logIdx < 0 {
		t.Fatalf("expected authentication/OK/log lines in output, got: %q", output)
	}
	if !(authIdx < okIdx && okIdx < logIdx) {
		t.Fatalf("expected OK line directly after authentication section, got: %q", output)
	}
	if strings.Contains(output, "[WARN]") {
		t.Fatalf("did not expect WARN when YubiKey is detected, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	tempRoot := t.TempDir()
	targetDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: util.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSourcePlan{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return errors.New("no YubiKey detected") })
	})

	authLine := "Authentication   : password + YubiKey"
	warnLine := "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting backup."
	logLine := "Log level        : debug"
	authIdx := strings.Index(output, authLine)
	warnIdx := strings.Index(output, warnLine)
	logIdx := strings.Index(output, logLine)
	if authIdx < 0 || warnIdx < 0 || logIdx < 0 {
		t.Fatalf("expected authentication/WARN/log lines in output, got: %q", output)
	}
	if !(authIdx < warnIdx && warnIdx < logIdx) {
		t.Fatalf("expected WARN line directly after authentication section, got: %q", output)
	}
	if strings.Contains(output, "[OK] YubiKey connected") {
		t.Fatalf("did not expect OK when YubiKey is not detected, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsLocalFreeSpaceWhenStagingEnabled(t *testing.T) {
	tempRoot := t.TempDir()
	targetDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	localStagingDir := filepath.Join(tempRoot, "local-staging")
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(localStagingDir, 0o750); err != nil {
		t.Fatalf("failed to create local staging dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSourcePlan{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: true, SameVolume: true, ResolvedTempDir: localStagingDir}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	localStagingLine := "Local staging    : enabled via "
	localFreeSpaceLine := "Free space local :"
	localStagingIdx := strings.Index(output, localStagingLine)
	localFreeSpaceIdx := strings.Index(output, localFreeSpaceLine)
	if localStagingIdx < 0 || localFreeSpaceIdx < 0 {
		t.Fatalf("expected local staging and local free-space lines in output, got: %q", output)
	}
	if localFreeSpaceIdx <= localStagingIdx {
		t.Fatalf("expected local free-space line to appear after local staging line, got: %q", output)
	}
}

func TestPrintBackupPreflightOmitsLocalFreeSpaceWhenStagingDisabled(t *testing.T) {
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
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	if strings.Contains(output, "Free space local :") {
		t.Fatalf("did not expect local free-space line when local staging is disabled, got: %q", output)
	}
}

func TestIsTargetSpaceInsufficient(t *testing.T) {
	t.Parallel()

	if !isTargetSpaceInsufficient(200, 100) {
		t.Fatal("expected insufficient-space predicate to be true")
	}
	if isTargetSpaceInsufficient(100, 100) {
		t.Fatal("did not expect insufficient-space predicate when estimate equals free bytes")
	}
	if isTargetSpaceInsufficient(0, 100) {
		t.Fatal("did not expect insufficient-space predicate for unknown/zero estimate")
	}
}

func TestValidateTargetSpaceForBackupSkipsWhenTargetUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	missingTarget := filepath.Join(root, "missing-target")
	sources := []backupSourcePlan{{Resolved: sourceDir}}

	if err := validateTargetSpaceForBackup(missingTarget, sources); err != nil {
		t.Fatalf("expected no error when free space cannot be determined, got: %v", err)
	}
}

func TestPrintBackupPreflightOrdersSourceBeforeTargetAndPlacesSourceSizeInSourceSection(t *testing.T) {
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

	if err := os.WriteFile(filepath.Join(sourceDir, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSourcePlan{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	output := testutil.CaptureStdout(t, func() {
		printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, func() error { return nil })
	})

	sourceIdx := strings.Index(output, "Source folder(s):")
	targetIdx := strings.Index(output, "Target folder:")
	if sourceIdx < 0 || targetIdx < 0 {
		t.Fatalf("expected Source folder(s) and Target folder sections, got: %q", output)
	}
	if sourceIdx > targetIdx {
		t.Fatalf("expected Source folder(s) section before Target folder section, got: %q", output)
	}
	if !strings.Contains(output, "[OK] Needed disk space (total):") {
		t.Fatalf("expected needed disk space line in Source folder(s) section, got: %q", output)
	}
	if strings.Contains(output, "Est. source size :") {
		t.Fatalf("did not expect standalone est-source-size field line, got: %q", output)
	}
	if !strings.Contains(output, "  [OK] "+sourceDir) {
		t.Fatalf("expected single-space [OK] source line, got: %q", output)
	}

	sourceEntryIdx := strings.Index(output, "  [OK] "+sourceDir)
	neededIdx := strings.Index(output, "  [OK] Needed disk space (total):")
	if sourceEntryIdx < 0 || neededIdx < 0 {
		t.Fatalf("expected source entry and needed disk space lines, got: %q", output)
	}
	if neededIdx <= sourceEntryIdx {
		t.Fatalf("expected needed disk space line after source entries, got: %q", output)
	}
}
