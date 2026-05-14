package security

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCombineWithKeyfileAppendsToPassword(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfilePath := filepath.Join(dir, "test.key")
	keyfileData := []byte("keyfile-content")
	if err := os.WriteFile(keyfilePath, keyfileData, 0o600); err != nil {
		t.Fatalf("failed to write keyfile: %v", err)
	}

	password := []byte("my-password")
	combined, err := CombineWithKeyfile(password, keyfilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := append([]byte("my-password"), keyfileData...)
	if !bytes.Equal(combined, expected) {
		t.Fatalf("combined mismatch: got %q, want %q", combined, expected)
	}
}

func TestCombineWithKeyfileDoesNotMutatePassword(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfilePath := filepath.Join(dir, "test.key")
	if err := os.WriteFile(keyfilePath, []byte("extra"), 0o600); err != nil {
		t.Fatalf("failed to write keyfile: %v", err)
	}

	original := []byte("pw")
	snapshot := make([]byte, len(original))
	copy(snapshot, original)

	_, err := CombineWithKeyfile(original, keyfilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(original, snapshot) {
		t.Fatalf("password slice was mutated: got %q, want %q", original, snapshot)
	}
}

func TestCombineWithKeyfileEmptyPasswordWithKeyfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfilePath := filepath.Join(dir, "test.key")
	keyfileData := []byte("only-keyfile")
	if err := os.WriteFile(keyfilePath, keyfileData, 0o600); err != nil {
		t.Fatalf("failed to write keyfile: %v", err)
	}

	combined, err := CombineWithKeyfile([]byte{}, keyfilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(combined, keyfileData) {
		t.Fatalf("expected keyfile bytes only, got %q", combined)
	}
}

func TestCombineWithKeyfileFileNotFound(t *testing.T) {
	t.Parallel()
	_, err := CombineWithKeyfile([]byte("pw"), "/nonexistent/path/to.key")
	if err == nil {
		t.Fatal("expected error for missing keyfile, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read keyfile") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCombineWithKeyfileEmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfilePath := filepath.Join(dir, "empty.key")
	if err := os.WriteFile(keyfilePath, []byte{}, 0o600); err != nil {
		t.Fatalf("failed to write empty keyfile: %v", err)
	}

	_, err := CombineWithKeyfile([]byte("pw"), keyfilePath)
	if err == nil {
		t.Fatal("expected error for empty keyfile, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCombineWithKeyfileBinaryContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfilePath := filepath.Join(dir, "binary.key")
	keyfileData := make([]byte, 32)
	for i := range keyfileData {
		keyfileData[i] = byte(i)
	}
	if err := os.WriteFile(keyfilePath, keyfileData, 0o600); err != nil {
		t.Fatalf("failed to write keyfile: %v", err)
	}

	password := []byte("pw")
	combined, err := CombineWithKeyfile(password, keyfilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(combined) != len(password)+len(keyfileData) {
		t.Fatalf("unexpected combined length: got %d, want %d", len(combined), len(password)+len(keyfileData))
	}
	if !bytes.Equal(combined[len(password):], keyfileData) {
		t.Fatal("keyfile bytes not correctly appended")
	}
}

func TestCombineWithKeyfileProducesDifferentResultsForDifferentKeyfiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path1 := filepath.Join(dir, "a.key")
	path2 := filepath.Join(dir, "b.key")
	if err := os.WriteFile(path1, []byte("keyfile-a"), 0o600); err != nil {
		t.Fatalf("failed to write keyfile a: %v", err)
	}
	if err := os.WriteFile(path2, []byte("keyfile-b"), 0o600); err != nil {
		t.Fatalf("failed to write keyfile b: %v", err)
	}

	password := []byte("same-password")
	combined1, err := CombineWithKeyfile(password, path1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined2, err := CombineWithKeyfile(password, path2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes.Equal(combined1, combined2) {
		t.Fatal("expected different combined values for different keyfiles")
	}
}
