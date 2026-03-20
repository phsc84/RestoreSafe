// Package testutil provides shared test fixtures and helpers for integration-style tests.
package testutil

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const (
	defaultSplitSizeMB = 1
)

// BackupFixture holds workspace paths and metadata for an integration-style backup test.
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
	bw := bufio.NewWriterSize(sw, util.SplitWriteBufferSize)

	pr, pw := io.Pipe()
	tarErrCh := make(chan error, 1)
	go func() {
		err := util.WriteTar(pw, srcDir, targetDir)
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
		t.Fatalf("WriteTar returned error: %v", tarErr)
	}

	return len(sw.Paths())
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
