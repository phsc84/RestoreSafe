package verify

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
)

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
