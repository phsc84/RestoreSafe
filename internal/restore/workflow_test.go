package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bytes"
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
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src-data")
	targetDir := filepath.Join(workspace, "target")
	restoreRoot := filepath.Join(workspace, "restore")

	if err := os.MkdirAll(filepath.Join(srcDir, "nested"), 0o750); err != nil {
		t.Fatalf("failed to create source folder structure: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	smallContent := []byte("hello restoresafe")
	if err := os.WriteFile(filepath.Join(srcDir, "nested", "small.txt"), smallContent, 0o600); err != nil {
		t.Fatalf("failed to write small source file: %v", err)
	}

	largeContent := bytes.Repeat([]byte("A"), 2*1024*1024+256)
	if err := os.WriteFile(filepath.Join(srcDir, "large.bin"), largeContent, 0o600); err != nil {
		t.Fatalf("failed to write large source file: %v", err)
	}

	backupDate := "2026-03-14"
	backupID := util.BackupID("ABC123")
	folderName := filepath.Base(srcDir)
	password := []byte("integration-test-password")

	partsCreated := util.CreateEncryptedSplitBackupForTest(t, srcDir, targetDir, folderName, backupDate, backupID, password, 1)
	if partsCreated < 2 {
		t.Fatalf("expected multiple split parts, got %d", partsCreated)
	}

	entry := util.BackupEntry{FolderName: folderName, Date: backupDate, ID: backupID}
	parts := catalog.CollectParts(targetDir, entry)
	if len(parts) < 2 {
		t.Fatalf("expected multiple split parts, got %d", len(parts))
	}

	if _, err := restoreEntry(entry, targetDir, restoreRoot, password, nil); err != nil {
		t.Fatalf("restoreEntry returned error: %v", err)
	}

	restoredDir := filepath.Join(restoreRoot, folderName)
	assertFileContentEqual(t, filepath.Join(srcDir, "nested", "small.txt"), filepath.Join(restoredDir, "nested", "small.txt"))
	assertFileContentEqual(t, filepath.Join(srcDir, "large.bin"), filepath.Join(restoredDir, "large.bin"))
}

func TestRestoreEntryWrongPassword(t *testing.T) {
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src")
	targetDir := filepath.Join(workspace, "target")
	restoreRoot := filepath.Join(workspace, "restore")

	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backupDate := "2026-03-14"
	backupID := util.BackupID("DEF456")
	folderName := filepath.Base(srcDir)

	util.CreateEncryptedSplitBackupForTest(t, srcDir, targetDir, folderName, backupDate, backupID, []byte("correct-password"), 1)

	entry := util.BackupEntry{FolderName: folderName, Date: backupDate, ID: backupID}
	_, err := restoreEntry(entry, targetDir, restoreRoot, []byte("wrong-password"), nil)
	if err == nil {
		t.Fatal("expected restoreEntry to fail for wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}

func assertFileContentEqual(t *testing.T, expectedPath, actualPath string) {
	t.Helper()

	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read expected file %s: %v", expectedPath, err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("failed to read actual file %s: %v", actualPath, err)
	}

	if !bytes.Equal(expected, actual) {
		t.Fatalf("file contents differ for %s vs %s", expectedPath, actualPath)
	}
}
