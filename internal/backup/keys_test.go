package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
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
	infos := []catalog.SetInfo{infoWithKeySet(ks)}

	if p := planKeys(cfg, nil); p.Existing != nil || !strings.Contains(p.NewKeysReason, "No existing keys") {
		t.Fatalf("empty directory: %+v", p)
	}
	if p := planKeys(cfg, infos); p.Existing == nil || p.Existing.ID != ks.ID {
		t.Fatalf("matching keys not reused: %+v", p)
	}
	for _, tc := range []struct {
		mutate func(*util.Config)
		want   string
	}{
		{func(c *util.Config) { c.AuthenticationMode = util.AuthModePasswordYubiKey }, "Authentication_mode changed"},
		{func(c *util.Config) { c.RecoveryCode = true }, "Recovery_code enabled"},
	} {
		c := *cfg
		tc.mutate(&c)
		if p := planKeys(&c, infos); p.Existing != nil || !strings.Contains(p.NewKeysReason, tc.want) {
			t.Fatalf("expected new keys with reason %q, got %+v", tc.want, p)
		}
	}
}

type enrollStub struct {
	passwords [][2]string // pairs returned by readPasswordConfirmedFn
	lines     []string
	connected string // "keyA" or "keyB": which YubiKey is plugged in
	secrets   map[string][]byte
	code      security.RecoveryCode
}

// stubEnrollment replaces the interactive credential sources. Not safe for
// parallel use.
func stubEnrollment(t *testing.T, s *enrollStub) {
	t.Helper()
	prevRead, prevLine, prevCheck, prevCombine, prevSpare, prevGen := readPasswordConfirmedFn, readLineFn, checkYubiKeyConnectedFn, combineWithPasswordFn, registerSpareFn, generateRecoveryCodeFn
	t.Cleanup(func() {
		readPasswordConfirmedFn, readLineFn, checkYubiKeyConnectedFn, combineWithPasswordFn, registerSpareFn, generateRecoveryCodeFn = prevRead, prevLine, prevCheck, prevCombine, prevSpare, prevGen
	})
	s.secrets = map[string][]byte{"keyA": bytes.Repeat([]byte{1}, 32), "keyB": bytes.Repeat([]byte{2}, 32)}
	readPasswordConfirmedFn = func(string, string) ([]byte, error) {
		if len(s.passwords) == 0 {
			return nil, errors.New("no more passwords")
		}
		p := s.passwords[0]
		s.passwords = s.passwords[1:]
		if p[0] != p[1] {
			return nil, security.ErrPasswordMismatch
		}
		return []byte(p[0]), nil
	}
	readLineFn = func(string) (string, error) {
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
	}
	checkYubiKeyConnectedFn = func() error { return nil }
	challenge := func(key string, noPassword bool) string {
		raw, _ := json.Marshal(security.ChallengeData{Version: 1, NoPassword: noPassword, CredID: map[string]string{"keyA": "Y3JlZC1h", "keyB": "Y3JlZC1i"}[key], Salt: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="})
		return string(raw)
	}
	s.connected = "keyA"
	combineWithPasswordFn = func(pw []byte, noPassword bool) ([]byte, string, error) {
		return security.CombinePasswordWithSecret(pw, s.secrets[s.connected]), challenge(s.connected, noPassword), nil
	}
	registerSpareFn = func(pw []byte, primary security.ChallengeData) ([]byte, string, error) {
		if s.connected == "keyA" {
			return nil, "", security.ErrYubiKeyAlreadyRegistered
		}
		return security.CombinePasswordWithSecret(pw, s.secrets[s.connected]), challenge(s.connected, primary.NoPassword), nil
	}
	code, _ := security.GenerateRecoveryCode()
	s.code = code
	generateRecoveryCodeFn = func() (security.RecoveryCode, error) { return code, nil }
}

func enroll(t *testing.T, cfg *util.Config) (*container.KeySet, []byte, error, string) {
	t.Helper()
	var ks *container.KeySet
	var master []byte
	var err error
	out := testutil.CaptureStdout(t, func() { ks, master, err = enrollKeySet(cfg, util.NewConsoleLogger("info")) })
	return ks, master, err, out
}

func TestEnrollKeySetPasswordModeEnforcesMinimumAndRetries(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"short", "short"}, {"long enough pw", "typo"}, {"long enough pw", "long enough pw"}}}
	stubEnrollment(t, s)
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}

	ks, master, err, out := enroll(t, cfg)
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
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword, PasswordMinLength: 8, Argon2: testutil.FastArgon2Config}
	if _, _, err, _ := enroll(t, cfg); err == nil || !strings.Contains(err.Error(), "No valid new password") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

