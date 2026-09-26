package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func infoWithKeySet(ks *container.KeySet) catalog.SetInfo {
	return catalog.SetInfo{Header: &container.Header{KeySet: *ks, CreatedUTC: ks.CreatedUTC}}
}

func TestPlanKeys(t *testing.T) {
	t.Parallel()

	ks, _ := testutil.NewPasswordKeySet(t, []byte("pw"))
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword}

	if p := planKeys(cfg, nil); p.Existing != nil || !strings.Contains(p.NewKeysReason, "No existing keys") {
		t.Fatalf("empty directory: %+v", p)
	}
	if p := planKeys(cfg, []catalog.SetInfo{infoWithKeySet(ks)}); p.Existing == nil || p.Existing.ID != ks.ID {
		t.Fatalf("matching keys not reused: %+v", p)
	}
	cfg.AuthenticationMode = util.AuthModePasswordYubiKey
	if p := planKeys(cfg, []catalog.SetInfo{infoWithKeySet(ks)}); p.Existing != nil || !strings.Contains(p.NewKeysReason, "authentication_mode changed") {
		t.Fatalf("mode change must require new keys: %+v", p)
	}
}

// stubEnrollment replaces the interactive credential sources. Not safe for
// parallel use.
func stubEnrollment(t *testing.T, password string, yubiSecret []byte) {
	t.Helper()
	prevRead, prevCheck, prevCombine := readPasswordConfirmedFn, checkYubiKeyConnectedFn, combineWithPasswordFn
	t.Cleanup(func() {
		readPasswordConfirmedFn, checkYubiKeyConnectedFn, combineWithPasswordFn = prevRead, prevCheck, prevCombine
	})
	readPasswordConfirmedFn = func(string, string) ([]byte, error) { return []byte(password), nil }
	checkYubiKeyConnectedFn = func() error { return nil }
	combineWithPasswordFn = func(pw []byte, noPassword bool) ([]byte, string, error) {
		cd := security.ChallengeData{Version: 1, NoPassword: noPassword, CredID: "Y3JlZC1pZA==", Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
		raw, _ := json.Marshal(cd)
		return security.CombinePasswordWithSecret(pw, yubiSecret), string(raw), nil
	}
}

func TestEnrollKeySetPasswordMode(t *testing.T) {
	stubEnrollment(t, "new-password", nil)
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword, Argon2: testutil.FastArgon2Config}

	var ks *container.KeySet
	var master []byte
	var err error
	testutil.CaptureStdout(t, func() { ks, master, err = enrollKeySet(cfg, util.NewConsoleLogger("info")) })
	if err != nil {
		t.Fatalf("enrollKeySet: %v", err)
	}
	if len(ks.Slots) != 1 || ks.Slots[0].Type != container.SlotPassword {
		t.Fatalf("unexpected slots: %+v", ks.Slots)
	}
	got, err := ks.Unlock(0, []byte("new-password"))
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password does not unlock the new keys: %v", err)
	}
}

func TestEnrollKeySetYubiKeyModes(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	for _, mode := range []util.AuthMode{util.AuthModePasswordYubiKey, util.AuthModeYubiKey} {
		stubEnrollment(t, "pw-and-key", secret)
		cfg := &util.Config{AuthenticationMode: mode, Argon2: testutil.FastArgon2Config}

		var ks *container.KeySet
		var master []byte
		var err error
		testutil.CaptureStdout(t, func() { ks, master, err = enrollKeySet(cfg, util.NewConsoleLogger("info")) })
		if err != nil {
			t.Fatalf("mode %d: enrollKeySet: %v", mode, err)
		}
		slot := ks.Slots[0]
		if slot.Type != container.RegularSlotType(int(mode)) || slot.Challenge == nil || slot.Label != "YubiKey 1" {
			t.Fatalf("mode %d: unexpected slot %+v", mode, slot)
		}
		unlockSecret := secret
		if mode == util.AuthModePasswordYubiKey {
			unlockSecret = security.CombinePasswordWithSecret([]byte("pw-and-key"), secret)
		}
		if got, err := ks.Unlock(0, unlockSecret); err != nil || !bytes.Equal(got, master) {
			t.Fatalf("mode %d: YubiKey secret does not unlock the new keys: %v", mode, err)
		}
	}
}

func TestEnrollKeySetRequiresConnectedYubiKey(t *testing.T) {
	stubEnrollment(t, "pw", nil)
	checkYubiKeyConnectedFn = func() error { return errors.New("not connected") }
	cfg := &util.Config{AuthenticationMode: util.AuthModePasswordYubiKey, Argon2: testutil.FastArgon2Config}
	var err error
	testutil.CaptureStdout(t, func() { _, _, err = enrollKeySet(cfg, util.NewConsoleLogger("info")) })
	if !errors.Is(err, security.ErrYubiKeyRequired) {
		t.Fatalf("expected ErrYubiKeyRequired, got %v", err)
	}
}

func TestObtainKeysReusesExistingKeySet(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	prev := unlockKeySetFn
	t.Cleanup(func() { unlockKeySetFn = prev })
	unlockKeySetFn = func(got *container.KeySet, _ string, _ *util.Logger) ([]byte, error) {
		if got.ID != ks.ID {
			t.Fatalf("unlocking wrong key set")
		}
		return append([]byte(nil), master...), nil
	}

	gotKS, gotMaster, err := obtainKeys(&util.Config{}, keyPlan{Existing: ks}, util.NewConsoleLogger("info"))
	if err != nil || gotKS.ID != ks.ID || !bytes.Equal(gotMaster, master) {
		t.Fatalf("existing keys not reused: %v", err)
	}
}

func TestDescribeKeySet(t *testing.T) {
	t.Parallel()

	ks, _ := testutil.NewPasswordKeySet(t, []byte("pw"))
	if got := describeKeySet(ks); !strings.Contains(got, "password only") {
		t.Fatalf("unexpected description: %q", got)
	}
}
