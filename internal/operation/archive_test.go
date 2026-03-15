package operation

import (
	"archive/tar"
	"bytes"
	"io"
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
