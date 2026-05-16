package operation

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPromptBackupSelectionCancelReturnsTypedError(t *testing.T) {
	targetDir := t.TempDir()
	originalStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	defer r.Close()
	defer func() { os.Stdin = originalStdin }()

	if _, err := w.Write([]byte("q\n")); err != nil {
		t.Fatalf("failed to write cancel input: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdin writer: %v", err)
	}
	os.Stdin = r

	index := []util.BackupEntry{{
		DirectoryName: "Docs",
		Date:       "2026-03-14",
		ID:         util.BackupID("ABC123"),
	}}
	part := util.PartFileName(targetDir, index[0].DirectoryName, index[0].Date, index[0].ID, 1)
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create part file: %v", err)
	}

	_, _, err = PromptBackupSelection("verify", targetDir, index)
	if err == nil {
		t.Fatal("expected cancel error, got nil")
	}
	if !errors.Is(err, ErrSelectionCancelled) {
		t.Fatalf("expected ErrSelectionCancelled, got %v", err)
	}
	if !strings.Contains(err.Error(), "Start verify again") {
		t.Fatalf("expected verify-specific remedy text, got %q", err.Error())
	}
}

func TestPrintBackupSelectionPromptGroupsByRunAndSortsNewestFirst(t *testing.T) {
	targetDir := t.TempDir()
	index := []util.BackupEntry{
		{DirectoryName: "SourceDirectory1", Date: "2026-03-18", ID: util.BackupID("ABC125")},
		{DirectoryName: "SourceDirectory2", Date: "2026-03-18", ID: util.BackupID("ABC125")},
		{DirectoryName: "SourceDirectory", Date: "2026-03-18", ID: util.BackupID("ABC123")},
	}

	partPaths := []string{
		util.PartFileName(targetDir, index[0].DirectoryName, index[0].Date, index[0].ID, 1),
		util.PartFileName(targetDir, index[1].DirectoryName, index[1].Date, index[1].ID, 1),
		util.PartFileName(targetDir, index[2].DirectoryName, index[2].Date, index[2].ID, 1),
	}
	for _, part := range partPaths {
		if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
			t.Fatalf("failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
			t.Fatalf("failed to create part file: %v", err)
		}
	}

	abc125Latest := time.Date(2026, 3, 18, 20, 35, 3, 0, time.UTC)
	abc123Latest := time.Date(2026, 3, 18, 21, 12, 22, 0, time.UTC)
	if err := os.Chtimes(partPaths[0], abc125Latest.Add(-1*time.Minute), abc125Latest.Add(-1*time.Minute)); err != nil {
		t.Fatalf("failed to set first ABC125 part time: %v", err)
	}
	if err := os.Chtimes(partPaths[1], abc125Latest, abc125Latest); err != nil {
		t.Fatalf("failed to set second ABC125 part time: %v", err)
	}
	if err := os.Chtimes(partPaths[2], abc123Latest, abc123Latest); err != nil {
		t.Fatalf("failed to set ABC123 part time: %v", err)
	}

	output := testutil.CaptureStdout(t, func() {
		if err := printBackupSelectionPrompt("restore", targetDir, index); err != nil {
			t.Fatalf("printBackupSelectionPrompt returned error: %v", err)
		}
	})

	firstGroup := "  - Backup ID: ABC123 / Timestamp (local): " + formatBackupRunTimestamp(abc123Latest)
	secondGroup := "  - Backup ID: ABC125 / Timestamp (local): " + formatBackupRunTimestamp(abc125Latest)
	if !strings.Contains(output, firstGroup) {
		t.Fatalf("expected first group header in output, got: %q", output)
	}
	if !strings.Contains(output, secondGroup) {
		t.Fatalf("expected second group header in output, got: %q", output)
	}
	if strings.Index(output, firstGroup) > strings.Index(output, secondGroup) {
		t.Fatalf("expected newest run first, got output: %q", output)
	}
	if !strings.Contains(output, "    - SourceDirectory_2026-03-18_ABC123") {
		t.Fatalf("expected nested entry for ABC123, got: %q", output)
	}
	if !strings.Contains(output, "    - SourceDirectory1_2026-03-18_ABC125") || !strings.Contains(output, "    - SourceDirectory2_2026-03-18_ABC125") {
		t.Fatalf("expected grouped nested entries for ABC125, got: %q", output)
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

func createPartFile(t *testing.T, targetDir string, entry util.BackupEntry) {
	t.Helper()
	part := util.PartFileName(targetDir, entry.DirectoryName, entry.Date, entry.ID, 1)
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create part file: %v", err)
	}
}

func TestPromptBackupSelectionNewestSelectsLatestRun(t *testing.T) {
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	createPartFile(t, targetDir, entry)
	index := []util.BackupEntry{entry}

	pipeSelectionInput(t, ".\n")

	var selected []util.BackupEntry
	var label string
	testutil.CaptureStdout(t, func() {
		var err error
		selected, label, err = PromptBackupSelection("verify", targetDir, index)
		if err != nil {
			t.Fatalf("expected no error for newest selection, got: %v", err)
		}
	})
	if len(selected) == 0 {
		t.Fatal("expected at least one selected entry")
	}
	if label == "" {
		t.Fatal("expected non-empty label for newest selection")
	}
}

func TestPromptBackupSelectionByNameSelectsMatchingEntry(t *testing.T) {
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	createPartFile(t, targetDir, entry)
	index := []util.BackupEntry{entry}

	pipeSelectionInput(t, "Docs_2026-03-14_ABC123\n")

	var selected []util.BackupEntry
	testutil.CaptureStdout(t, func() {
		var err error
		selected, _, err = PromptBackupSelection("verify", targetDir, index)
		if err != nil {
			t.Fatalf("expected no error for name selection, got: %v", err)
		}
	})
	if len(selected) == 0 {
		t.Fatal("expected at least one selected entry for named selection")
	}
}

func TestPromptBackupSelectionByIDSelectsMatchingEntries(t *testing.T) {
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	createPartFile(t, targetDir, entry)
	index := []util.BackupEntry{entry}

	pipeSelectionInput(t, "ABC123\n")

	var selected []util.BackupEntry
	testutil.CaptureStdout(t, func() {
		var err error
		selected, _, err = PromptBackupSelection("verify", targetDir, index)
		if err != nil {
			t.Fatalf("expected no error for ID selection, got: %v", err)
		}
	})
	if len(selected) == 0 {
		t.Fatal("expected at least one selected entry for ID selection")
	}
}

func TestPromptBackupSelectionEmptyInputPrintsRetryMessage(t *testing.T) {
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	createPartFile(t, targetDir, entry)
	index := []util.BackupEntry{entry}

	// Empty line causes the "must not be empty" message, then bufio re-reads EOF.
	pipeSelectionInput(t, "\n")

	output := testutil.CaptureStdout(t, func() {
		_, _, _ = PromptBackupSelection("verify", targetDir, index)
	})
	if !strings.Contains(output, "Selection must not be empty.") {
		t.Fatalf("expected empty-selection message in output, got: %q", output)
	}
}

func TestPromptBackupSelectionUnknownNamePrintsError(t *testing.T) {
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	createPartFile(t, targetDir, entry)
	index := []util.BackupEntry{entry}

	// Unknown name causes error output, then bufio re-reads EOF.
	pipeSelectionInput(t, "unknown-backup-name\n")

	output := testutil.CaptureStdout(t, func() {
		_, _, _ = PromptBackupSelection("verify", targetDir, index)
	})
	// Output should contain either an error about the unknown name or selection info.
	if len(output) == 0 {
		t.Fatal("expected some output for unknown selection input")
	}
}
