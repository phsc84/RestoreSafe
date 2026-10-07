package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/recovery"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/plan"
	"RestoreSafe/internal/workflow/unlock"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type enrollStub struct {
	passwords [][2]string // password and its confirmation, per new password prompt
	lines     []string
	connected string // "keyA" or "keyB": which YubiKey is plugged in
	secrets   map[string][]byte
	code      recovery.Code
	out       bytes.Buffer
	console   *interacttest.Script
}

// stubEnrollment scripts the user's answers and replaces the YubiKey and
// recovery code sources. Not safe for parallel use.
func stubEnrollment(t *testing.T, s *enrollStub) {
	t.Helper()
	prevCheck, prevCombine, prevSpare, prevGen := checkYubiKeyConnectedFn, combineWithPasswordFn, registerSpareFn, generateRecoveryCodeFn
	t.Cleanup(func() {
		checkYubiKeyConnectedFn, combineWithPasswordFn, registerSpareFn, generateRecoveryCodeFn = prevCheck, prevCombine, prevSpare, prevGen
	})
	s.secrets = map[string][]byte{"keyA": bytes.Repeat([]byte{1}, 32), "keyB": bytes.Repeat([]byte{2}, 32)}
	var passwords []string
	for _, p := range s.passwords {
		passwords = append(passwords, p[0], p[1])
	}
	s.console = &interacttest.Script{
		Out: &s.out,
		ReadPassword: func(string) ([]byte, error) {
			if len(passwords) == 0 {
				return nil, errors.New("no more passwords")
			}
			p := passwords[0]
			passwords = passwords[1:]
			return []byte(p), nil
		},
		ReadLine: func(string) (string, error) {
			if len(s.lines) == 0 {
				return "", errors.New("no more lines")
			}
			l := s.lines[0]
			s.lines = s.lines[1:]
			if l == "<swap>" {
				s.connected = "keyB"
				return "", nil
			}
			return l, nil
		},
	}
	checkYubiKeyConnectedFn = func() error { return nil }
	challenge := func(key string, noPassword bool) string {
		raw, _ := json.Marshal(yubikey.ChallengeData{Version: 1, NoPassword: noPassword, CredID: map[string]string{"keyA": "Y3JlZC1h", "keyB": "Y3JlZC1i"}[key], Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="})
		return string(raw)
	}
	s.connected = "keyA"
	combineWithPasswordFn = func(pw []byte, noPassword bool) ([]byte, string, error) {
		return yubikey.CombinePasswordWithSecret(pw, s.secrets[s.connected]), challenge(s.connected, noPassword), nil
	}
	registerSpareFn = func(pw []byte, primary yubikey.ChallengeData) ([]byte, string, error) {
		if s.connected == "keyA" {
			return nil, "", yubikey.ErrAlreadyRegistered
		}
		return yubikey.CombinePasswordWithSecret(pw, s.secrets[s.connected]), challenge(s.connected, primary.NoPassword), nil
	}
	code, _ := recovery.Generate()
	s.code = code
	generateRecoveryCodeFn = func() (recovery.Code, error) { return code, nil }
}

func enroll(t *testing.T, s *enrollStub, cfg *config.Config) (*container.KeySet, []byte, string, error) {
	t.Helper()
	ks, master, err := enrollKeySet(s.console, cfg, logging.NewConsoleLogger("info", &s.out))
	return ks, master, s.out.String(), err
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

func TestEnrollKeySetWithSpareYubiKeyAndRecoveryCode(t *testing.T) {
	for _, mode := range []config.AuthMode{config.AuthModePasswordYubiKey, config.AuthModeYubiKey} {
		s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}}
		stubEnrollment(t, s)
		// First Enter with YubiKey 1 still connected (refused), then swap to
		// the spare.
		s.lines = []string{"", "<swap>"}
		cfg := &config.Config{AuthenticationMode: mode, YubiKeySpare: true, RecoveryCode: true, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}

		ks, master, out, err := enroll(t, s, cfg)
		if err != nil {
			t.Fatalf("mode %d: enrollKeySet: %v\n%s", mode, err, out)
		}
		if !strings.Contains(out, "This is YubiKey 1") || !strings.Contains(out, s.code.String()) {
			t.Fatalf("mode %d: unexpected output %q", mode, out)
		}
		if len(ks.Slots) != 3 || ks.YubiKeyCount() != 2 || !ks.HasSlotType(container.SlotRecovery) {
			t.Fatalf("mode %d: unexpected slots %+v", mode, ks.Slots)
		}
		if err := ks.Validate(); err != nil {
			t.Fatalf("mode %d: key set invalid: %v", mode, err)
		}
		for i, key := range []string{"keyA", "keyB"} {
			secret := s.secrets[key]
			if mode == config.AuthModePasswordYubiKey {
				secret = yubikey.CombinePasswordWithSecret([]byte("a long password"), secret)
			}
			if got, err := ks.Unlock(i, secret); err != nil || !bytes.Equal(got, master) {
				t.Fatalf("mode %d: %s does not unlock slot %d: %v", mode, key, i, err)
			}
		}
		if got, err := ks.Unlock(2, s.code.Secret()); err != nil || !bytes.Equal(got, master) {
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

	gotKS, gotMaster, err := obtainKeys(&interacttest.Script{}, &config.Config{}, plan.Keys{Existing: ks}, logging.NewConsoleLogger("info", nil))
	if err != nil || gotKS.ID != ks.ID || !bytes.Equal(gotMaster, master) {
		t.Fatalf("existing keys not reused: %v", err)
	}
}
