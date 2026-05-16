package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreSelectedEntriesRoundTrip(t *testing.T) {
	password := []byte("selected-entries-password")
	fx := testutil.NewRestoreFixture(t, password)

	total, err := restoreSelectedEntries(
		[]util.BackupEntry{fx.Entry},
		fx.TargetDir,
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
		fx.TargetDir,
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
		fx.TargetDir,
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

func TestRestoreSelectionWarningCountNonIDSelection(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
	}
	if got := restoreSelectionWarningCount("all", index); got != 0 {
		t.Fatalf("expected 0 warnings for non-ID selection, got %d", got)
	}
}

func TestRestoreSelectionWarningCountSingleDateNoWarning(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
	}
	if got := restoreSelectionWarningCount("ABC123", index); got != 0 {
		t.Fatalf("expected 0 warnings for single-date backup ID, got %d", got)
	}
}

func TestRestoreSelectionWarningCountMultipleDatesWarns(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{DirectoryName: "Photos", Date: "2026-03-15", ID: util.BackupID("ABC123")},
	}
	if got := restoreSelectionWarningCount("ABC123", index); got != 1 {
		t.Fatalf("expected 1 warning for multi-date backup ID, got %d", got)
	}
}

func TestSummarizeNames(t *testing.T) {
	t.Parallel()

	if got := summarizeNames(nil); got != "none" {
		t.Fatalf("expected 'none' for empty names, got %q", got)
	}
	if got := summarizeNames([]string{"A", "B"}); got != "A, B" {
		t.Fatalf("unexpected summary for 2 names: %q", got)
	}
	got := summarizeNames([]string{"A", "B", "C", "D", "E"})
	if !strings.Contains(got, "+2 more") {
		t.Fatalf("expected '+2 more' for 5 names, got %q", got)
	}
}
