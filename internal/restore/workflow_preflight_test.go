package restore

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
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

	if items[0].Err == nil {
		t.Fatal("expected error for existing output directory")
	}
	if items[1].Err == nil {
		t.Fatal("expected error for missing part files")
	}
}

func TestPrintRestorePreflightShowsYubiKeyOKAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	restorePath := t.TempDir()
	items := []restorePreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printRestorePreflightWithYubiKeyCheck(targetDir, restorePath, items, true, false, operation.LocalStagingPlan{}, func() error { return nil })
	})

	authLine := "Authentication : password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected now before starting restore."
	itemsLine := "Items selected : 1"
	authIdx := strings.Index(output, authLine)
	okIdx := strings.Index(output, okLine)
	itemsIdx := strings.Index(output, itemsLine)
	if authIdx < 0 || okIdx < 0 || itemsIdx < 0 {
		t.Fatalf("expected authentication/OK/items lines in output, got: %q", output)
	}
	if !(authIdx < okIdx && okIdx < itemsIdx) {
		t.Fatalf("expected OK line between authentication and items lines, got: %q", output)
	}
	if strings.Contains(output, "[WARN] YubiKey authentication is enabled") {
		t.Fatalf("did not expect YubiKey WARN when key is detected, got: %q", output)
	}
}

func TestPrintRestorePreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	restorePath := t.TempDir()
	items := []restorePreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printRestorePreflightWithYubiKeyCheck(targetDir, restorePath, items, true, false, operation.LocalStagingPlan{}, func() error { return errors.New("no YubiKey detected") })
	})

	authLine := "Authentication : password + YubiKey"
	warnLine := "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting restore."
	itemsLine := "Items selected : 1"
	authIdx := strings.Index(output, authLine)
	warnIdx := strings.Index(output, warnLine)
	itemsIdx := strings.Index(output, itemsLine)
	if authIdx < 0 || warnIdx < 0 || itemsIdx < 0 {
		t.Fatalf("expected authentication/WARN/items lines in output, got: %q", output)
	}
	if !(authIdx < warnIdx && warnIdx < itemsIdx) {
		t.Fatalf("expected WARN line between authentication and items lines, got: %q", output)
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

	output := testutil.CaptureStdout(t, func() {
		printRestorePreflightWithYubiKeyCheck(targetDir, restorePath, items, false, false, operation.LocalStagingPlan{}, func() error { return nil })
	})

	if !strings.Contains(output, "[ERROR] Insufficient free space for restore:") {
		t.Fatalf("expected insufficient-space restore error line, got: %q", output)
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

func TestIsRestoreSpaceInsufficient(t *testing.T) {
	t.Parallel()

	if !isRestoreSpaceInsufficient(200, 100) {
		t.Fatal("expected insufficient-space predicate to be true")
	}
	if isRestoreSpaceInsufficient(100, 100) {
		t.Fatal("did not expect insufficient-space predicate when estimate equals free bytes")
	}
	if isRestoreSpaceInsufficient(0, 100) {
		t.Fatal("did not expect insufficient-space predicate for zero estimate")
	}
}
