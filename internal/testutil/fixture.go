// Package testutil provides shared test fixtures and helpers for integration-style tests.
package testutil

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	splitWriteBufferSize = 32 * 1024 * 1024
	defaultSplitSizeMB   = 1
)

// BackupFixture holds workspace paths and metadata for an integration-style backup test.
// It creates a workspace with standard source files and a completed encrypted split backup.
// The backup always contains nested/small.txt (17 bytes) and large.bin (2 MB+) so that at
// least two parts are produced at the default 1 MB split size.
type BackupFixture struct {
	SrcDir    string
	TargetDir string
	Entry     util.BackupEntry
	Parts     int
	Password  []byte
}

// NewBackupFixture creates a workspace with source files and a completed encrypted split backup.
func NewBackupFixture(t testing.TB, password []byte) *BackupFixture {
	t.Helper()

	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src-data")
	targetDir := filepath.Join(workspace, "target")

	mustMkdirAll(t, filepath.Join(srcDir, "nested"), 0o750)
	mustMkdirAll(t, targetDir, 0o750)
	mustWriteFile(t, filepath.Join(srcDir, "nested", "small.txt"), []byte("hello restoresafe"))
	mustWriteFile(t, filepath.Join(srcDir, "large.bin"), bytes.Repeat([]byte("A"), 2*1024*1024+256))

	folderName := filepath.Base(srcDir)
	backupDate := "2026-03-14"
	backupID := util.BackupID("FIX001")
	parts := createEncryptedSplitBackup(t, srcDir, targetDir, folderName, backupDate, backupID, password, defaultSplitSizeMB)

	return &BackupFixture{
		SrcDir:    srcDir,
		TargetDir: targetDir,
		Entry:     util.BackupEntry{FolderName: folderName, Date: backupDate, ID: backupID},
		Parts:     parts,
		Password:  password,
	}
}

// RestoreFixture extends BackupFixture with a restore output directory in the same workspace.
type RestoreFixture struct {
	*BackupFixture
	RestoreRoot string
}

// NewRestoreFixture creates a BackupFixture and an additional restore output directory.
func NewRestoreFixture(t testing.TB, password []byte) *RestoreFixture {
	t.Helper()

	bf := NewBackupFixture(t, password)
	restoreRoot := filepath.Join(filepath.Dir(bf.SrcDir), "restore")
	mustMkdirAll(t, restoreRoot, 0o750)

	return &RestoreFixture{BackupFixture: bf, RestoreRoot: restoreRoot}
}

func createEncryptedSplitBackup(t testing.TB, srcDir, targetDir, folderName, backupDate string, backupID util.BackupID, password []byte, splitSizeMB int64) int {
	t.Helper()

	nameFunc := func(seq int) string {
		return util.PartFileName(targetDir, folderName, backupDate, backupID, seq)
	}
	sw := util.NewWriter(nameFunc, splitSizeMB*1024*1024)
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

func mustMkdirAll(t testing.TB, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

func mustWriteFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}
