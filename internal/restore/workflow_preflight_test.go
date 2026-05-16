package restore

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRestorePreflightReportsErrors(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	restorePath := t.TempDir()

	entryWithParts := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	entryWithoutParts := util.BackupEntry{FolderName: "Missing", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part := util.PartFileName(targetDir, entryWithParts.FolderName, entryWithParts.Date, entryWithParts.ID, 1)
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create part file: %v", err)
	}

	existingOutDir := filepath.Join(restorePath, entryWithParts.FolderName)
	if err := os.MkdirAll(existingOutDir, 0o750); err != nil {
		t.Fatalf("failed to create restore output dir: %v", err)
	}

	items := buildRestorePreflight([]util.BackupEntry{entryWithParts, entryWithoutParts}, targetDir, restorePath)
	if len(items) != 2 {
		t.Fatalf("expected 2 preflight items, got %d", len(items))
	}

	if items[0].OutputDirErr == nil {
		t.Fatal("expected OutputDirErr for existing output directory")
	}
	if items[1].Err == nil {
		t.Fatal("expected Err for missing part files")
	}
}

func TestPrintRestorePreflightShowsRestoreFoldersWithPerFolderErrors(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	restorePath := t.TempDir()
	outputDir := filepath.Join(restorePath, "Docs")
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}
	items := []restorePreflightItem{{
		Entry:        entry,
		PartCount:    4,
		OutputDir:    outputDir,
		OutputDirErr: errors.New("Target directory already exists. Remedy: Choose a different restore destination or rename/delete the existing target directory."),
	}}

	var sb strings.Builder
	printRestorePreflightWithYubiKeyCheck(&sb, &util.Config{}, targetDir, restorePath, items, false, false, operation.LocalStagingPlan{}, func() error { return nil })
	output := sb.String()

	if strings.Contains(output, "Restore target :") {
		t.Fatalf("did not expect Restore target field, got: %q", output)
	}
	selectionLine := "  [OK] " + entry.String() + " (parts: 4)"
	folderLine := "  [ERROR] " + filepath.ToSlash(outputDir)
	errorText := "Target directory already exists. Remedy: Choose a different restore destination or rename/delete the existing target directory."
	if !strings.Contains(output, selectionLine) {
		t.Fatalf("expected backup selection to remain an OK archive entry, got: %q", output)
	}
	if !strings.Contains(output, "Restored folder(s):\n"+folderLine) {
		t.Fatalf("expected restore folder section with absolute destination path, got: %q", output)
	}
	if !strings.Contains(output, errorText) {
		t.Fatalf("expected restore folder error text, got: %q", output)
	}
	if strings.Index(output, errorText) <= strings.Index(output, folderLine) {
		t.Fatalf("expected restore folder error below folder line, got: %q", output)
	}
}

func TestPrintRestorePreflightShowsYubiKeyOKAfterAuthentication(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	restorePath := t.TempDir()
	items := []restorePreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	var sb strings.Builder
	printRestorePreflightWithYubiKeyCheck(&sb, &util.Config{}, targetDir, restorePath, items, true, false, operation.LocalStagingPlan{}, func() error { return nil })
	output := sb.String()

	authLine := "Authentication: password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected now before starting restore."
	neededLine := "  Used disk space (total):"
	authIdx := strings.Index(output, authLine)
	okIdx := strings.Index(output, okLine)
	neededIdx := strings.Index(output, neededLine)
	if authIdx < 0 || okIdx < 0 || neededIdx < 0 {
		t.Fatalf("expected authentication/OK/needed-disk-space lines in output, got: %q", output)
	}
	if !(neededIdx < authIdx && authIdx < okIdx) {
		t.Fatalf("expected needed-disk-space line before authentication, and OK line after authentication, got: %q", output)
	}
	if strings.Contains(output, "[WARN] YubiKey authentication is enabled") {
		t.Fatalf("did not expect YubiKey WARN when key is detected, got: %q", output)
	}
}

func TestPrintRestorePreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	restorePath := t.TempDir()
	items := []restorePreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	var sb strings.Builder
	printRestorePreflightWithYubiKeyCheck(&sb, &util.Config{}, targetDir, restorePath, items, true, false, operation.LocalStagingPlan{}, func() error { return errors.New("no YubiKey detected") })
	output := sb.String()

	authLine := "Authentication: password + YubiKey"
	warnLine := "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting restore."
	neededLine := "  Used disk space (total):"
	authIdx := strings.Index(output, authLine)
	warnIdx := strings.Index(output, warnLine)
	neededIdx := strings.Index(output, neededLine)
	if authIdx < 0 || warnIdx < 0 || neededIdx < 0 {
		t.Fatalf("expected authentication/WARN/needed-disk-space lines in output, got: %q", output)
	}
	if !(neededIdx < authIdx && authIdx < warnIdx) {
		t.Fatalf("expected WARN line between authentication and needed-disk-space lines, got: %q", output)
	}
	if strings.Contains(output, "[OK] YubiKey connected") {
		t.Fatalf("did not expect YubiKey OK when key is not detected, got: %q", output)
	}
}

func TestBuildRestorePreflightIncludesTotalSizeBytes(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	restorePath := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part := util.PartFileName(targetDir, entry.FolderName, entry.Date, entry.ID, 1)
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(part, []byte("1234567"), 0o600); err != nil {
		t.Fatalf("failed to create part file: %v", err)
	}

	items := buildRestorePreflight([]util.BackupEntry{entry}, targetDir, restorePath)
	if len(items) != 1 {
		t.Fatalf("expected one preflight item, got %d", len(items))
	}
	if items[0].Err != nil {
		t.Fatalf("expected no preflight error, got %v", items[0].Err)
	}
	if items[0].TotalSizeBytes != 7 {
		t.Fatalf("expected total size 7 bytes, got %d", items[0].TotalSizeBytes)
	}
}

