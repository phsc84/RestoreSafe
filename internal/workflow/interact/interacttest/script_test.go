package interacttest

import (
	"errors"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func TestScriptAnswersInOrderAndNamesAMissingAnswer(t *testing.T) {
	s := &Script{Starts: []interact.BackupStart{interact.BackupAsPlanned, interact.BackupCancel}}
	for _, want := range []bool{true, false} {
		if got, err := s.ConfirmStart("verification"); err != nil || got != want {
			t.Fatalf("ConfirmStart = %v, %v; want %v", got, err, want)
		}
	}
	if _, err := s.ConfirmStart("restore"); err == nil || !strings.Contains(err.Error(), "ConfirmStart(restore)") {
		t.Fatalf("expected the missing answer to be named, got %v", err)
	}
	if len(s.Questions) != 3 || s.Questions[2] != "restore" {
		t.Fatalf("questions = %v", s.Questions)
	}
}

func TestConfirmBackupStartRefusesAnAnswerThatIsNotOffered(t *testing.T) {
	cases := []struct {
		name   string
		opts   interact.BackupStartOptions
		answer interact.BackupStart
		ok     bool
	}{
		{"as planned", interact.BackupStartOptions{}, interact.BackupAsPlanned, true},
		{"cancel is always offered", interact.BackupStartOptions{Blocked: true}, interact.BackupCancel, true},
		{"a blocked plan cannot start", interact.BackupStartOptions{Blocked: true}, interact.BackupAsPlanned, false},
		{"full backup offered", interact.BackupStartOptions{OfferFull: true}, interact.BackupFull, true},
		{"full backup not offered", interact.BackupStartOptions{OfferNewKeys: true}, interact.BackupFull, false},
		{"new keys not offered", interact.BackupStartOptions{OfferFull: true}, interact.BackupNewKeys, false},
		{"automatic plan not offered", interact.BackupStartOptions{OfferFull: true}, interact.BackupAutomatic, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Script{Starts: []interact.BackupStart{tc.answer}}
			got, err := s.ConfirmBackupStart(tc.opts)
			if tc.ok && (err != nil || got != tc.answer) {
				t.Fatalf("ConfirmBackupStart = %v, %v; want %v", got, err, tc.answer)
			}
			if !tc.ok && (err == nil || got != interact.BackupCancel) {
				t.Fatalf("ConfirmBackupStart = %v, %v; want an error", got, err)
			}
		})
	}
}

func TestNewPasswordTakesTwoSecrets(t *testing.T) {
	s := &Script{Secrets: []string{"long enough", "long enough", "one", "two"}}
	if pw, err := s.NewPassword(interact.NewPasswordQuestion{}); err != nil || string(pw) != "long enough" {
		t.Fatalf("NewPassword = %q, %v", pw, err)
	}
	if _, err := s.NewPassword(interact.NewPasswordQuestion{}); !errors.Is(err, interact.ErrPasswordMismatch) {
		t.Fatalf("expected a mismatch, got %v", err)
	}
	if _, err := s.Password(interact.SecretQuestion{}); err == nil {
		t.Fatal("expected an error once the secrets are used up")
	}
}

func TestScriptRecordsWhatItIsShown(t *testing.T) {
	s := &Script{}
	s.ShowRecoveryCode([]byte("ABCD-EFGH")) //nolint:errcheck
	s.ShowResult(interact.Result{Warnings: 2, LogPath: "C:/Backups/run.log"})
	if len(s.RecoveryCodes) != 1 || s.RecoveryCodes[0] != "ABCD-EFGH" || len(s.Results) != 1 || s.Results[0].Warnings != 2 {
		t.Fatalf("codes %q, results %+v", s.RecoveryCodes, s.Results)
	}
}
