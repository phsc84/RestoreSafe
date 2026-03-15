package verify

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	valid := []verifyPreflightItem{{}, {}}
	if err := validateVerifyPreflight(valid); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}

	invalid := []verifyPreflightItem{{}, {Err: errors.New("broken")}}
	err := validateVerifyPreflight(invalid)
	if err == nil {
		t.Fatal("expected error for invalid verify preflight, got nil")
	}
	if !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}

func TestVerifyEntryWrongPassword(t *testing.T) {
	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src")
	targetDir := filepath.Join(workspace, "target")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	entry := util.BackupEntry{FolderName: filepath.Base(srcDir), Date: "2026-03-14", ID: util.BackupID("ABC123")}

	util.CreateEncryptedSplitBackupForTest(t, srcDir, targetDir, entry.FolderName, entry.Date, entry.ID, []byte("correct-pass"), 1)

	err := verifyEntry(entry, targetDir, []byte("wrong-pass"), nil)
	if err == nil {
		t.Fatal("expected verifyEntry to fail with wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
