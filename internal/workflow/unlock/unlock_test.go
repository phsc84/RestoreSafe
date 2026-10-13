package unlock

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
)

type unlockStub struct {
	passwordCalls int
	lines         []string
	out           bytes.Buffer
	console       *interacttest.Script
}

// stubUnlockInputs scripts the user's answers for one test: passwords for
// secret prompts and lines for line prompts. The YubiKey stub answers with
// the challenge at yubiIndex and yubiSecret. Not safe for parallel use.
func stubUnlockInputs(t *testing.T, passwords []string, yubiIndex int, yubiSecret []byte, lines ...string) *unlockStub {
	t.Helper()
	prevCheck, prevDerive := checkYubiKeyConnectedFn, deriveYubiKeySecretFn
	t.Cleanup(func() {
		checkYubiKeyConnectedFn, deriveYubiKeySecretFn = prevCheck, prevDerive
	})
	stub := &unlockStub{lines: lines}
	stub.console = &interacttest.Script{
		Out: &stub.out,
		ReadPassword: func(string) ([]byte, error) {
			if stub.passwordCalls >= len(passwords) {
				return nil, errors.New("no more passwords")
			}
			pw := passwords[stub.passwordCalls]
			stub.passwordCalls++
			return []byte(pw), nil
		},
		ReadLine: func(string) (string, error) {
			if len(stub.lines) == 0 {
				return "", errors.New("no more lines")
			}
			l := stub.lines[0]
			stub.lines = stub.lines[1:]
			return l, nil
		},
	}
	checkYubiKeyConnectedFn = func() error { return nil }
	deriveYubiKeySecretFn = func(challenges []yubikey.ChallengeData) (int, []byte, error) {
		if yubiIndex >= len(challenges) {
			return 0, nil, errors.New("unknown YubiKey")
		}
		return yubiIndex, append([]byte(nil), yubiSecret...), nil
	}
	return stub
}

func testChallenge(noPassword bool, credID string) *yubikey.ChallengeData {
	return &yubikey.ChallengeData{Version: 1, NoPassword: noPassword, CredID: credID, Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
}

// yubiKeySet creates a key set with one YubiKey slot per secret.
func yubiKeySet(t *testing.T, mode config.AuthMode, password []byte, yubiSecrets ...[]byte) (*container.KeySet, []byte) {
	t.Helper()
	ks, master, err := container.NewKeySet(mode)
	if err != nil {
		t.Fatal(err)
	}
	for i, ys := range yubiSecrets {
		secret := ys
		if mode == config.AuthModePasswordYubiKey {
			secret = yubikey.CombinePasswordWithSecret(password, ys)
		}
		cred := []string{"Y3JlZC1h", "Y3JlZC1i"}[i]
		if err := ks.AddSlot(master, container.RegularSlotType(mode), []string{"YubiKey 1", "YubiKey 2 (spare)"}[i], secret, testutil.FastArgon2, testChallenge(mode == config.AuthModeYubiKey, cred), ""); err != nil {
			t.Fatal(err)
		}
	}
	return ks, master
}

// unlock runs KeySet with the scripted answers and returns the output
// of this call.
func (s *unlockStub) unlock(t *testing.T, ks *container.KeySet, opts Options) ([]byte, string, error) {
	t.Helper()
	s.out.Reset()
	got, err := KeySet(s.console, ks, opts, nil)
	return got, s.out.String(), err
}

func TestUnlockKeySetPasswordRetriesAfterWrongPassword(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("right"))
	stub := stubUnlockInputs(t, []string{"wrong", "right"}, 0, nil)

	got, out, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
	if stub.passwordCalls != 2 || !strings.Contains(out, "2 attempt(s) remaining") {
		t.Fatalf("expected one retry, calls=%d out=%q", stub.passwordCalls, out)
	}
}

func TestUnlockKeySetPasswordGivesUpAfterThreeAttempts(t *testing.T) {
	ks, _ := testutil.NewPasswordKeySet(t, []byte("right"))
	stub := stubUnlockInputs(t, []string{"a", "b", "c"}, 0, nil)

	_, _, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

func TestUnlockKeySetPasswordAndYubiKey(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	ks, master := yubiKeySet(t, config.AuthModePasswordYubiKey, []byte("pw"), secret)
	stub := stubUnlockInputs(t, []string{"pw"}, 0, secret)

	got, _, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
}

func TestUnlockKeySetWithSpareYubiKey(t *testing.T) {
	primary := bytes.Repeat([]byte{1}, 32)
	spare := bytes.Repeat([]byte{2}, 32)
	for _, mode := range []config.AuthMode{config.AuthModePasswordYubiKey, config.AuthModeYubiKey} {
		ks, master := yubiKeySet(t, mode, []byte("pw"), primary, spare)
		// The spare YubiKey (index 1) is connected.
		stub := stubUnlockInputs(t, []string{"pw"}, 1, spare)
		got, out, err := stub.unlock(t, ks, Options{Action: "restore"})
		if err != nil || !bytes.Equal(got, master) {
			t.Fatalf("mode %d: unlock with spare failed: %v", mode, err)
		}
		if !strings.Contains(out, "YubiKey 1 or YubiKey 2 (spare)") {
			t.Fatalf("mode %d: expected hint about both YubiKeys, got %q", mode, out)
		}
	}
}

func TestUnlockKeySetYubiKeyOnlyRejectsOtherYubiKey(t *testing.T) {
	ks, master := yubiKeySet(t, config.AuthModeYubiKey, nil, bytes.Repeat([]byte{7}, 32))

	stub := stubUnlockInputs(t, nil, 0, bytes.Repeat([]byte{7}, 32))
	got, _, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock with right YubiKey failed: %v", err)
	}

	stub = stubUnlockInputs(t, nil, 0, bytes.Repeat([]byte{8}, 32))
	_, _, err = stub.unlock(t, ks, Options{Action: "restore"})
	if err == nil || !strings.Contains(err.Error(), "does not unlock the backup") {
		t.Fatalf("expected wrong-YubiKey error, got %v", err)
	}
}

