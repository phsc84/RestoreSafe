package catalog

import (
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"strings"
	"testing"
	"time"
)

func completeInfo(dir, chain, run, date string, created time.Time) SetInfo {
	return SetInfo{
		Entry:  naming.BackupEntry{DirectoryName: dir, ChainID: naming.BackupID(chain), Date: date},
		Header: &container.Header{RunID: run, Date: date, CreatedUTC: created.UTC().Format(time.RFC3339)},
	}
}

func TestBackupRunSummariesGroupsByRunNewestFirst(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	infos := []SetInfo{
		completeInfo("Docs", "OLD001", "OLD001", "2026-03-15", t0),
		completeInfo("Pics", "OLD001", "OLD001", "2026-03-15", t0.Add(time.Minute)),
		completeInfo("Docs", "NEW001", "NEW001", "2026-03-16", t0.Add(24*time.Hour)),
		{Entry: naming.BackupEntry{DirectoryName: "Broken", ChainID: "BRK001", Date: "2026-03-17"}, Err: &container.ErrIncomplete{Reason: "x"}},
	}

	runs := BackupRunSummaries(infos)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs (incomplete set excluded), got %d", len(runs))
	}
	if runs[0].RunID != "NEW001" || runs[1].RunID != "OLD001" {
		t.Fatalf("unexpected order: %v, %v", runs[0].RunID, runs[1].RunID)
	}
	if len(runs[1].Entries) != 2 || runs[1].Entries[0].DirectoryName != "Docs" {
		t.Fatalf("run entries not grouped/sorted: %+v", runs[1].Entries)
	}
}

func TestResolveSelectionByRunIDAndName(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	runs := BackupRunSummaries([]SetInfo{
		completeInfo("Docs", "ABC123", "ABC123", "2026-03-15", t0),
		completeInfo("Pics", "ABC123", "ABC123", "2026-03-15", t0),
	})

	byID, err := ResolveSelection("abc123", runs)
	if err != nil || len(byID) != 2 {
		t.Fatalf("by ID: %v, %v", byID, err)
	}
	byName, err := ResolveSelection("docs_ABC123_2026-03-15_full", runs)
	if err != nil || len(byName) != 1 || byName[0].DirectoryName != "Docs" {
		t.Fatalf("by name: %v, %v", byName, err)
	}
	if _, err := ResolveSelection("ZZZ999", runs); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestIsRawBackupID(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]bool{"ABC123": true, "abc123": false, "ABC12": false, "ABC-12": false} {
		if got := IsRawBackupID(input); got != want {
			t.Fatalf("IsRawBackupID(%q) = %v, want %v", input, got, want)
		}
	}
}
