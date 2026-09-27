package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if a := partCountAdvisory(naming.MaxPartSequence); a == "" {
		t.Fatal("expected advisory at the limit, got empty")
	}
	if a := partCountAdvisory(naming.MaxPartSequence + 1); a != "" {
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
	payload := make([]byte, (naming.MaxPartSequence+1)*oneMB)
	if err := os.WriteFile(filepath.Join(sourceDir, "big.bin"), payload, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := &config.Config{SplitSizeMB: 1}
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

	cfg := &config.Config{SplitSizeMB: 1}
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "info"}
	sources := []backupSource{{Resolved: sourceDir}}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, operation.LocalStagingPlan{}, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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
	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "info"}
	sources := []backupSource{
		{Resolved: filepath.Join(backupDir, "Docs"), BackupName: "CustomDocs", Err: errors.New("access denied")},
		{Resolved: filepath.Join(backupDir, "Photos"), Warning: "Large source"},
	}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
	output := sb.String()

	warnLinePrefix := "→ Source and backup directories are on the same drive/share"
	if strings.Contains(output, warnLinePrefix) {
		t.Fatalf("did not expect same-volume warning on local drive/share, got output: %q", output)
	}
}

func TestPrintBackupPreflightShowsSameVolumeWarningForNetworkShare(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "debug"}
	backupDir := `\\server\share\target`
	sources := []backupSource{{Resolved: `\\server\share\source`}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: config.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 2, AuthenticationMode: config.AuthModePasswordYubiKey, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return errors.New("no YubiKey detected") }))
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: true, SameVolume: true, ResolvedTempDir: localStagingDir}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
	output := sb.String()

	if strings.Contains(output, "Temp directory:") {
		t.Fatalf("did not expect Temp directory section when local staging is disabled, got: %q", output)
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

	cfg := &config.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: config.AuthModePassword, LogLevel: "debug"}
	sources := []backupSource{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false}

	var sb strings.Builder
	interact.WriteReport(&sb, backupPreflightReport(cfg, backupDir, sources, stagingPlan, keyPlan{NewKeysReason: "No existing keys found in the backup directory"}, nil, estimateBackupSpace(cfg, backupDir, sources, nil), func() error { return nil }))
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

func TestBackupPreflightIssuesCollectsEveryFailedCheck(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "huge")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, (naming.MaxPartSequence+1)*1024*1024)
	if err := os.WriteFile(filepath.Join(sourceDir, "big.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SplitSizeMB: 1}
	sources := []backupSource{{Resolved: sourceDir}, {Resolved: filepath.Join(root, "gone"), Err: errors.New("not found")}}

	issues, err := backupPreflightIssues(cfg, root, sources, operation.LocalStagingPlan{}, estimateBackupSpace(cfg, root, sources, nil))
	if err == nil || !strings.HasPrefix(err.Error(), "Backup preflight failed: 1 source directory(s)") {
		t.Fatalf("expected the source error first, got %v", err)
	}
	if len(issues) != 2 || issues[0].Status != interact.StatusError || !strings.Contains(issues[1].Text, "part files") {
		t.Fatalf("expected source and part count issues, got %+v", issues)
	}
	if strings.HasPrefix(issues[0].Text, "Backup preflight failed") {
		t.Fatalf("issue text must not repeat the error prefix: %q", issues[0].Text)
	}

	if issues, err := backupPreflightIssues(cfg, root, nil, operation.LocalStagingPlan{}, spaceEstimate{}); err != nil || len(issues) != 0 {
		t.Fatalf("no sources to check: got %+v, %v", issues, err)
	}
}

func TestBackupPreflightReportStructure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := &config.Config{SplitSizeMB: 64, AuthenticationMode: config.AuthModeYubiKey, LogLevel: "info"}
	sources := []backupSource{{Resolved: root}, {Resolved: filepath.Join(root, "gone"), Err: errors.New("not found")}}

	r := backupPreflightReport(cfg, root, sources, operation.LocalStagingPlan{}, keyPlan{NewKeysReason: "No existing keys"}, nil, spaceEstimate{}, func() error { return errors.New("no") })
	if r.Title != "Backup preflight" || len(r.Sections) != 2 {
		t.Fatalf("unexpected report: %+v", r)
	}
	var statuses []interact.Status
	for _, row := range r.Sections[0].Rows {
		if row.Kind == interact.RowItem {
			statuses = append(statuses, row.Status)
		}
	}
	// Sources OK and ERROR, backup directory OK, YubiKey WARN, two key notes.
	want := []interact.Status{interact.StatusOK, interact.StatusError, interact.StatusOK, interact.StatusWarn, interact.StatusInfo, interact.StatusInfo}
	if len(statuses) != len(want) {
		t.Fatalf("item statuses %v, want %v", statuses, want)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("item statuses %v, want %v", statuses, want)
		}
	}
}

func TestEstimateBackupSpaceMeasuresRunnableSources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcA := filepath.Join(root, "A")
	srcB := filepath.Join(root, "B")
	for dir, content := range map[string]string{srcA: "12345", srcB: "1234567890"} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	notADir := filepath.Join(root, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := []backupSource{
		{Resolved: srcA},
		{Resolved: srcB, Skip: true},
		{Resolved: filepath.Join(root, "missing"), Err: os.ErrNotExist},
		{Resolved: filepath.Join(root, "gone")},
	}

	est := estimateBackupSpace(&config.Config{}, filepath.Join(root, "Backups"), sources, nil)
	if est.full != 5 || est.likely != 5 || est.anyDiff || len(est.sizes) != 1 {
		t.Fatalf("unexpected estimate %+v", est)
	}
	if len(est.warnings) != 1 || !strings.Contains(est.warnings[0], "gone") {
		t.Fatalf("expected a warning for the unreadable source, got %#v", est.warnings)
	}
	if est.neededText() != "5 B" {
		t.Fatalf("full backups show one size, got %q", est.neededText())
	}
}

