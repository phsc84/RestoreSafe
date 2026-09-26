package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// stubUnlockInputs replaces the credential sources for one test. Not safe for
// parallel use.
func stubUnlockInputs(t *testing.T, passwords []string, yubiSecret []byte) *int {
	t.Helper()
	prevRead, prevCheck, prevDerive := readPasswordFn, checkYubiKeyConnectedFn, deriveYubiKeySecretFn
	t.Cleanup(func() {
		readPasswordFn, checkYubiKeyConnectedFn, deriveYubiKeySecretFn = prevRead, prevCheck, prevDerive
	})
	calls := 0
	readPasswordFn = func(string) ([]byte, error) {
		if calls >= len(passwords) {
			return nil, errors.New("no more passwords")
		}
		pw := passwords[calls]
		calls++
		return []byte(pw), nil
	}
	checkYubiKeyConnectedFn = func() error { return nil }
	deriveYubiKeySecretFn = func(string) ([]byte, error) { return append([]byte(nil), yubiSecret...), nil }
	return &calls
}

func testChallenge(noPassword bool) *security.ChallengeData {
	return &security.ChallengeData{Version: 1, NoPassword: noPassword, CredID: "Y3JlZC1pZA==", Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
}

func yubiKeySet(t *testing.T, mode int, password, yubiSecret []byte) (*container.KeySet, []byte) {
	t.Helper()
	ks, master, err := container.NewKeySet(mode)
	if err != nil {
		t.Fatal(err)
	}
	secret := yubiSecret
	if mode == container.AuthModePasswordYubiKey {
		secret = security.CombinePasswordWithSecret(password, yubiSecret)
	}
	if err := ks.AddSlot(master, container.RegularSlotType(mode), "YubiKey 1", secret, testutil.FastArgon2, testChallenge(mode == container.AuthModeYubiKey), ""); err != nil {
		t.Fatal(err)
	}
	return ks, master
}

func TestUnlockKeySetPasswordRetriesAfterWrongPassword(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("right"))
	calls := stubUnlockInputs(t, []string{"wrong", "right"}, nil)

	var got []byte
	var err error
	out := testutil.CaptureStdout(t, func() { got, err = UnlockKeySet(ks, "pw: ", nil) })
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
	if *calls != 2 || !strings.Contains(out, "2 attempt(s) remaining") {
		t.Fatalf("expected one retry, calls=%d out=%q", *calls, out)
	}
}

func TestUnlockKeySetPasswordGivesUpAfterThreeAttempts(t *testing.T) {
	ks, _ := testutil.NewPasswordKeySet(t, []byte("right"))
	stubUnlockInputs(t, []string{"a", "b", "c"}, nil)

	var err error
	testutil.CaptureStdout(t, func() { _, err = UnlockKeySet(ks, "pw: ", nil) })
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

func TestUnlockKeySetPasswordAndYubiKey(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	ks, master := yubiKeySet(t, container.AuthModePasswordYubiKey, []byte("pw"), secret)
	stubUnlockInputs(t, []string{"pw"}, secret)

	var got []byte
	var err error
	testutil.CaptureStdout(t, func() { got, err = UnlockKeySet(ks, "pw: ", nil) })
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
}

func TestUnlockKeySetYubiKeyOnlyRejectsOtherYubiKey(t *testing.T) {
	ks, master := yubiKeySet(t, container.AuthModeYubiKey, nil, bytes.Repeat([]byte{7}, 32))

	stubUnlockInputs(t, nil, bytes.Repeat([]byte{7}, 32))
	var got []byte
	var err error
	testutil.CaptureStdout(t, func() { got, err = UnlockKeySet(ks, "pw: ", nil) })
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock with right YubiKey failed: %v", err)
	}

	stubUnlockInputs(t, nil, bytes.Repeat([]byte{8}, 32))
	testutil.CaptureStdout(t, func() { _, err = UnlockKeySet(ks, "pw: ", nil) })
	if err == nil || !strings.Contains(err.Error(), "does not unlock the backup") {
		t.Fatalf("expected wrong-YubiKey error, got %v", err)
	}
}

func TestUnlockKeySetsAuthenticatesOncePerKeySet(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, util.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	calls := stubUnlockInputs(t, []string{"pw", "pw"}, nil)

	var keys MasterKeys
	testutil.CaptureStdout(t, func() { keys, err = UnlockKeySets(infos, "pw: ", nil) })
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Zero()
	if *calls != 1 || len(keys) != 1 {
		t.Fatalf("expected one prompt for one shared key set, calls=%d keys=%d", *calls, len(keys))
	}
}
