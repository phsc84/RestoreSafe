package catalog

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildBackupRunSummariesGroupsEntriesByDateAndID(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "Pics", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "Music", Date: "2026-03-14", ID: util.BackupID("BBB222")},
	}

	runs := BuildBackupRunSummaries(index)
	if len(runs) != 2 {
		t.Fatalf("expected 2 run summaries, got %d", len(runs))
	}
	if runs[0].ID != util.BackupID("BBB222") {
		t.Fatalf("expected first run to sort by date/id desc, got %#v", runs[0])
	}
	if runs[1].ID != util.BackupID("AAA111") || len(runs[1].Entries) != 2 {
		t.Fatalf("unexpected grouped run: %#v", runs[1])
	}
	if runs[1].Entries[0].FolderName != "Docs" || runs[1].Entries[1].FolderName != "Pics" {
		t.Fatalf("expected grouped run entries to be folder-sorted, got %#v", runs[1].Entries)
	}
}

func TestFormatRunFolderList(t *testing.T) {
	t.Parallel()

	entries := []util.BackupEntry{{FolderName: "A"}, {FolderName: "B"}, {FolderName: "C"}, {FolderName: "D"}}
	if got := FormatRunFolderList(entries); got != "A, B, C, +1 more" {
		t.Fatalf("unexpected folder list summary: %q", got)
	}
}

func TestSortedBackupDatesDeduplicatesAndCounts(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "Pics", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "Old", Date: "2026-03-14", ID: util.BackupID("BBB222")},
		{FolderName: "Music", Date: "2026-03-13", ID: util.BackupID("CCC333")},
	}

	items := SortedBackupDates(index)
	if len(items) != 2 {
		t.Fatalf("expected 2 date summaries, got %d", len(items))
	}
	if items[0].Date != "2026-03-14" || items[0].EntryCount != 3 || items[0].RunCount != 2 {
		t.Fatalf("unexpected first date summary: %#v", items[0])
	}
	if items[1].Date != "2026-03-13" || items[1].EntryCount != 1 || items[1].RunCount != 1 {
		t.Fatalf("unexpected second date summary: %#v", items[1])
	}
}

func TestFilterRunSummariesByDate(t *testing.T) {
	t.Parallel()

	runs := []BackupRunSummary{
		{Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{Date: "2026-03-14", ID: util.BackupID("BBB222")},
		{Date: "2026-03-13", ID: util.BackupID("CCC333")},
	}

	filtered := FilterRunSummariesByDate(runs, "2026-03-14")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered runs, got %d", len(filtered))
	}

	all := FilterRunSummariesByDate(runs, "")
	if len(all) != len(runs) {
		t.Fatalf("expected %d runs without filter, got %d", len(runs), len(all))
	}
	if &all[0] == &runs[0] {
		t.Fatal("expected FilterRunSummariesByDate to return a copy for empty filter")
	}
}

func TestIsDateFilterInput(t *testing.T) {
	t.Parallel()

	if !IsDateFilterInput("2026-03-15") {
		t.Fatal("expected valid ISO date to be accepted")
	}
	if IsDateFilterInput("15.03.2026") {
		t.Fatal("expected non-ISO date to be rejected")
	}
}

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

func TestSortedBackupIDDatesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	index := []util.BackupEntry{
		{FolderName: "A", Date: "2026-03-13", ID: util.BackupID("ZZZ999")},
		{FolderName: "B", Date: "2026-03-14", ID: util.BackupID("BBB222")},
		{FolderName: "C", Date: "2026-03-14", ID: util.BackupID("AAA111")},
		{FolderName: "D", Date: "2026-03-14", ID: util.BackupID("AAA111")},
	}

	items := SortedBackupIDDates(index)
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

func TestCompletedActionLabel(t *testing.T) {
	t.Parallel()

	if got := CompletedActionLabel("restore"); got != "restored" {
		t.Fatalf("expected restored, got %q", got)
	}
	if got := CompletedActionLabel("verify"); got != "verified" {
		t.Fatalf("expected verified, got %q", got)
	}
	if got := CompletedActionLabel("clean"); got != "cleaned" {
		t.Fatalf("expected cleaned fallback, got %q", got)
	}
}
