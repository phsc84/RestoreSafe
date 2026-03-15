package util

import (
	"RestoreSafe/internal/security"
	"archive/tar"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const splitWriteBufferSize = 32 * 1024 * 1024

// CreateEncryptedSplitBackupForTest creates split encrypted backup parts for integration-style tests.
func CreateEncryptedSplitBackupForTest(t testing.TB, srcDir, targetDir, folderName, backupDate string, backupID BackupID, password []byte, splitSizeMB int64) int {
	t.Helper()

	nameFunc := func(seq int) string {
		return PartFileName(targetDir, folderName, backupDate, backupID, seq)
	}
	splitSizeBytes := splitSizeMB * 1024 * 1024
	sw := NewWriter(nameFunc, splitSizeBytes)
	bw := bufio.NewWriterSize(sw, splitWriteBufferSize)

	pr, pw := io.Pipe()
	tarErrCh := make(chan error, 1)
	go func() {
		err := writeTarForTest(pw, srcDir, targetDir)
		pw.CloseWithError(err) //nolint:errcheck
		tarErrCh <- err
	}()

	encryptErr := security.Encrypt(bw, pr, password)
	pr.Close() //nolint:errcheck
	if encryptErr != nil {
		t.Fatalf("security.Encrypt returned error: %v", encryptErr)
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("failed to flush split buffer: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("failed to close split writer: %v", err)
	}
	if tarErr := <-tarErrCh; tarErr != nil {
		t.Fatalf("writeTarForTest returned error: %v", tarErr)
	}

	return len(sw.Paths())
}

func writeTarForTest(w io.Writer, srcDir string, excludeDirs ...string) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	srcDir = filepath.Clean(srcDir)

	exs := make([]string, 0, len(excludeDirs))
	for _, e := range excludeDirs {
		if e == "" {
			continue
		}
		exs = append(exs, filepath.Clean(e))
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk source path %q: %w", path, err)
		}

		for _, ex := range exs {
			rel, relErr := filepath.Rel(ex, path)
			if relErr == nil && rel != "" && !strings.HasPrefix(rel, "..") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header for %q: %w", path, err)
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header for %q: %w", path, err)
		}

		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open source file %q: %w", path, err)
		}

		if _, err := io.Copy(tw, f); err != nil {
			f.Close() //nolint:errcheck
			return fmt.Errorf("failed to write tar content %q: %w", path, err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to close source file %q: %w", path, err)
		}

		return nil
	})
}
