package view

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func TestUnlockDialog(t *testing.T) {
	t.Parallel()
	d := UnlockDialogOf(interact.SecretQuestion{Action: "backup", Attempt: interact.Attempt{N: 1, Left: 3}})
	if d.Title != "Unlock your backups" || d.Intro != "Enter the password of your backups." || d.Error != "" {
		t.Fatalf("first question %+v", d)
	}
	if len(d.Fields) != 1 || !d.Fields[0].Masked || d.OK != "Unlock" || d.Cancel == "" {
		t.Fatalf("fields %+v", d)
	}

	wrong := errors.New("Wrong password.")
	d = UnlockDialogOf(interact.SecretQuestion{Action: "backup", Attempt: interact.Attempt{N: 2, Left: 2, Failure: wrong}})
	if d.Error != "Wrong password. 2 attempts left." {
		t.Fatalf("retry %q", d.Error)
	}
	d = UnlockDialogOf(interact.SecretQuestion{Action: "backup", Attempt: interact.Attempt{N: 3, Left: 1, Failure: wrong}})
	if d.Error != "Wrong password. 1 attempt left." {
		t.Fatalf("last retry %q", d.Error)
	}

	d = UnlockDialogOf(interact.SecretQuestion{Kind: interact.SecretRecoveryCode, Attempt: interact.Attempt{N: 1, Left: 3}})
	if len(d.Fields) != 1 || d.Fields[0].Masked || d.Fields[0].Label != "Recovery code" {
		t.Fatalf("the recovery code is typed unmasked: %+v", d)
	}
	checkWriting(t, d)
}

func TestNewKeysDialogsCountTheSteps(t *testing.T) {
	t.Parallel()
	all := interact.KeyPlan{New: true, Password: true, YubiKeys: 2, RecoveryCode: true}
	q := interact.NewPasswordQuestion{MinLength: 12, Attempt: interact.Attempt{N: 1, Left: 3}}
	d := NewPasswordDialogOf(q, all)
	if d.Title != "Create your keys · Step 1 of 2" || !strings.HasPrefix(d.Hint, "At least 12 characters.") || len(d.Fields) != 2 {
		t.Fatalf("password step %+v", d)
	}
	if d.Note != "Steps: password › register your YubiKey › register your spare YubiKey › recovery code" {
		t.Fatalf("steps %q", d.Note)
	}
	if s := SpareYubiKeyDialogOf(interact.SpareQuestion{Attempt: interact.Attempt{N: 1}}, all); s.Title != "Create your keys · Step 2 of 2" || s.Error != "" {
		t.Fatalf("spare step %+v", s)
	}
	isOne := errors.New("This is YubiKey 1. Remove it and insert your spare YubiKey.")
	s := SpareYubiKeyDialogOf(interact.SpareQuestion{Attempt: interact.Attempt{N: 2, Failure: isOne}}, all)
	if s.Error != "This is YubiKey 1. Remove it and insert your spare YubiKey." {
		t.Fatalf("spare retry %q", s.Error)
	}
	r := RecoveryCodeDialogOf([]byte("K7QF-9M2D-XW4P-HT6N-3JBV-R8LC"), all)
	if r.Title != "Your recovery code" || r.Cancel != "" || string(r.Code) != "K7QF-9M2D-XW4P-HT6N-3JBV-R8LC" || len(r.Fields) != 0 {
		t.Fatalf("recovery code %+v", r)
	}

	only := interact.KeyPlan{New: true, Password: true}
	mismatch := q
	mismatch.Attempt = interact.Attempt{N: 2, Left: 2, Failure: interact.ErrPasswordMismatch}
	if d := NewPasswordDialogOf(mismatch, only); d.Title != "Create your keys" || d.Note != "" || d.Error != "Passwords do not match." {
		t.Fatalf("password only %+v", d)
	}
	checkWriting(t, NewPasswordDialogOf(mismatch, all))
}

func TestUnlockChoiceByKeyType(t *testing.T) {
	t.Parallel()
	older := &interact.OtherKeys{
		Set:     naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-01"},
		Created: time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local),
	}
	d := UnlockChoiceOf(interact.UnlockChoice{Mode: config.AuthModePassword, Keys: older})
	if len(d.Fields) != 1 || !d.Fields[0].Masked || d.Link != "Use your recovery code instead" || !strings.Contains(d.Hint, "created 2026-09-01") {
		t.Fatalf("password keys %+v", d)
	}
	if d := UnlockChoiceOf(interact.UnlockChoice{Mode: config.AuthModePasswordYubiKey}); len(d.Fields) != 0 || !strings.Contains(d.Intro, "YubiKey first") || d.Hint != "" {
		t.Fatalf("password + YubiKey %+v", d)
	}
	if d := UnlockChoiceOf(interact.UnlockChoice{Mode: config.AuthModeYubiKey}); len(d.Fields) != 0 || !strings.Contains(d.Intro, "touch it") {
		t.Fatalf("YubiKey only %+v", d)
	}
	checkWriting(t, d)

	// The password dialog of other keys names them too, until a retry.
	if d := UnlockDialogOf(interact.SecretQuestion{Action: "restore", Keys: older, Attempt: interact.Attempt{N: 1, Left: 3}}); !strings.Contains(d.Hint, "Docs_ABC123_2026-09-01") {
		t.Fatalf("other keys %+v", d)
	}
	retry := interact.SecretQuestion{Action: "restore", Keys: older, Attempt: interact.Attempt{N: 2, Left: 2, Failure: errors.New("Wrong password.")}}
	if d := UnlockDialogOf(retry); d.Hint != "" {
		t.Fatalf("a retry shows the error instead: %+v", d)
	}
}
