package operation

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTarAcceptsRegularArchive(t *testing.T) {
	t.Parallel()

	archiveBytes := makeTarBytes(t, []tarEntry{
		{name: "folder", typeflag: tar.TypeDir, mode: 0o750},
		{name: "folder/file.txt", typeflag: tar.TypeReg, mode: 0o640, body: "hello"},
	})

	if err := ValidateTar(bytes.NewReader(archiveBytes)); err != nil {
		t.Fatalf("ValidateTar returned error: %v", err)
	}
}

func TestValidateTarRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	archiveBytes := makeTarBytes(t, []tarEntry{
		{name: "../evil.txt", typeflag: tar.TypeReg, mode: 0o640, body: "bad"},
	})

	err := ValidateTar(bytes.NewReader(archiveBytes))
	if err == nil {
		t.Fatal("expected ValidateTar error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("expected path traversal error, got: %v", err)
	}
}

func TestValidateTarRejectsAbsolutePath(t *testing.T) {
	t.Parallel()

	archiveBytes := makeTarBytes(t, []tarEntry{
		{name: "C:/Windows/evil.txt", typeflag: tar.TypeReg, mode: 0o640, body: "bad"},
	})

	err := ValidateTar(bytes.NewReader(archiveBytes))
	if err == nil {
		t.Fatal("expected ValidateTar error for absolute path, got nil")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute path error, got: %v", err)
	}
}

func TestValidateTarRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	archiveBytes := makeTarBytes(t, []tarEntry{
		{name: "   ", typeflag: tar.TypeReg, mode: 0o640, body: "bad"},
	})

	err := ValidateTar(bytes.NewReader(archiveBytes))
	if err == nil {
		t.Fatal("expected ValidateTar error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "empty TAR entry name") {
		t.Fatalf("expected empty path error, got: %v", err)
	}
}

func TestWriteTarAndExtractTarRoundTripWithExclude(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	destDir := filepath.Join(dir, "dest")
	excludedDir := filepath.Join(srcDir, "target")

	if err := os.MkdirAll(filepath.Join(srcDir, "docs"), 0o750); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	if err := os.MkdirAll(excludedDir, 0o750); err != nil {
		t.Fatalf("failed to create excluded dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(srcDir, "docs", "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("failed to write included file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(excludedDir, "skip.txt"), []byte("skip"), 0o600); err != nil {
		t.Fatalf("failed to write excluded file: %v", err)
	}

	var archive bytes.Buffer
	if err := WriteTar(&archive, srcDir, excludedDir); err != nil {
		t.Fatalf("WriteTar returned error: %v", err)
	}

	if err := ValidateTar(bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("ValidateTar returned error for produced archive: %v", err)
	}

	if err := ExtractTar(bytes.NewReader(archive.Bytes()), destDir); err != nil {
		t.Fatalf("ExtractTar returned error: %v", err)
	}

	includedPath := filepath.Join(destDir, "docs", "keep.txt")
	got, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatalf("failed to read extracted included file: %v", err)
	}
	if string(got) != "keep" {
		t.Fatalf("unexpected included file content: %q", got)
	}

	excludedPath := filepath.Join(destDir, "target", "skip.txt")
	if _, err := os.Stat(excludedPath); !os.IsNotExist(err) {
		t.Fatalf("excluded file should not be extracted, stat err=%v", err)
	}
}

func TestExtractTarRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	archiveBytes := makeTarBytes(t, []tarEntry{
		{name: "../escape.txt", typeflag: tar.TypeReg, mode: 0o640, body: "bad"},
	})

	err := ExtractTar(bytes.NewReader(archiveBytes), t.TempDir())
	if err == nil {
		t.Fatal("expected ExtractTar error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     string
}

func makeTarBytes(t *testing.T, entries []tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, entry := range entries {
		hdr := &tar.Header{
			Name:     entry.name,
			Typeflag: entry.typeflag,
			Mode:     entry.mode,
		}
		if entry.typeflag == tar.TypeReg {
			hdr.Size = int64(len(entry.body))
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if entry.typeflag == tar.TypeReg {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatalf("failed to write tar body: %v", err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}

	return buf.Bytes()
}
