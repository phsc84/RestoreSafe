package util

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitWriterAndSequentialReaderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	nameFunc := func(seq int) string {
		return filepath.Join(dir, fmt.Sprintf("part-%03d.bin", seq))
	}

	w := NewWriter(nameFunc, 5)
	input := []byte("hello world!")

	n, err := w.Write(input)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(input) {
		t.Fatalf("expected %d bytes written, got %d", len(input), n)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	paths := w.Paths()
	if len(paths) != 3 {
		t.Fatalf("expected 3 part files, got %d", len(paths))
	}

	stats := w.Stats()
	if stats.PartsOpened != 3 {
		t.Fatalf("expected 3 opened parts, got %d", stats.PartsOpened)
	}
	if stats.PartsClosed != 3 {
		t.Fatalf("expected 3 closed parts, got %d", stats.PartsClosed)
	}
	if stats.FileWriteBytes != int64(len(input)) {
		t.Fatalf("expected %d file bytes, got %d", len(input), stats.FileWriteBytes)
	}

	r := NewSequentialReader(paths)
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("round-trip mismatch: expected %q, got %q", input, got)
	}
}

func TestSplitWriterRejectsNonPositivePartSize(t *testing.T) {
	w := NewWriter(func(seq int) string {
		return filepath.Join(t.TempDir(), fmt.Sprintf("p-%03d.bin", seq))
	}, 0)

	_, err := w.Write([]byte("x"))
	if err == nil {
		t.Fatal("expected error for non-positive part size, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid split part size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSequentialReaderMissingPartFile(t *testing.T) {
	r := NewSequentialReader([]string{filepath.Join(t.TempDir(), "missing.bin")})
	defer r.Close()

	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err == nil {
		t.Fatal("expected error for missing part file, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to open part file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
