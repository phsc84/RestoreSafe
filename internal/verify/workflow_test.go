package verify

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestVerifyEntryWrongPassword(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))

	err := verifyEntry(fx.Entry, fx.TargetDir, []byte("wrong-pass"), nil)
	if err == nil {
		t.Fatal("expected verifyEntry to fail with wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}

func TestResolveVerifySelectionNonInteractiveUsesNewestRun(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	oldEntry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-01", ID: util.BackupID("AAAAAA")}
	newEntry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-02", ID: util.BackupID("BBBBBB")}

	oldPart := filepath.Join(targetDir, "[Docs]_2026-03-01_AAAAAA-001.enc")
	newPart := filepath.Join(targetDir, "[Docs]_2026-03-02_BBBBBB-001.enc")
	if err := os.WriteFile(oldPart, []byte("old"), 0o600); err != nil {
		t.Fatalf("failed to write old part: %v", err)
	}
	if err := os.WriteFile(newPart, []byte("new"), 0o600); err != nil {
		t.Fatalf("failed to write new part: %v", err)
	}

	now := time.Now()
	if err := os.Chtimes(oldPart, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("failed to set old part timestamps: %v", err)
	}
	if err := os.Chtimes(newPart, now, now); err != nil {
		t.Fatalf("failed to set new part timestamps: %v", err)
	}

	cfg := &util.Config{NonInteractive: true}
	selected, selection, err := resolveVerifySelection(cfg, targetDir, []util.BackupEntry{oldEntry, newEntry})
	if err != nil {
		t.Fatalf("resolveVerifySelection returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("expected exactly 1 selected entry, got %d", len(selected))
	}
	if selected[0] != newEntry {
		t.Fatalf("expected newest entry %v, got %v", newEntry, selected[0])
	}
	if !strings.Contains(strings.ToLower(selection), "newest") {
		t.Fatalf("expected selection label to mention newest, got %q", selection)
	}
}

func TestPlanVerifyLocalStaging(t *testing.T) {
	t.Parallel()
	// Just test that the staging behavior is consistent with restore/backup behavior
	// The actual path detection is tested in util/path_test.go
	plan := operation.PlanLocalStaging(`M:\Backups`, `M:\Restore`, `C:\Temp`)
	if !plan.Enabled {
		t.Error("Expected staging to be enabled for same-volume sources with local temp")
	}
	if !plan.SameVolume {
		t.Error("Expected same-volume flag to be set")
	}
}
