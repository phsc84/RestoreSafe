package keycheck

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
)

const (
	credID = "Y3JlZC1zZWNyZXQtaWQ="
	salt   = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
)

var testTime = time.Date(2026, 10, 16, 8, 52, 0, 0, time.Local)

func now() time.Time { return testTime }

// keySet is a key set of mode with the slots the mode has.
func keySet(mode config.AuthMode) *container.KeySet {
	ks := &container.KeySet{AuthMode: mode}
	switch mode {
	case config.AuthModePassword:
		ks.Slots = []container.Slot{{Type: container.SlotPassword}}
	default:
		cd := &yubikey.ChallengeData{Version: 1, NoPassword: mode == config.AuthModeYubiKey, CredID: credID, Salt: salt}
		ks.Slots = []container.Slot{{Type: container.RegularSlotType(mode), Challenge: cd}, {Type: container.SlotRecovery}}
	}
	return ks
}

// fake replaces the YubiKey calls for one test and records them. The secret
// it hands out is kept, so the test can check that it was zeroed. Not safe
// for parallel use.
type fake struct {
	secret  []byte
	asked   []yubikey.ChallengeData
	probes  int
	failure error
	// debug is the debug output the test set; nil after the test.
	debug io.Writer
}

func stub(t *testing.T, failure error) *fake {
	t.Helper()
	f := &fake{failure: failure}
	prevDerive, prevProbe, prevDebug := deriveSecretFn, probeFn, setDebugFn
	t.Cleanup(func() { deriveSecretFn, probeFn, setDebugFn = prevDerive, prevProbe, prevDebug })
	setDebugFn = func(w io.Writer) { f.debug = w }
	deriveSecretFn = func(cd yubikey.ChallengeData) ([]byte, error) {
		f.asked = append(f.asked, cd)
		fmt.Fprintln(f.debug, "[fido2-debug] hmac-secret output length: 32 bytes")
		if f.failure != nil {
			return nil, f.failure
		}
		f.secret = bytes.Repeat([]byte{0xAB}, 32)
		return f.secret, nil
	}
	probeFn = func() error {
		f.probes++
		fmt.Fprintln(f.debug, "[fido2-debug] credential ID length: 64 bytes")
		return f.failure
	}
	return f
}

func TestMatchAsksForTheSecretOfTheCurrentKeys(t *testing.T) {
	for _, mode := range []config.AuthMode{config.AuthModePasswordYubiKey, config.AuthModeYubiKey} {
		f := stub(t, nil)
		r := Run(keySet(mode), now)
		if r.Kind != Match || !r.Passed() || r.Cancelled || r.HRESULT != 0 || !r.Time.Equal(testTime) {
			t.Fatalf("mode %d: %+v", mode, r)
		}
		if len(f.asked) != 1 || f.asked[0].CredID != credID || f.probes != 0 {
			t.Fatalf("mode %d: asked %+v, probes %d", mode, f.asked, f.probes)
		}
		if !bytes.Equal(f.secret, make([]byte, 32)) {
			t.Fatalf("mode %d: the secret wasn't zeroed: %x", mode, f.secret)
		}
		if !strings.Contains(r.Trace, "hmac-secret output length") {
			t.Fatalf("mode %d: trace %q", mode, r.Trace)
		}
		if f.debug != nil {
			t.Fatal("the debug output must be reset after the test")
		}
	}
}

func TestCapabilityWithoutYubiKeyKeys(t *testing.T) {
	for name, ks := range map[string]*container.KeySet{
		"no keys":       nil,
		"password keys": keySet(config.AuthModePassword),
		// A YubiKey mode whose slot lacks its challenge can't be matched.
		"no challenge": {AuthMode: config.AuthModeYubiKey, Slots: []container.Slot{{Type: container.SlotYubiKey}}},
	} {
		f := stub(t, nil)
		r := Run(ks, now)
		if r.Kind != Capability || !r.Passed() || f.probes != 1 || len(f.asked) != 0 {
			t.Fatalf("%s: %+v, probes %d", name, r, f.probes)
		}
		if !strings.Contains(r.Trace, "credential ID length") || f.debug != nil {
			t.Fatalf("%s: trace %q", name, r.Trace)
		}
	}
}

func TestFailuresKeepTheirHRESULT(t *testing.T) {
	wrap := func(code uint32) error {
		return fmt.Errorf("FIDO2 hmac-secret failed: %w", problem.Errorf("%w.", &yubikey.HRESULTError{Op: "WebAuthNAuthenticatorGetAssertion", Code: code}))
	}
	for _, tc := range []struct {
		name      string
		ks        *container.KeySet
		err       error
		kind      Kind
		cancelled bool
		hresult   uint32
	}{
		{"cancelled match", keySet(config.AuthModeYubiKey), wrap(0x800704C7), Match, true, 0x800704C7},
		{"failed match", keySet(config.AuthModePasswordYubiKey), wrap(0x80090029), Match, false, 0x80090029},
		{"cancelled capability", nil, wrap(0x80090036), Capability, true, 0x80090036},
		{"error without a code", nil, errors.New("no FIDO2 authenticator detected"), Capability, false, 0},
	} {
		f := stub(t, tc.err)
		r := Run(tc.ks, now)
		if r.Passed() || r.Kind != tc.kind || r.Cancelled != tc.cancelled || r.HRESULT != tc.hresult || !errors.Is(r.Err, tc.err) {
			t.Errorf("%s: %+v", tc.name, r)
		}
		if f.debug != nil {
			t.Fatalf("%s: the debug output must be reset after a failed test", tc.name)
		}
	}
}

// The result says nothing about the keys beyond what the page shows.
func TestResultHoldsNoSecretSaltOrCredentialID(t *testing.T) {
	f := stub(t, nil)
	r := Run(keySet(config.AuthModeYubiKey), now)
	text := fmt.Sprintf("%+v", r)
	for _, secret := range []string{credID, salt, fmt.Sprintf("%x", bytes.Repeat([]byte{0xAB}, 32)), string(bytes.Repeat([]byte{0xAB}, 32))} {
		if strings.Contains(text, secret) {
			t.Fatalf("result contains %q: %s", secret, text)
		}
	}
	if len(f.asked) != 1 {
		t.Fatalf("asked %d times", len(f.asked))
	}
}
