package restore

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"errors"
	"path/filepath"
	"testing"
)

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
