package catalog

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveNewestBackupRunSelectionUsesNewestModTime(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	oldEntry := util.BackupEntry{FolderName: "Old", Date: "2026-03-15", ID: util.BackupID("ZZZ999")}
	newDocs := util.BackupEntry{FolderName: "Docs", Date: "2026-03-15", ID: util.BackupID("AAA111")}
	newPics := util.BackupEntry{FolderName: "Pics", Date: "2026-03-15", ID: util.BackupID("AAA111")}

	oldPart := util.PartFileName(targetDir, oldEntry.FolderName, oldEntry.Date, oldEntry.ID, 1)
	newDocsPart := util.PartFileName(targetDir, newDocs.FolderName, newDocs.Date, newDocs.ID, 1)
	newPicsPart := util.PartFileName(targetDir, newPics.FolderName, newPics.Date, newPics.ID, 1)

	for _, part := range []string{oldPart, newDocsPart, newPicsPart} {
		if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
			t.Fatalf("failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
			t.Fatalf("failed to create part file: %v", err)
		}
	}

	olderTime := time.Date(2026, 3, 15, 8, 0, 0, 0, time.UTC)
	newerTime := olderTime.Add(2 * time.Hour)
	if err := os.Chtimes(oldPart, olderTime, olderTime); err != nil {
		t.Fatalf("failed to set old part time: %v", err)
	}
	if err := os.Chtimes(newDocsPart, newerTime, newerTime); err != nil {
		t.Fatalf("failed to set new docs part time: %v", err)
	}
	if err := os.Chtimes(newPicsPart, newerTime, newerTime); err != nil {
		t.Fatalf("failed to set new pics part time: %v", err)
	}

	selected, label, err := ResolveNewestBackupRunSelection(targetDir, []util.BackupEntry{oldEntry, newDocs, newPics})
	if err != nil {
		t.Fatalf("ResolveNewestBackupRunSelection returned error: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected 2 entries in newest run, got %d", len(selected))
	}
	for _, entry := range selected {
		if entry.ID != util.BackupID("AAA111") {
			t.Fatalf("expected newest run ID AAA111, got %s", entry.ID)
		}
	}
	if label == "" {
		t.Fatal("expected non-empty newest selection label")
	}
}

func TestBackupRunSummariesSortByNewestPartAcrossRun(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	olderRun := []util.BackupEntry{{FolderName: "OldDocs", Date: "2026-03-18", ID: util.BackupID("OLD111")}}
	newerRun := []util.BackupEntry{
		{FolderName: "Docs", Date: "2026-03-18", ID: util.BackupID("NEW222")},
		{FolderName: "Pics", Date: "2026-03-18", ID: util.BackupID("NEW222")},
	}

	oldPart := util.PartFileName(targetDir, olderRun[0].FolderName, olderRun[0].Date, olderRun[0].ID, 1)
	newDocsPart := util.PartFileName(targetDir, newerRun[0].FolderName, newerRun[0].Date, newerRun[0].ID, 1)
	newPicsPart := util.PartFileName(targetDir, newerRun[1].FolderName, newerRun[1].Date, newerRun[1].ID, 1)

	for _, part := range []string{oldPart, newDocsPart, newPicsPart} {
		if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
			t.Fatalf("failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
			t.Fatalf("failed to create part file: %v", err)
		}
	}

	olderTime := time.Date(2026, 3, 18, 20, 35, 3, 0, time.UTC)
	newDocsTime := time.Date(2026, 3, 18, 21, 11, 0, 0, time.UTC)
	newPicsTime := time.Date(2026, 3, 18, 21, 12, 22, 0, time.UTC)
	if err := os.Chtimes(oldPart, olderTime, olderTime); err != nil {
		t.Fatalf("failed to set old part time: %v", err)
	}
	if err := os.Chtimes(newDocsPart, newDocsTime, newDocsTime); err != nil {
		t.Fatalf("failed to set new docs part time: %v", err)
	}
	if err := os.Chtimes(newPicsPart, newPicsTime, newPicsTime); err != nil {
		t.Fatalf("failed to set new pics part time: %v", err)
	}

	runs, err := BackupRunSummaries(targetDir, []util.BackupEntry{olderRun[0], newerRun[0], newerRun[1]})
	if err != nil {
		t.Fatalf("BackupRunSummaries returned error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID != util.BackupID("NEW222") {
		t.Fatalf("expected newest run first, got %s", runs[0].ID)
	}
	if !runs[0].NewestTime.Equal(newPicsTime) {
		t.Fatalf("expected run newest time %v, got %v", newPicsTime, runs[0].NewestTime)
	}
	if len(runs[0].Entries) != 2 {
		t.Fatalf("expected grouped run to contain 2 entries, got %d", len(runs[0].Entries))
	}
	if runs[0].Entries[0].FolderName != "Docs" || runs[0].Entries[1].FolderName != "Pics" {
		t.Fatalf("expected run entries sorted by folder name, got %#v", runs[0].Entries)
	}
}

func TestResolveSelectionByIDAndFullName(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Pics", Date: "2026-03-14", ID: util.BackupID("ABC123")},
		{FolderName: "Music", Date: "2026-03-13", ID: util.BackupID("ZZZ999")},
	}

	matchedByID, err := ResolveSelection("abc123", index)
	if err != nil {
		t.Fatalf("ResolveSelection by ID returned error: %v", err)
	}
	if len(matchedByID) != 2 {
		t.Fatalf("expected 2 matches by ID, got %d", len(matchedByID))
	}

	matchedByName, err := ResolveSelection("docs_2026-03-14_abc123", index)
	if err != nil {
		t.Fatalf("ResolveSelection by full name returned error: %v", err)
	}
	if len(matchedByName) != 1 || matchedByName[0].FolderName != "Docs" {
		t.Fatalf("expected single Docs match, got %#v", matchedByName)
	}

	if _, err := ResolveSelection("NOPE00", index); err == nil {
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

	selected, err := ResolveSelection("ABC123", index)
	if err != nil {
		t.Fatalf("ResolveSelection returned error: %v", err)
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
