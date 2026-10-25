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
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
)

type unlockStub struct {
	ui *interacttest.Script
}

// stubUnlockInputs scripts the user's answers for one test: passwords for
// the secret questions and the answers to the unlock method question
// (true: recovery code). The YubiKey stub answers with yubiSecret. Not safe
// for parallel use.
func stubUnlockInputs(t *testing.T, passwords []string, yubiSecret []byte, recovery ...bool) *unlockStub {
	t.Helper()
	prevCheck, prevDerive := checkYubiKeyConnectedFn, deriveYubiKeySecretFn
	t.Cleanup(func() {
		checkYubiKeyConnectedFn, deriveYubiKeySecretFn = prevCheck, prevDerive
	})
	stub := &unlockStub{ui: &interacttest.Script{Secrets: passwords, Recovery: recovery}}
	checkYubiKeyConnectedFn = func() error { return nil }
	deriveYubiKeySecretFn = func(yubikey.ChallengeData) ([]byte, error) {
		return append([]byte(nil), yubiSecret...), nil
	}
	return stub
}

func testChallenge(noPassword bool, credID string) *yubikey.ChallengeData {
	return &yubikey.ChallengeData{Version: 1, NoPassword: noPassword, CredID: credID, Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
}

// yubiKeySet creates a key set with a YubiKey slot for yubiSecret.
func yubiKeySet(t *testing.T, mode config.AuthMode, password, yubiSecret []byte) (*container.KeySet, []byte) {
	t.Helper()
	ks, master, err := container.NewKeySet(mode)
	if err != nil {
		t.Fatal(err)
	}
	secret := yubiSecret
	if mode == config.AuthModePasswordYubiKey {
		secret = yubikey.CombinePasswordWithSecret(password, yubiSecret)
	}
	if err := ks.AddSlot(master, container.RegularSlotType(mode), "YubiKey", secret, testutil.FastArgon2, testChallenge(mode == config.AuthModeYubiKey, "Y3JlZC1h"), ""); err != nil {
		t.Fatal(err)
	}
	return ks, master
}

// unlock runs KeySet with the scripted answers.
func (s *unlockStub) unlock(t *testing.T, ks *container.KeySet, opts Options) ([]byte, error) {
	t.Helper()
	return KeySet(s.ui, ks, opts, nil)
}

// secrets returns the secret questions asked.
func (s *unlockStub) secrets() []interact.SecretQuestion {
	var qs []interact.SecretQuestion
	for _, q := range s.ui.Questions {
		if sq, ok := q.(interact.SecretQuestion); ok {
			qs = append(qs, sq)
		}
	}
	return qs
}

func TestKeySetPasswordRetriesAfterWrongPassword(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("right"))
	stub := stubUnlockInputs(t, []string{"wrong", "right"}, nil)

	got, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
	if qs := stub.secrets(); len(qs) != 2 || !qs[1].Retry() || qs[1].Left != 2 || qs[1].Failure == nil {
		t.Fatalf("expected one retry with 2 attempts left, got %+v", qs)
	}
}

func TestKeySetPasswordGivesUpAfterThreeAttempts(t *testing.T) {
	ks, _ := testutil.NewPasswordKeySet(t, []byte("right"))
	stub := stubUnlockInputs(t, []string{"a", "b", "c"}, nil)

	_, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

func TestKeySetPasswordAndYubiKey(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	ks, master := yubiKeySet(t, config.AuthModePasswordYubiKey, []byte("pw"), secret)
	stub := stubUnlockInputs(t, []string{"pw"}, secret)

	got, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock failed: %v", err)
	}
}

func TestKeySetYubiKeyOnlyRejectsOtherYubiKey(t *testing.T) {
	ks, master := yubiKeySet(t, config.AuthModeYubiKey, nil, bytes.Repeat([]byte{7}, 32))

	stub := stubUnlockInputs(t, nil, bytes.Repeat([]byte{7}, 32))
	got, err := stub.unlock(t, ks, Options{Action: "restore"})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("unlock with right YubiKey failed: %v", err)
	}

	stub = stubUnlockInputs(t, nil, bytes.Repeat([]byte{8}, 32))
	_, err = stub.unlock(t, ks, Options{Action: "restore"})
	var yk *YubiKeyError
	if !errors.As(err, &yk) || !strings.Contains(err.Error(), "does not unlock the backup") {
		t.Fatalf("expected wrong-YubiKey error, got %v", err)
	}
}

