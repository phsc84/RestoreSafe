package fsx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	if err := CopyFile(context.Background(), srcPath, dstPath, nil); err != nil {
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

	if err := CopyFile(context.Background(), srcPath, dstPath, nil); err != nil {
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

func TestCopyFileMissingSourceReturnsError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "missing.txt")
	dstPath := filepath.Join(tempDir, "dst.txt")

	err := CopyFile(context.Background(), srcPath, dstPath, nil)
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to open source file") {
		t.Fatalf("expected source-open error message, got: %q", err.Error())
	}
}

func TestCopyFileInvalidDestinationReturnsError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "src.txt")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	// Destination parent does not exist, so opening destination must fail.
	dstPath := filepath.Join(tempDir, "missing-parent", "dst.txt")
	err := CopyFile(context.Background(), srcPath, dstPath, nil)
	if err == nil {
		t.Fatal("expected destination creation error, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to create destination file") {
		t.Fatalf("expected destination-create error message, got: %q", err.Error())
	}
}

func TestCopyFileCountsBytesAndStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	var done atomic.Int64
	if err := CopyFile(context.Background(), srcPath, filepath.Join(dir, "a.bin"), &done); err != nil || done.Load() != 1000 {
		t.Fatalf("copy: err=%v done=%d", err, done.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CopyFile(ctx, srcPath, filepath.Join(dir, "b.bin"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
