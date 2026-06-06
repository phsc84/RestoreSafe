package main

import (
	"errors"
	"io"
	"os"
	"testing"
)

func TestReportOperationErrorStartsWithSingleBlankLine(t *testing.T) {
	output := captureStderr(t, func() {
		reportOperationError("Restore", errors.New("Too many wrong password attempts."))
	})

	expected := "\nRestore failed: Too many wrong password attempts.\n\n"
	if output != expected {
		t.Fatalf("unexpected output.\nexpected: %q\n     got: %q", expected, output)
	}
}

func TestReportPreflightErrorStartsWithSingleBlankLine(t *testing.T) {
	output := captureStderr(t, func() {
		reportOperationError("Backup", errors.New("Backup preflight failed: source is invalid"))
	})

	expected := "\nBackup failed.\n\n"
	if output != expected {
		t.Fatalf("unexpected output.\nexpected: %q\n     got: %q", expected, output)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	prevStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = prevStderr
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stderr writer: %v", err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close stderr reader: %v", err)
	}

	return string(output)
}
