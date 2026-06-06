package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnableSourceCountCountsOnlyRunnablePlans(t *testing.T) {
	t.Parallel()
	plans := []backupSource{
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

func TestValidateSourceDirectoriesIncludesFailureCount(t *testing.T) {
	t.Parallel()
	err := validateSourceDirectories([]backupSource{{Resolved: "A", Err: errors.New("bad")}, {Resolved: "B", Err: errors.New("bad")}})
	if err == nil {
		t.Fatal("expected preflight validation error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2 source directory(s)") {
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

	sources := []backupSource{
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

	total, warnings := estimateSelectedSourceBytes([]backupSource{{Resolved: filePath}})
	if total != 0 {
		t.Fatalf("expected total 0 bytes when estimation fails, got %d", total)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", warnings)
	}
}

func TestEstimatePartCountUsesCeilingDivision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		size, split, want int64
	}{
		{size: 0, split: 100, want: 0},
		{size: 1, split: 100, want: 1},
		{size: 100, split: 100, want: 1},
		{size: 101, split: 100, want: 2},
		{size: 999 * 100, split: 100, want: 999},
		{size: 999*100 + 1, split: 100, want: 1000},
		{size: 100, split: 0, want: 0},
		{size: 100, split: -5, want: 0},
	}
	for _, tc := range cases {
		if got := estimatePartCount(tc.size, tc.split); got != tc.want {
			t.Fatalf("estimatePartCount(%d, %d) = %d, want %d", tc.size, tc.split, got, tc.want)
		}
	}
}

func TestEstimatePartCountWithMarginAddsHeadroom(t *testing.T) {
	t.Parallel()
	const split = 100
	cases := []struct{ size, want int64 }{
		// 5% margin: 2000 → 2100 → 21 parts (vs. 20 without margin).
		{size: 2000, want: 21},
		// 1000 → 1050 → 11 parts (vs. 10 without margin).
		{size: 1000, want: 11},
		{size: 0, want: 0},
	}
	for _, tc := range cases {
		got := estimatePartCountWithMargin(tc.size, split)
		if got != tc.want {
			t.Fatalf("estimatePartCountWithMargin(%d, %d) = %d, want %d", tc.size, split, got, tc.want)
		}
		if got < estimatePartCount(tc.size, split) {
			t.Fatalf("margin estimate %d is below raw estimate for size %d", got, tc.size)
		}
	}
}

func TestPartCountAdvisoryWarnsOnlyWhenApproachingLimit(t *testing.T) {
	t.Parallel()
	if a := partCountAdvisory(partCountWarnThreshold - 1); a != "" {
		t.Fatalf("expected no advisory below threshold, got %q", a)
	}
	if a := partCountAdvisory(partCountWarnThreshold); a == "" {
		t.Fatal("expected advisory at warn threshold, got empty")
	}
	if a := partCountAdvisory(util.MaxPartSequence); a == "" {
		t.Fatal("expected advisory at the limit, got empty")
	}
	if a := partCountAdvisory(util.MaxPartSequence + 1); a != "" {
		t.Fatalf("expected no advisory over the limit (hard-stopped elsewhere), got %q", a)
	}
}

func TestValidateBackupPartCountRejectsBackupsExceedingLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "huge")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	// With a 1 MB split, MaxPartSequence+1 MB of data exceeds the part limit.
	oneMB := 1024 * 1024
	payload := make([]byte, (util.MaxPartSequence+1)*oneMB)
	if err := os.WriteFile(filepath.Join(sourceDir, "big.bin"), payload, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1}
	sources := []backupSource{{Resolved: sourceDir}}

	err := validateBackupPartCount(cfg, sources)
	if err == nil {
		t.Fatal("expected error when backup would exceed the part limit, got nil")
	}
	if !strings.Contains(err.Error(), "part files") || !strings.Contains(err.Error(), "split_size_mb") {
		t.Fatalf("expected clear remedy mentioning part files and split_size_mb, got: %q", err.Error())
	}
}

func TestValidateBackupPartCountAllowsBackupsWithinLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "ok")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "small.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1}
	sources := []backupSource{
		{Resolved: sourceDir},
		{Resolved: filepath.Join(root, "skipped"), Skip: true},
		{Resolved: filepath.Join(root, "errored"), Err: errors.New("inaccessible")},
	}

	if err := validateBackupPartCount(cfg, sources); err != nil {
		t.Fatalf("expected no error for a small backup, got: %v", err)
	}
}

