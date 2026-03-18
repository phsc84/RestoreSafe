package operation

import (
	"RestoreSafe/internal/util"
	"errors"
	"io"
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
		FolderName: "Docs",
		Date:       "2026-03-14",
		ID:         util.BackupID("ABC123"),
	}}
	part := util.PartFileName(targetDir, index[0].FolderName, index[0].Date, index[0].ID, 1)
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
		{FolderName: "SourceFolder1", Date: "2026-03-18", ID: util.BackupID("ABC125")},
		{FolderName: "SourceFolder2", Date: "2026-03-18", ID: util.BackupID("ABC125")},
		{FolderName: "SourceFolder", Date: "2026-03-18", ID: util.BackupID("ABC123")},
	}

	partPaths := []string{
		util.PartFileName(targetDir, index[0].FolderName, index[0].Date, index[0].ID, 1),
		util.PartFileName(targetDir, index[1].FolderName, index[1].Date, index[1].ID, 1),
		util.PartFileName(targetDir, index[2].FolderName, index[2].Date, index[2].ID, 1),
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

	output := captureStdout(t, func() {
		if err := printBackupSelectionPrompt("restore", targetDir, index); err != nil {
			t.Fatalf("printBackupSelectionPrompt returned error: %v", err)
		}
	})

	firstGroup := "  - Backup ID: ABC123 / Timestamp: 2026-03-18 21:12:22"
	secondGroup := "  - Backup ID: ABC125 / Timestamp: 2026-03-18 20:35:03"
	if !strings.Contains(output, firstGroup) {
		t.Fatalf("expected first group header in output, got: %q", output)
	}
	if !strings.Contains(output, secondGroup) {
		t.Fatalf("expected second group header in output, got: %q", output)
	}
	if strings.Index(output, firstGroup) > strings.Index(output, secondGroup) {
		t.Fatalf("expected newest run first, got output: %q", output)
	}
	if !strings.Contains(output, "    - SourceFolder_2026-03-18_ABC123") {
		t.Fatalf("expected nested entry for ABC123, got: %q", output)
	}
	if !strings.Contains(output, "    - SourceFolder1_2026-03-18_ABC125") || !strings.Contains(output, "    - SourceFolder2_2026-03-18_ABC125") {
		t.Fatalf("expected grouped nested entries for ABC125, got: %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdout writer: %v", err)
	}
	os.Stdout = originalStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close stdout reader: %v", err)
	}
	return string(out)
}
