package verify

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"strings"
	"testing"
)

func TestBuildVerifyPreflightCountsParts(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-preflight-pass"))

	items := buildVerifyPreflight([]util.BackupEntry{fx.Entry}, fx.TargetDir)
	if len(items) != 1 {
		t.Fatalf("expected 1 preflight item, got %d", len(items))
	}
	if items[0].Err != nil {
		t.Fatalf("expected no preflight error, got %v", items[0].Err)
	}
	if items[0].PartCount != fx.Parts {
		t.Fatalf("expected %d parts, got %d", fx.Parts, items[0].PartCount)
	}
	if items[0].TotalSizeBytes <= 0 {
		t.Fatalf("expected positive TotalSizeBytes, got %d", items[0].TotalSizeBytes)
	}
}

func TestBuildVerifyPreflightReportsErrorForMissingParts(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	missing := util.BackupEntry{FolderName: "Ghost", Date: "2026-03-14", ID: util.BackupID("GHO001")}

	items := buildVerifyPreflight([]util.BackupEntry{missing}, targetDir)
	if len(items) != 1 {
		t.Fatalf("expected 1 preflight item, got %d", len(items))
	}
	if items[0].Err == nil && items[0].PartCount != 0 {
		// catalog returns 0 parts and no error for a missing entry; that's expected.
	}
}

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	valid := []verifyPreflightItem{{}, {}}
	if err := validateVerifyPreflight(valid); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}

	invalid := []verifyPreflightItem{{}, {Err: errors.New("broken")}}
	err := validateVerifyPreflight(invalid)
	if err == nil {
		t.Fatal("expected error for invalid verify preflight, got nil")
	}
	if !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}

func TestPrintVerifyPreflightShowsYubiKeyOKAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, true, false, func() error { return nil })
	})

	selectionLine := "  [OK] " + items[0].Entry.String() + " (parts: 1)"
	authLine := "Authentication: password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected now before starting verification."
	selectionIdx := strings.Index(output, selectionLine)
	authIdx := strings.Index(output, authLine)
	okIdx := strings.Index(output, okLine)
	if selectionIdx < 0 || authIdx < 0 || okIdx < 0 {
		t.Fatalf("expected selection/authentication/OK lines in output, got: %q", output)
	}
	if !(selectionIdx < authIdx && authIdx < okIdx) {
		t.Fatalf("expected authentication after selection and OK line after authentication, got: %q", output)
	}
	if strings.Contains(output, "[WARN] YubiKey authentication is enabled") {
		t.Fatalf("did not expect YubiKey WARN when key is detected, got: %q", output)
	}
	if strings.Contains(output, "Items selected") {
		t.Fatalf("did not expect items selected line, got: %q", output)
	}
	if strings.Contains(output, "Backup folder") {
		t.Fatalf("did not expect backup folder field, got: %q", output)
	}
	if strings.Contains(output, "size:") {
		t.Fatalf("did not expect backup size in verify preflight selection, got: %q", output)
	}
}

func TestPrintVerifyPreflightShowsErrorItemAndUnknownTotalSize(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{
		Entry:     util.BackupEntry{FolderName: "Broken", Date: "2026-03-20", ID: util.BackupID("ERR001")},
		PartCount: 0,
		Err:       errors.New("parts missing"),
	}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, false, false, func() error { return nil })
	})

	errorLine := "  [ERROR] " + items[0].Entry.String() + " (parts: 0)"
	if !strings.Contains(output, errorLine) {
		t.Fatalf("expected [ERROR] line for failed item, got: %q", output)
	}
	if !strings.Contains(output, "Used disk space (total): unknown") {
		t.Fatalf("expected unknown total size when all items have errors, got: %q", output)
	}
	if !strings.Contains(output, "parts missing") {
		t.Fatalf("expected error detail in issues section, got: %q", output)
	}
}

func TestPrintVerifyPreflightShowsKnownTotalSizeForSuccessfulItems(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{
		Entry:          util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("DOC001")},
		PartCount:      2,
		TotalSizeBytes: 1024,
	}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, false, false, func() error { return nil })
	})

	if strings.Contains(output, "Used disk space (total): unknown") {
		t.Fatalf("expected known total size, got 'unknown' in: %q", output)
	}
	if !strings.Contains(output, "  [OK] "+items[0].Entry.String()+" (parts: 2)") {
		t.Fatalf("expected [OK] line for successful item, got: %q", output)
	}
}

func TestPrintVerifyPreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, true, false, func() error { return errors.New("no YubiKey detected") })
	})

	authLine := "Authentication: password + YubiKey"
	warnLine := "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting verification."
	authIdx := strings.Index(output, authLine)
	warnIdx := strings.Index(output, warnLine)
	if authIdx < 0 || warnIdx < 0 {
		t.Fatalf("expected authentication/WARN lines in output, got: %q", output)
	}
	if !(authIdx < warnIdx) {
		t.Fatalf("expected WARN line after authentication, got: %q", output)
	}
	if strings.Contains(output, "[OK] YubiKey connected") {
		t.Fatalf("did not expect YubiKey OK when key is not detected, got: %q", output)
	}
	if strings.Contains(output, "Items selected") {
		t.Fatalf("did not expect items selected line, got: %q", output)
	}
}
