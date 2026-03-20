package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectorySizeBytesSumsFilesRecursively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("abcd"), 0o600); err != nil {
		t.Fatalf("failed to create a.txt: %v", err)
	}

	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "b.txt"), []byte("123456"), 0o600); err != nil {
		t.Fatalf("failed to create b.txt: %v", err)
	}

	size, err := DirectorySizeBytes(root)
	if err != nil {
		t.Fatalf("DirectorySizeBytes returned error: %v", err)
	}
	if size != 10 {
		t.Fatalf("expected total size 10 bytes, got %d", size)
	}
}

func TestDirectorySizeBytesNonDirectoryReturnsRemedy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	_, err := DirectorySizeBytes(filePath)
	if err == nil {
		t.Fatal("expected error for non-directory input, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Path is not a directory") {
		t.Fatalf("expected non-directory error message, got: %q", msg)
	}
	if !strings.Contains(msg, "Remedy:") {
		t.Fatalf("expected remedy text, got: %q", msg)
	}
}

func TestDirectorySizeBytesMissingPathReturnsError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")

	_, err := DirectorySizeBytes(missing)
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}
