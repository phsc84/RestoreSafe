package restore

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReturnsNilWhenNoBackupsFound(t *testing.T) {
	t.Parallel()
	emptyDir := t.TempDir()
	cfg := &util.Config{TargetFolder: emptyDir}

	output := testutil.CaptureStdout(t, func() {
		if err := Run(cfg, ""); err != nil {
			t.Errorf("expected nil for empty target dir, got: %v", err)
		}
	})

	if !strings.Contains(output, "No backups found") {
		t.Fatalf("expected no-backups message in output, got: %q", output)
	}
}

func TestRunReturnsErrorWhenTargetDirNotFound(t *testing.T) {
	t.Parallel()
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	cfg := &util.Config{TargetFolder: nonExistent}

	err := Run(cfg, "")
	if err == nil {
		t.Fatal("expected error for non-existent target dir, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to scan target folder") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func TestRunCancelsSelectionWhenUserEntersQ(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("cancel-pw"))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := fmt.Fprintln(w, "q"); err != nil {
		t.Fatalf("failed to write q to pipe: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})

	cfg := &util.Config{TargetFolder: fx.TargetDir}
	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = Run(cfg, "")
	})

	if runErr != nil {
		t.Fatalf("expected nil for cancelled selection, got: %v", runErr)
	}
	if !strings.Contains(output, "Restore cancelled.") {
		t.Fatalf("expected cancel message in output, got: %q", output)
	}
}

func TestRunReturnsErrorWhenDestinationPromptClosed(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("prompt-pw"))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	// "." selects newest backup; closing the write end causes the next readline
	// (promptRestoreDestination) to get EOF and return an error.
	if _, err := fmt.Fprintln(w, "."); err != nil {
		t.Fatalf("failed to write selection to pipe: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})

	cfg := &util.Config{TargetFolder: fx.TargetDir}
	var runErr error
	testutil.CaptureStdout(t, func() {
		runErr = Run(cfg, "")
	})

	if runErr == nil {
		t.Fatal("expected error when stdin closes before destination prompt, got nil")
	}
}
