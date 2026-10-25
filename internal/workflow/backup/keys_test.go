package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

type enrollStub struct {
	passwords [][2]string // password and its confirmation, per new password prompt
	yubiKey   []byte      // the hmac-secret of the connected YubiKey
	code      string      // display form of the recovery code
	secret    []byte      // its key material
	ui        *interacttest.Script
}

// stubEnrollment scripts the user's answers and replaces the YubiKey and
// recovery code sources. Not safe for parallel use.
func stubEnrollment(t *testing.T, s *enrollStub) {
	t.Helper()
	prevCheck, prevCombine, prevGen := checkYubiKeyConnectedFn, combineWithPasswordFn, generateRecoveryCodeFn
	t.Cleanup(func() {
		checkYubiKeyConnectedFn, combineWithPasswordFn, generateRecoveryCodeFn = prevCheck, prevCombine, prevGen
	})
	s.yubiKey = bytes.Repeat([]byte{1}, 32)
	var passwords []string
	for _, p := range s.passwords {
		passwords = append(passwords, p[0], p[1])
	}
	s.ui = &interacttest.Script{Secrets: passwords}
	checkYubiKeyConnectedFn = func() error { return nil }
	combineWithPasswordFn = func(pw []byte, noPassword bool) ([]byte, string, error) {
		raw, _ := json.Marshal(yubikey.ChallengeData{Version: 1, NoPassword: noPassword, CredID: "Y3JlZC1h", Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="})
		return yubikey.CombinePasswordWithSecret(pw, s.yubiKey), string(raw), nil
	}
	code, _ := recovery.Generate()
	s.code, s.secret = string(code.Display()), code.Secret()
	// A new Code on every call: the workflow zeroes the one it gets.
	generateRecoveryCodeFn = func() (recovery.Code, error) { return recovery.Parse([]byte(s.code)) }
}

// enroll creates keys with the scripted answers. Besides the keys, it
// returns what the user was shown, one per line: why a new password was
// refused, and the recovery codes.
func enroll(t *testing.T, s *enrollStub, cfg *config.Config) (*container.KeySet, []byte, string, error) {
	t.Helper()
	ks, master, err := enrollKeySet(s.ui, cfg, nil)
	var shown []string
	for _, q := range s.ui.Questions {
		if q, ok := q.(interact.NewPasswordQuestion); ok && q.Failure != nil {
			shown = append(shown, q.Failure.Error())
		}
	}
	return ks, master, strings.Join(append(shown, s.ui.RecoveryCodes...), "\n"), err
}

func TestEnrollKeySetPasswordModeEnforcesMinimumAndRetries(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"short", "short"}, {"long enough pw", "typo"}, {"long enough pw", "long enough pw"}}}
	stubEnrollment(t, s)
	cfg := &config.Config{AuthenticationMode: config.AuthModePassword, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}

	ks, master, out, err := enroll(t, s, cfg)
	if err != nil {
		t.Fatalf("enrollKeySet: %v", err)
	}
	if !strings.Contains(out, "at least 12 are required") || !strings.Contains(out, "Passwords do not match") {
		t.Fatalf("expected minimum and mismatch messages, got %q", out)
	}
	if got, err := ks.Unlock(0, []byte("long enough pw")); err != nil || !bytes.Equal(got, master) {
		t.Fatalf("password does not unlock the new keys: %v", err)
	}
}

func TestEnrollKeySetGivesUpAfterThreeInvalidPasswords(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"a", "a"}, {"b", "b"}, {"c", "c"}}}
	stubEnrollment(t, s)
	cfg := &config.Config{AuthenticationMode: config.AuthModePassword, PasswordMinLength: 8, Argon2: testutil.FastArgon2Config}
	if _, _, _, err := enroll(t, s, cfg); err == nil || !strings.Contains(err.Error(), "No valid new password") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

func TestEnrollKeySetCountsCharactersNotBytes(t *testing.T) {
	// 8 characters, 16 bytes in UTF-8.
	s := &enrollStub{passwords: [][2]string{{"äöüßäöüß", "äöüßäöüß"}}}
	stubEnrollment(t, s)
	cfg := &config.Config{AuthenticationMode: config.AuthModePassword, PasswordMinLength: 8, Argon2: testutil.FastArgon2Config}
	if _, _, _, err := enroll(t, s, cfg); err != nil {
		t.Fatalf("8-character password must be accepted: %v", err)
	}
}

// With a YubiKey, new keys always get a recovery code, even with
// recovery_code: false.
func TestEnrollKeySetWithYubiKeyAddsTheRecoveryCode(t *testing.T) {
	for _, mode := range []config.AuthMode{config.AuthModePasswordYubiKey, config.AuthModeYubiKey} {
		s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}}
		stubEnrollment(t, s)
		cfg := &config.Config{AuthenticationMode: mode, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}

		ks, master, out, err := enroll(t, s, cfg)
		if err != nil {
			t.Fatalf("mode %d: enrollKeySet: %v\n%s", mode, err, out)
		}
		if !strings.Contains(out, s.code) {
			t.Fatalf("mode %d: the recovery code wasn't shown: %q", mode, out)
		}
		if len(ks.Slots) != 2 || ks.Slots[0].Label != "YubiKey" || !ks.HasSlotType(container.SlotRecovery) {
			t.Fatalf("mode %d: unexpected slots %+v", mode, ks.Slots)
		}
		if err := ks.Validate(); err != nil {
			t.Fatalf("mode %d: key set invalid: %v", mode, err)
		}
		secret := s.yubiKey
		if mode == config.AuthModePasswordYubiKey {
			secret = yubikey.CombinePasswordWithSecret([]byte("a long password"), secret)
		}
		if got, err := ks.Unlock(0, secret); err != nil || !bytes.Equal(got, master) {
			t.Fatalf("mode %d: the YubiKey does not unlock: %v", mode, err)
		}
		if got, err := ks.Unlock(1, s.secret); err != nil || !bytes.Equal(got, master) {
			t.Fatalf("mode %d: recovery code does not unlock: %v", mode, err)
		}
	}
}

func TestEnrollKeySetRequiresConnectedYubiKey(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}}
	stubEnrollment(t, s)
	checkYubiKeyConnectedFn = func() error { return errors.New("not connected") }
	cfg := &config.Config{AuthenticationMode: config.AuthModePasswordYubiKey, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}
	if _, _, _, err := enroll(t, s, cfg); !errors.Is(err, yubikey.ErrRequired) {
		t.Fatalf("expected yubikey.ErrRequired, got %v", err)
	}
}

func TestObtainKeysReusesExistingKeySet(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	prev := unlockKeySetFn
	t.Cleanup(func() { unlockKeySetFn = prev })
	unlockKeySetFn = func(_ interact.UI, got *container.KeySet, opts unlock.Options, _ *logging.Logger) ([]byte, error) {
		if got.ID != ks.ID || opts.AllowRecovery {
			t.Fatalf("unexpected unlock request (recovery must not be offered for backups)")
		}
		return append([]byte(nil), master...), nil
	}

	gotKS, gotMaster, err := obtainKeys(&interacttest.Script{}, &config.Config{}, plan.Keys{Existing: ks}, nil)
	if err != nil || gotKS.ID != ks.ID || !bytes.Equal(gotMaster, master) {
		t.Fatalf("existing keys not reused: %v", err)
	}
}