func TestPrintBackupPreflightOmitsPartCountWhenWellBelowLimit(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "info"}
	sources := []backupSource{{Resolved: sourceDir}}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, operation.LocalStagingPlan{}, func() error { return nil }, func() error { return nil })
	output := sb.String()

	// A tiny source is nowhere near the part limit, so the summary should stay
	// quiet about part counts - neither the old info line nor the advisory.
	if strings.Contains(output, "Parts per dir") {
		t.Fatalf("did not expect a parts-per-directory line for a small backup, got: %q", output)
	}
	if strings.Contains(output, "approaching the") {
		t.Fatalf("did not expect a part-limit advisory for a small backup, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsErrorSourceAndWarnSource(t *testing.T) {
	t.Parallel()
	backupDir := t.TempDir()
	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "info"}
	sources := []backupSource{
		{Resolved: filepath.Join(backupDir, "Docs"), BackupName: "CustomDocs", Err: errors.New("access denied")},
		{Resolved: filepath.Join(backupDir, "Photos"), Warning: "Large source"},
	}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	if !strings.Contains(output, "[ERROR]") {
		t.Fatalf("expected [ERROR] line for source with error, got: %q", output)
	}
	if !strings.Contains(output, "access denied") {
		t.Fatalf("expected error message in output, got: %q", output)
	}
	if !strings.Contains(output, "→ backup name: CustomDocs") {
		t.Fatalf("expected custom backup name in error section, got: %q", output)
	}
	if !strings.Contains(output, "[WARN]") {
		t.Fatalf("expected [WARN] line for source with warning, got: %q", output)
	}
	if !strings.Contains(output, "Large source") {
		t.Fatalf("expected warning message in output, got: %q", output)
	}
}

func TestPrintBackupPreflightSuppressesSameVolumeWarningOnLocalDrive(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	warnLinePrefix := "→ Source and backup directories are on the same drive/share"
	if strings.Contains(output, warnLinePrefix) {
		t.Fatalf("did not expect same-volume warning on local drive/share, got output: %q", output)
	}
}

