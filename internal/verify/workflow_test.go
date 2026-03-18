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

func TestStageSelectedVerifyPartsCopiesOnlySelectedEntries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "backups")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source directory: %v", err)
	}

	selectedEntry := util.BackupEntry{FolderName: "Alpha", Date: "2026-03-16", ID: util.BackupID("AAA111")}
	otherEntry := util.BackupEntry{FolderName: "Beta", Date: "2026-03-16", ID: util.BackupID("BBB222")}

	selectedPart1 := util.PartFileName(sourceDir, selectedEntry.FolderName, selectedEntry.Date, selectedEntry.ID, 1)
	selectedPart2 := util.PartFileName(sourceDir, selectedEntry.FolderName, selectedEntry.Date, selectedEntry.ID, 2)
	otherPart := util.PartFileName(sourceDir, otherEntry.FolderName, otherEntry.Date, otherEntry.ID, 1)

	for filePath, content := range map[string]string{
		selectedPart1: "selected-1",
		selectedPart2: "selected-2",
		otherPart:     "other-1",
	} {
		if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write test part %s: %v", filePath, err)
		}
	}

	stagedDir, err := stageSelectedVerifyParts([]util.BackupEntry{selectedEntry}, sourceDir, tempDir, nil)
	if err != nil {
		t.Fatalf("stageSelectedVerifyParts failed: %v", err)
	}

	stagedEntries, err := os.ReadDir(stagedDir)
	if err != nil {
		t.Fatalf("failed to list staged directory: %v", err)
	}

	stagedNames := make(map[string]bool, len(stagedEntries))
	for _, entry := range stagedEntries {
		stagedNames[entry.Name()] = true
	}

	if !stagedNames[filepath.Base(selectedPart1)] {
		t.Fatalf("expected selected part %q to be staged", filepath.Base(selectedPart1))
	}
	if !stagedNames[filepath.Base(selectedPart2)] {
		t.Fatalf("expected selected part %q to be staged", filepath.Base(selectedPart2))
	}
	if stagedNames[filepath.Base(otherPart)] {
		t.Fatalf("did not expect unselected part %q to be staged", filepath.Base(otherPart))
	}
	if len(stagedNames) != 2 {
		t.Fatalf("expected exactly 2 staged files, got %d", len(stagedNames))
	}
}

func TestStageSelectedVerifyPartsCleanupRemovesStageRoot(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "backups")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source directory: %v", err)
	}

	entry := util.BackupEntry{FolderName: "Gamma", Date: "2026-03-18", ID: util.BackupID("CCC333")}
	part := util.PartFileName(sourceDir, entry.FolderName, entry.Date, entry.ID, 1)
	if err := os.WriteFile(part, []byte("selected"), 0o600); err != nil {
		t.Fatalf("failed to write test part: %v", err)
	}

	stagedDir, err := stageSelectedVerifyParts([]util.BackupEntry{entry}, sourceDir, tempDir, nil)
	if err != nil {
		t.Fatalf("stageSelectedVerifyParts failed: %v", err)
	}

	if err := os.RemoveAll(stagedDir); err != nil {
		t.Fatalf("failed to remove staged directory %q: %v", stagedDir, err)
	}

	remainingStageRoots, err := filepath.Glob(filepath.Join(tempDir, "verify-stage-*"))
	if err != nil {
		t.Fatalf("failed to glob verify-stage directories: %v", err)
	}
	if len(remainingStageRoots) > 0 {
		t.Fatalf("expected no remaining verify-stage roots after cleanup, found: %v", remainingStageRoots)
	}
}
