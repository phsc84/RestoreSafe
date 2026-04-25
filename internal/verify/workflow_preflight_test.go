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
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, true, false, operation.LocalStagingPlan{}, func() error { return nil })
	})

	selectionLine := "  [OK]    " + items[0].Entry.String() + " (parts: 1)"
	authLine := "Authentication : password + YubiKey"
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

func TestPrintVerifyPreflightShowsYubiKeyWarnAfterAuthentication(t *testing.T) {
	targetDir := t.TempDir()
	items := []verifyPreflightItem{{Entry: util.BackupEntry{FolderName: "Docs", Date: "2026-03-20", ID: util.BackupID("ABC123")}, PartCount: 1}}

	output := testutil.CaptureStdout(t, func() {
		printVerifyPreflightWithYubiKeyCheck(&util.Config{}, targetDir, items, true, false, operation.LocalStagingPlan{}, func() error { return errors.New("no YubiKey detected") })
	})

	authLine := "Authentication : password + YubiKey"
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
