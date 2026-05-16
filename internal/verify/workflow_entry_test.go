package verify

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"strings"
	"testing"
)

func TestVerifyEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Ghost", Date: "2026-03-14", ID: util.BackupID("GHO001")}

	err := verifyEntry(entry, targetDir, []byte("pw"), nil)
	if err == nil {
		t.Fatal("expected error when no parts found, got nil")
	}
	if !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got: %v", err)
	}
}

func TestVerifyEntryRoundTrip(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-correct-pass"))

	if err := verifyEntry(fx.Entry, fx.TargetDir, fx.Password, nil); err != nil {
		t.Fatalf("verifyEntry failed for correct password: %v", err)
	}
}

func TestVerifyEntryRejectsWrongPassword(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))

	err := verifyEntry(fx.Entry, fx.TargetDir, []byte("wrong-pass"), nil)
	if err == nil {
		t.Fatal("expected verifyEntry to fail with wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