func TestKeySetReportsAMissingOrFailingYubiKey(t *testing.T) {
	ks, _ := yubiKeySet(t, config.AuthModeYubiKey, nil, bytes.Repeat([]byte{7}, 32))

	stub := stubUnlockInputs(t, nil, nil)
	checkYubiKeyConnectedFn = func() error { return errors.New("not connected") }
	if _, err := stub.unlock(t, ks, Options{Action: "restore"}); !errors.Is(err, yubikey.ErrRequired) || errors.As(err, new(*YubiKeyError)) {
		t.Fatalf("no YubiKey: got %v, want yubikey.ErrRequired", err)
	}

	stub = stubUnlockInputs(t, nil, nil)
	timedOut := errors.New("touch timed out")
	deriveYubiKeySecretFn = func(yubikey.ChallengeData) ([]byte, error) { return nil, timedOut }
	var yk *YubiKeyError
	if _, err := stub.unlock(t, ks, Options{Action: "restore"}); !errors.As(err, &yk) || !errors.Is(err, timedOut) || !strings.Contains(err.Error(), "YubiKey authentication failed: touch timed out") {
		t.Fatalf("failing YubiKey: %v", err)
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

func TestKeySetWithRecoveryCode(t *testing.T) {
	ks, master, code := keySetWithRecovery(t)
	other, _ := recovery.Generate()
	typo := code.Display()
	typo[0] ^= 0x01

	// A typo and a code of other keys are rejected before the right code works.
	stub := stubUnlockInputs(t, []string{string(typo), string(other.Display()), strings.ToLower(string(code.Display()))}, nil, true)
	got, err := stub.unlock(t, ks, Options{Action: "restore", AllowRecovery: true})
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("recovery unlock failed: %v", err)
	}
	qs := stub.secrets()
	if len(qs) != 3 || qs[0].Kind != interact.SecretRecoveryCode || qs[2].Failure == nil || !strings.Contains(qs[2].Failure.Error(), "different keys") {
		t.Fatalf("expected typo and foreign code to be rejected first, got %+v", qs)
	}
}

func TestKeySetRecoveryOfferedOnlyWhenAllowed(t *testing.T) {
	ks, master, _ := keySetWithRecovery(t)

	// Without AllowRecovery no choice is asked; the password unlocks.
	stub := stubUnlockInputs(t, []string{"pw"}, nil)
	if got, err := stub.unlock(t, ks, Options{Action: "restore"}); err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password unlock failed: %v", err)
	}

	// With AllowRecovery the default answer keeps the password path.
	stub = stubUnlockInputs(t, []string{"pw"}, nil, false)
	if got, err := stub.unlock(t, ks, Options{Action: "restore", AllowRecovery: true}); err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password unlock with recovery offered failed: %v", err)
	}
}

func TestKeySetsAuthenticatesOncePerKeySet(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, naming.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	stub := stubUnlockInputs(t, []string{"pw", "pw"}, nil)

	var keys MasterKeys
	keys, err = KeySets(stub.ui, infos, "pw: ", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Zero()
	if len(stub.secrets()) != 1 || len(keys) != 1 {
		t.Fatalf("expected one prompt for one shared key set, calls=%d keys=%d", len(stub.secrets()), len(keys))
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

// passwordOf returns the password of info's key set in twoKeySets.
func passwordOf(info catalog.SetInfo) string {
	if info.Entry.ChainID == "OLD001" {
		return "old"
	}
	return "pw"
}

func TestKeySetsAsksAgainForOtherKeys(t *testing.T) {
	infos := twoKeySets(t)
	stub := stubUnlockInputs(t, []string{passwordOf(infos[0]), passwordOf(infos[1])}, nil)

	keys, err := KeySets(stub.ui, infos, "restore", nil)
	if err != nil {
		t.Fatalf("KeySets: %v", err)
	}
	defer keys.Zero()
	if len(keys) != 2 || len(stub.secrets()) != 2 {
		t.Fatalf("expected two key sets from two prompts, got %d keys, %d prompts", len(keys), len(stub.secrets()))
	}
	if qs := stub.secrets(); qs[0].Keys != nil || qs[1].Keys == nil || qs[1].Keys.Set != infos[1].Entry {
		t.Fatalf("only the second question names the other keys, got %+v", qs)
	}
}

func TestKeySetsFailsWhenTheOtherKeysStayLocked(t *testing.T) {
	infos := twoKeySets(t)
	stub := stubUnlockInputs(t, []string{passwordOf(infos[0]), "wrong", "wrong", "wrong"}, nil)

	keys, err := KeySets(stub.ui, infos, "restore", nil)
	if err == nil || keys != nil {
		t.Fatalf("expected no keys when the other keys stay locked, got %d keys, err %v", len(keys), err)
	}
	if len(stub.secrets()) != 4 {
		t.Fatalf("expected one prompt for the first keys and three for the other, got %d", len(stub.secrets()))
	}
}