func TestPrintBackupPreflightShowsSameVolumeWarningForNetworkShare(t *testing.T) {
	t.Parallel()
	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	backupDir := `\\server\share\target`
	sources := []backupSource{{Resolved: `\\server\share\source`}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	warnLinePrefix := "→ Source and backup directories are on the same drive/share"
	if !strings.Contains(output, warnLinePrefix) {
		t.Fatalf("expected same-volume warning line for network share, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsYubiKeyOKAfterAuthentication(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: util.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	authLine := "Authentication: password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected before starting backup."
	logLine := "Log level          : debug"
	authIdx := strings.Index(output, authLine)
	okIdx := strings.Index(output, okLine)
	logIdx := strings.Index(output, logLine)
	if authIdx < 0 || okIdx < 0 || logIdx < 0 {
		t.Fatalf("expected authentication/OK/log lines in output, got: %q", output)
	}
	if !(authIdx < okIdx && okIdx < logIdx) {
		t.Fatalf("expected OK after authentication, log after OK, got: %q", output)
	}
	if strings.Contains(output, "[WARN]") {
		t.Fatalf("did not expect WARN when YubiKey is detected, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: util.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return errors.New("no YubiKey detected") })
	output := sb.String()

	authLine := "Authentication: password + YubiKey"
	warnLine := "  [WARN] YubiKey not connected. Remedy: Connect the YubiKey before starting backup."
	logLine := "Log level          : debug"
	authIdx := strings.Index(output, authLine)
	warnIdx := strings.Index(output, warnLine)
	logIdx := strings.Index(output, logLine)
	if authIdx < 0 || warnIdx < 0 || logIdx < 0 {
		t.Fatalf("expected authentication/WARN/log lines in output, got: %q", output)
	}
	if !(authIdx < warnIdx && warnIdx < logIdx) {
		t.Fatalf("expected WARN after authentication, log after WARN, got: %q", output)
	}
	if strings.Contains(output, "[OK] YubiKey connected") {
		t.Fatalf("did not expect OK when YubiKey is not detected, got: %q", output)
	}
}

func TestPrintBackupPreflightShowsLocalFreeSpaceWhenStagingEnabled(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	localStagingDir := filepath.Join(tempRoot, "local-staging")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(localStagingDir, 0o750); err != nil {
		t.Fatalf("failed to create local staging dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: true, SameVolume: true, ResolvedTempDir: localStagingDir}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	localStagingLine := "Local staging via temp directory enabled, because source directory(s) and backup directory share the same drive"
	tempDirLine := "Temp directory:"
	localStagingIdx := strings.Index(output, localStagingLine)
	tempDirIdx := strings.Index(output, tempDirLine)
	if localStagingIdx < 0 || tempDirIdx < 0 {
		t.Fatalf("expected local staging and temp directory lines in output, got: %q", output)
	}
	if localStagingIdx >= tempDirIdx {
		t.Fatalf("expected local staging line before temp directory line, got: %q", output)
	}
	freeSpaceAfterTempDir := strings.Index(output[tempDirIdx:], "  Free disk space:")
	if freeSpaceAfterTempDir < 0 {
		t.Fatalf("expected free-space line under Temp directory section, got: %q", output)
	}
}

func TestPrintBackupPreflightOmitsLocalFreeSpaceWhenStagingDisabled(t *testing.T) {
	t.Parallel()
	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	if strings.Contains(output, "Temp directory:") {
		t.Fatalf("did not expect Temp directory section when local staging is disabled, got: %q", output)
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
	sources := []backupSource{{Resolved: sourceDir}}

	if err := validateTargetSpaceForBackup(missingTarget, sources); err != nil {
		t.Fatalf("expected no error when free space cannot be determined, got: %v", err)
	}
}

func TestValidateStagingSpaceSkipsWhenStagingDisabled(t *testing.T) {
	t.Parallel()

	sources := []backupSource{{Resolved: "C:/some/source"}}
	plan := operation.LocalStagingPlan{Enabled: false}

	if err := validateStagingSpaceForBackup(plan, sources); err != nil {
		t.Fatalf("expected no error when staging is disabled, got: %v", err)
	}
}

func TestValidateStagingSpaceSkipsWhenEstimatedBytesZero(t *testing.T) {
	t.Parallel()

	// All sources are skipped → estimatedBytes = 0 → return nil.
	sources := []backupSource{{Resolved: "C:/source", Skip: true}}
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: t.TempDir()}

	if err := validateStagingSpaceForBackup(plan, sources); err != nil {
		t.Fatalf("expected no error when estimated bytes is zero, got: %v", err)
	}
}

func TestValidateStagingSpacePassesWhenSpaceIsSufficient(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	stagingDir := filepath.Join(root, "staging")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(stagingDir, 0o750); err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "small.bin"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	sources := []backupSource{{Resolved: sourceDir}}
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: stagingDir}

	if err := validateStagingSpaceForBackup(plan, sources); err != nil {
		t.Fatalf("expected no error when space is sufficient, got: %v", err)
	}
}

func TestValidateTargetSpacePassesWhenSpaceIsSufficient(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	backupDir := filepath.Join(root, "target")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "small.bin"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	sources := []backupSource{{Resolved: sourceDir}}
	if err := validateTargetSpaceForBackup(backupDir, sources); err != nil {
		t.Fatalf("expected no error when target space is sufficient, got: %v", err)
	}
}

func TestValidateStagingSpaceReturnsErrorWhenTempSpaceInsufficient(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	stagingDir := filepath.Join(root, "staging")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(stagingDir, 0o750); err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}

	bigFile := make([]byte, 10*1024*1024)
	if err := os.WriteFile(filepath.Join(sourceDir, "big.bin"), bigFile, 0o600); err != nil {
		t.Fatalf("failed to write big file: %v", err)
	}

	freeBytes, err := util.QueryFreeSpaceBytes(stagingDir)
	if err != nil || freeBytes > uint64(len(bigFile)) {
		t.Skip("staging dir has sufficient free space; cannot test insufficient-space path")
	}

	sources := []backupSource{{Resolved: sourceDir}}
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: stagingDir}

	if err := validateStagingSpaceForBackup(plan, sources); err == nil {
		t.Fatal("expected error for insufficient staging space, got nil")
	}
}

func TestPrintBackupPreflightOrdersSourceBeforeTargetAndPlacesNeededSpaceInSummary(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	backupDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	var sb strings.Builder
	printBackupPreflightWithYubiKeyCheck(&sb, cfg, backupDir, sources, stagingPlan, func() error { return nil }, func() error { return nil })
	output := sb.String()

	sourceIdx := strings.Index(output, "Source directory(s):")
	targetIdx := strings.Index(output, "Backup directory:")
	if sourceIdx < 0 || targetIdx < 0 {
		t.Fatalf("expected Source directory(s) and Backup directory sections, got: %q", output)
	}
	if sourceIdx > targetIdx {
		t.Fatalf("expected Source directory(s) section before Backup directory section, got: %q", output)
	}
	if !strings.Contains(output, "Needed space       : 5 B") {
		t.Fatalf("expected needed space line in summary section, got: %q", output)
	}
	if strings.Contains(output, "Needed disk space (total):") {
		t.Fatalf("did not expect needed disk space line in source section, got: %q", output)
	}
	if !strings.Contains(output, "  [OK] "+sourceDir) {
		t.Fatalf("expected single-space [OK] source line, got: %q", output)
	}

	sourceEntryIdx := strings.Index(output, "  [OK] "+sourceDir)
	neededIdx := strings.Index(output, "Needed space       : 5 B")
	if sourceEntryIdx < 0 || neededIdx < 0 {
		t.Fatalf("expected source entry and needed disk space lines, got: %q", output)
	}
	if neededIdx <= targetIdx {
		t.Fatalf("expected needed space summary after backup directory section, got: %q", output)
	}
}
