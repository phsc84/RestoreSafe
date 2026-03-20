package restore

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
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
