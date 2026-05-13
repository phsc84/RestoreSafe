package verify

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"errors"
	"testing"
)

func TestVerifyEntryRoundTrip(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-correct-pass"))

	if err := verifyEntry(fx.Entry, fx.TargetDir, fx.Password, nil); err != nil {
		t.Fatalf("verifyEntry failed for correct password: %v", err)
	}
}

func TestVerifyEntryWrongPassword(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))

	err := verifyEntry(fx.Entry, fx.TargetDir, []byte("wrong-pass"), nil)
	if err == nil {
		t.Fatal("expected verifyEntry to fail with wrong password")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
