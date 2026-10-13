package catalog

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

// keySet is a key set of mode with slots of the given types; KeySetMismatch
// reads nothing else.
func keySet(mode config.AuthMode, slots ...string) *container.KeySet {
	ks := &container.KeySet{AuthMode: mode}
	for _, s := range slots {
		ks.Slots = append(ks.Slots, container.Slot{Type: s})
	}
	return ks
}

func TestKeySetMismatchNamesTheChangedSetting(t *testing.T) {
	t.Parallel()

	twoKeys := keySet(config.AuthModePasswordYubiKey, container.SlotPasswordYubiKey, container.SlotPasswordYubiKey)
	for _, tc := range []struct {
		cfg  config.Config
		ks   *container.KeySet
		want string
	}{
		{config.Config{AuthenticationMode: config.AuthModePassword}, keySet(config.AuthModePassword, container.SlotPassword), ""},
		{config.Config{AuthenticationMode: config.AuthModeYubiKey}, keySet(config.AuthModePassword, container.SlotPassword), "authentication_mode changed"},
		{config.Config{AuthenticationMode: config.AuthModePasswordYubiKey, YubiKeySpare: true}, keySet(config.AuthModePasswordYubiKey, container.SlotPasswordYubiKey), "yubikey_spare enabled"},
		{config.Config{AuthenticationMode: config.AuthModePasswordYubiKey}, twoKeys, "yubikey_spare disabled"},
		{config.Config{AuthenticationMode: config.AuthModePassword, RecoveryCode: true}, keySet(config.AuthModePassword, container.SlotPassword), "recovery_code enabled"},
		{config.Config{AuthenticationMode: config.AuthModePassword}, keySet(config.AuthModePassword, container.SlotPassword, container.SlotRecovery), "recovery_code disabled"},
	} {
		if got := KeySetMismatch(&tc.cfg, tc.ks); !strings.HasPrefix(got, tc.want) || (tc.want == "") != (got == "") {
			t.Fatalf("%+v with %+v: got %q, want %q", tc.cfg, tc.ks.Slots, got, tc.want)
		}
	}
}

func TestCurrentKeySetIsTheNewestCompleteSet(t *testing.T) {
	t.Parallel()

	older := &container.Header{KeySet: container.KeySet{ID: "older"}}
	infos := []SetInfo{
		{Err: errors.New("damaged")},
		{Header: older},
	}
	if ks := CurrentKeySet(infos); ks == nil || ks.ID != "older" {
		t.Fatalf("expected the newest complete set's keys, got %+v", ks)
	}
	if CurrentKeySet(infos[:1]) != nil {
		t.Fatal("no complete set: no current keys")
	}
}

func TestBaseOfFindsTheFullBackupOfTheChain(t *testing.T) {
	t.Parallel()

	full := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}
	diff := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20", DiffNumber: 1}
	other := naming.BackupEntry{DirectoryName: "Pics", ChainID: "ABC123", Date: "2026-03-14"}
	complete := []SetInfo{{Entry: diff, Header: &container.Header{}}, {Entry: other, Header: &container.Header{}}, {Entry: full, Header: &container.Header{}}}

	if base, err := BaseOf(complete, diff); err != nil || base.Entry != full {
		t.Fatalf("expected %s, got %+v, %v", full, base, err)
	}
	damaged := []SetInfo{{Entry: diff, Header: &container.Header{}}, {Entry: full, Err: errors.New("trailer not found")}}
	if _, err := BaseOf(damaged, diff); err == nil || !strings.Contains(err.Error(), "is incomplete") {
		t.Fatalf("an incomplete full backup: %v", err)
	}
	if _, err := BaseOf(complete[:2], diff); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("a missing full backup: %v", err)
	}
}

func TestCollectPartsReportsAnUnreadableDirectory(t *testing.T) {
	t.Parallel()

	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}
	if _, err := CollectParts(filepath.Join(t.TempDir(), "missing"), entry); err == nil || !strings.Contains(err.Error(), "Failed to read backup directory") {
		t.Fatalf("expected the directory error, got %v", err)
	}
	if parts, err := CollectParts(t.TempDir(), entry); err != nil || len(parts) != 0 {
		t.Fatalf("an empty directory: %v, %v", parts, err)
	}
}
