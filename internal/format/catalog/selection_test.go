package catalog

import (
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
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
