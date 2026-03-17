package operation

import (
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPromptBackupSelectionCancelReturnsTypedError(t *testing.T) {
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

	_, _, err = PromptBackupSelection("verify", t.TempDir(), index)
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
