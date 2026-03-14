package engine

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSelectionByIDAndFullName(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Pics", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Music", Date: "2026-03-13", ID: util.BackupID("ZZZ999")},
	}

	matchedByID, err := resolveSelection("abc123", index)
	if err != nil {
		t.Fatalf("resolveSelection by ID returned error: %v", err)
	}
	if len(matchedByID) != 2 {
		t.Fatalf("expected 2 matches by ID, got %d", len(matchedByID))
	}

	matchedByName, err := resolveSelection("docs_2026-03-14_abc123", index)
	if err != nil {
		t.Fatalf("resolveSelection by full name returned error: %v", err)
	}
	if len(matchedByName) != 1 || matchedByName[0].FolderName != "Docs" {
		t.Fatalf("expected single Docs match, got %#v", matchedByName)
	}

	if _, err := resolveSelection("NOPE00", index); err == nil {
		t.Fatal("expected error for unknown backup selection")
	}
}

func TestResolveSelectionByIDUsesNewestDateOnly(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Pics", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Old", Date: "2026-03-10", ID: util.BackupID("ABC123")},
	}

	selected, err := resolveSelection("ABC123", index)
	if err != nil {
		t.Fatalf("resolveSelection returned error: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected entries from newest date, got %d", len(selected))
	}
	for _, entry := range selected {
		if entry.Date != "2026-03-14" {
			t.Fatalf("expected only newest date entries, got entry date %s", entry.Date)
		}
	}
}

func TestSortedBackupIDDatesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "A", Date: "2026-03-13", ID: util.BackupID("ZZZ999")},
		{FolderName: "B", Date: "2026-03-14", ID: util.BackupID("BBB222")},
		{FolderName: "C", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "D", Date: "2026-03-14", ID: util.BackupID("AAA111")},
	}

	items := sortedBackupIDDates(index)
	if len(items) != 3 {
		t.Fatalf("expected 3 unique date/ID items, got %d", len(items))
	}

	if items[0].Date != "2026-03-14" || items[0].ID != "AAA111" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[1].Date != "2026-03-14" || items[1].ID != "BBB222" {
		t.Fatalf("unexpected second item: %#v", items[1])
	}
	if items[2].Date != "2026-03-13" || items[2].ID != "ZZZ999" {
		t.Fatalf("unexpected third item: %#v", items[2])
	}
}

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

func TestCompletedActionLabel(t *testing.T) {
	t.Parallel()

	if got := completedActionLabel("restore"); got != "restored" {
		t.Fatalf("expected restored, got %q", got)
	}
	if got := completedActionLabel("verify"); got != "verified" {
		t.Fatalf("expected verified, got %q", got)
	}
	if got := completedActionLabel("clean"); got != "cleaned" {
		t.Fatalf("expected cleaned fallback, got %q", got)
	}
}
