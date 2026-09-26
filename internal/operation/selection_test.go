package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func testRuns() []catalog.BackupRunSummary {
	return []catalog.BackupRunSummary{
		{
			RunID:   "ABC125",
			Date:    "2026-03-18",
			Created: time.Date(2026, 3, 18, 21, 12, 22, 0, time.UTC),
			Entries: []util.BackupEntry{
				{DirectoryName: "SourceDirectory1", ChainID: "ABC125", Date: "2026-03-18"},
				{DirectoryName: "SourceDirectory2", ChainID: "ABC125", Date: "2026-03-18"},
			},
		},
		{
			RunID:   "ABC123",
			Date:    "2026-03-18",
			Created: time.Date(2026, 3, 18, 20, 35, 3, 0, time.UTC),
			Entries: []util.BackupEntry{{DirectoryName: "SourceDirectory", ChainID: "ABC123", Date: "2026-03-18"}},
		},
	}
}

// pipeSelectionInput replaces os.Stdin with a pipe containing the given lines,
// restoring the original stdin via t.Cleanup. Not safe for parallel use.
func pipeSelectionInput(t *testing.T, lines string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := w.WriteString(lines); err != nil {
		t.Fatalf("failed to write to stdin pipe: %v", err)
	}
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
	})
}

func selectWith(t *testing.T, input string) ([]util.BackupEntry, error, string) {
	t.Helper()
	pipeSelectionInput(t, input)
	var selected []util.BackupEntry
	var err error
	out := testutil.CaptureStdout(t, func() {
		selected, err = PromptBackupSelection("verify", testRuns())
	})
	return selected, err, out
}

func TestPromptBackupSelectionCancelReturnsTypedError(t *testing.T) {
	_, err, _ := selectWith(t, "q\n")
	if !errors.Is(err, ErrSelectionCancelled) {
		t.Fatalf("expected ErrSelectionCancelled, got %v", err)
	}
}

func TestPrintBackupSelectionPromptListsRunsInOrder(t *testing.T) {
	output := testutil.CaptureStdout(t, func() { printBackupSelectionPrompt("restore", testRuns()) })

	first := "  - Backup ID: ABC125 / Timestamp (local): " + formatBackupRunTimestamp(testRuns()[0].Created)
	second := "  - Backup ID: ABC123 / Timestamp (local): " + formatBackupRunTimestamp(testRuns()[1].Created)
	if !strings.Contains(output, first) || !strings.Contains(output, second) || strings.Index(output, first) > strings.Index(output, second) {
		t.Fatalf("unexpected run listing: %q", output)
	}
	if !strings.Contains(output, "    - SourceDirectory1_ABC125_2026-03-18_FULL") {
		t.Fatalf("expected nested entry, got %q", output)
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

func TestPromptBackupSelectionNewestSelectsLatestRun(t *testing.T) {
	selected, err, _ := selectWith(t, ".\n")
	if err != nil || len(selected) != 2 || selected[0].ChainID != "ABC125" {
		t.Fatalf("unexpected newest selection: %v, %v", selected, err)
	}
}

func TestPromptBackupSelectionByNameSelectsMatchingEntry(t *testing.T) {
	selected, err, _ := selectWith(t, "SourceDirectory_ABC123_2026-03-18_FULL\n")
	if err != nil || len(selected) != 1 || selected[0].DirectoryName != "SourceDirectory" {
		t.Fatalf("unexpected name selection: %v, %v", selected, err)
	}
}

func TestPromptBackupSelectionByIDSelectsRunEntries(t *testing.T) {
	selected, err, _ := selectWith(t, "abc125\n")
	if err != nil || len(selected) != 2 {
		t.Fatalf("unexpected ID selection: %v, %v", selected, err)
	}
}

func TestPromptBackupSelectionEmptyInputPrintsRetryMessage(t *testing.T) {
	_, _, out := selectWith(t, "\n")
	if !strings.Contains(out, "Selection must not be empty.") {
		t.Fatalf("expected empty-selection message, got %q", out)
	}
}

func TestPromptBackupSelectionUnknownNamePrintsError(t *testing.T) {
	_, _, out := selectWith(t, "unknown-backup-name\n")
	if !strings.Contains(out, "not found") {
		t.Fatalf("expected not-found message, got %q", out)
	}
}
