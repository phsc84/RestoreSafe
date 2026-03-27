package verify

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"strings"
	"testing"
)

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
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1, TotalSizeBytes: 1024}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(targetDir, items, true, false, operation.LocalStagingPlan{}, func() error { return nil })
	})

	authLine := "Authentication : password + YubiKey"
	okLine := "  [OK] YubiKey connected. Keep it connected now before starting verification."
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

func TestPrintVerifyPreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1, TotalSizeBytes: 1024}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(targetDir, items, true, false, operation.LocalStagingPlan{}, func() error { return errors.New("no YubiKey detected") })
	})

	authLine := "Authentication : password + YubiKey"
	warnLine := "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting verification."
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
