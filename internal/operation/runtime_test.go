package operation

import (
	"io"
	"os"
	"testing"

	"RestoreSafe/internal/util"
)

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &util.Config{LogLevel: "info"}

	log := OpenLogger(cfg, tmpDir, "2026-03-14", util.BackupID("ABC123"))
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if log.IsConsoleOnly() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	log.Close()
}

func TestPasswordFailurePrefix(t *testing.T) {
	t.Parallel()
	if got := PasswordFailurePrefix(true, false); got != "Wrong password or invalid YubiKey response." {
		t.Fatalf("unexpected prefix for YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(false, false); got != "Wrong password." {
		t.Fatalf("unexpected prefix without YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(true, true); got != "Wrong YubiKey or corrupted file." {
		t.Fatalf("unexpected prefix for YubiKey-only: %q", got)
	}
}

func TestPromptStartActionPrintsSingleBlankLineBeforeAndAfterPrompt(t *testing.T) {
	prevReadLine := readLineFn
	prevStdout := os.Stdout
	t.Cleanup(func() {
		readLineFn = prevReadLine
		os.Stdout = prevStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	readLineFn = func(prompt string) (string, error) {
		if prompt != "Start verification now? [Y/n]: " {
			t.Fatalf("unexpected prompt: %q", prompt)
		}
		return "y", nil
	}

	confirmed, err := PromptStartAction("verification")
	if err != nil {
		t.Fatalf("PromptStartAction returned error: %v", err)
	}
	if !confirmed {
		t.Fatal("expected confirmation true")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close write end: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close read end: %v", err)
	}

	if got := string(out); got != "\n\n" {
		t.Fatalf("expected exactly one empty line before and after prompt, got %q", got)
	}
}

func TestPromptStartActionPrintsSingleBlankLineOnRetry(t *testing.T) {
	prevReadLine := readLineFn
	prevStdout := os.Stdout
	t.Cleanup(func() {
		readLineFn = prevReadLine
		os.Stdout = prevStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	call := 0
	readLineFn = func(prompt string) (string, error) {
		if prompt != "Start verification now? [Y/n]: " {
			t.Fatalf("unexpected prompt: %q", prompt)
		}
		if call == 0 {
			call++
			return "maybe", nil
		}
		return "n", nil
	}

	confirmed, err := PromptStartAction("verification")
	if err != nil {
		t.Fatalf("PromptStartAction returned error: %v", err)
	}
	if confirmed {
		t.Fatal("expected confirmation false")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close write end: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close read end: %v", err)
	}

	got := string(out)
	expected := "\n\nPlease enter y (yes) or n (no).\n\n\n"
	if got != expected {
		t.Fatalf("unexpected stdout.\nexpected: %q\n     got: %q", expected, got)
	}
}
