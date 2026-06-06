package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"path/filepath"
	"testing"
)

func TestRestoreSelectedEntriesRoundTrip(t *testing.T) {
	password := []byte("selected-entries-password")
	fx := testutil.NewRestoreFixture(t, password)

	total, err := restoreSelectedEntries(
		[]util.BackupEntry{fx.Entry},
		fx.BackupDir,
		fx.RestoreRoot,
		password,
		nil,
		operation.LocalStagingPlan{},
	)
	if err != nil {
		t.Fatalf("restoreSelectedEntries failed: %v", err)
	}
	if total != fx.Parts {
		t.Fatalf("expected %d total parts processed, got %d", fx.Parts, total)
	}

	restoredDir := filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName)
	testutil.AssertFileContentEqual(t,
		filepath.Join(fx.SrcDir, "large.bin"),
		filepath.Join(restoredDir, "large.bin"),
	)
}

func TestRestoreSelectedEntriesWrongPasswordFails(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("correct-password"))

	_, err := restoreSelectedEntries(
		[]util.BackupEntry{fx.Entry},
		fx.BackupDir,
		fx.RestoreRoot,
		[]byte("wrong-password"),
		nil,
		operation.LocalStagingPlan{},
	)
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestRestoreSelectedEntriesWithStagingRoundTrip(t *testing.T) {
	password := []byte("staged-entries-password")
	fx := testutil.NewRestoreFixture(t, password)

	stagingDir := t.TempDir()
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: stagingDir}

	total, err := restoreSelectedEntries(
		[]util.BackupEntry{fx.Entry},
		fx.BackupDir,
		fx.RestoreRoot,
		password,
		nil,
		plan,
	)
	if err != nil {
		t.Fatalf("restoreSelectedEntries with staging failed: %v", err)
	}
	if total != fx.Parts {
		t.Fatalf("expected %d parts, got %d", fx.Parts, total)
	}

	// Staging dir should be cleaned up after successful restore.
	stagedParts, _ := catalog.CollectParts(stagingDir, fx.Entry)
	if len(stagedParts) != 0 {
		t.Fatalf("expected staging directory to be cleaned up, found %d leftover parts", len(stagedParts))
	}
}
