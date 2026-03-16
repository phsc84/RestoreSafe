package operation

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"RestoreSafe/internal/security"
)

func TestBackupAuthenticationLabel(t *testing.T) {
	t.Parallel()
	if got := BackupAuthenticationLabel(true, false); got != "password + YubiKey (detected)" {
		t.Fatalf("unexpected label for YubiKey: %q", got)
	}
	if got := BackupAuthenticationLabel(false, false); got != "password only" {
		t.Fatalf("unexpected label without YubiKey: %q", got)
	}
	if got := BackupAuthenticationLabel(true, true); got != "YubiKey only (no password)" {
		t.Fatalf("unexpected label for YubiKey-only: %q", got)
	}
}

func TestPasswordFailurePrefix(t *testing.T) {
	t.Parallel()
	if got := PasswordFailurePrefix(true, false); got != "Wrong password or invalid YubiKey response." {
		t.Fatalf("unexpected prefix for YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(false, false); got != "Wrong password." {
		t.Fatalf("unexpected prefix without YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(true, true); got != "Wrong YubiKey or corrupted file." {
		t.Fatalf("unexpected prefix for YubiKey-only: %q", got)
	}
}

func TestReadChallengeFileTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "sample.challenge")
	if err := os.WriteFile(challengePath, []byte("  abcd1234  \n"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	challenge, err := readChallengeFile(challengePath)
	if err != nil {
		t.Fatalf("readChallengeFile returned error: %v", err)
	}
	if challenge != "abcd1234" {
		t.Fatalf("expected trimmed challenge, got %q", challenge)
	}
}

func TestVerifyPassword(t *testing.T) {
	dir := t.TempDir()
	partPath := filepath.Join(dir, "part-001.enc")
	password := []byte("restore-safe")

	f, err := os.Create(partPath)
	if err != nil {
		t.Fatalf("failed to create encrypted part: %v", err)
	}
	if err := security.Encrypt(f, bytes.NewReader([]byte("payload")), password); err != nil {
		f.Close()
		t.Fatalf("failed to encrypt payload: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close encrypted part: %v", err)
	}

	if err := verifyPassword(partPath, password); err != nil {
		t.Fatalf("verifyPassword should accept correct password, got: %v", err)
	}

	err = verifyPassword(partPath, []byte("wrong"))
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword for invalid password, got: %v", err)
	}
}

func TestVerifyPasswordMissingFile(t *testing.T) {
	t.Parallel()
	err := verifyPassword(filepath.Join(t.TempDir(), "missing.enc"), []byte("pw"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to open file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