func TestPrintRestorePreflightShowsInsufficientSpaceError(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	restorePath := t.TempDir()
	items := []restorePreflightItem{{
		Entry:          util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")},
		PartCount:      1,
		TotalSizeBytes: math.MaxInt64,
	}}

	var sb strings.Builder
	printRestorePreflightWithYubiKeyCheck(&sb, &util.Config{}, targetDir, restorePath, items, false, false, operation.LocalStagingPlan{}, func() error { return nil })
	output := sb.String()

	if !strings.Contains(output, "[ERROR] Insufficient free space for restore:") {
		t.Fatalf("expected insufficient-space restore error line, got: %q", output)
	}
}

func TestValidateRestorePreflightPassesWhenAllItemsOK(t *testing.T) {
	t.Parallel()

	items := []restorePreflightItem{
		{Entry: util.BackupEntry{FolderName: "A"}, PartCount: 1},
		{Entry: util.BackupEntry{FolderName: "B"}, PartCount: 2},
	}
	if err := validateRestorePreflight(items); err != nil {
		t.Fatalf("expected no error for valid items, got %v", err)
	}
}

func TestValidateRestorePreflightFailsForPartError(t *testing.T) {
	t.Parallel()

	items := []restorePreflightItem{
		{Entry: util.BackupEntry{FolderName: "A"}, PartCount: 1},
		{Entry: util.BackupEntry{FolderName: "B"}, Err: errors.New("no parts found")},
	}
	err := validateRestorePreflight(items)
	if err == nil {
		t.Fatal("expected error for item with Err set, got nil")
	}
	if !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestValidateRestorePreflightFailsForOutputDirError(t *testing.T) {
	t.Parallel()

	items := []restorePreflightItem{
		{Entry: util.BackupEntry{FolderName: "A"}, PartCount: 1, OutputDirErr: errors.New("already exists")},
	}
	err := validateRestorePreflight(items)
	if err == nil {
		t.Fatal("expected error for item with OutputDirErr set, got nil")
	}
}

func TestValidateStagingSpacePassesWhenNotEnabled(t *testing.T) {
	t.Parallel()
	plan := operation.LocalStagingPlan{Enabled: false}
	items := []restorePreflightItem{{TotalSizeBytes: math.MaxInt64}}
	if err := validateStagingSpace(plan, items); err != nil {
		t.Fatalf("expected no error when staging is disabled, got: %v", err)
	}
}

func TestValidateStagingSpacePassesWhenSufficientSpace(t *testing.T) {
	t.Parallel()
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: t.TempDir()}
	items := []restorePreflightItem{{TotalSizeBytes: 1}}
	if err := validateStagingSpace(plan, items); err != nil {
		t.Fatalf("expected no error when space is sufficient, got: %v", err)
	}
}

func TestValidateStagingSpaceReturnsErrorWhenInsufficient(t *testing.T) {
	t.Parallel()
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: t.TempDir()}
	items := []restorePreflightItem{{TotalSizeBytes: math.MaxInt64}}
	err := validateStagingSpace(plan, items)
	if err == nil {
		t.Fatal("expected insufficient-staging-space error, got nil")
	}
	if !strings.Contains(err.Error(), "insufficient free space at temp directory") {
		t.Fatalf("unexpected staging-space error: %v", err)
	}
}

func TestValidateRestoreTargetSpaceReturnsErrorWhenInsufficient(t *testing.T) {
	t.Parallel()

	restorePath := t.TempDir()
	items := []restorePreflightItem{{
		Entry:          util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")},
		PartCount:      1,
		TotalSizeBytes: math.MaxInt64,
	}}

	err := validateRestoreTargetSpace(restorePath, items)
	if err == nil {
		t.Fatal("expected insufficient-space error, got nil")
	}
	if !strings.Contains(err.Error(), "Restore preflight failed: Insufficient free space for restore:") {
		t.Fatalf("unexpected restore target-space error: %v", err)
	}
}

func TestValidateRestoreTargetSpaceReturnsNilWhenEstimatedZero(t *testing.T) {
	t.Parallel()
	items := []restorePreflightItem{{TotalSizeBytes: 0}}
	if err := validateRestoreTargetSpace(t.TempDir(), items); err != nil {
		t.Fatalf("expected nil when estimated bytes is zero, got: %v", err)
	}
}

func TestQueryRestoreTargetFreeBytesForExistingDir(t *testing.T) {
	t.Parallel()
	existingDir := t.TempDir()
	free, err := queryRestoreTargetFreeBytes(existingDir)
	if err != nil {
		t.Fatalf("expected no error for existing directory, got: %v", err)
	}
	if free == 0 {
		t.Fatal("expected non-zero free bytes for existing directory")
	}
}

func TestQueryRestoreTargetFreeBytesWalksUpToExistingParent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// A non-existent subdirectory path; queryRestoreTargetFreeBytes should walk up to base.
	nonExistent := filepath.Join(base, "missing", "subdir")
	free, err := queryRestoreTargetFreeBytes(nonExistent)
	if err != nil {
		t.Fatalf("expected no error when walking up to existing parent, got: %v", err)
	}
	if free == 0 {
		t.Fatal("expected non-zero free bytes after walking up to existing parent")
	}
}