func keySetWithRecovery(t *testing.T) (*container.KeySet, []byte, recovery.Code) {
	t.Helper()
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	code, err := recovery.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.AddSlot(master, container.SlotRecovery, "Recovery code", code.Secret(), testutil.FastArgon2, nil, code.Check()); err != nil {
		t.Fatal(err)
	}
	return ks, master, code
}

func TestUnlockKeySetWithRecoveryCode(t *testing.T) {
	ks, master, code := keySetWithRecovery(t)
	other, _ := recovery.Generate()
	typo := code.Display()
	typo[0] ^= 0x01

	// A typo and a code of other keys are rejected before the right code works.
	stub := stubUnlockInputs(t, []string{string(typo), string(other.Display()), strings.ToLower(string(code.Display()))}, 0, nil, "r")
	got, out, err := stub.unlock(t, ks, Options{Action: "restore", AllowRecovery: true})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("recovery unlock failed: %v (output %q)", err, out)
	}
	if stub.passwordCalls != 3 || !strings.Contains(out, "different keys") {
		t.Fatalf("expected typo and foreign code to be rejected first, output %q", out)
	}
}

func TestUnlockKeySetRecoveryOfferedOnlyWhenAllowed(t *testing.T) {
	ks, master, _ := keySetWithRecovery(t)

	// Without AllowRecovery no choice is asked; the password unlocks.
	stub := stubUnlockInputs(t, []string{"pw"}, 0, nil)
	if got, _, err := stub.unlock(t, ks, Options{Action: "restore"}); err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password unlock failed: %v", err)
	}

	// With AllowRecovery the default answer keeps the password path.
	stub = stubUnlockInputs(t, []string{"pw"}, 0, nil, "")
	if got, _, err := stub.unlock(t, ks, Options{Action: "restore", AllowRecovery: true}); err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password unlock with recovery offered failed: %v", err)
	}
}

func TestUnlockKeySetsAuthenticatesOncePerKeySet(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, naming.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	stub := stubUnlockInputs(t, []string{"pw", "pw"}, 0, nil)

	var keys MasterKeys
	keys, err = KeySets(stub.console, infos, "pw: ", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Zero()
	if stub.passwordCalls != 1 || len(keys) != 1 {
		t.Fatalf("expected one prompt for one shared key set, calls=%d keys=%d", stub.passwordCalls, len(keys))
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

// twoKeySets is a backup directory with a set of the fixture's keys and an
// older set of other keys, protected by "old".
func twoKeySets(t *testing.T) []catalog.SetInfo {
	t.Helper()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	oldKeys, oldMaster := testutil.NewPasswordKeySet(t, []byte("old"))
	testutil.WriteFullSet(t, fx.SrcDir, fx.BackupDir, naming.BackupEntry{DirectoryName: "Older", ChainID: "OLD001", Date: "2026-03-01"}, oldKeys, oldMaster)
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil || len(infos) != 2 || infos[0].Header.KeySet.ID == infos[1].Header.KeySet.ID {
		t.Fatalf("expected two sets with different keys, got %d (%v)", len(infos), err)
	}
	return infos
}

// answerByKeys answers the first key set's prompts with first, and the
// prompts after the other keys were announced with other.
func answerByKeys(stub *unlockStub, first, other string) {
	stub.console.ReadPassword = func(string) ([]byte, error) {
		stub.passwordCalls++
		if strings.Contains(stub.out.String(), "uses different keys") {
			return []byte(other), nil
		}
		return []byte(first), nil
	}
}

// passwordOf returns the password of info's key set in twoKeySets.
func passwordOf(info catalog.SetInfo) string {
	if info.Entry.ChainID == "OLD001" {
		return "old"
	}
	return "pw"
}

func TestUnlockKeySetsAsksAgainForOtherKeys(t *testing.T) {
	infos := twoKeySets(t)
	stub := stubUnlockInputs(t, nil, 0, nil)
	answerByKeys(stub, passwordOf(infos[0]), passwordOf(infos[1]))

	keys, err := KeySets(stub.console, infos, "restore", nil)
	if err != nil {
		t.Fatalf("KeySets: %v\n%s", err, stub.out.String())
	}
	defer keys.Zero()
	if len(keys) != 2 || stub.passwordCalls != 2 {
		t.Fatalf("expected two key sets from two prompts, got %d keys, %d prompts", len(keys), stub.passwordCalls)
	}
}

func TestUnlockKeySetsFailsWhenTheOtherKeysStayLocked(t *testing.T) {
	infos := twoKeySets(t)
	stub := stubUnlockInputs(t, nil, 0, nil)
	answerByKeys(stub, passwordOf(infos[0]), "wrong")

	keys, err := KeySets(stub.console, infos, "restore", nil)
	if err == nil || keys != nil {
		t.Fatalf("expected no keys when the other keys stay locked, got %d keys, err %v", len(keys), err)
	}
	if stub.passwordCalls != 4 {
		t.Fatalf("expected one prompt for the first keys and three for the other, got %d", stub.passwordCalls)
	}
}
