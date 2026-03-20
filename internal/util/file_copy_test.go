package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFileCopiesContent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "src.txt")
	dstPath := filepath.Join(tempDir, "dst.txt")
	content := []byte("hello restoresafe")
	if err := os.WriteFile(srcPath, content, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile returned error: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected %q, got %q", string(content), string(got))
	}
}

func TestCopyFileOverwritesDestination(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "src.txt")
	dstPath := filepath.Join(tempDir, "dst.txt")
	if err := os.WriteFile(srcPath, []byte("new content"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("old content"), 0o600); err != nil {
		t.Fatalf("failed to write destination file: %v", err)
	}

	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile returned error: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("expected destination overwrite, got %q", string(got))
	}
}

func TestCopyFileMissingSourceReturnsRemedy(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "missing.txt")
	dstPath := filepath.Join(tempDir, "dst.txt")

	err := CopyFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Failed to open source file") {
		t.Fatalf("expected source-open error message, got: %q", msg)
	}
	if !strings.Contains(msg, "Remedy:") {
		t.Fatalf("expected remedy text, got: %q", msg)
	}
}

func TestCopyFileInvalidDestinationReturnsRemedy(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "src.txt")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	// Destination parent does not exist, so opening destination must fail.
	dstPath := filepath.Join(tempDir, "missing-parent", "dst.txt")
	err := CopyFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected destination creation error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Failed to create destination file") {
		t.Fatalf("expected destination-create error message, got: %q", msg)
	}
	if !strings.Contains(msg, "Remedy:") {
		t.Fatalf("expected remedy text, got: %q", msg)
	}
}