func TestEnrollKeySetCountsCharactersNotBytes(t *testing.T) {
	// 8 characters, 16 bytes in UTF-8.
	s := &enrollStub{passwords: [][2]string{{"äöüßäöüß", "äöüßäöüß"}}}
	stubEnrollment(t, s)
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword, PasswordMinLength: 8, Argon2: testutil.FastArgon2Config}
	if _, _, err, _ := enroll(t, cfg); err != nil {
		t.Fatalf("8-character password must be accepted: %v", err)
	}
}

func TestEnrollKeySetWithSpareYubiKeyAndRecoveryCode(t *testing.T) {
	for _, mode := range []util.AuthMode{util.AuthModePasswordYubiKey, util.AuthModeYubiKey} {
		s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}}
		stubEnrollment(t, s)
		// First Enter with YubiKey 1 still connected (refused), then swap to
		// the spare; then a wrong and the right recovery code.
		s.lines = []string{"", "<swap>", "WRONG-CODE", s.code.String()}
		cfg := &util.Config{AuthenticationMode: mode, YubiKeySpare: true, RecoveryCode: true, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}

		ks, master, err, out := enroll(t, cfg)
		if err != nil {
			t.Fatalf("mode %d: enrollKeySet: %v\n%s", mode, err, out)
		}
		if !strings.Contains(out, "This is YubiKey 1") || !strings.Contains(out, s.code.String()) || !strings.Contains(out, "Recovery code confirmed") {
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
			if mode == util.AuthModePasswordYubiKey {
				secret = security.CombinePasswordWithSecret([]byte("a long password"), secret)
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

func TestEnrollKeySetFailsWhenRecoveryCodeNotConfirmed(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}, lines: []string{"x", "y", "z"}}
	stubEnrollment(t, s)
	cfg := &util.Config{AuthenticationMode: util.AuthModePassword, RecoveryCode: true, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}
	if _, _, err, _ := enroll(t, cfg); err == nil || !strings.Contains(err.Error(), "Recovery code not confirmed") {
		t.Fatalf("expected unconfirmed recovery code error, got %v", err)
	}
}

func TestEnrollKeySetRequiresConnectedYubiKey(t *testing.T) {
	s := &enrollStub{passwords: [][2]string{{"a long password", "a long password"}}}
	stubEnrollment(t, s)
	checkYubiKeyConnectedFn = func() error { return errors.New("not connected") }
	cfg := &util.Config{AuthenticationMode: util.AuthModePasswordYubiKey, PasswordMinLength: 12, Argon2: testutil.FastArgon2Config}
	if _, _, err, _ := enroll(t, cfg); !errors.Is(err, security.ErrYubiKeyRequired) {
		t.Fatalf("expected ErrYubiKeyRequired, got %v", err)
	}
}

func TestObtainKeysReusesExistingKeySet(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	prev := unlockKeySetFn
	t.Cleanup(func() { unlockKeySetFn = prev })
	unlockKeySetFn = func(got *container.KeySet, opts operation.UnlockOptions, _ *util.Logger) ([]byte, error) {
		if got.ID != ks.ID || opts.AllowRecovery {
			t.Fatalf("unexpected unlock request (recovery must not be offered for backups)")
		}
		return append([]byte(nil), master...), nil
	}

	gotKS, gotMaster, err := obtainKeys(&util.Config{}, keyPlan{Existing: ks}, util.NewConsoleLogger("info"))
	if err != nil || gotKS.ID != ks.ID || !bytes.Equal(gotMaster, master) {
		t.Fatalf("existing keys not reused: %v", err)
	}
}

func TestPromptBackupStartOffersNewKeys(t *testing.T) {
	ks, _ := testutil.NewPasswordKeySet(t, []byte("pw"))
	prev := readLineFn
	t.Cleanup(func() { readLineFn = prev })
	answers := []string{"maybe", "k"}
	readLineFn = func(string) (string, error) { a := answers[0]; answers = answers[1:]; return a, nil }

	var ok bool
	var plan keyPlan
	var err error
	out := testutil.CaptureStdout(t, func() { ok, plan, err = promptBackupStart(keyPlan{Existing: ks}) })
	if err != nil || !ok || plan.Existing != nil || plan.NewKeysReason != "New keys requested" {
		t.Fatalf("expected new keys to be chosen, got ok=%v plan=%+v err=%v", ok, plan, err)
	}
	if !strings.Contains(out, "Please enter y (yes), k (new keys), or n (no).") {
		t.Fatalf("expected retry hint, got %q", out)
	}
}
