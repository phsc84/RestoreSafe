package verify

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"testing"
)

func TestVerifySelectedEntriesSucceeds(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-selected-pass"))

	if _, err := verifySelectedEntries([]util.BackupEntry{fx.Entry}, fx.BackupDir, fx.Password, nil); err != nil {
		t.Fatalf("verifySelectedEntries failed: %v", err)
	}
}

func TestVerifySelectedEntriesRejectsWrongPassword(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("correct-password"))

	_, err := verifySelectedEntries(
		[]util.BackupEntry{fx.Entry},
		fx.BackupDir,
		[]byte("wrong-password"),
		nil,
	)
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}

func TestVerifySelectedEntriesProcessesMultipleEntries(t *testing.T) {
	password := []byte("multi-verify-pass")
	fx1 := testutil.NewBackupFixture(t, password)

	// Create a second independent backup in the same target dir.
	password2 := []byte("multi-verify-pass")
	entry2 := util.BackupEntry{DirectoryName: fx1.Entry.DirectoryName + "2", Date: "2026-04-01", ID: util.BackupID("VER002")}
	testutil.CreateBackupInDir(t, fx1.BackupDir, entry2, password2)

	_, err := verifySelectedEntries(
		[]util.BackupEntry{fx1.Entry, entry2},
		fx1.BackupDir,
		password,
		nil,
	)
	if err != nil {
		t.Fatalf("verifySelectedEntries for multiple entries failed: %v", err)
	}
}
