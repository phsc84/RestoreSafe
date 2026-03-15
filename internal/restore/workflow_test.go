package restore

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
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

func TestBackupAndRestoreEntryRoundTrip(t *testing.T) {
	password := []byte("integration-test-password")
	fx := testutil.NewRestoreFixture(t, password)

	if fx.Parts < 2 {
		t.Fatalf("expected multiple split parts, got %d", fx.Parts)
	}

	if _, err := restoreEntry(fx.Entry, fx.TargetDir, fx.RestoreRoot, password, nil); err != nil {
		t.Fatalf("restoreEntry returned error: %v", err)
	}

	restoredDir := filepath.Join(fx.RestoreRoot, fx.Entry.FolderName)
	testutil.AssertFileContentEqual(t, filepath.Join(fx.SrcDir, "nested", "small.txt"), filepath.Join(restoredDir, "nested", "small.txt"))
	testutil.AssertFileContentEqual(t, filepath.Join(fx.SrcDir, "large.bin"), filepath.Join(restoredDir, "large.bin"))
}

func TestRestoreEntryWrongPassword(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("correct-password"))

	_, err := restoreEntry(fx.Entry, fx.TargetDir, fx.RestoreRoot, []byte("wrong-password"), nil)
	if err == nil {
		t.Fatal("expected restoreEntry to fail for wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