func TestEstimateBackupSpaceCountsOnlyChangedFilesForDifferentials(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "Docs")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The full backup was made after the file was written: nothing changed
	// since. A full backup made in the past counts the file as changed.
	base := func(created time.Time) map[string]*dirPlan {
		return map[string]*dirPlan{"Docs": {Base: &catalog.SetInfo{Header: &container.Header{CreatedUTC: created.UTC().Format(time.RFC3339)}}}}
	}
	sources := []backupSource{{Resolved: src}}

	est := estimateBackupSpace(&config.Config{}, root, sources, base(time.Now().Add(time.Hour)))
	if !est.anyDiff || est.full != 10 || est.likely != 0 {
		t.Fatalf("unchanged since the full backup: %+v", est)
	}
	est = estimateBackupSpace(&config.Config{}, root, sources, base(time.Now().Add(-time.Hour)))
	if est.full != 10 || est.likely != 10 {
		t.Fatalf("written after the full backup: %+v", est)
	}
	if got := est.neededText(); got != "about 10 B (files changed since the full backup); up to 10 B if everything is stored again" {
		t.Fatalf("unexpected text %q", got)
	}
}

func TestValidateSpaceErrorOnlyWhenTheEstimateDoesNotFit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	free, err := fsx.QueryFreeSpaceBytes(dir)
	if err != nil {
		t.Skip("free space unknown")
	}
	staging := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: dir}
	tooMuch := int64(free) + 1<<30

	cases := []struct {
		name     string
		est      spaceEstimate
		wantErr  bool
		wantWarn bool
	}{
		{"fits", spaceEstimate{full: 1, likely: 1}, false, false},
		{"full backup too large", spaceEstimate{full: tooMuch, likely: tooMuch}, true, false},
		{"differential estimate too large", spaceEstimate{full: tooMuch, likely: tooMuch, anyDiff: true}, true, false},
		{"differential fits only by its estimate", spaceEstimate{full: tooMuch, likely: 1, anyDiff: true}, false, true},
	}
	for _, c := range cases {
		for where, check := range map[string]func() (string, error){
			"target":  func() (string, error) { return validateTargetSpaceForBackup(dir, c.est) },
			"staging": func() (string, error) { return validateStagingSpaceForBackup(staging, c.est) },
		} {
			warning, err := check()
			if (err != nil) != c.wantErr || (warning != "") != c.wantWarn {
				t.Errorf("%s / %s: warning %q, error %v", c.name, where, warning, err)
			}
			if warning != "" && !strings.Contains(warning, "Moved or renamed files count as new") {
				t.Errorf("%s / %s: the warning must explain the risk: %q", c.name, where, warning)
			}
		}
	}

	// Unknown free space and disabled staging never block.
	if w, err := validateTargetSpaceForBackup(filepath.Join(dir, "missing"), spaceEstimate{full: tooMuch, likely: tooMuch}); err != nil || w != "" {
		t.Fatalf("unknown free space: %q, %v", w, err)
	}
	if w, err := validateStagingSpaceForBackup(operation.LocalStagingPlan{}, spaceEstimate{full: tooMuch, likely: tooMuch}); err != nil || w != "" {
		t.Fatalf("staging disabled: %q, %v", w, err)
	}
}

func TestBackupPreflightIssuesWarnsForALargeDifferential(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	free, err := fsx.QueryFreeSpaceBytes(dir)
	if err != nil {
		t.Skip("free space unknown")
	}
	est := spaceEstimate{full: int64(free) + 1<<30, likely: 1, anyDiff: true}
	issues, err := backupPreflightIssues(&config.Config{}, dir, nil, operation.LocalStagingPlan{}, est)
	if err != nil || len(issues) != 1 || issues[0].Status != interact.StatusWarn {
		t.Fatalf("expected one warning and no error, got %+v, %v", issues, err)
	}
}

func TestCheckSpaceForFullBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	free, err := fsx.QueryFreeSpaceBytes(dir)
	if err != nil {
		t.Skip("free space unknown")
	}
	tooMuch := int64(free) + 1<<30
	if err := checkSpaceForFullBackups(dir, operation.LocalStagingPlan{}, spaceEstimate{full: tooMuch, likely: 1, anyDiff: true}); err == nil || !strings.HasPrefix(err.Error(), "Full backup not started: Insufficient free space") {
		t.Fatalf("a full backup that does not fit must be refused, got %v", err)
	}
	if err := checkSpaceForFullBackups(dir, operation.LocalStagingPlan{}, spaceEstimate{full: 1, likely: 1, anyDiff: true}); err != nil {
		t.Fatalf("a full backup that fits: %v", err)
	}
	if err := checkSpaceForFullBackups(dir, operation.LocalStagingPlan{}, spaceEstimate{full: tooMuch, likely: tooMuch}); err != nil {
		t.Fatalf("without differentials the preflight already checked the full size: %v", err)
	}
}
